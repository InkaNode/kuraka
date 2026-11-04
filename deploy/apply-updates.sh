#!/usr/bin/env bash
# Applies the Stalwart and Libredesk updates requested in Kuraka >
# Integrations > Updates. Runs as root from kuraka-updates.timer, installed
# by deploy/setup.sh.
#
# Kuraka only writes <dir>/libredesk-<container>.json = {"container", "tag"}
# and <dir>/stalwart.json = {"tag"}. No field is trusted: all must match
# strict patterns, and the compose directory comes from the container's
# Docker labels (Libredesk) or is this deploy directory (Stalwart), never
# from the file. The result goes to <request>.result.json.
set -euo pipefail
umask 077

DEPLOY=${KURAKA_DEPLOY:-/root/kuraka/deploy}
DIR=$DEPLOY/updates
IMAGE_RE='s#(image: libredesk/libredesk:)v[0-9.]+#\1'
ENV_FILE=$DEPLOY/.env
COMPOSE=(docker compose --project-directory "$DEPLOY" -f "$DEPLOY/docker-compose.yml" --env-file "$ENV_FILE")
# The STALWART_VERSION default in docker-compose.yml.
STALWART_DEFAULT=v1.0.2
CURL_IMAGE=curlimages/curl:latest

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
PRIVATE=$DEPLOY/.updates-tmp
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
  local container=$1 tag=$2 since logs attempt
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
  (cd "$desk_dir" && docker compose pull app) >&2 || { ERROR="could not pull libredesk:$tag"; return 1; }
  # From here the new version may have migrated the database.
  started=1
  (cd "$desk_dir" && docker compose up -d app) >&2 || { ERROR="could not start libredesk:$tag"; return 1; }
  for ((attempt = 0; attempt < 60; attempt++)); do
    logs=$(cd "$desk_dir" && docker compose logs --since "$since" app 2>/dev/null) || logs=
    [[ $logs == *'server starting'* ]] && return 0
    sleep 3
  done
  ERROR="Libredesk did not start within 180 seconds"
  return 1
}

# rollback puts the previous tag back and restarts the app. If the new
# version started, it may have migrated the database, so the backup is
# restored first; the old version may not run on a migrated schema.
rollback() {
  (( changed )) || return 0
  sed -i -E "$IMAGE_RE$old#" "$compose_file" || { log "could not restore $old in $compose_file"; return 0; }
  if (( started )); then
    if ! (cd "$desk_dir" && docker compose stop app &&
      docker compose exec -T db psql -v ON_ERROR_STOP=1 -U libredesk -d postgres         -c 'DROP DATABASE libredesk WITH (FORCE)' -c 'CREATE DATABASE libredesk OWNER libredesk' &&
      docker compose exec -T db psql -v ON_ERROR_STOP=1 -q -U libredesk -d libredesk <"$backup") >&2; then
      ERROR="$ERROR; database restore failed, restore $backup by hand"
      log "$container: database restore failed, restore $backup by hand"
    else
      log "$container: database restored from $backup"
    fi
  fi
  (cd "$desk_dir" && docker compose up -d app) >&2 || log "could not restart libredesk:$old"
}

