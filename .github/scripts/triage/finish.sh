#!/usr/bin/env bash
# Runs after Claude, outside its reach, over every issue the run covered, and
# drops needs-triage — which only a successful run clears, so an issue still
# carrying it is one the run failed on.
#
#   finish.sh 12 34 56
#
# It does not set the native Issue Type. The Actions token cannot: GitHub
# answers the update with 200 and leaves the type unset (checked on #134),
# and a dedicated token able to would sit beside the Claude token in a job
# anyone opening an issue can start. The type labels carry that instead.
set -euo pipefail

for n in "$@"; do
  labels=$(gh api "repos/$GITHUB_REPOSITORY/issues/$n" --jq '.labels[].name')
  if grep -qxF needs-triage <<<"$labels"; then
    gh issue edit "$n" --remove-label needs-triage >/dev/null
    echo "#$n triaged"
  fi
done
