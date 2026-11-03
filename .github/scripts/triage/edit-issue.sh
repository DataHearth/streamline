#!/usr/bin/env bash
# Adds and removes triage labels on one issue — nothing else about the issue
# can be changed through it.
#
#   edit-issue.sh 123 --add-label area/tv --add-label bug --remove-label area/web
#
# Only labels of the groups this run may touch are accepted (lib.sh
# triage_labels), needs-info only on an issue a maintainer did not open, and
# an issue ends with at most two area labels.
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

n="${1:-}"
shift || die "usage: edit-issue.sh <issue> [--add-label L]… [--remove-label L]…"
require_issue "$n"

add=() remove=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --add-label) add+=("${2:?--add-label needs a label}") ;;
    --remove-label) remove+=("${2:?--remove-label needs a label}") ;;
    *) die "only --add-label and --remove-label are accepted" ;;
  esac
  shift 2
done
[[ ${#add[@]} -gt 0 || ${#remove[@]} -gt 0 ]] || die "nothing to change"

permitted=$(triage_labels)
for label in "${add[@]}" "${remove[@]}"; do
  grep -qxF -- "$label" <<<"$permitted" ||
    die "'$label' is not a label this run may change; use one of: $(tr '\n' ' ' <<<"$permitted")"
done

if printf '%s\n' "${add[@]}" | grep -qxF needs-info && author_is_maintainer; then
  die "needs-info is never added to an issue a maintainer opened"
fi

current=$(gh issue view "$n" --json labels --jq '.labels[].name')
areas=$({
  grep '^area/' <<<"$current" || true
  printf '%s\n' "${add[@]}" | grep '^area/' || true
} | sort -u | grep -vxF -f <(printf '%s\n' "${remove[@]}" '') || true)
[[ $(grep -c . <<<"$areas" || true) -le 2 ]] ||
  die "issue #$n would end with more than two area labels: $(tr '\n' ' ' <<<"$areas")"

args=()
for label in "${add[@]}"; do args+=(--add-label "$label"); done
for label in "${remove[@]}"; do args+=(--remove-label "$label"); done
gh issue edit "$n" "${args[@]}" >/dev/null

[[ ${#add[@]} -eq 0 ]] || echo "#$n added: ${add[*]}"
[[ ${#remove[@]} -eq 0 ]] || echo "#$n removed: ${remove[*]}"
