#!/usr/bin/env bash
# Installs Libredesk and applies Libredesk, Stalwart and Kuraka updates requested in the panel.
# Runs as root from kuraka-updates.timer, installed by deploy/setup.sh.
#
# Kuraka only writes <dir>/libredesk-<container>.json = {"container", "tag"}
# and <dir>/stalwart.json = {"tag"}, <dir>/kuraka.json = {"tag", "commit"}.
# Installs use <dir>/install-<name>.json = {"name", "host", "system_password"}.
# Root renders install files with its trusted Kuraka image, never with panel files.
# Kuraka runs last because its checkout can replace this script.
# No field is trusted: all must match
# strict patterns, and the compose directory comes from the container's
# Docker labels (Libredesk updates), $DEPLOY/desks/<name> (installs) or this
# deploy directory (Stalwart and Kuraka), never from a path in the request.
# The result goes to <request>.result.json.
set -euo pipefail
umask 077

if [[ ! ${KURAKA_UPDATES_COPY+x} ]]; then
  copy=$(mktemp /run/kuraka-updates.XXXXXX)
  cp -- "$0" "$copy" || { rm -f "$copy"; exit 1; }
  KURAKA_UPDATES_COPY=1 exec bash "$copy" "$@"
fi
trap 'rm -f -- "$0"' EXIT

DEPLOY=${KURAKA_DEPLOY:-/root/kuraka/deploy}
REPO=$(dirname "$DEPLOY")
DIR=$DEPLOY/updates
IMAGE_RE='s#(image: libredesk/libredesk:)v[0-9.]+#\1'
ENV_FILE=$DEPLOY/.env
COMPOSE=(docker compose --project-directory "$DEPLOY" -f "$DEPLOY/docker-compose.yml" --env-file "$ENV_FILE")
# The STALWART_VERSION default in docker-compose.yml.
STALWART_DEFAULT=v1.0.2
CURL_IMAGE=curlimages/curl:latest
BACKUPS=$DEPLOY/backups

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

