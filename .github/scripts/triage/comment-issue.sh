#!/usr/bin/env bash
# Posts the one comment a triage run may leave, on the new issue it was
# started for, and only when a maintainer did not open it.
#
#   comment-issue.sh 123 --body "Could you add …"
#
# The body is plain text from the model: at most 1500 characters and no
# @-mention, so an issue cannot make the bot notify somebody. The workflow
# caps this script at one call per run (CLAUDE_CODE_SCRIPT_CAPS).
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[[ "$EVENT" == issues ]] || die "comments are only posted on a newly opened issue, never on a backfill"
n="${1:-}"
require_issue "$n"
[[ "${2:-}" == --body && $# -eq 3 ]] || die "usage: comment-issue.sh <issue> --body \"text\""
body="$3"

! author_is_maintainer || die "a maintainer opened #$n; it gets labels only, no comment"
[[ -n "${body//[[:space:]]/}" ]] || die "the body is empty"
[[ ${#body} -le 1500 ]] || die "the body is ${#body} characters; keep it under 1500"
! grep -qE '(^|[^[:alnum:]_.])@[[:alnum:]]' <<<"$body" || die "the body must not @-mention anyone"

printf '%s\n\n---\n_Automated triage by [Claude Code](https://claude.ai/code) — a maintainer reads every issue._\n' "$body" |
  gh issue comment "$n" --body-file - >/dev/null
echo "commented on #$n"
