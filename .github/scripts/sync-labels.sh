#!/usr/bin/env bash
# Makes the repository's labels match .github/labels.yml: creates the missing
# ones, updates colour and description on the rest, and renames an alias in
# place so the label stays on every issue that carries it. Never deletes a
# label — one missing from the file is left alone.
#
# Usage: GH_TOKEN=… GH_REPO=owner/repo .github/scripts/sync-labels.sh [labels.yml]
set -euo pipefail

file="${1:-.github/labels.yml}"

existing=$(gh label list --limit 500 --json name --jq '.[].name')
has() { grep -qixF -- "$1" <<<"$existing"; }

while IFS= read -r label; do
  name=$(jq -r '.name' <<<"$label")
  color=$(jq -r '.color' <<<"$label")
  description=$(jq -r '.description' <<<"$label")

  if has "$name"; then
    gh label edit "$name" --color "$color" --description "$description" >/dev/null
    echo "updated  $name"
    continue
  fi

  renamed=false
  while IFS= read -r alias; do
    if [[ -z "$alias" ]] || ! has "$alias"; then continue; fi
    gh label edit "$alias" --name "$name" --color "$color" --description "$description" >/dev/null
    echo "renamed  $alias -> $name"
    renamed=true
    break
  done < <(jq -r '.aliases // [] | .[]' <<<"$label")

  if [[ "$renamed" == false ]]; then
    gh label create "$name" --color "$color" --description "$description" >/dev/null
    echo "created  $name"
  fi
done < <(yq -o=json '.' "$file" | jq -c '.[]')
