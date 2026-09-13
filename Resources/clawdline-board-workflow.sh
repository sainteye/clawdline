#!/bin/sh
# clawdline-board-workflow protocol 1
# Resolve an exact live assistant identity, then submit one semantic workflow boundary. The
# credential is read only here and is never put in assistant context, argv, JSON or output.
set -eu
umask 077

usage() {
    echo "usage: clawdline-board-workflow <conversation-id> <idempotency-key> < command.json" >&2
    echo "Read the installed JSON contract first: clawdline-board-workflow --help" >&2
    exit 64
}

if [ "${1:-}" = "--help" ] && [ "$#" -eq 1 ]; then
    here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
    [ -f "$here/board-workflow.md" ] || {
        echo "clawdline-board-workflow: workflow_guide_unavailable" >&2; exit 69;
    }
    command cat "$here/board-workflow.md"
    exit 0
fi

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
port=${CLAWDLINE_PORT:-7717}
case "$port" in *[!0-9]*|'') echo "clawdline-board-workflow: port_invalid" >&2; exit 64;; esac

body=$(mktemp "${TMPDIR:-/tmp}/clawdline-board-workflow.XXXXXX")
identity=$(mktemp "${TMPDIR:-/tmp}/clawdline-board-identity.XXXXXX")
header=$(mktemp "${TMPDIR:-/tmp}/clawdline-board-header.XXXXXX")
credential=$(mktemp "${TMPDIR:-/tmp}/clawdline-board-credential.XXXXXX")
trap 'rm -f "$body" "$identity" "$header" "$credential"' EXIT HUP INT TERM
# Snapshot at most one byte beyond the largest supported encoding. Validate those exact
# bytes, not one matching line from a file that will later be read again into an HTTP header.
# RemoteAuth.newToken emits 32 random bytes as unpadded base64url (43 characters). Retain
# compatibility with the 64-character lowercase hex encoding accepted by protocol 1.
dd if="$token_file" of="$credential" bs=65 count=1 2>/dev/null \
    || { echo "clawdline-board-workflow: machine_credential_unavailable" >&2; exit 77; }
credential_size=$(wc -c < "$credential" | tr -d ' ')
invalid_bytes=$(LC_ALL=C tr -d 'A-Za-z0-9_-' < "$credential" | wc -c | tr -d ' ')
[ "$invalid_bytes" = 0 ] && {
    { [ "$credential_size" = 43 ] && LC_ALL=C grep -Eq '^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$' "$credential"; } \
    || { [ "$credential_size" = 64 ] && LC_ALL=C grep -Eq '^[a-f0-9]{64}$' "$credential"; }
} || { echo "clawdline-board-workflow: machine_credential_unavailable" >&2; exit 77; }
{ printf 'X-Clawdline-Orchestrator: '; command cat "$credential"; printf '\n'; } > "$header"
chmod 600 "$header"
command cat > "$body"
[ "$(wc -c < "$body" | tr -d ' ')" -le 65536 ] \
    || { echo "clawdline-board-workflow: command_too_large" >&2; exit 65; }
connect_timeout=2
request_timeout=15

curl --fail-with-body -sS --connect-timeout "$connect_timeout" --max-time "$request_timeout" \
    -G "http://127.0.0.1:$port/v1/orchestrator/whoami" \
    -H "@$header" \
    --data-urlencode "conversation_id=$conversation_id" > "$identity"
terminal_id=$(/usr/bin/plutil -extract terminal_id raw -o - "$identity" 2>/dev/null || true)
[ -n "$terminal_id" ] \
    || { echo "clawdline-board-workflow: workflow_identity_unavailable" >&2; exit 69; }
printf '%s\n' "$terminal_id" | grep -Eq '^%?[A-Za-z0-9._~-]+$' \
    || { echo "clawdline-board-workflow: terminal_id_malformed" >&2; exit 69; }
case "$terminal_id" in
    %*) terminal_segment="%25${terminal_id#%}" ;;
    *) terminal_segment=$terminal_id ;;
esac

curl --fail-with-body -sS --connect-timeout "$connect_timeout" --max-time "$request_timeout" \
    -X POST \
    "http://127.0.0.1:$port/v1/orchestrator/sessions/$terminal_segment/workflow" \
    -H "@$header" \
    -H "Idempotency-Key: $request_id" \
    -H 'Content-Type: application/json' \
    --data-binary "@$body"
