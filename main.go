package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const version = "1.1.0"

// dateRe matches ISO 8601 datetimes embedded in DDG snippet text.
// dateRe matches ISO 8601 date/datetime patterns embedded in DDG snippet text.
var dateRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}(?:T\d{2}:\d{2}:\d{2}(?:\.\d+)?)?`)

type Result struct {
	Index     int    `json:"index"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Abstract  string `json:"abstract"`
	Published string `json:"published,omitempty"`
}

type FetchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Text        string `json:"text"`
	Description string `json:"description,omitempty"`
	Published   string `json:"published,omitempty"`
}

var (
	ddgClient   *http.Client
	ddgInitOnce sync.Once
)

func initDDGSession() {
	req, err := http.NewRequest("GET", "https://duckduckgo.com/", nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", randomUA())
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("DNT", "1")
	ddgClient.Do(req)
}

func newDDGClient() *http.Client {
	tr := &http.Transport{
		TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}
	return &http.Client{Transport: tr, Jar: jar}
}

var userAgents = []string{
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func randomUA() string {
	return userAgents[rng.Intn(len(userAgents))]
}

func randomDelay() {
	d := time.Duration(500+rng.Intn(2001)) * time.Millisecond
	time.Sleep(d)
}

func init() {
	ddgClient = newDDGClient()
}

func main() {
	args := parseArgs()

	switch args.mode {
	case modeSearch:
		doSearch(args)
	case modeFetch:
		doFetch(args.query, args)
	}
}

const (
	modeSearch = iota
	modeFetch
)

func doSearch(args cliArgs) {
	var allResults []Result
	var source string

	if args.engine != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		engine := strings.ToLower(args.engine)
		var results []Result
		var err error
		switch engine {
		case "ddg", "duckduckgo":
			results, err = searchDDG(ctx, args)
			source = "ddg"
		case "bing":
			results, err = searchBing(ctx, args)
			source = "bing"
		default:
			die("unknown engine %q (use ddg or bing)", args.engine)
		}
		if err != nil {
			die("search failed on %s: %v", source, err)
		}
		allResults = results
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		type engineResult struct {
			name    string
			results []Result
		}

		ch := make(chan engineResult, 2)

		go func() {
			results, err := searchDDG(ctx, args)
			if err == nil {
				select {
				case ch <- engineResult{"ddg", results}:
				case <-ctx.Done():
				}
			}
		}()

		go func() {
			results, err := searchBing(ctx, args)
			if err == nil {
				select {
				case ch <- engineResult{"bing", results}:
				case <-ctx.Done():
				}
			}
		}()

		var ddgResults, bingResults []Result
	loop:
		for range 2 {
			select {
			case r := <-ch:
				if r.name == "ddg" {
					ddgResults = r.results
				} else {
					bingResults = r.results
				}
			case <-ctx.Done():
				break loop
			}
		}

		switch {
		case ddgResults != nil:
			allResults = ddgResults
			source = "ddg"
		case bingResults != nil:
			allResults = bingResults
			source = "bing"
		default:
			die("all search engines failed")
		}
	}

	if args.num > 0 && args.num < len(allResults) {
		allResults = allResults[:args.num]
	}

	output := map[string]any{
		"mode":      "search",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"query":     args.query,
		"num":       args.num,
		"returned":  len(allResults),
		"region":    args.region,
		"source":    source,
		"results":   allResults,
	}
	if args.timeSpan != "" {
		output["time"] = args.timeSpan
	}
	if args.site != "" {
		output["site"] = args.site
	}

	printJSON(output)
}

func doFetch(targetURL string, _ cliArgs) {
	result := fetchPage(targetURL)
	output := map[string]any{
		"mode":      "fetch",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"title":     result.Title,
		"url":       result.URL,
		"text":      result.Text,
	}
	if result.Description != "" {
		output["description"] = result.Description
	}
	if result.Published != "" {
		output["published"] = result.Published
	}
	printJSON(output)
}

func searchDDG(ctx context.Context, args cliArgs) ([]Result, error) {
	ddgInitOnce.Do(initDDGSession)
	randomDelay()

	query := args.query
	if args.site != "" {
		query += " site:" + args.site
	}

	formData := url.Values{}
	formData.Set("q", query)
	formData.Set("b", "")
	formData.Set("df", args.timeSpan)
	formData.Set("kf", "-1")
	formData.Set("kh", "1")
	formData.Set("kl", args.region)
	formData.Set("kp", "-1")
	formData.Set("k1", "-1")

	body := strings.NewReader(formData.Encode())
	req, err := http.NewRequestWithContext(ctx, "POST", "https://html.duckduckgo.com/html", body)
	if err != nil {
		return nil, err
	}
	setHeaders(req)

	resp, err := ddgClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	htmlBytes, err := io.ReadAll(decompress(resp))
	if err != nil {
		return nil, err
	}

	allResults := parseSearchResults(bytes.NewReader(htmlBytes))
	if isDDGChallenge(htmlBytes) || len(allResults) == 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		randomDelay()
		allResults = searchLite(args.query, args.region, args.site)
		if len(allResults) == 0 {
			return nil, fmt.Errorf("DDG blocked")
		}
	}

	return allResults, nil
}

