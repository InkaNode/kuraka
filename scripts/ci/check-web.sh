#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

while IFS= read -r -d '' file; do
  node --check "$file"
done < <(find web/static -type d -name vendor -prune -o -type f -name '*.js' -print0)

pattern='innerHTML|outerHTML|insertAdjacentHTML|style="|\.style\b|https?://(?!www\.w3\.org)'
failed=0
while IFS= read -r -d '' file; do
  # Keep line numbers from the original file; exempt only the Lucide license
  # comment line, never other comments or the rest of the icons module.
  matches=$(grep -nHP "$pattern" "$file") && status=0 || status=$?
  if (( status > 1 )); then
    exit "$status"
  fi
  if (( status == 0 )); then
    # Also exempt the branding CSS variables: CSSOM setProperty is allowed by
    # the CSP, unlike style attributes.
    if violations=$(printf '%s\n' "$matches" | grep -vP '^[^:]+:[0-9]+:\s*//\s*Lucide icons, ISC license\.' |
      grep -vP '^web/static/assets/core/branding\.js:[0-9]+:\s*const style = document\.documentElement\.style;$'); then
      printf '%s\n' "$violations"
      failed=1
    fi
  fi
done < <(find web/static/assets -type d -name vendor -prune -o -type f -print0)

exit "$failed"
