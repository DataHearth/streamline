#!/usr/bin/env bash
# Runs in issue-triage.yaml's `finish` job, after Claude's job succeeded and
# without the Claude token, over every issue the run covered: sets the native
# Issue Type from the type label when the issue has none (bug → Bug,
# enhancement → Feature, documentation → Task), and drops needs-triage, which
# only a successful run clears.
#
#   finish.sh 12 34 56
#
# Setting a type takes push access; without it REST answers 200 and drops the
# field, so the type is read back from the response rather than trusted.
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
      got=$(gh api -X PATCH "repos/$GITHUB_REPOSITORY/issues/$n" -f type="$want" --jq '.type.name // ""' || true)
      if [[ "$got" == "$want" ]]; then
        echo "#$n type: $want"
      else
        echo "::warning::could not set the Issue Type of #$n to $want (GitHub kept '${got:-none}')"
      fi
    fi
  fi

  if grep -qxF needs-triage <<<"$labels"; then
    gh issue edit "$n" --remove-label needs-triage >/dev/null
    echo "#$n triaged"
  fi
done
