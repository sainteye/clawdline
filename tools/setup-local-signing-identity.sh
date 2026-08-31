#!/bin/bash
# Creates the one stable identity used by local Clawdline builds. It never deletes, replaces, or
# changes trust for another certificate.
#
# **It never learns your Keychain password.** Apple's SecurityTool contract requires `-k password`
# for `set-key-partition-list`; omitting it is not a documented prompt path. This script therefore
# never runs that command and refuses the legacy option rather than turning a fake's success into
# a claim about real macOS. See docs/cloud.md.
set -euo pipefail

IDENTITY_NAME="Clawdline Local Development"
KEYCHAIN="${CLAWDLINE_LOCAL_SIGN_KEYCHAIN:-$HOME/Library/Keychains/login.keychain-db}"
# Every command here can reach a dialog that waits for a person. Bounded, so an unattended run
# ends with a sentence instead of a process nobody notices is still there.
SETUP_TIMEOUT="${CLAWDLINE_SIGN_QUERY_TIMEOUT:-30}"
PARTITION_TIMEOUT="${CLAWDLINE_PARTITION_LIST_TIMEOUT:-120}"

# BEGIN keychain-rebuild-focused: setup timeout validation
require_positive_integer() {
  local name=$1 value=$2
  case "$value" in
    ''|*[!0-9]*|0)
      echo "!! $name must be a positive integer, got: $value" >&2
      return 2
      ;;
  esac
}
require_positive_integer CLAWDLINE_SIGN_QUERY_TIMEOUT "$SETUP_TIMEOUT" || exit $?
require_positive_integer CLAWDLINE_PARTITION_LIST_TIMEOUT "$PARTITION_TIMEOUT" || exit $?
# END keychain-rebuild-focused: setup timeout validation

for argument in "$@"; do
  case "$argument" in
    --set-partition-list)
      echo "!! --set-partition-list is unsupported and no Keychain command was run" >&2
      echo "   /usr/bin/security requires -k password; Clawdline will not accept or pass it." >&2
      echo "   Configure key access yourself in Keychain Access or invoke SecurityTool manually." >&2
      exit 2
      ;;
    *)
      echo "!! unknown argument: $argument" >&2
      echo "   usage: $0" >&2
      exit 2
      ;;
  esac
done

# BEGIN keychain-rebuild-focused: bounded setup commands
# The same watchdog build.sh uses, and for the same reason: macOS ships no timeout(1), and
# /bin/bash here is 3.2, so `wait -n` does not exist. The marker file is what separates a
# timeout from an ordinary non-zero exit; a signal number cannot.
CLAWDLINE_BOUNDED_OUTCOME=not_run
clawdline_bounded() {
  local seconds=$1 outfile=$2
  shift 2
  local marker="$outfile.timed-out"
  rm -f "$marker"
  "$@" >"$outfile" 2>&1 &
  local pid=$!
  (
    sleep "$seconds"
    kill -TERM "$pid" 2>/dev/null && : > "$marker"
    sleep 2
    kill -KILL "$pid" 2>/dev/null
  ) >/dev/null 2>&1 &
  local watchdog=$!
  local status=0
  wait "$pid" 2>/dev/null || status=$?
  kill -TERM "$watchdog" 2>/dev/null || true
  wait "$watchdog" 2>/dev/null || true
  if [ -e "$marker" ]; then
    rm -f "$marker"
    CLAWDLINE_BOUNDED_OUTCOME=timeout
    return 124
  fi
  CLAWDLINE_BOUNDED_OUTCOME=exit
  return "$status"
}
# END keychain-rebuild-focused: bounded setup commands

probe_out=$(mktemp "${TMPDIR:-/tmp}/clawdline-setup-probe.XXXXXX")
cleanup_probe() { rm -f "$probe_out" "$probe_out.timed-out"; }
trap cleanup_probe EXIT

identity_hashes() {
  local status=0
  clawdline_bounded "$SETUP_TIMEOUT" "$probe_out" \
    security find-identity -v -p codesigning "$KEYCHAIN" || status=$?
  if [ "$status" -ne 0 ]; then
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      echo "!! inspecting $KEYCHAIN did not answer within ${SETUP_TIMEOUT}s; it may be locked" >&2
      echo "   Unlock it yourself:  security unlock-keychain $KEYCHAIN" >&2
    else
      echo "!! could not inspect valid code-signing identities in $KEYCHAIN" >&2
    fi
    return 1
  fi
  awk -v name="$IDENTITY_NAME" 'index($0, "\"" name "\"") { print $2 }' "$probe_out"
}

