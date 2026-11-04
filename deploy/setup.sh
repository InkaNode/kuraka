#!/usr/bin/env bash
# First install of the Kuraka stack on one VPS. See docs/install.md.
#
#   bash deploy/setup.sh                # install (safe to re-run)
#   bash deploy/setup.sh refresh-cert   # weekly: let Stalwart load the renewed certificate
#
# Secrets are written to deploy/.env (mode 600) and never printed, except the
# one-time Kuraka setup token.
set +x
set -euo pipefail
umask 077

DEPLOY_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ENV_FILE=$DEPLOY_DIR/.env
NETWORK=kuraka_mail
SUBNET=172.30.0.0/24
CURL_IMAGE=curlimages/curl:latest
# uid/gid of the stalwart user inside stalwartlabs/stalwart:v1.
STALWART_GID=2000
# uid/gid of the nonroot user in the Kuraka image (see Dockerfile).
KURAKA_UID=65532
ISSUER_DIR=acme-v02.api.letsencrypt.org-directory
# Overridden to "local" below when CADDY_EXTRA_GLOBAL=local_certs (local testing).
USING='["urn:ietf:params:jmap:core","urn:stalwart:jmap"]'
WEBUI_URL=https://github.com/InkaNode/stalwart-webui/releases/latest/download/webui.zip
# Keep in sync with ServiceKeyDisabled in internal/stalwart/apppasswords.go.
SERVICE_KEY_DISABLED=(
  sysDomainDestroy
  sysNetworkListenerCreate sysNetworkListenerUpdate sysNetworkListenerDestroy
  sysRoleCreate sysRoleUpdate sysRoleDestroy
)

log() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

compose() {
  docker compose --project-directory "$DEPLOY_DIR" -f "$DEPLOY_DIR/docker-compose.yml" --env-file "$ENV_FILE" "$@"
}

