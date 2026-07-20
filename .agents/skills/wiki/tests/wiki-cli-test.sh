#!/usr/bin/env bash
set -euo pipefail

TEST_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
SOURCE_CLI=$TEST_DIR/../scripts/wiki-cli
FIXTURE=$(mktemp -d "${TMPDIR:-/tmp}/wiki-cli-test.XXXXXX")
trap 'rm -rf -- "$FIXTURE"' EXIT INT TERM

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_eq() {
  local expected=$1 actual=$2 label=$3
  [[ $actual == "$expected" ]] || fail "$label: expected '$expected', got '$actual'"
}

assert_contains() {
  local haystack=$1 needle=$2 label=$3
  [[ $haystack == *"$needle"* ]] || fail "$label: missing '$needle'"
}

assert_not_contains() {
  local haystack=$1 needle=$2 label=$3
  [[ $haystack != *"$needle"* ]] || fail "$label: unexpectedly contained '$needle'"
}

mkdir -p \
  "$FIXTURE/bin" \
  "$FIXTURE/scripts" \
  "$FIXTURE/wiki/concepts" \
  "$FIXTURE/wiki/problems" \
  "$FIXTURE/wiki/sources/sample" \
  "$FIXTURE/outside"
cp "$SOURCE_CLI" "$FIXTURE/scripts/wiki-cli"
git -C "$FIXTURE" init -q

printf '%s\n' \
  '---' \
  'title: One' \
  'type: concept' \
  'tags: [topic/alpha, method/test]' \
  '---' \
  'needle [[Two]] [[Duplicate]] [[Missing]] [PDF](../sources/sample/source.pdf)' \
  >"$FIXTURE/wiki/concepts/One.md"
printf '%s\n' \
  '---' \
  'title: Two' \
  'tags:' \
  '  - topic/beta' \
  '---' \
  '#inline/tag [One](One.md)' \
  >"$FIXTURE/wiki/concepts/Two.md"
printf '%s\n' '---' 'title: Duplicate A' '---' >"$FIXTURE/wiki/concepts/Duplicate.md"
printf '%s\n' '---' 'title: Duplicate B' '---' >"$FIXTURE/wiki/problems/Duplicate.md"
printf '%s\n' 'extracted needle' >"$FIXTURE/wiki/sources/sample/content.md"
printf '%s\n' 'binary needle' >"$FIXTURE/wiki/sources/sample/source.pdf"
printf '%s\n' 'yaml needle' >"$FIXTURE/wiki/data.yaml"
printf '%s\n' 'yml needle' >"$FIXTURE/wiki/data.yml"
printf '%s\n' 'mdx needle' >"$FIXTURE/wiki/page.mdx"
printf '%s\n' '{"value":"json needle"}' >"$FIXTURE/wiki/data.json"
printf '%s\n' 'text needle' >"$FIXTURE/wiki/ignored.txt"
printf '%s\n' 'outside needle' >"$FIXTURE/outside/decoy.md"

REAL_RG=$(command -v rg)
REAL_FD=$(command -v fd || command -v fdfind)
RG_LOG=$FIXTURE/rg.log
FD_LOG=$FIXTURE/fd.log
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'printf "rg\n" >>"$WIKI_TEST_RG_LOG"' \
  'exec "$WIKI_TEST_REAL_RG" "$@"' \
  >"$FIXTURE/bin/rg"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'printf "fd\n" >>"$WIKI_TEST_FD_LOG"' \
  'exec "$WIKI_TEST_REAL_FD" "$@"' \
  >"$FIXTURE/bin/fd"
chmod +x "$FIXTURE/bin/rg" "$FIXTURE/bin/fd"

run_cli() {
  PATH="$FIXTURE/bin:$PATH" \
    WIKI_TEST_RG_LOG="$RG_LOG" \
    WIKI_TEST_FD_LOG="$FD_LOG" \
    WIKI_TEST_REAL_RG="$REAL_RG" \
    WIKI_TEST_REAL_FD="$REAL_FD" \
    "$FIXTURE/scripts/wiki-cli" "$@"
}

: >"$RG_LOG"
: >"$FD_LOG"
search_output=$(run_cli search 'needle|alpha')
assert_contains "$search_output" 'concepts/One.md' 'search Markdown'
assert_contains "$search_output" 'sources/sample/content.md' 'search extracted source'
assert_contains "$search_output" 'page.mdx' 'search MDX'
assert_contains "$search_output" 'data.yaml' 'search YAML'
assert_contains "$search_output" 'data.yml' 'search YML'
assert_contains "$search_output" 'data.json' 'search JSON'
assert_not_contains "$search_output" '/source.pdf:' 'search PDF exclusion'
assert_not_contains "$search_output" '/ignored.txt:' 'search text exclusion'
assert_not_contains "$search_output" '/outside/decoy.md:' 'search vault boundary'
assert_eq 1 "$(wc -l <"$RG_LOG")" 'search rg process count'
assert_eq 0 "$(wc -l <"$FD_LOG")" 'search fd process count'

: >"$RG_LOG"
: >"$FD_LOG"
tags_output=$(run_cli tags)
assert_contains "$tags_output" '#topic/alpha' 'flow-list tag'
assert_contains "$tags_output" '#topic/beta' 'block-list tag'
assert_contains "$tags_output" '#inline/tag' 'inline tag'
assert_eq 1 "$(wc -l <"$RG_LOG")" 'tags rg process count'
assert_eq 1 "$(wc -l <"$FD_LOG")" 'tags fd process count'

backlinks_output=$(run_cli backlinks Two)
assert_contains "$backlinks_output" $'concepts/One.md:6\tTwo' 'backlinks output'

links_output=$(run_cli links concepts/One)
assert_contains "$links_output" $'concepts/One.md:6\twiki\tTwo' 'links output'

tag_output=$(run_cli tag topic)
assert_contains "$tag_output" '#topic/alpha' 'nested tag query'
assert_contains "$tag_output" '#topic/beta' 'nested tag query descendant'

orphans_output=$(run_cli orphans)
assert_contains "$orphans_output" 'sources/sample/content.md' 'orphan output'

set +e
check_output=$(run_cli check 2>&1)
check_status=$?
set -e
assert_eq 1 "$check_status" 'check failure status'
assert_contains "$check_output" 'missing: Missing' 'missing-link report'
assert_contains "$check_output" 'ambiguous: Duplicate' 'ambiguous-link report'

set +e
missing_output=$(run_cli missing 2>&1)
missing_status=$?
ambiguous_output=$(run_cli ambiguous 2>&1)
ambiguous_status=$?
set -e
assert_eq 1 "$missing_status" 'missing command status'
assert_contains "$missing_output" 'Missing' 'missing command output'
assert_eq 1 "$ambiguous_status" 'ambiguous command status'
assert_contains "$ambiguous_output" 'Duplicate' 'ambiguous command output'

: >"$RG_LOG"
: >"$FD_LOG"
for i in {1..100}; do
  printf '[[Two]]\n' >"$FIXTURE/wiki/concepts/Scale-$i.md"
done
set +e
run_cli check >/dev/null 2>&1
scaled_check_status=$?
set -e
assert_eq 1 "$scaled_check_status" 'scaled check failure status'
assert_eq 1 "$(wc -l <"$RG_LOG")" 'scaled check rg process count'
assert_eq 1 "$(wc -l <"$FD_LOG")" 'scaled check fd process count'

stats_output=$(run_cli stats)
assert_contains "$stats_output" 'Missing links:   1' 'stats missing count'
assert_contains "$stats_output" 'Ambiguous links: 1' 'stats ambiguous count'

printf 'wiki-cli tests passed\n'
