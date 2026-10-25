#!/usr/bin/env bash
# The GraphQL documents are single-quoted on purpose: $owner and friends are
# GraphQL variables, bound by -f/-F, not shell ones.
# shellcheck disable=SC2016
#
# Runs after Claude, outside its reach, over every issue the run covered:
# sets the native Issue Type from the type label when the issue has none
# (bug → Bug, enhancement → Feature, documentation → Task), and drops
# needs-triage, which only a successful run clears.
#
#   finish.sh 12 34 56
set -euo pipefail

owner="${GITHUB_REPOSITORY%/*}" name="${GITHUB_REPOSITORY#*/}"

types=$(gh api graphql -f owner="$owner" -f name="$name" -f query='
  query($owner: String!, $name: String!) {
    repository(owner: $owner, name: $name) { issueTypes(first: 25) { nodes { id name } } }
  }' --jq '.data.repository.issueTypes.nodes // []')

for n in "$@"; do
  issue=$(gh api graphql -f owner="$owner" -f name="$name" -F number="$n" -f query='
    query($owner: String!, $name: String!, $number: Int!) {
      repository(owner: $owner, name: $name) {
        issue(number: $number) { id issueType { name } labels(first: 50) { nodes { name } } }
      }
    }' --jq '.data.repository.issue')
  [[ "$issue" != null ]] || continue
  labels=$(jq -r '.labels.nodes[].name' <<<"$issue")

  if [[ "$(jq -r '.issueType.name // empty' <<<"$issue")" == "" ]]; then
    want=""
    if grep -qxF bug <<<"$labels"; then want=Bug
    elif grep -qxF enhancement <<<"$labels"; then want=Feature
    elif grep -qxF documentation <<<"$labels"; then want=Task
    fi
    type_id=$(jq -r --arg w "$want" '.[] | select(.name == $w) | .id' <<<"$types")
    if [[ -n "$want" && -n "$type_id" ]]; then
      if gh api graphql -f id="$(jq -r .id <<<"$issue")" -f type="$type_id" -f query='
        mutation($id: ID!, $type: ID!) { updateIssue(input: {id: $id, issueTypeId: $type}) { issue { number } } }' >/dev/null; then
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
