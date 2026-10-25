#!/usr/bin/env bash
# Runs after Claude, outside its reach, over every issue the run covered:
# sets the native Issue Type from the type label when the issue has none
# (bug → Bug, enhancement → Feature, documentation → Task), and drops
# needs-triage, which only a successful run clears.
#
#   finish.sh 12 34 56
#
# The type is set by name through REST. The first version looked the type's
# node id up through GraphQL (repository.issueTypes) and, on the first real
# run, set nothing and said nothing — a lookup that came back empty skipped
# the issue silently. A failure here now always warns.
set -euo pipefail

for n in "$@"; do
  issue=$(gh api "repos/$GITHUB_REPOSITORY/issues/$n")
  labels=$(jq -r '.labels[].name' <<<"$issue")

  if [[ -z "$(jq -r '.type.name // empty' <<<"$issue")" ]]; then
    want=""
    if grep -qxF bug <<<"$labels"; then want=Bug
    elif grep -qxF enhancement <<<"$labels"; then want=Feature
    elif grep -qxF documentation <<<"$labels"; then want=Task
    fi
    if [[ -n "$want" ]]; then
      if gh api -X PATCH "repos/$GITHUB_REPOSITORY/issues/$n" -f type="$want" >/dev/null; then
        echo "#$n type: $want"
      else
        echo "::warning::could not set the Issue Type of #$n to $want"
      fi
    fi
  fi

  if grep -qxF needs-triage <<<"$labels"; then
    gh issue edit "$n" --remove-label needs-triage >/dev/null
    echo "#$n triaged"
  fi
done
