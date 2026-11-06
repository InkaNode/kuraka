#!/usr/bin/env bash
# Kuraka builds Stalwart WebUI and Libredesk from upstream plus patches/<app>:
#   UPSTREAM  REPO and REF (the upstream tag) as shell variables.
#   series    patch files in apply order; "-name.patch" is skipped (it no longer
#             applies; the panel shows its Kuraka-Missing-* text until fixed).
# Each patch's commit message carries the trailers Kuraka-Severity (urgent,
# normal or required), Kuraka-Missing-En and Kuraka-Missing-Es: what users lose
# without it. A skipped "required" patch (one that changes the database) fails
# checkout, so CI fails and nothing merges or releases until it is fixed.
#
#   checkout <app> [dir]       clone upstream at REF and apply the series.
#   export <app> [dir]         write the commits after REF back to patches/<app>.
#   rebase <app> <ref> [dir]   move to upstream <ref>: apply what still applies,
#                              mark the rest skipped, export, and write a report
#                              for the pull request to <dir>.report.md.
#
# To fix a skipped patch: checkout, then in the clone run
# git am --3way <path to patch>, resolve, git am --continue, and export.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

usage() {
  printf 'Usage: bash scripts/patches.sh checkout|export <app> [dir]\n       bash scripts/patches.sh rebase <app> <ref> [dir]\n' >&2
  exit 2
}
(( $# >= 2 )) || usage
cmd=$1 app=$2
pdir=$PWD/patches/$app
if [[ ! $app =~ ^[a-z0-9-]+$ || ! -f $pdir/UPSTREAM ]]; then
  printf 'Unknown app: %s\n' "$app" >&2
  exit 2
fi
source "$pdir/UPSTREAM"

git_() { git -C "$dir" -c user.name=Kuraka -c user.email=kuraka@localhost "$@"; }

clone() {
  if [[ -e $dir ]]; then
    printf '%s already exists\n' "$dir" >&2
    exit 1
  fi
  # Blobless, not shallow: rebase needs upstream history between the two tags.
  git clone -q -c core.autocrlf=false --filter=blob:none --no-checkout "$REPO" "$dir"
  git -C "$dir" -c advice.detachedHead=false checkout -q --detach "refs/tags/$1"
}

series() { grep -v '^[[:space:]]*$' "$pdir/series" || true; }

trailer() { sed -n "s/^$1: //p" "$2" | head -n 1; }

export_patches() {
  local sha name key entry n
  local -a old names=()
  mapfile -t old < <(series)
  for sha in $(git -C "$dir" rev-list --reverse "refs/tags/$REF..HEAD"); do
    for key in Kuraka-Severity Kuraka-Missing-En Kuraka-Missing-Es; do
      if [[ -z $(git -C "$dir" log -1 --format="%(trailers:key=$key,valueonly)" "$sha") ]]; then
        printf 'Commit %s has no %s trailer\n' "$sha" "$key" >&2
        exit 1
      fi
    done
    name=$(git -C "$dir" log -1 --format=%f "$sha" | cut -c1-60 | sed "s/-*$//").patch
    for n in "${names[@]}"; do
      [[ $n != "$name" ]] || { printf 'Two commits are named %s; reword one\n' "$name" >&2; exit 1; }
    done
    git -C "$dir" format-patch -1 --stdout -N --zero-commit --no-signature "$sha" > "$pdir/$name.tmp"
    mv "$pdir/$name.tmp" "$pdir/$name"
    names+=("$name")
  done
  # Skipped patches stay, after the applied ones, until a commit replaces them.
  for entry in "${old[@]}"; do
    [[ $entry == -* ]] || continue
    for n in "${names[@]}"; do
      [[ $n != "${entry#-}" ]] || continue 2
    done
    names+=("$entry")
  done
  (( ${#names[@]} )) && printf '%s\n' "${names[@]}" > "$pdir/series" || : > "$pdir/series"
  for n in "$pdir"/*.patch; do
    [[ -e $n ]] || continue
    grep -qxF -e "${n##*/}" -e "-${n##*/}" "$pdir/series" || rm "$n"
  done
}

# report_skip <patch> <git am output> <old ref>: explain one failed patch.
report_skip() {
  local file=$pdir/$1 files
  printf '### `%s` (%s)\n\n' "$1" "$(trailer Kuraka-Severity "$file")"
  if [[ $(trailer Kuraka-Severity "$file") == required ]]; then
    printf 'Required: CI fails and this pull request cannot merge until it is fixed.\n\n'
  else
    printf 'Until this is fixed, Kuraka shows: %s\n\n' "$(trailer Kuraka-Missing-En "$file")"
  fi
  printf '<details><summary>git am output</summary>\n\n```\n%s\n```\n</details>\n\n' "$(head -n 40 <<< "$2")"
  files=$(git -C "$dir" diff --name-only --diff-filter=U)
  if [[ -n $files ]]; then
    printf 'Conflicts in:\n\n%s\n\n' "$(sed 's/.*/- `&`/' <<< "$files")"
    printf '<details><summary>Conflicts</summary>\n\n```diff\n%s\n```\n</details>\n\n' "$(git -C "$dir" diff | head -n 200)"
  else
    files=$(sed -n 's|^diff --git a/\(.*\) b/.*|\1|p' "$file")
    printf 'Files the patch changes:\n\n%s\n\n' "$(sed 's/.*/- `&`/' <<< "$files")"
  fi
  printf 'Upstream commits since %s that touch these files:\n\n' "$3"
  mapfile -t paths <<< "$files"
  git -C "$dir" log --format="- [%h]($REPO/commit/%H) %s" "refs/tags/$3..HEAD" -- "${paths[@]}" | head -n 20
  printf '\n'
}

case $cmd in
  checkout)
    dir=${3:-.work/$app}
    clone "$REF"
    mapfile -t entries < <(series)
    for entry in "${entries[@]}"; do
      if [[ $entry != -* ]]; then
        git_ am -q --3way "$pdir/$entry"
      elif [[ $(trailer Kuraka-Severity "$pdir/${entry#-}") == required ]]; then
        printf '%s is required but skipped; fix it before building
' "${entry#-}" >&2
        exit 1
      fi
    done
    ;;
  export)
    dir=${3:-.work/$app}
    export_patches
    ;;
  rebase)
    (( $# >= 3 )) || usage
    new=$3 dir=${4:-.work/$app}
    [[ $new =~ ^v?[0-9]+(\.[0-9]+)*$ ]] || { printf 'Bad upstream tag: %s\n' "$new" >&2; exit 2; }
    old_ref=$REF report=$dir.report.md
    # The old tag's blobs come down with its checkout; git am --3way needs them.
    clone "$old_ref"
    git -C "$dir" -c advice.detachedHead=false checkout -q --detach "refs/tags/$new"
    mapfile -t entries < <(series)
    : > "$report.skips"
    : > "$pdir/series"
    applied=0
    for entry in "${entries[@]}"; do
      name=${entry#-}
      if out=$(git_ am --3way "$pdir/$name" 2>&1); then
        printf '%s\n' "$name" >> "$pdir/series"
        applied=$((applied + 1))
      else
        printf -- '-%s\n' "$name" >> "$pdir/series"
        report_skip "$name" "$out" "$old_ref" >> "$report.skips"
        git_ am --abort
      fi
    done
    sed -i "s/^REF=.*/REF=$new/" "$pdir/UPSTREAM"
    REF=$new
    export_patches
    skipped=$(grep -c '^-' "$pdir/series" || true)
    {
      printf 'Upstream released [%s](%s/releases/tag/%s); Kuraka was on %s.\n\n' "$new" "$REPO" "$new" "$old_ref"
      printf '%s patches applied, %s skipped.\n\n' "$applied" "$skipped"
      if [[ -s $report.skips ]]; then
        printf '## Skipped patches\n\nKuraka builds without them and tells admins on the Updates page what does not work yet.\n\n'
        cat "$report.skips"
        printf '## How to fix\n\n```sh\nbash scripts/patches.sh checkout %s\ncd .work/%s\ngit am --3way ../../patches/%s/<patch>   # resolve, git add, git am --continue\ncd ../..\nbash scripts/patches.sh export %s\n```\n\nIf upstream now does what the patch did, delete the patch and its line in `series` instead.\n\n' "$app" "$app" "$app" "$app"
      fi
      printf 'CI merges this pull request when every check passes. If a check fails, it stays open.\n'
    } > "$report"
    rm "$report.skips"
    ;;
  *)
    usage
    ;;
esac
