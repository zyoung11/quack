---
name: quack
description: DuckDuckGo search and web page content extraction via the quack CLI. Use for searching the web, fetching article content, or extracting metadata from URLs.
---

# quack

## Check

```bash
quack --help
```

## Install if not exist

```bash
go install github.com/zyoung11/quack@v1.0.0
```

## Search

```bash
quack KEYWORDS... [options]
```

Outputs JSON with results including title, url, abstract, and optional published date.

## Fetch Page Content

```bash
quack URL [options]
```

Outputs JSON with title, text content, and optional description/published time.

## Options

| Flag | Description |
|------|-------------|
| `-n N` | Result count (default 10) |
| `-t SPAN` | Time range: d, w, m, y |
| `-w SITE` | Restrict to site |
| `-r REG` | Region (default us-en), e.g. cn-zh |

## Examples

```bash
quack "golang generics" -n 5
quack "News" -n 10 -t w -r cn-zh
quack "https://go.dev/blog/"
```
