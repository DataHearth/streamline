#!/usr/bin/env bash
# Runs after Claude, outside its reach, over every issue the run covered, and
# drops needs-triage — which only a successful run clears, so an issue still
# carrying it is one the run failed on.
#
#   finish.sh 12 34 56
#
# It does not set the native Issue Type: those are an organization feature
# and this repository belongs to a personal account. GitHub answers the
# update with 200 and keeps no type — for the Actions token, with or without
# contents: write, and for the owner's own token alike (all checked on #134).
# The type labels carry it. If the repository moves to an organization, the
# type step needs push access, which belongs in its own job, never in the
# one holding the Claude token.
set -euo pipefail

for n in "$@"; do
  labels=$(gh api "repos/$GITHUB_REPOSITORY/issues/$n" --jq '.labels[].name')
  if grep -qxF needs-triage <<<"$labels"; then
    gh issue edit "$n" --remove-label needs-triage >/dev/null
    echo "#$n triaged"
  fi
done