# env_get KEY prints the value of KEY in .env, without surrounding quotes.
env_get() {
  local v
  v=$(K="$1" awk 'index($0, ENVIRON["K"] "=") == 1 { v = substr($0, length(ENVIRON["K"]) + 2) } END { print v }' "$ENV_FILE")
  v=${v%$'\r'}
  if [[ $v == \'*\' || $v == \"*\" ]]; then
    v=${v:1:${#v}-2}
  fi
  printf '%s' "$v"
}

# env_set KEY VALUE replaces or appends KEY in .env. Values are single-quoted
# so Docker Compose does not interpolate them.
env_set() {
  [[ $2 != *"'"* && $2 != *$'\n'* ]] || die "refusing to write $1: unsupported characters in value"
  local tmp
  tmp=$(mktemp "$ENV_FILE.XXXXXX")
  K="$1" L="$1='$2'" awk '
    index($0, ENVIRON["K"] "=") == 1 { if (!done) print ENVIRON["L"]; done = 1; next }
    { print }
    END { if (!done) print ENVIRON["L"] }' "$ENV_FILE" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$ENV_FILE"
}

# Stalwart's HTTP listener is not published, so JMAP calls run from a
# throwaway curl container on the mail network. Credentials and body go
# through a curl config on stdin, never on a command line.
curl_conf_escape() {
  local s=${1//\\/\\\\}
  printf '%s' "${s//\"/\\\"}"
}

# jmap USER:SECRET BODY prints the response. Fails on HTTP errors.
jmap() {
  printf 'url = "http://stalwart:8080/jmap/"\nuser = "%s"\nheader = "Content-Type: application/json"\ndata = "%s"\n' \
    "$(curl_conf_escape "$1")" "$(curl_conf_escape "$2")" |
    docker run --rm -i --network "$NETWORK" "$CURL_IMAGE" \
      --silent --show-error --fail-with-body --max-time 30 --config -
}

# jmap_error RESPONSE CALL_ID OBJECT_KEY prints the method or per-object error.
jmap_error() {
  jq -r --arg id "$2" --arg k "$3" '
    [.methodResponses[]? | select(.[2] == $id) |
      if .[0] == "error" then .[1].type
      else (.[1].notCreated[$k] // .[1].notUpdated[$k] // empty | "\(.type): \(.description // "")")
      end] | first // "unexpected response"' <<<"$1" 2>/dev/null || printf 'unreadable response'
}

wait_stalwart() {
  local deadline=$((SECONDS + 120)) code
  while (( SECONDS < deadline )); do
    code=$(docker run --rm --network "$NETWORK" "$CURL_IMAGE" --silent --output /dev/null \
      --write-out '%{http_code}' --max-time 2 http://stalwart:8080/jmap/session 2>/dev/null) || code=000
    case "$code" in
      200|401) return 0 ;;
    esac
    sleep 2
  done
  die "Stalwart did not answer on stalwart:8080 within 2 minutes; see: docker compose logs stalwart"
}

restart_stalwart() {
  compose restart stalwart >/dev/null
  wait_stalwart
}

cert_dir() { printf '/caddy/certificates/%s/%s' "$ISSUER_DIR" "$MAIL_HOST"; }

# Caddy writes keys as root with mode 0600 in 0700 directories. Give the
# stalwart group read access to the MAIL_HOST certificate only. Renewals
# rewrite the key, so refresh-cert runs this again.
fix_cert_perms() {
  compose exec -T caddy sh -eu -c '
    base=/data/caddy/certificates
    dir=$base/$1/$2
    for d in /data /data/caddy "$base" "$base/$1" "$dir"; do
      chgrp "$3" "$d"
      chmod g+rx "$d"
    done
    chgrp "$3" "$dir/$2.crt" "$dir/$2.key"
    chmod 0644 "$dir/$2.crt"
    chmod 0640 "$dir/$2.key"' sh "$ISSUER_DIR" "$MAIL_HOST" "$STALWART_GID"
}

load_env() {
  [[ -f $ENV_FILE ]] || die "deploy/.env not found; run: cp deploy/.env.example deploy/.env and edit it"
  chmod 600 "$ENV_FILE"
  PANEL_HOST=$(env_get PANEL_HOST)
  MAIL_HOST=$(env_get MAIL_HOST)
  MAIL_DOMAIN=$(env_get MAIL_DOMAIN)
  ACME_EMAIL=$(env_get ACME_EMAIL)
  if [[ $(env_get CADDY_EXTRA_GLOBAL) == *local_certs* ]]; then
    ISSUER_DIR=local
    warn "CADDY_EXTRA_GLOBAL=local_certs: using Caddy's internal CA (local testing only)"
  fi
  local name
  for name in PANEL_HOST MAIL_HOST MAIL_DOMAIN ACME_EMAIL; do
    [[ -n ${!name} ]] || die "$name is empty in deploy/.env"
    [[ ${!name} != *example.com ]] || die "$name still has the example value in deploy/.env"
  done
  [[ $MAIL_HOST =~ ^[A-Za-z0-9.-]+$ ]] || die "MAIL_HOST must be a plain hostname"
}

# Kuraka asks for Stalwart and Libredesk updates by writing files in
# deploy/updates; this timer applies them as root, so Kuraka never needs the
# Docker socket.
install_update_timer() {
  install -d -m 0750 -o "$KURAKA_UID" -g "$KURAKA_UID" "$DEPLOY_DIR/updates"
  if ! command -v systemctl >/dev/null; then
    warn "systemctl not found: Stalwart and Libredesk updates from Kuraka need $DEPLOY_DIR/apply-updates.sh run as root every minute"
    return 0
  fi
  chmod 700 "$DEPLOY_DIR/apply-updates.sh"
  cat >/etc/systemd/system/kuraka-updates.service <<EOF
[Unit]
Description=Apply updates requested in Kuraka

[Service]
Type=oneshot
Environment="KURAKA_DEPLOY=$DEPLOY_DIR"
ExecStart="$DEPLOY_DIR/apply-updates.sh"
EOF
  cat >/etc/systemd/system/kuraka-updates.timer <<'EOF'
[Unit]
Description=Apply updates requested in Kuraka every minute

[Timer]
OnBootSec=2min
OnUnitActiveSec=1min

[Install]
WantedBy=timers.target
EOF
  chmod 644 /etc/systemd/system/kuraka-updates.service /etc/systemd/system/kuraka-updates.timer
  systemctl daemon-reload
  systemctl enable --now kuraka-updates.timer >/dev/null 2>&1
  log "kuraka-updates.timer enabled"
}

refresh_cert() {
  load_env
  fix_cert_perms
  compose restart stalwart >/dev/null
  log "Stalwart restarted with the current certificate"
}

main() {
  local cmd
  for cmd in docker curl jq openssl; do
    command -v "$cmd" >/dev/null || die "$cmd is required"
  done
  docker compose version >/dev/null 2>&1 || die "the Docker Compose plugin is required"
  load_env

  # 1. Secrets.
  if [[ -z $(env_get KURAKA_SECRET_KEY) ]]; then
    env_set KURAKA_SECRET_KEY "$(openssl rand -hex 32)"
    log "generated KURAKA_SECRET_KEY"
  fi
  if [[ -z $(env_get STALWART_ADMIN) && -z $(env_get STALWART_RECOVERY_ADMIN) ]]; then
    env_set STALWART_RECOVERY_ADMIN "admin:$(openssl rand -hex 24)"
    log "generated STALWART_RECOVERY_ADMIN"
  fi

  # 2. Caddy and Stalwart; wait for the MAIL_HOST certificate.
  log "starting caddy and stalwart"
  compose up -d caddy stalwart >/dev/null
  local dir deadline=$((SECONDS + 300))
  dir=/data$(cert_dir)
  log "waiting for the certificate of $MAIL_HOST (up to 5 minutes)"
  until compose exec -T caddy test -f "$dir/$MAIL_HOST.crt" -a -f "$dir/$MAIL_HOST.key" 2>/dev/null; do
    if (( SECONDS >= deadline )); then
      die "no certificate for $MAIL_HOST after 5 minutes. Check that the DNS A records of $MAIL_HOST and $PANEL_HOST point to this server (Cloudflare proxy OFF for $MAIL_HOST), that ports 80 and 443 are open, and: docker compose logs caddy"
    fi
    sleep 5
  done
  fix_cert_perms
  wait_stalwart

  # 3. Stalwart bootstrap, certificate and service API key.
  local body resp user secret admin err
  admin=$(env_get STALWART_ADMIN)
  if [[ -z $admin ]]; then
    log "bootstrapping Stalwart"
    body=$(jq -nc --argjson using "$USING" --arg h "$MAIL_HOST" --arg d "$MAIL_DOMAIN" \
      '{using: $using, methodCalls: [["x:Bootstrap/set", {update: {singleton: {defaultHostname: $h, defaultDomain: $d, requestTlsCertificate: false, generateDkimKeys: true}}}, "s"]]}')
    resp=$(jmap "$(env_get STALWART_RECOVERY_ADMIN)" "$body") ||
      die "bootstrap request failed ($(jmap_error "$resp" s singleton)); check STALWART_RECOVERY_ADMIN and docker compose logs stalwart"
    # Never print the response: updated.singleton holds the admin secret.
    if user=$(jq -er '.methodResponses[] | select(.[0] == "x:Bootstrap/set" and .[2] == "s") | .[1].updated.singleton.username | select(type == "string" and length > 0)' <<<"$resp") &&
      secret=$(jq -er '.methodResponses[] | select(.[0] == "x:Bootstrap/set" and .[2] == "s") | .[1].updated.singleton.secret | select(type == "string" and length > 0)' <<<"$resp"); then
      admin="$user:$secret"
      env_set STALWART_ADMIN "$admin"
      unset resp secret
      log "bootstrap done; admin stored as STALWART_ADMIN in deploy/.env (rotate it after setup)"
      restart_stalwart
    else
      err=$(jmap_error "$resp" s singleton)
      unset resp
      log "Stalwart is already bootstrapped ($err); skipping"
    fi
  else
    log "Stalwart already bootstrapped; skipping"
  fi

  local cert_created=0
  if [[ -z $(env_get STALWART_CERTIFICATE_ID) ]]; then
    [[ -n $admin ]] || die "STALWART_ADMIN is missing from deploy/.env; set it to user:secret of a Stalwart admin and re-run"
    log "registering the $MAIL_HOST certificate in Stalwart"
    # Shape from the Stalwart docs; the "File" variant is not yet verified
    # against a live server. Check on the first run with:
    #   openssl s_client -connect MAIL_HOST:993 -servername MAIL_HOST </dev/null
    body=$(jq -nc --argjson using "$USING" --arg crt "/caddy-data$(cert_dir)/$MAIL_HOST.crt" --arg key "/caddy-data$(cert_dir)/$MAIL_HOST.key" \
      '{using: $using, methodCalls: [["x:Certificate/set", {create: {c: {certificate: {"@type": "File", filePath: $crt}, privateKey: {"@type": "File", filePath: $key}}}}, "c"]]}')
    local id
    if resp=$(jmap "$admin" "$body") &&
      id=$(jq -er '.methodResponses[] | select(.[0] == "x:Certificate/set" and .[2] == "c") | .[1].created.c.id | select(type == "string" and length > 0)' <<<"$resp"); then
      env_set STALWART_CERTIFICATE_ID "$id"
      cert_created=1
      log "certificate registered"
    else
      die "could not register the certificate ($(jmap_error "${resp:-}" c c)); see docs/install.md and re-run"
    fi
  fi

  if [[ -z $(env_get KURAKA_STALWART_API_KEY) ]]; then
    [[ -n $admin ]] || die "STALWART_ADMIN is missing from deploy/.env; set it to user:secret of a Stalwart admin and re-run"
    log "creating the Kuraka API key in Stalwart"
    body=$(jq -nc --argjson using "$USING" --arg subnet "$SUBNET" '
      ($ARGS.positional | map({(.): true}) | add) as $disabled |
      {using: $using, methodCalls: [["x:ApiKey/set", {create: {k: {
        description: "kuraka",
        permissions: {"@type": "Disable", permissions: $disabled},
        allowedIps: {($subnet): true}}}}, "k"]]}' --args "${SERVICE_KEY_DISABLED[@]}")
    resp=$(jmap "$admin" "$body") || die "API key request failed ($(jmap_error "$resp" k k))"
    secret=$(jq -er '.methodResponses[] | select(.[0] == "x:ApiKey/set" and .[2] == "k") | .[1].created.k.secret | select(type == "string" and length > 0)' <<<"$resp") ||
      die "API key was not created ($(jmap_error "$resp" k k))"
    env_set KURAKA_STALWART_API_KEY "$secret"
    unset resp secret
    log "API key stored as KURAKA_STALWART_API_KEY in deploy/.env"
  fi

  # Web interface from the InkaNode fork: it hides the views Kuraka manages.
  local webui_set=0 app
  if [[ -z $admin ]]; then
    log "STALWART_ADMIN is not set; skipping the web interface URL"
  else
    body=$(jq -nc --argjson using "$USING" '{using: $using, methodCalls: [["x:Application/get", {}, "g"]]}')
    resp=$(jmap "$admin" "$body") || die "could not read Stalwart applications ($(jmap_error "$resp" g g))"
    app=$(jq -r --arg url "$WEBUI_URL" '.methodResponses[] | select(.[0] == "x:Application/get") | .[1].list[] | select(.urlPrefix["/admin"] and .resourceUrl != $url) | .id' <<<"$resp" | head -n1)
    if [[ -n $app ]]; then
      body=$(jq -nc --argjson using "$USING" --arg id "$app" --arg url "$WEBUI_URL" '{using: $using, methodCalls: [["x:Application/set", {update: {($id): {resourceUrl: $url}}}, "u"]]}')
      if resp=$(jmap "$admin" "$body") &&
        jq -e --arg id "$app" '.methodResponses[] | select(.[0] == "x:Application/set" and .[2] == "u") | .[1].updated | has($id)' <<<"$resp" >/dev/null; then
        webui_set=1
        log "Stalwart web interface set to $WEBUI_URL"
      else
        die "could not set the web interface URL ($(jmap_error "${resp:-}" u "$app"))"
      fi
    fi
  fi
  local metrics_set=0
  if [[ -n $admin && -z $(env_get KURAKA_STALWART_METRICS_SECRET) ]]; then
    secret=$(openssl rand -hex 24)
    body=$(printf '%s' "$secret" | jq -Rsc --argjson using "$USING" '{using: $using, methodCalls: [["x:Metrics/set", {update: {singleton: {prometheus: {"@type": "Enabled", authUsername: "kuraka-metrics", authSecret: {"@type": "Value", secret: .}}}}}, "m"]]}')
    if resp=$(jmap "$admin" "$body") &&
      jq -e '.methodResponses[] | select(.[0] == "x:Metrics/set" and .[2] == "m") | .[1].updated | has("singleton")' <<<"$resp" >/dev/null; then
      env_set KURAKA_STALWART_METRICS_SECRET "$secret"
      metrics_set=1
      log "Stalwart Prometheus metrics enabled"
    else
      die "could not enable metrics ($(jmap_error "${resp:-}" m singleton))"
    fi
    unset secret body resp
  fi
  unset admin
  # Certificates, the web interface and metrics need a Stalwart restart.
  if (( cert_created || webui_set || metrics_set )); then
    restart_stalwart
  fi

  # 4. Kuraka. The updates directory must exist before Docker mounts it.
  install_update_timer
  log "starting kuraka"
  compose up -d kuraka >/dev/null
  local token="" since
  # Only the running container's logs: older starts printed stale tokens.
  since=$(docker inspect -f '{{.State.StartedAt}}' "$(compose ps -q kuraka)")
  deadline=$((SECONDS + 60))
  while [[ -z $token ]] && (( SECONDS < deadline )); do
    sleep 2
    token=$(compose logs --no-log-prefix --since "$since" kuraka 2>/dev/null |
      jq -Rr 'fromjson? | select(.msg == "setup required") | .token // empty' | tail -n 1)
  done

  printf '\n'
  if [[ -n $token ]]; then
    printf 'Kuraka setup token (valid until the first superadmin exists): %s\n' "$token"
  else
    printf 'No setup token in the Kuraka log (a superadmin may already exist). Check: docker compose logs kuraka\n'
  fi
  cat <<EOF

Next steps:
  1. Open https://$PANEL_HOST/#/setup and create the superadmin with the token above.
  2. At your VPS provider, set the PTR (reverse DNS) record of this server's IP to $MAIL_HOST.
  3. In Kuraka, add $MAIL_DOMAIN to a tenant and publish its DNS records (Cloudflare or manually).
  4. Rotate STALWART_ADMIN, then remove STALWART_RECOVERY_ADMIN from deploy/.env
     and run: docker compose -f deploy/docker-compose.yml up -d stalwart
  5. Let Stalwart pick up renewed certificates weekly, e.g. /etc/cron.d/kuraka:
     17 4 * * 1 root bash $DEPLOY_DIR/setup.sh refresh-cert >/dev/null
EOF
}

case "${1:-}" in
  "") main ;;
  refresh-cert) refresh_cert ;;
  *) die "usage: bash deploy/setup.sh [refresh-cert]" ;;
esac
