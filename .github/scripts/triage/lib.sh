# shellcheck shell=bash
# Shared by the triage wrappers. Everything that decides what a wrapper may do
# comes from the workflow's own event payload or from .github/labels.yml —
# never from an argument, which is whatever Claude typed.

set -euo pipefail

REPO="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set}"
EVENT="${GITHUB_EVENT_NAME:?GITHUB_EVENT_NAME must be set}"
PAYLOAD="${GITHUB_EVENT_PATH:?GITHUB_EVENT_PATH must be set}"
export GH_REPO="$REPO" GH_HOST=github.com

LABELS_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/labels.yml"

die() {
  echo "Error: $*" >&2
  exit 1
}

# Labels of the given groups, one per line.
labels_in() {
  local groups
  groups=$(printf '%s\n' "$@" | jq -R . | jq -sc .)
  yq -o=json '.' "$LABELS_FILE" |
    jq -r --argjson g "$groups" '.[] | select(.group as $x | $g | index($x)) | .name'
}

# What Claude may add or remove on this run. A backfill re-labels old issues,
# so it gets type and area only: a status label is a statement about a report
# as it was just filed. needs-triage is the workflow's own marker, set and
# cleared around the run, so it is never Claude's.
triage_labels() {
  if [[ "$EVENT" == issues ]]; then
    labels_in type area status | grep -vxF needs-triage
  else
    labels_in type area
  fi
}

event_issue() { jq -r '.issue.number // empty' "$PAYLOAD"; }

# True when the issue in the payload was opened by the owner, an org member or
# a collaborator. Their issues are labelled but never asked for more detail.
author_is_maintainer() {
  case "$(jq -r '.issue.author_association // empty' "$PAYLOAD")" in
    OWNER | MEMBER | COLLABORATOR) return 0 ;;
    *) return 1 ;;
  esac
}

# Fails unless this run may touch issue $1: on a new issue, that issue alone;
# on a dispatch, the numbers it was given, or any issue (never a PR) when it
# was given none.
require_issue() {
  local n="$1"
  [[ "$n" =~ ^[1-9][0-9]*$ ]] || die "the issue number must be a plain number, got '$n'"
  case "$EVENT" in
    issues)
      [[ "$n" == "$(event_issue)" ]] || die "this run may only edit issue #$(event_issue)"
      ;;
    workflow_dispatch)
      local list
      list=$(jq -r '.inputs.issues // ""' "$PAYLOAD" | tr -s ' ,' '\n' | grep -v '^$' || true)
      if [[ -n "$list" ]]; then
        grep -qxF "$n" <<<"$list" || die "issue #$n is not in this run's list"
      fi
      [[ "$(gh api "repos/$REPO/issues/$n" --jq '.pull_request != null')" == false ]] ||
        die "#$n is a pull request; only issues are triaged"
      ;;
    *) die "unsupported event '$EVENT'" ;;
  esac
}
