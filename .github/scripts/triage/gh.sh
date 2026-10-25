#!/usr/bin/env bash
# Read-only gh for the triage run, scoped to this repository. Adapted from
# anthropics/claude-code-action's scripts/gh.sh.
#
#   gh.sh issue view 123 [--comments]
#   gh.sh issue list [--state open|closed|all] [--limit N] [--label L] [--search Q]
#   gh.sh search issues "query" [--state open|closed] [--limit N]
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cmd="${1:-} ${2:-}"
shift 2 || die "usage: gh.sh issue view|issue list|search issues …"

case "$cmd" in
  "issue view") allowed=(--comments) valued=() ;;
  "issue list") allowed=(--state --limit --label --search) valued=(--state --limit --label --search) ;;
  "search issues") allowed=(--state --limit) valued=(--state --limit) ;;
  *) die "only 'issue view', 'issue list' and 'search issues' are allowed" ;;
esac

positional=() flags=() expect_value=false
for arg in "$@"; do
  if [[ "$expect_value" == true ]]; then
    flags+=("$arg")
    expect_value=false
    continue
  fi
  if [[ "$arg" != -* ]]; then
    positional+=("$arg")
    continue
  fi
  flag="${arg%%=*}"
  printf '%s\n' "${allowed[@]}" | grep -qxF -- "$flag" || die "flag $flag is not allowed for '$cmd' (allowed: ${allowed[*]})"
  flags+=("$arg")
  if [[ "$arg" != *=* ]] && printf '%s\n' "${valued[@]}" | grep -qxF -- "$flag"; then
    expect_value=true
  fi
done
[[ "$expect_value" == false ]] || die "flag ${flags[-1]} needs a value"

case "$cmd" in
  "issue view")
    [[ ${#positional[@]} -eq 1 && "${positional[0]}" =~ ^[0-9]+$ ]] || die "issue view takes exactly one issue number"
    gh issue view "${positional[0]}" "${flags[@]}"
    ;;
  "issue list")
    [[ ${#positional[@]} -eq 0 ]] || die "issue list takes no positional argument"
    gh issue list "${flags[@]}"
    ;;
  "search issues")
    [[ ${#positional[@]} -eq 1 ]] || die "search issues takes exactly one quoted query"
    query_lower=$(tr '[:upper:]' '[:lower:]' <<<"${positional[0]}")
    [[ "$query_lower" != *repo:* && "$query_lower" != *org:* && "$query_lower" != *user:* ]] ||
      die "the query must not carry repo:, org: or user: qualifiers"
    gh search issues "${positional[0]}" --repo "$REPO" "${flags[@]}"
    ;;
esac