func searchBing(ctx context.Context, args cliArgs) ([]Result, error) {
	bingURL := "https://www.bing.com/search?q=" + url.QueryEscape(args.query)
	if args.region != "" && args.region != "us-en" {
		mkt := args.region
		if strings.HasPrefix(mkt, "cn-") {
			mkt = "zh-CN"
		}
		bingURL += "&mkt=" + url.QueryEscape(mkt)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", bingURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", randomUA())
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bing HTTP %d", resp.StatusCode)
	}

	htmlBytes, err := io.ReadAll(decompress(resp))
	if err != nil {
		return nil, err
	}

	results := parseBingResults(bytes.NewReader(htmlBytes), 0)

	if args.site != "" {
		site := args.site
		if idx := strings.IndexByte(site, '/'); idx >= 0 {
			site = site[:idx]
		}
		filtered := results[:0]
		for _, r := range results {
			u, err := url.Parse(r.URL)
			if err == nil && strings.HasSuffix(u.Hostname(), site) {
				filtered = append(filtered, r)
			}
		}
		results = filtered
		for i := range results {
			results[i].Index = i + 1
		}
	}

	return results, nil
}

func decodeBingURL(trackingURL string) string {
	u, err := url.Parse(trackingURL)
	if err != nil {
		return trackingURL
	}
	encoded := u.Query().Get("u")
	if encoded == "" {
		return trackingURL
	}

	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		return trackingURL
	}

	for _, prefix := range []int{0, 2} {
		s := decoded[prefix:]
		switch len(s) % 4 {
		case 1:
			s += "==="
		case 2:
			s += "=="
		case 3:
			s += "="
		}
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			continue
		}
		result := string(raw)
		if strings.HasPrefix(result, "http://") || strings.HasPrefix(result, "https://") {
			return result
		}
	}

	return trackingURL
}

func parseBingResults(r io.Reader, maxResults int) []Result {
	results := make([]Result, 0)
	z := html.NewTokenizer(r)

	var curTitle, curURL, curSnippet string
	inAlgo := false
	inH2 := false
	inTitleLink := false
	inSnippet := false

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return results
			}
			return results

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttrs := z.TagName()
			tag := string(name)

			if tag == "li" && hasAttrs && hasClass(getAttr(z, "class"), "b_algo") {
				inAlgo = true
				curTitle = ""
				curURL = ""
				curSnippet = ""
				continue
			}

			if inAlgo {
				switch {
				case tag == "h2":
					inH2 = true
					continue
				case tag == "a" && inH2 && hasAttrs:
					href := getAttr(z, "href")
					if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
						curURL = href
						if strings.Contains(href, "bing.com/ck/") || strings.Contains(href, "bing.com/url/") {
							if dec := decodeBingURL(href); dec != href {
								curURL = dec
							}
						}
						inTitleLink = true
						continue
					}
				case tag == "p" && hasAttrs && hasClass(getAttr(z, "class"), "b_lineclamp2"):
					inSnippet = true
					continue
				}
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)

			if !inAlgo {
				continue
			}

			switch tag {
			case "li":
				if curURL != "" && curTitle != "" {
					results = append(results, Result{
						Index:    len(results) + 1,
						Title:    strings.TrimSpace(curTitle),
						URL:      curURL,
						Abstract: strings.TrimSpace(curSnippet),
					})
					if maxResults > 0 && len(results) >= maxResults {
						return results
					}
				}
				inAlgo = false
				inH2 = false
				inTitleLink = false
				inSnippet = false
			case "h2":
				inH2 = false
				inTitleLink = false
			case "a":
				inTitleLink = false
			case "p":
				inSnippet = false
			}

		case html.TextToken:
			text := string(z.Text())
			if inTitleLink {
				curTitle += text
			}
			if inSnippet {
				curSnippet += text
			}
		}
	}
}