# BEGIN keychain-rebuild-focused: partition list contract
report_partition_list_contract() {
  echo "  codesign may still ask for Keychain access on each rebuild. Apple's SecurityTool"
  echo "  requires '-k password' to change the partition list. Clawdline does not accept or"
  echo "  pass that password; configure key access yourself in Keychain Access or invoke"
  echo "  SecurityTool manually after reviewing '/usr/bin/security help set-key-partition-list'."
}
# END keychain-rebuild-focused: partition list contract

if ! hashes=$(identity_hashes); then exit 1; fi
count=$(printf '%s\n' "$hashes" | awk 'NF { count++ } END { print count + 0 }')
if [ "$count" -gt 1 ]; then
  echo "!! multiple valid code-signing identities are named $IDENTITY_NAME" >&2
  printf '   %s\n' $hashes >&2
  echo "   Nothing was changed; remove or rename the extra identity and run this again." >&2
  exit 1
fi
if [ "$count" -eq 1 ]; then
  echo "✓ local signing identity already exists: $IDENTITY_NAME"
  report_partition_list_contract
  echo "  After changing signing identity, first use may show up to three Keychain prompts (machine credential and two Cloud keys); approve each item you use."
  exit 0
fi

command -v openssl >/dev/null 2>&1 || {
  echo "!! openssl is required to create the local signing identity" >&2
  exit 1
}
[ -e "$KEYCHAIN" ] || {
  echo "!! Keychain does not exist: $KEYCHAIN" >&2
  exit 1
}

work=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-local-signing.XXXXXX")
# Replacing the EXIT trap rather than adding to it, so the probe file it was holding has to be
# named here too or it survives the run.
cleanup() { rm -rf "$work"; cleanup_probe; }
trap cleanup EXIT
password=$(/usr/bin/uuidgen)

openssl req -new -newkey rsa:2048 -x509 -sha256 -days 3650 -nodes \
  -subj "/CN=$IDENTITY_NAME/O=Tsunami Works/OU=Local Development" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=codeSigning" \
  -keyout "$work/key.pem" -out "$work/certificate.pem" >/dev/null 2>&1
openssl pkcs12 -export -legacy -name "$IDENTITY_NAME" \
  -inkey "$work/key.pem" -in "$work/certificate.pem" \
  -out "$work/identity.p12" -passout "pass:$password" >/dev/null 2>&1
import_status=0
clawdline_bounded "$PARTITION_TIMEOUT" "$probe_out" \
  security import "$work/identity.p12" -k "$KEYCHAIN" -P "$password" \
  -T /usr/bin/codesign || import_status=$?
if [ "$import_status" -ne 0 ]; then
  cat "$probe_out" >&2
  if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
    echo "!! importing into $KEYCHAIN did not finish within ${PARTITION_TIMEOUT}s" >&2
    echo "   Import state is unknown: the identity may have landed before the watchdog stopped it." >&2
    echo "   Re-run identity discovery before retrying; do not assume nothing was imported." >&2
  else
    echo "!! could not import the generated identity into $KEYCHAIN (exit $import_status)" >&2
  fi
  exit 1
fi
# Trust only the certificate this invocation just generated, and only for code signing. An
# expired or otherwise invalid namesake is deliberately left untouched above.
echo "→ macOS may ask once to trust this newly generated certificate for code signing"
trust_status=0
clawdline_bounded "$PARTITION_TIMEOUT" "$probe_out" \
  security add-trusted-cert -r trustRoot -p codeSign -k "$KEYCHAIN" \
  "$work/certificate.pem" || trust_status=$?
if [ "$trust_status" -ne 0 ]; then
  cat "$probe_out" >&2
  if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
    echo "!! the trust dialog was not answered within ${PARTITION_TIMEOUT}s" >&2
    echo "   Trust state is unknown; inspect the certificate before retrying." >&2
  else
    echo "!! could not mark the generated certificate trusted (exit $trust_status)" >&2
  fi
  echo "   The identity was imported but is not yet valid for code signing." >&2
  exit 1
fi

if ! hashes=$(identity_hashes); then exit 1; fi
count=$(printf '%s\n' "$hashes" | awk 'NF { count++ } END { print count + 0 }')
[ "$count" -eq 1 ] || {
  echo "!! Keychain import completed but found $count valid identities named $IDENTITY_NAME" >&2
  [ -z "$hashes" ] || printf '   %s\n' $hashes >&2
  exit 1
}
hash=$(printf '%s\n' "$hashes" | awk 'NF { print; exit }')

echo "✓ created local signing identity: $IDENTITY_NAME ($hash)"
report_partition_list_contract
echo "  After changing signing identity, first use may show up to three Keychain prompts (machine credential and two Cloud keys); approve each item you use."
