---
name: quack
description: DuckDuckGo and Bing search and web page content extraction via the quack CLI. Use for searching the web, fetching article content, or extracting metadata from URLs.
---

# quack

## Check

```bash
quack --help
```

## Install if not exist

```bash
go install github.com/zyoung11/quack@latest
```

## Search

```bash
quack KEYWORDS... [options]
```

Searches both DuckDuckGo and Bing in parallel. Outputs JSON with results including title, url, abstract, and optional published date. A `source` field indicates which engine was used.

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
| `-w SITE` | Restrict to site (domain or domain/path) |
| `-r REG` | Region (default us-en), e.g. cn-zh |
| `-e E` | Force engine: `ddg` or `bing` (default: both) |
| `-v` | Print version |
| `-h` | Print help |

## Examples

```bash
quack "golang generics" -n 5
quack -e bing "golang" -n 5
quack "News" -n 10 -t w -r cn-zh
quack -w "go.dev" "golang"
quack "https://go.dev/blog/"
```