func isDDGChallenge(body []byte) bool {
	s := string(body)
	return strings.Contains(s, "anomaly-modal") || strings.Contains(s, "challenge")
}

func setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", randomUA())
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("DNT", "1")
	req.Header.Set("Referer", "https://duckduckgo.com/")
}

func decompress(resp *http.Response) io.ReadCloser {
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err := gzip.NewReader(resp.Body)
		if err != nil {
			die("decompress: %v", err)
		}
		return reader
	default:
		return resp.Body
	}
}

func fetchPage(targetURL string) FetchResult {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		die("create fetch request: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		die("fetch page: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		die("fetch page: HTTP %d", resp.StatusCode)
	}

	reader := decompress(resp)
	doc, err := html.Parse(reader)
	if err != nil {
		die("parse HTML: %v", err)
	}

	title := extractTitle(doc)
	text := extractBodyText(doc, 100000)
	desc := extractMeta(doc, "description")
	if desc == "" {
		desc = extractMeta(doc, "og:description")
	}
	pub := extractMeta(doc, "article:published_time")

	return FetchResult{
		Title:       title,
		URL:         targetURL,
		Text:        text,
		Description: desc,
		Published:   pub,
	}
}

func extractTitle(doc *html.Node) string {
	var walk func(*html.Node) string
	walk = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "title" {
			if n.FirstChild != nil {
				return strings.TrimSpace(n.FirstChild.Data)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if s := walk(c); s != "" {
				return s
			}
		}
		return ""
	}
	return walk(doc)
}

var blockLevelTags = map[string]bool{
	"p": true, "div": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "li": true, "tr": true,
	"br": true, "hr": true, "pre": true, "blockquote": true,
	"ol": true, "ul": true, "dl": true, "table": true, "section": true,
	"article": true, "main": true, "details": true, "summary": true,
}

var skipTags = map[string]bool{
	"script": true, "style": true, "nav": true, "footer": true,
	"header": true, "aside": true, "noscript": true, "svg": true,
	"form": true, "select": true, "textarea": true, "button": true,
	"input": true, "iframe": true, "canvas": true, "audio": true,
	"video": true, "picture": true, "figure": true, "figcaption": true,
}

func extractBodyText(doc *html.Node, maxChars int) string {
	var buf strings.Builder
	buf.Grow(maxChars)

	var walk func(*html.Node, bool)
	walk = func(n *html.Node, newBlock bool) {
		if buf.Len() >= maxChars {
			return
		}

		if n.Type == html.ElementNode {
			if skipTags[n.Data] {
				return
			}
			if blockLevelTags[n.Data] && buf.Len() > 0 && lastByte(buf) != '\n' {
				buf.WriteByte('\n')
			}
		}

		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if len(text) > 2 || newBlock {
					if buf.Len() > 0 {
						last := lastByte(buf)
						if last != '\n' && last != ' ' {
							buf.WriteByte(' ')
						}
					}
					buf.WriteString(text)
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			isBlock := blockLevelTags[n.Data] || n.Data == "body"
			walk(c, isBlock)
		}

		if n.Type == html.ElementNode && blockLevelTags[n.Data] && buf.Len() > 0 && lastByte(buf) != '\n' {
			buf.WriteByte('\n')
		}
	}

	var findBody func(*html.Node) *html.Node
	findBody = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data == "body" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if body := findBody(c); body != nil {
				return body
			}
		}
		return nil
	}

	body := findBody(doc)
	if body != nil {
		walk(body, true)
	} else {
		walk(doc, true)
	}

	result := buf.String()
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}

