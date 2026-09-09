#!/bin/sh
# clawdline-board-workflow protocol 1
# Resolve an exact live assistant identity, then submit one semantic workflow boundary. The
# credential is read only here and is never put in assistant context, argv, JSON or output.
set -eu
umask 077

usage() {
    echo "usage: clawdline-board-workflow <conversation-id> <idempotency-key> < command.json" >&2
    exit 64
}

if [ "${1:-}" = "--version" ] && [ "$#" -eq 1 ]; then
    echo "clawdline-board-workflow 1"
    exit 0
fi
[ "$#" -eq 2 ] || usage
conversation_id=$1
request_id=$2
printf '%s\n' "$conversation_id" | grep -Eq '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' \
    || { echo "clawdline-board-workflow: conversation_id_malformed" >&2; exit 64; }
[ -n "$request_id" ] && [ "${#request_id}" -le 200 ] \
    || { echo "clawdline-board-workflow: idempotency_key_invalid" >&2; exit 64; }

token_file=${CLAWDLINE_ORCHESTRATOR_TOKEN_FILE:-"$HOME/.config/clawdline/orchestrator-token"}
[ -f "$token_file" ] || { echo "clawdline-board-workflow: machine_credential_unavailable" >&2; exit 77; }
grep -Eq '^[a-f0-9]{64}$' "$token_file" \
    || { echo "clawdline-board-workflow: machine_credential_unavailable" >&2; exit 77; }
port=${CLAWDLINE_PORT:-7717}
case "$port" in *[!0-9]*|'') echo "clawdline-board-workflow: port_invalid" >&2; exit 64;; esac

body=$(mktemp "${TMPDIR:-/tmp}/clawdline-board-workflow.XXXXXX")
identity=$(mktemp "${TMPDIR:-/tmp}/clawdline-board-identity.XXXXXX")
header=$(mktemp "${TMPDIR:-/tmp}/clawdline-board-header.XXXXXX")
trap 'rm -f "$body" "$identity" "$header"' EXIT HUP INT TERM
{ printf 'X-Clawdline-Orchestrator: '; command cat "$token_file"; printf '\n'; } > "$header"
chmod 600 "$header"
command cat > "$body"
[ "$(wc -c < "$body" | tr -d ' ')" -le 65536 ] \
    || { echo "clawdline-board-workflow: command_too_large" >&2; exit 65; }

curl --fail-with-body -sS -G "http://127.0.0.1:$port/v1/orchestrator/whoami" \
    -H "@$header" \
    --data-urlencode "conversation_id=$conversation_id" > "$identity"
terminal_id=$(/usr/bin/plutil -extract terminal_id raw -o - "$identity" 2>/dev/null || true)
[ -n "$terminal_id" ] \
    || { echo "clawdline-board-workflow: workflow_identity_unavailable" >&2; exit 69; }
case "$terminal_id" in
    %*) terminal_segment="%25${terminal_id#%}" ;;
    *) terminal_segment=$terminal_id ;;
esac
printf '%s\n' "$terminal_segment" | grep -Eq '^[A-Za-z0-9._~-]+$' \
    || { echo "clawdline-board-workflow: terminal_id_malformed" >&2; exit 69; }

curl --fail-with-body -sS -X POST \
    "http://127.0.0.1:$port/v1/orchestrator/sessions/$terminal_segment/workflow" \
    -H "@$header" \
    -H "Idempotency-Key: $request_id" \
    -H 'Content-Type: application/json' \
    --data-binary "@$body"
