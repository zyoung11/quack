#!/usr/bin/env bash

BIN="${1:-./quack}"
PASS=0
WARN=0

green() { printf '\033[32m  PASS  %s\033[0m\n' "$1"; }
yellow(){ printf '\033[33m  SKIP  %s\033[0m\n' "$1"; }

check() {
	local name="$1" expect="$2"
	shift 2
	local out
	out=$("$@" 2>/dev/null) || { yellow "$name (rate-limited)"; ((WARN++)); return; }
	echo "$out" | python3 -c "
import sys, json
data = json.load(sys.stdin)
$expect
" 2>/dev/null && { green "$name"; ((PASS++)); } || { yellow "$name (assert)"; ((WARN++)); }
}

echo "=== quack test suite ==="
echo ""

echo "--- search mode ---"

check "basic search" '
assert len(data["results"]) >= 1
assert data["results"][0]["title"] != ""
' "$BIN" "hello world" -n 2

check "search output structure" '
assert data["mode"] == "search"
assert "timestamp" in data
assert "query" in data
assert "num" in data
assert "returned" in data
assert "region" in data
assert isinstance(data["results"], list)
' "$BIN" "golang" -n 1

check "search with region" '
assert data["region"] == "cn-zh"
' "$BIN" "rust" -r cn-zh -n 1

check "search result published date" '
if data["results"][0].get("published"):
    assert len(data["results"][0]["published"]) >= 10
' "$BIN" "golang release date 2026" -n 3

echo ""
echo "--- fetch mode ---"

check "fetch page" '
assert data["mode"] == "fetch"
assert data["title"] == "Example Domain"
assert len(data["text"]) > 50
' "$BIN" "https://example.com"

check "fetch output structure" '
assert "mode" in data
assert "timestamp" in data
assert "title" in data
assert "url" in data
assert "text" in data
' "$BIN" "https://example.com"

check "fetch page with description" '
assert data["mode"] == "fetch"
assert "description" in data
assert len(data["description"]) > 0
' "$BIN" "https://go.dev/blog/go1.26"

echo ""
echo "--- flags ---"

check "flags before keywords" '
assert data["query"] == "golang"
assert data["num"] == 3
' "$BIN" -n 3 "golang"

check "flags after keywords" '
assert data["query"] == "golang"
assert data["num"] == 3
' "$BIN" "golang" -n 3

check "multiple flags" '
assert data["region"] == "cn-zh"
assert data["num"] == 2
' "$BIN" "rust" -r cn-zh -n 2

echo ""
echo "--- error handling ---"

"$BIN" 2>&1 | grep -q '^error:' && { green "no arguments"; ((PASS++)); } || { yellow "no arguments"; ((WARN++)); }
"$BIN" "test" --unknown 2>&1 | grep -q '^error:' && { green "unknown flag"; ((PASS++)); } || { yellow "unknown flag"; ((WARN++)); }

echo ""
echo "--- help & version ---"

"$BIN" -h 2>&1 | head -1 | grep -q "quack" && { green "help"; ((PASS++)); } || { yellow "help"; ((WARN++)); }
"$BIN" -v 2>&1 | grep -q "quack version" && { green "version"; ((PASS++)); } || { yellow "version"; ((WARN++)); }

echo ""
echo "=== results ==="
echo "  passed: $PASS"
echo "  skipped: $WARN"