func extractMeta(doc *html.Node, key string) string {
	var walk func(*html.Node) string
	walk = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "meta" {
			var name, content string
			for _, attr := range n.Attr {
				if (attr.Key == "name" || attr.Key == "property") && attr.Val == key {
					name = key
				}
				if attr.Key == "content" {
					content = attr.Val
				}
			}
			if name != "" && content != "" {
				return strings.TrimSpace(content)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if s := walk(c); s != "" {
				return s
			}
		}
		return ""
	}
	return walk(doc)
}

func parseSearchResults(r io.Reader) []Result {
	results := make([]Result, 0)
	z := html.NewTokenizer(r)

	type rd struct {
		title    string
		url      string
		abstract string
	}

	var cur *rd
	inResult := false
	inTitle := false
	inSnippet := false
	depth := 0

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return results
			}
			return results

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttrs := z.TagName()
			tag := string(name)
			cls := ""
			if hasAttrs {
				cls = getAttr(z, "class")
			}

			if tag == "div" && hasClass(cls, "links_main") && !inResult {
				inResult = true
				cur = &rd{}
				depth = 1
				continue
			}

			if inResult {
				if tag == "div" {
					depth++
					continue
				}
				if tag == "h2" && hasClass(cls, "result__title") {
					inTitle = true
					continue
				}
				if tag == "a" && hasClass(cls, "result__snippet") && hasAttrExist(z, "href") {
					inSnippet = true
					continue
				}
				if tag == "a" && inTitle && hasClass(cls, "result__a") {
					href := getAttr(z, "href")
					if href != "" {
						cur.url = href
					}
					continue
				}
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)

			if !inResult {
				continue
			}

			switch {
			case tag == "div":
				depth--
				if depth == 0 {
					if cur != nil && cur.url != "" {
						pub := ""
						if m := dateRe.FindString(cur.abstract); m != "" {
							pub = m
							cur.abstract = strings.Replace(cur.abstract, m, "", 1)
						}
						results = append(results, Result{
							Index:     len(results) + 1,
							Title:     cur.title,
							URL:       cur.url,
							Abstract:  strings.TrimSpace(cur.abstract),
							Published: pub,
						})
					}
					inResult = false
					cur = nil
				}
			case tag == "h2":
				inTitle = false
			case tag == "a" && inSnippet:
				inSnippet = false
			}

		case html.TextToken:
			if inResult {
				text := strings.TrimSpace(string(z.Text()))
				if text == "" {
					continue
				}
				if inTitle {
					cur.title += text
				}
				if inSnippet {
					cur.abstract += text
				}
			}
		}
	}
}

func searchLite(query, region, site string) []Result {
	ddgInitOnce.Do(initDDGSession)
	q := query
	if site != "" {
		q += " site:" + site
	}
	formData := url.Values{}
	formData.Set("q", q)
	formData.Set("kl", region)

	body := strings.NewReader(formData.Encode())
	req, err := http.NewRequest("POST", "https://lite.duckduckgo.com/lite/", body)
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", randomUA())
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("DNT", "1")
	req.Header.Set("Referer", "https://duckduckgo.com/")

	resp, err := ddgClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	htmlBytes, err := io.ReadAll(decompress(resp))
	if err != nil {
		return nil
	}

	if isDDGChallenge(htmlBytes) {
		return nil
	}

	return parseLiteResults(bytes.NewReader(htmlBytes))
}

