#!/bin/bash
# Creates the one stable identity used by local Clawdline builds. It never deletes, replaces, or
# changes trust for another certificate.
set -euo pipefail

IDENTITY_NAME="Clawdline Local Development"
KEYCHAIN="${CLAWDLINE_LOCAL_SIGN_KEYCHAIN:-$HOME/Library/Keychains/login.keychain-db}"

identity_hashes() {
  local output
  if ! output=$(security find-identity -v -p codesigning "$KEYCHAIN" 2>&1); then
    echo "!! could not inspect valid code-signing identities in $KEYCHAIN" >&2
    return 1
  fi
  printf '%s\n' "$output" \
    | awk -v name="$IDENTITY_NAME" 'index($0, "\"" name "\"") { print $2 }'
}

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
cleanup() { rm -rf "$work"; }
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
security import "$work/identity.p12" -k "$KEYCHAIN" -P "$password" \
  -T /usr/bin/codesign >/dev/null
# Trust only the certificate this invocation just generated, and only for code signing. An
# expired or otherwise invalid namesake is deliberately left untouched above.
echo "→ macOS may ask once to trust this newly generated certificate for code signing"
security add-trusted-cert -r trustRoot -p codeSign -k "$KEYCHAIN" \
  "$work/certificate.pem" >/dev/null

if ! hashes=$(identity_hashes); then exit 1; fi
count=$(printf '%s\n' "$hashes" | awk 'NF { count++ } END { print count + 0 }')
[ "$count" -eq 1 ] || {
  echo "!! Keychain import completed but found $count valid identities named $IDENTITY_NAME" >&2
  [ -z "$hashes" ] || printf '   %s\n' $hashes >&2
  exit 1
}
hash=$(printf '%s\n' "$hashes" | awk 'NF { print; exit }')

echo "✓ created local signing identity: $IDENTITY_NAME ($hash)"
echo "  After changing signing identity, first use may show up to three Keychain prompts (machine credential and two Cloud keys); approve each item you use."
