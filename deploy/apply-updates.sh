#!/usr/bin/env bash
# Applies the Libredesk updates requested in Kuraka > Integrations > Updates.
# Runs as root from kuraka-updates.timer, installed by deploy/setup.sh.
#
# Kuraka only writes <dir>/libredesk-<container>.json = {"container", "tag"}.
# Neither field is trusted: both must match strict patterns, and the compose
# directory comes from the container's Docker labels, never from the file.
# The result goes to <dir>/libredesk-<container>.result.json.
set -euo pipefail
umask 077

DIR=${KURAKA_DEPLOY:-/root/kuraka/deploy}/updates
IMAGE_RE='s#(image: libredesk/libredesk:)v[0-9.]+#\1'

log() { printf '%s\n' "$*" >&2; }

for cmd in docker jq flock; do
  command -v "$cmd" >/dev/null || { log "$cmd is required"; exit 1; }
done
exec 9>/run/kuraka-updates.lock
flock -n 9 || exit 0
shopt -s nullglob

# Kuraka owns $DIR, so root never writes, chowns or chmods there: results
# are built in a root-only directory on the same filesystem and renamed in
# (rename replaces any planted symlink instead of following it). They hold
# no secrets and are world-readable.
PRIVATE=${KURAKA_DEPLOY:-/root/kuraka/deploy}/.updates-tmp
install -d -m 0700 "$PRIVATE"

# write_result FILE TAG STATUS MESSAGE CURRENT
write_result() {
  local tmp
  tmp=$(mktemp "$PRIVATE/result.XXXXXX")
  jq -n --arg tag "$2" --arg status "$3" --arg message "$4" --arg current "$5"     --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"     '{tag: $tag, status: $status, message: $message, at: $at, current: $current}' >"$tmp"
  chmod 644 "$tmp"
  mv -T "$tmp" "$1"
}

# update CONTAINER TAG backs up the database, switches the image tag and
# waits for Libredesk. On failure it sets ERROR and returns 1.
update() {
  local container=$1 tag=$2 since logs attempt backup
  desk_dir=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null) ||
    { ERROR="container not found"; return 1; }
  compose_file=$desk_dir/docker-compose.yml
  [[ $desk_dir == /* && -f $compose_file ]] || { ERROR="docker-compose.yml not found"; return 1; }
  old=$(sed -n -E 's#^[[:space:]]*image: libredesk/libredesk:(v[0-9.]+)[[:space:]]*$#\1#p' "$compose_file" | head -n 1)
  [[ -n $old ]] || { ERROR="libredesk image not found in docker-compose.yml"; return 1; }

  backup=$desk_dir/backup-$(date -u +%Y%m%dT%H%M%SZ).sql
  if ! (cd "$desk_dir" && docker compose exec -T db pg_dump -U libredesk libredesk) >"$backup"; then
    rm -f "$backup"
    ERROR="database backup failed"
    return 1
  fi
  chmod 600 "$backup"
  log "$container: database saved to $backup"

  changed=1
  sed -i -E "$IMAGE_RE$tag#" "$compose_file" || { ERROR="could not edit docker-compose.yml"; return 1; }
  since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  (cd "$desk_dir" && docker compose pull app && docker compose up -d app) >&2 ||
    { ERROR="could not pull or start libredesk:$tag"; return 1; }
  for ((attempt = 0; attempt < 60; attempt++)); do
    logs=$(cd "$desk_dir" && docker compose logs --since "$since" app 2>/dev/null) || logs=
    [[ $logs == *'server starting'* ]] && return 0
    sleep 3
  done
  ERROR="Libredesk did not start within 180 seconds"
  return 1
}

# rollback puts the previous tag back and restarts the app.
rollback() {
  (( changed )) || return 0
  sed -i -E "$IMAGE_RE$old#" "$compose_file" || { log "could not restore $old in $compose_file"; return 0; }
  (cd "$desk_dir" && docker compose up -d app) >&2 || log "could not restart libredesk:$old"
}

for request in "$DIR"/libredesk-*.json; do
  [[ $request == *.result.json ]] && continue
  container= tag= desk_dir= compose_file= old= changed=0 ERROR=
  if [[ -f $request && ! -L $request ]]; then
    container=$(jq -r '.container // empty' "$request" 2>/dev/null) || container=
    tag=$(jq -r '.tag // empty' "$request" 2>/dev/null) || tag=
  fi
  if [[ ! $container =~ ^kuraka-[a-z0-9-]+-desk$ || ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ||
    $request != "$DIR/libredesk-$container.json" ]]; then
    log "rejected $request"
    rm -f "$request"
    continue
  fi
  result=$DIR/libredesk-$container.result.json
  log "$container: updating to $tag"
  if update "$container" "$tag"; then
    write_result "$result" "$tag" ok "" "$tag"
    log "$container: running $tag"
  else
    log "$container: $ERROR"
    rollback
    current=${old:-$(jq -r '.current // empty' "$result" 2>/dev/null || true)}
    write_result "$result" "$tag" error "$ERROR" "$current"
  fi
  rm -f "$request"
done
