# quack

DuckDuckGo CLI search + page content fetcher with JSON outputs.

![Example](./example.jpg)

## Install

```bash
go install github.com/zyoung11/quack@v1.0.0
```

Or build from source:

```bash
git clone https://github.com/zyoung11/quack.git
cd quack
go build -ldflags="-s -w" .
```

## Usage

**Search DuckDuckGo:**

```bash
quack KEYWORDS... [options]
```

**Fetch page content:**

```bash
quack URL
```

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
quack "AI news" -n 10 -t w -r cn-zh
quack https://go.dev/blog/
```

## Output

Both modes return JSON with a `timestamp` field (current time in UTC). Search results include `published` when a date is available in the snippet. Fetched pages include `description` and `published` when found in HTML meta tags.
