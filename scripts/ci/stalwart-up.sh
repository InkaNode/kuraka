#!/usr/bin/env bash
# Source this script locally to retain the API key without printing it, or set
# GITHUB_ENV to an environment file that will receive all three variables.
set +x
set -euo pipefail

if [[ $# -ne 1 || ! $1 =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ ]]; then
  printf 'Usage: bash scripts/ci/stalwart-up.sh <tag>\n' >&2
  exit 1
fi

# A dedicated name and overridable ports so a local run never touches another Stalwart.
container=${KURAKA_IT_CONTAINER:-kuraka-it-stalwart}
http_port=${KURAKA_IT_HTTP_PORT:-8080}
smtp_port=${KURAKA_IT_SMTP_PORT:-2525}
base_url=http://127.0.0.1:${http_port}
if docker container inspect "$container" >/dev/null 2>&1; then
  printf 'Container %s already exists; remove it first.\n' "$container" >&2
  exit 1
fi
recovery_password=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
docker run -d --name "$container" \
  -e "STALWART_RECOVERY_ADMIN=admin:${recovery_password}" \
  -p "127.0.0.1:${http_port}:8080" \
  -p "127.0.0.1:${smtp_port}:25" \
  "stalwartlabs/stalwart:$1" >/dev/null

wait_for_stalwart() {
  local deadline=$((SECONDS + 60)) status
  while (( SECONDS < deadline )); do
    status=$(curl --silent --output /dev/null --write-out '%{http_code}' \
      --max-time 1 "$base_url/jmap/session") || status=000
    case "$status" in
      200|401) return 0 ;;
    esac
    sleep 1
  done
  printf 'Stalwart did not become ready within 60 seconds.\n' >&2
  return 1
}

wait_for_stalwart
bootstrap=$(curl --fail --silent --show-error --max-time 30 \
  --user "admin:$recovery_password" \
  --header 'Content-Type: application/json' \
  --data '{"using":["urn:ietf:params:jmap:core","urn:stalwart:jmap"],"methodCalls":[["x:Bootstrap/set",{"update":{"singleton":{"defaultHostname":"mail.ci.test","defaultDomain":"ci.test","requestTlsCertificate":false}}},"s"]]}' \
  "$base_url/jmap/")

# Never print the response: updated.singleton contains the real admin secret.
admin=$(jq -er '.methodResponses[] | select(.[0] == "x:Bootstrap/set" and .[2] == "s") | .[1].updated.singleton.username | select(type == "string" and length > 0)' <<<"$bootstrap")
admin_secret=$(jq -er '.methodResponses[] | select(.[0] == "x:Bootstrap/set" and .[2] == "s") | .[1].updated.singleton.secret | select(type == "string" and length > 0)' <<<"$bootstrap")
unset bootstrap recovery_password

docker restart "$container" >/dev/null
wait_for_stalwart
key_response=$(curl --fail --silent --show-error --max-time 30 \
  --user "$admin:$admin_secret" \
  --header 'Content-Type: application/json' \
  --data '{"using":["urn:ietf:params:jmap:core","urn:stalwart:jmap"],"methodCalls":[["x:ApiKey/set",{"create":{"k":{"description":"ci"}}},"k"]]}' \
  "$base_url/jmap/")
api_key=$(jq -er '.methodResponses[] | select(.[0] == "x:ApiKey/set" and .[2] == "k") | .[1].created.k.secret | select(type == "string" and length > 0)' <<<"$key_response")
unset key_response admin admin_secret

export KURAKA_IT_STALWART_URL="$base_url"
export KURAKA_IT_STALWART_API_KEY="$api_key"
export KURAKA_IT_SMTP_ADDR=127.0.0.1:${smtp_port}
unset api_key

if [[ -n ${GITHUB_ENV:-} ]]; then
  {
    printf 'KURAKA_IT_STALWART_URL=%s\n' "$KURAKA_IT_STALWART_URL"
    printf 'KURAKA_IT_STALWART_API_KEY=%s\n' "$KURAKA_IT_STALWART_API_KEY"
    printf 'KURAKA_IT_SMTP_ADDR=%s\n' "$KURAKA_IT_SMTP_ADDR"
  } >>"$GITHUB_ENV"
else
  printf 'export KURAKA_IT_STALWART_URL=%q\n' "$KURAKA_IT_STALWART_URL"
  printf 'export KURAKA_IT_STALWART_API_KEY\n'
  printf 'export KURAKA_IT_SMTP_ADDR=%q\n' "$KURAKA_IT_SMTP_ADDR"
  printf 'API key withheld from stdout; source this script or set GITHUB_ENV to retain it.\n' >&2
fi