func parseLiteResults(r io.Reader) []Result {
	results := make([]Result, 0)
	z := html.NewTokenizer(r)

	var title, url, abstract string
	haveLink := false
	inTitle := false
	inSnippet := false

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				if haveLink && url != "" {
					results = append(results, Result{
						Index:    len(results) + 1,
						Title:    strings.TrimSpace(title),
						URL:      url,
						Abstract: strings.TrimSpace(abstract),
					})
				}
				return results
			}
			return results

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttrs := z.TagName()
			tag := string(name)
			cls := ""
			if hasAttrs {
				cls = getAttr(z, "class")
			}

			if tag == "a" && hasClass(cls, "result-link") {
				href := getAttr(z, "href")
				if href != "" {
					if haveLink && url != "" {
						results = append(results, Result{
							Index:    len(results) + 1,
							Title:    strings.TrimSpace(title),
							URL:      url,
							Abstract: strings.TrimSpace(abstract),
						})
					}
					title = ""
					url = href
					abstract = ""
					haveLink = true
					inTitle = true
				}
				continue
			}

			if tag == "td" && hasClass(cls, "result-snippet") {
				inSnippet = true
				continue
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)

			if tag == "a" {
				inTitle = false
			}
			if tag == "td" {
				inSnippet = false
			}

		case html.TextToken:
			text := string(z.Text())
			if inTitle {
				title += text
			}
			if inSnippet {
				abstract += text
			}
		}
	}
}

type cliArgs struct {
	query    string
	mode     int
	num      int
	timeSpan string
	site     string
	region   string
	engine   string
}

func parseArgs() cliArgs {
	var a cliArgs
	a.num = 10
	a.region = "us-en"

	args := os.Args[1:]
	i := 0
	for i < len(args) {
		arg := args[i]
		switch arg {
		case "-n", "--num":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err == nil {
					a.num = n
				}
				i += 2
			} else {
				i++
			}
		case "-t", "--time":
			if i+1 < len(args) {
				a.timeSpan = args[i+1]
				i += 2
			} else {
				i++
			}
		case "-w", "--site":
			if i+1 < len(args) {
				a.site = args[i+1]
				i += 2
			} else {
				i++
			}
		case "-r", "--region":
			if i+1 < len(args) {
				a.region = args[i+1]
				i += 2
			} else {
				i++
			}
		case "-e", "--engine":
			if i+1 < len(args) {
				a.engine = args[i+1]
				i += 2
			} else {
				i++
			}
		case "-v", "--version":
			fmt.Println("quack version", version)
			os.Exit(0)
		case "-h", "--help":
			printHelp()
			os.Exit(0)
		default:
			if strings.HasPrefix(arg, "-") {
				die("unknown option %s", arg)
			}
			if a.query != "" {
				a.query += " "
			}
			a.query += arg
			i++
		}
	}

	if a.query == "" {
		die("no keywords provided")
	}

	first := strings.Fields(a.query)[0]
	if strings.HasPrefix(first, "http://") || strings.HasPrefix(first, "https://") {
		a.mode = modeFetch
	} else {
		a.mode = modeSearch
	}

	return a
}

func printHelp() {
	fmt.Print(`quack — DuckDuckGo + Bing CLI search & page fetcher

Usage:
  quack KEYWORDS... [options]     search (parallel DDG + Bing, DDG preferred)
  quack URL [options]             fetch page content

Options:
  -n, --num N       result count (default 10)
  -t, --time SPAN   time range: d(ay), w(eek), m(onth), y(ear)
  -w, --site SITE   restrict to site
  -r, --region REG  region (default us-en), e.g. cn-zh, wt-wt
  -e, --engine E    force engine: ddg or bing (default: both, DDG preferred)
  -v, --version
  -h, --help

By default searches both DuckDuckGo and Bing in parallel.
Output includes a "source" field indicating which engine was used.

Examples:
  quack golang -n 5
  quack -e bing golang -n 5
  quack "AI news" -n 10 -t w -r cn-zh
  quack https://go.dev/blog/
`)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("  ", "  ")
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func hasClass(classAttr, target string) bool {
	classes := strings.Fields(classAttr)
	return slices.Contains(classes, target)
}

func getAttr(z *html.Tokenizer, key string) string {
	for {
		k, v, more := z.TagAttr()
		if string(k) == key {
			return string(v)
		}
		if !more {
			break
		}
	}
	return ""
}

func hasAttrExist(z *html.Tokenizer, key string) bool {
	for {
		k, _, more := z.TagAttr()
		if string(k) == key {
			return true
		}
		if !more {
			break
		}
	}
	return false
}

func lastByte(buf strings.Builder) byte {
	s := buf.String()
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1]
}