# write_result FILE TAG STATUS MESSAGE CURRENT [COMMIT]
write_result() {
  local tmp
  tmp=$(mktemp "$PRIVATE/result.XXXXXX")
  jq -n --arg tag "$2" --arg status "$3" --arg message "$4" --arg current "$5" \
    --arg commit "${6:-}" --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{tag: $tag, status: $status, message: $message, at: $at, current: $current, commit: $commit}' >"$tmp"
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

# set_env KEY VALUE replaces or appends KEY in .env and keeps the file mode.
set_env() {
  local tmp
  tmp=$(mktemp "$ENV_FILE.XXXXXX") || return 1
  if K="$1" L="$1=$2" awk '
    index($0, ENVIRON["K"] "=") == 1 { if (!done) print ENVIRON["L"]; done = 1; next }
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

# stalwart_volumes CMD... runs CMD as root in a throwaway container with
# Stalwart's volumes and $BACKUPS at /backup.
stalwart_volumes() {
  local cid
  cid=$("${COMPOSE[@]}" ps -aq stalwart 2>/dev/null) && [[ -n $cid ]] || return 1
  docker run --rm --user 0 --network none --volumes-from "$cid" -v "$BACKUPS:/backup"     --entrypoint "$1" "$CURL_IMAGE" "${@:2}" >&2
}

# update_stalwart TAG stops Stalwart and copies its data to $BACKUPS, sets
# STALWART_VERSION in .env, starts the new image once
# with STALWART_UPDATE_ROLE_PERMISSIONS=true (adds new permissions to the
# default roles) and waits for it, then recreates Stalwart without the
# variable and Kuraka so KURAKA_STALWART_VERSION follows. On failure it sets
# ERROR and returns 1.
update_stalwart() {
  local tag=$1 deadline
  [[ -f $ENV_FILE && ! -L $ENV_FILE ]] || { ERROR="deploy/.env not found"; return 1; }
  old=$(stalwart_version)
  install -d -m 0700 "$BACKUPS"
  backup=stalwart-$(date -u +%Y%m%dT%H%M%SZ).tar
  # Stopped, so the copy is consistent.
  stopped=1
  "${COMPOSE[@]}" stop stalwart >&2 || { ERROR="could not stop stalwart"; return 1; }
  if ! stalwart_volumes tar -cf "/backup/$backup" /etc/stalwart /var/lib/stalwart; then
    rm -f "$BACKUPS/$backup"
    ERROR="data backup failed"
    return 1
  fi
  chmod 600 "$BACKUPS/$backup"
  log "stalwart: data saved to $BACKUPS/$backup"
  changed=1
  set_env STALWART_VERSION "$tag" || { ERROR="could not edit deploy/.env"; return 1; }
  "${COMPOSE[@]}" pull stalwart >&2 || { ERROR="could not pull stalwart:$tag"; return 1; }
  # From here the new version may have changed the data.
  started=1
  STALWART_UPDATE_ROLE_PERMISSIONS=true "${COMPOSE[@]}" up -d stalwart >&2 ||
    { ERROR="could not start stalwart:$tag"; return 1; }
  deadline=$((SECONDS + 180))
  until stalwart_ready; do
    (( SECONDS < deadline )) || { ERROR="Stalwart did not start within 180 seconds"; return 1; }
    sleep 3
  done
  "${COMPOSE[@]}" up -d stalwart kuraka >&2 || { ERROR="could not restart stalwart and kuraka"; return 1; }
}

# rollback_stalwart puts the previous tag back and restarts Stalwart. If the
# new version started, its data is replaced by the backup first; the old
# version may not read data the new one changed.
rollback_stalwart() {
  (( stopped )) || return 0
  if (( changed )); then
    set_env STALWART_VERSION "$old" || { log "could not restore $old in $ENV_FILE"; return 0; }
  fi
  if (( started )); then
    if "${COMPOSE[@]}" stop stalwart >&2 &&
      stalwart_volumes sh -c 'find /etc/stalwart /var/lib/stalwart -mindepth 1 -delete && tar -xf "/backup/$1" -C /'         sh "$backup"; then
      log "stalwart: data restored from $BACKUPS/$backup"
    else
      ERROR="$ERROR; data restore failed, restore $BACKUPS/$backup by hand"
      log "stalwart: data restore failed, restore $BACKUPS/$backup by hand"
    fi
  fi
  "${COMPOSE[@]}" up -d stalwart >&2 || log "could not restart stalwart:$old"
}

install_desk() {
  local name=$1 host=$2 password=$3 desk_dir version image started=0
  desk_dir=$DEPLOY/desks/$name
  [[ ! -e $desk_dir/docker-compose.yml ]] || { ERROR="already installed"; return 1; }
  install -d -m 0700 "$desk_dir" || { ERROR="could not create desk directory"; return 1; }
  if [[ ! -f $ENV_FILE || -L $ENV_FILE ]]; then
    ERROR="deploy/.env not found"
  elif ! version=$(awk 'index($0, "KURAKA_VERSION=") == 1 { v = substr($0, 16) } END { print v }' "$ENV_FILE"); then
    ERROR="could not read KURAKA_VERSION"
  else
    version=${version%$'\r'}
    version=${version//[\"\']/}
    version=${version:-latest}
    image=ghcr.io/inkanode/kuraka:$version
    # Passed through the environment, not argv, so ps never shows it.
    if KURAKA_BUNDLE_SYSTEM_PASSWORD=$password docker run --rm --network none -e KURAKA_BUNDLE_SYSTEM_PASSWORD "$image" bundle "$name" "$host" |
      tar -xf - -C "$desk_dir"; then
      # The installer may partially start Compose even when it fails.
      started=1
      (cd "$desk_dir" && KURAKA_DEPLOY="$DEPLOY" sh install.sh) >&2 && return 0
      ERROR="Libredesk install failed"
    else
      ERROR="could not render Libredesk bundle"
    fi
  fi
  if (( ! started )); then
    rm -rf -- "$desk_dir"
  fi
  return 1
}

for request in "$DIR"/install-*.json; do
  [[ $request == *.result.json ]] && continue
  [[ -f $request && ! -L $request ]] || continue
  name= host= password= ERROR=
  name=$(jq -r '.name // empty' "$request" 2>/dev/null) || name=
  host=$(jq -r '.host // empty' "$request" 2>/dev/null) || host=
  password=$(jq -r '.system_password // empty' "$request" 2>/dev/null) || password=
  if [[ ! $name =~ ^[a-z0-9]+(-[a-z0-9]+)*$ || ${#name} -gt 48 ||
    ! $host =~ ^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$ ||
    ! $password =~ ^Aa9![0-9a-f]{48}$ || $request != "$DIR/install-$name.json" ]]; then
    log "rejected $request"
    rm -f "$request"
    continue
  fi
  log "$name: installing Libredesk"
  if install_desk "$name" "$host" "$password"; then
    write_result "$DIR/install-$name.result.json" "" ok "" ""
    log "$name: installed"
  else
    log "$name: $ERROR"
    write_result "$DIR/install-$name.result.json" "" error "$ERROR" ""
  fi
  rm -f "$request"
done

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
  tag= old= backup= stopped=0 changed=0 started=0 ERROR=
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

# Kuraka: only a release tag and a full commit on origin/main are accepted.
# This runs last; the private script copy survives either checkout.
kuraka_volumes() {
  docker run --rm --user 0 --network none -v "$data_volume:/data" -v "$BACKUPS:/backup" \
    --entrypoint "$1" "$CURL_IMAGE" "${@:2}" >&2
}

update_kuraka() {
  local commit=$1 cid image deadline health
  [[ $(git -C "$REPO" rev-parse --is-inside-work-tree 2>/dev/null) == true ]] ||
    { ERROR="deploy is not a git checkout"; return 1; }
  git -C "$REPO" fetch -q origin >&2 || { ERROR="could not fetch origin"; return 1; }
  git -C "$REPO" merge-base --is-ancestor "$commit" origin/main ||
    { ERROR="commit is not on main"; return 1; }
  old_commit=$(git -C "$REPO" rev-parse HEAD) || { ERROR="could not read HEAD"; return 1; }
  [[ -f $ENV_FILE && ! -L $ENV_FILE ]] || { ERROR="deploy/.env not found"; return 1; }
  old_version=$(awk 'index($0, "KURAKA_VERSION=") == 1 { v = substr($0, 16) } END { print v }' "$ENV_FILE") ||
    { ERROR="could not read KURAKA_VERSION"; return 1; }
  old_version=${old_version%$'\r'}
  old_version=${old_version//[\"\']/}
  old_version=${old_version:-latest}
  cid=$("${COMPOSE[@]}" ps -aq kuraka) && [[ -n $cid ]] ||
    { ERROR="kuraka container not found"; return 1; }
  data_volume=$(docker inspect -f '{{range .Mounts}}{{if and (eq .Destination "/data") (eq .Type "volume")}}{{.Name}}{{end}}{{end}}' "$cid") ||
    { ERROR="could not inspect kuraka data volume"; return 1; }
  [[ $data_volume =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] ||
    { ERROR="kuraka data volume not found at /data"; return 1; }
  install -d -m 0700 "$BACKUPS" || { ERROR="could not create backups directory"; return 1; }
  backup=kuraka-$(date -u +%Y%m%dT%H%M%SZ).tar
  # Set flags before commands which may partially change the deployment.
  stopped=1
  "${COMPOSE[@]}" stop kuraka >&2 || { ERROR="could not stop kuraka"; return 1; }
  if ! kuraka_volumes tar -cf "/backup/$backup" -C /data .; then
    rm -f "$BACKUPS/$backup"
    ERROR="kuraka data backup failed"
    return 1
  fi
  chmod 600 "$BACKUPS/$backup" || { ERROR="could not protect kuraka data backup"; return 1; }
  log "kuraka: data saved to $BACKUPS/$backup"
  reset=1
  git -C "$REPO" reset -q --hard "$commit" >&2 || { ERROR="could not reset to $commit"; return 1; }
  set_env KURAKA_VERSION "sha-$commit" || { ERROR="could not edit deploy/.env"; return 1; }
  image=ghcr.io/inkanode/kuraka:sha-$commit
  if ! docker pull "$image" >&2; then
    log "kuraka: pull failed; building $image locally"
    docker build -q --build-arg "KURAKA_COMMIT=$commit" -t "$image" "$REPO" >&2 ||
      { ERROR="could not build $image"; return 1; }
  fi
  # From here the new version may have migrated the database.
  started=1
  "${COMPOSE[@]}" up -d --build >&2 || { ERROR="could not start $image"; return 1; }
  "${COMPOSE[@]}" exec -T caddy caddy reload --config /etc/caddy/Caddyfile >&2 ||
    { ERROR="could not reload caddy"; return 1; }
  cid=$("${COMPOSE[@]}" ps -q kuraka) && [[ -n $cid ]] ||
    { ERROR="kuraka container not found"; return 1; }
  deadline=$((SECONDS + 180))
  while true; do
    health=$(docker inspect -f '{{.State.Health.Status}}' "$cid") ||
      { ERROR="could not inspect kuraka health"; return 1; }
    [[ $health == healthy ]] && return 0
    (( SECONDS < deadline )) || { ERROR="Kuraka did not become healthy within 180 seconds"; return 1; }
    sleep 3
  done
}

# Always attempt every recovery step, even if an earlier one fails. Never
# overwrite a live database: restoring the volume requires a successful stop.
rollback_kuraka() {
  if (( reset )); then
    if git -C "$REPO" reset -q --hard "$old_commit" >&2; then
      log "kuraka: checkout restored to $old_commit"
    else
      ERROR="$ERROR; could not restore checkout $old_commit"
      log "kuraka: could not restore checkout $old_commit"
    fi
    if set_env KURAKA_VERSION "$old_version"; then
      log "kuraka: KURAKA_VERSION restored to $old_version"
    else
      ERROR="$ERROR; could not restore KURAKA_VERSION=$old_version"
      log "kuraka: could not restore KURAKA_VERSION=$old_version"
    fi
  fi
  if (( started )); then
    if "${COMPOSE[@]}" stop kuraka >&2 &&
      kuraka_volumes sh -c 'find /data -mindepth 1 -delete && tar -xf "/backup/$1" -C /data' sh "$backup"; then
      log "kuraka: data restored from $BACKUPS/$backup"
    else
      ERROR="$ERROR; data restore failed, restore $BACKUPS/$backup by hand"
      log "kuraka: data restore failed, restore $BACKUPS/$backup by hand"
    fi
  fi
  if (( stopped )); then
    if "${COMPOSE[@]}" up -d --build >&2; then
      log "kuraka: restarted $old_version"
    else
      ERROR="$ERROR; could not restart kuraka:$old_version"
      log "kuraka: could not restart kuraka:$old_version"
    fi
    if "${COMPOSE[@]}" exec -T caddy caddy reload --config /etc/caddy/Caddyfile >&2; then
      log "kuraka: caddy reloaded"
    else
      ERROR="$ERROR; could not reload caddy during rollback"
      log "kuraka: could not reload caddy during rollback"
    fi
  fi
}

request=$DIR/kuraka.json
if [[ -e $request || -L $request ]]; then
  tag= commit= old_commit= old_version= data_volume= backup= stopped=0 reset=0 started=0 ERROR=
  if [[ -f $request && ! -L $request ]]; then
    tag=$(jq -r '.tag // empty' "$request" 2>/dev/null) || tag=
    commit=$(jq -r '.commit // empty' "$request" 2>/dev/null) || commit=
  fi
  if [[ ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ || ! $commit =~ ^[0-9a-f]{40}$ ]]; then
    log "rejected $request"
  else
    result=$DIR/kuraka.result.json
    current=$(jq -r '.current // empty' "$result" 2>/dev/null || true)
    log "kuraka: updating to $tag ($commit)"
    if update_kuraka "$commit"; then
      write_result "$result" "$tag" ok "" "$tag" "$commit"
      log "kuraka: running $tag ($commit)"
    else
      log "kuraka: $ERROR"
      rollback_kuraka
      write_result "$result" "$tag" error "$ERROR" "$current" "$commit"
    fi
  fi
  rm -f "$request"
fi