# stalwart_version prints STALWART_VERSION from .env, or the compose default
# when it is missing or not a stable tag.
stalwart_version() {
  local v
  v=$(awk 'index($0, "STALWART_VERSION=") == 1 { v = substr($0, 18) } END { print v }' "$ENV_FILE")
  v=${v%$'\r'}
  v=${v//[\"\']/}
  [[ $v =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || v=$STALWART_DEFAULT
  printf '%s' "$v"
}

# set_stalwart_version TAG replaces or appends STALWART_VERSION in .env and
# keeps the file mode.
set_stalwart_version() {
  local tmp
  tmp=$(mktemp "$ENV_FILE.XXXXXX") || return 1
  if L="STALWART_VERSION=$1" awk '
    index($0, "STALWART_VERSION=") == 1 { if (!done) print ENVIRON["L"]; done = 1; next }
    { print }
    END { if (!done) print ENVIRON["L"] }' "$ENV_FILE" >"$tmp" &&
    chmod --reference="$ENV_FILE" "$tmp" && mv -T "$tmp" "$ENV_FILE"; then
    return 0
  fi
  rm -f "$tmp"
  return 1
}

# stalwart_ready succeeds once Stalwart is healthy or, without a healthcheck,
# running and answering JMAP (the check deploy/setup.sh uses).
stalwart_ready() {
  local cid health code
  cid=$("${COMPOSE[@]}" ps -q stalwart 2>/dev/null) || return 1
  [[ -n $cid ]] || return 1
  health=$(docker inspect -f '{{.State.Status}}/{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$cid" 2>/dev/null) ||
    return 1
  case $health in
    running/healthy) return 0 ;;
    running/) ;;
    *) return 1 ;;
  esac
  code=$(docker run --rm --network kuraka_mail "$CURL_IMAGE" --silent --output /dev/null \
    --write-out '%{http_code}' --max-time 2 http://stalwart:8080/jmap/session 2>/dev/null) || code=000
  [[ $code == 200 || $code == 401 ]]
}

# update_stalwart TAG sets STALWART_VERSION in .env, starts the new image once
# with STALWART_UPDATE_ROLE_PERMISSIONS=true (adds new permissions to the
# default roles) and waits for it, then recreates Stalwart without the
# variable and Kuraka so KURAKA_STALWART_VERSION follows. On failure it sets
# ERROR and returns 1.
update_stalwart() {
  local tag=$1 deadline
  [[ -f $ENV_FILE && ! -L $ENV_FILE ]] || { ERROR="deploy/.env not found"; return 1; }
  old=$(stalwart_version)
  changed=1
  set_stalwart_version "$tag" || { ERROR="could not edit deploy/.env"; return 1; }
  "${COMPOSE[@]}" pull stalwart >&2 || { ERROR="could not pull stalwart:$tag"; return 1; }
  STALWART_UPDATE_ROLE_PERMISSIONS=true "${COMPOSE[@]}" up -d stalwart >&2 ||
    { ERROR="could not start stalwart:$tag"; return 1; }
  deadline=$((SECONDS + 180))
  until stalwart_ready; do
    (( SECONDS < deadline )) || { ERROR="Stalwart did not start within 180 seconds"; return 1; }
    sleep 3
  done
  "${COMPOSE[@]}" up -d stalwart kuraka >&2 || { ERROR="could not restart stalwart and kuraka"; return 1; }
}

# rollback_stalwart puts the previous tag back and restarts Stalwart.
rollback_stalwart() {
  (( changed )) || return 0
  set_stalwart_version "$old" || { log "could not restore $old in $ENV_FILE"; return 0; }
  "${COMPOSE[@]}" up -d stalwart >&2 || log "could not restart stalwart:$old"
}

for request in "$DIR"/libredesk-*.json; do
  [[ $request == *.result.json ]] && continue
  container= tag= desk_dir= compose_file= old= backup= changed=0 started=0 ERROR=
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

# Stalwart: only the tag is read and it must match a strict pattern. The
# compose project is always $DEPLOY, and the tag goes into its .env because
# deploys reset docker-compose.yml to origin/main.
request=$DIR/stalwart.json
if [[ -e $request || -L $request ]]; then
  tag= old= changed=0 ERROR=
  if [[ -f $request && ! -L $request ]]; then
    tag=$(jq -r '.tag // empty' "$request" 2>/dev/null) || tag=
  fi
  if [[ ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log "rejected $request"
  else
    result=$DIR/stalwart.result.json
    log "stalwart: updating to $tag"
    if update_stalwart "$tag"; then
      write_result "$result" "$tag" ok "" "$tag"
      log "stalwart: running $tag"
    else
      log "stalwart: $ERROR"
      rollback_stalwart
      current=${old:-$(jq -r '.current // empty' "$result" 2>/dev/null || true)}
      write_result "$result" "$tag" error "$ERROR" "$current"
    fi
  fi
  rm -f "$request"
fi
