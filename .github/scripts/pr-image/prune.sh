#!/usr/bin/env bash
# Deletes PR images from the PR package (ghcr.io/<owner>/streamline-pr).
#
#   prune.sh pr <number>   versions of one PR, run when it closes
#   prune.sh sweep         versions of every closed PR, plus untagged versions
#                          older than a day: what a failed or cancelled
#                          cleanup left behind
#
# GHCR deletes by version, not by tag. A version is a PR's only when every tag
# on it is `pr-<n>` or starts with `pr-<n>-`, so PR 15 never matches pr-159-*.
# The commit tags (`pr-<n>-<sha>[-vaapi]`) are what keep that true after a new
# push moves `pr-<n>` to the next version. An untagged version can belong to
# nobody but a PR: this package holds nothing else.
#
# GHCR refuses to delete a package's last tagged version, so a run that would
# leave none keeps the newest one; the next sweep after another PR publishes
# removes it.
#
# Env: GITHUB_REPOSITORY, GH_TOKEN; PACKAGE (default streamline-pr);
# DRY_RUN=1 lists what would go and deletes nothing; VERSIONS_FILE reads the
# version listing from a file instead of the API (with DRY_RUN, for testing).

set -euo pipefail

REPO="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set}"
OWNER="${REPO%%/*}"
PACKAGE="${PACKAGE:-streamline-pr}"
DRY_RUN="${DRY_RUN:-0}"
export GH_REPO="$REPO" GH_HOST=github.com

die() {
  echo "::error::$*" >&2
  exit 1
}

usage() { die "usage: prune.sh pr <number> | prune.sh sweep"; }

# The package API is under /users or /orgs depending on who owns the repo.
package_path() {
  local kind
  case "$(gh api "users/$OWNER" --jq .type)" in
    Organization) kind=orgs ;;
    *) kind=users ;;
  esac
  echo "$kind/$OWNER/packages/container/$PACKAGE"
}

# Every version as one JSON array of {id, updated_at, tags}. An empty array
# when the package does not exist yet (nothing was ever published).
list_versions() {
  if [[ -n "${VERSIONS_FILE:-}" ]]; then
    jq -c '[.[] | {id, updated_at, tags: (.metadata.container.tags // [])}]' "$VERSIONS_FILE"
    return
  fi
  local err out
  err=$(mktemp)
  if ! out=$(gh api --paginate "$BASE/versions?per_page=100" \
    --jq '.[] | {id, updated_at, tags: (.metadata.container.tags // [])}' 2>"$err"); then
    if grep -q 'HTTP 404' "$err"; then
      echo "::notice::package $PACKAGE does not exist yet; nothing to delete" >&2
      rm -f "$err"
      echo '[]'
      return
    fi
    cat "$err" >&2
    rm -f "$err"
    die "could not list the versions of $PACKAGE"
  fi
  rm -f "$err"
  jq -sc . <<<"$out"
}

# Ids of the versions whose tags all belong to PR $1.
versions_of_pr() {
  jq -r --arg p "pr-$1" \
    '.[] | select((.tags | length) > 0 and all(.tags[]; . == $p or startswith($p + "-"))) | .id' \
    <<<"$VERSIONS"
}

# PR numbers named by any tag, one per line.
prs_in_package() {
  jq -r '[.[].tags[] | capture("^pr-(?<n>[1-9][0-9]*)(-|$)").n] | unique | .[]' <<<"$VERSIONS"
}

# Ids of untagged versions last updated more than a day ago.
stale_untagged() {
  jq -r '.[] | select((.tags | length) == 0 and (.updated_at | fromdateiso8601) < (now - 86400)) | .id' \
    <<<"$VERSIONS"
}

# Copies the selection on stdin to stdout, less the newest tagged version
# when the selection holds every tagged version of the package.
keep_last_tagged() {
  local sel keep
  sel=$(grep -v '^$' || true)
  keep=$(jq -r --arg sel "$sel" '
    ($sel | split("\n") | map(select(. != "") | tonumber)) as $s
    | map(select(.tags | length > 0)) as $t
    | if ($t | length) > 0 and ($t | all(.[]; .id as $i | $s | any(.[]; . == $i)))
      then $t | max_by(.updated_at) | .id
      else empty end' <<<"$VERSIONS")
  if [[ -n "$keep" ]]; then
    echo "::notice::keeping version $keep ($(tags_of "$keep")): GHCR refuses to delete a package's last tagged version" >&2
    grep -vxF "$keep" <<<"$sel" || true
  else
    printf '%s\n' "$sel"
  fi
}

tags_of() { jq -r --argjson i "$1" '.[] | select(.id == $i) | .tags | if length == 0 then "untagged" else join(", ") end' <<<"$VERSIONS"; }

# Deletes the versions whose ids are on stdin. Writes a Markdown list of what
# it did to stdout (the workflow tees it into the step summary) and fails when
# any delete did.
delete_versions() {
  local failed=0 count=0 id
  while read -r id; do
    [[ -n "$id" ]] || continue
    count=$((count + 1))
    if [[ "$DRY_RUN" == 1 ]]; then
      echo "- would delete version $id ($(tags_of "$id"))"
    elif gh api --method DELETE "$BASE/versions/$id" --silent; then
      echo "- deleted version $id ($(tags_of "$id"))"
    else
      echo "::error::could not delete version $id ($(tags_of "$id"))" >&2
      echo "- **failed** to delete version $id ($(tags_of "$id"))"
      failed=1
    fi
  done
  [[ "$count" -gt 0 ]] || echo "Nothing to delete."
  return "$failed"
}

[[ $# -ge 1 ]] || usage
mode="$1"
if [[ -n "${VERSIONS_FILE:-}" ]]; then
  BASE="(file)"
else
  BASE=$(package_path)
fi
VERSIONS=$(list_versions)

case "$mode" in
  pr)
    [[ $# -eq 2 && "$2" =~ ^[1-9][0-9]*$ ]] || usage
    versions_of_pr "$2" | keep_last_tagged | delete_versions
    ;;
  sweep)
    [[ $# -eq 1 ]] || usage
    selected=$(stale_untagged)
    while read -r n; do
      [[ -n "$n" ]] || continue
      state=$(gh api "repos/$REPO/pulls/$n" --jq .state)
      if [[ "$state" == closed ]]; then
        selected+=$'\n'$(versions_of_pr "$n")
      fi
    done < <(prs_in_package)
    keep_last_tagged <<<"$selected" | delete_versions
    ;;
  *) usage ;;
esac
