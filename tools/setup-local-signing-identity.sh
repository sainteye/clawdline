#!/bin/bash
# Creates the one stable identity used by local Clawdline builds. It never deletes, replaces, or
# changes trust for another certificate.
set -euo pipefail

IDENTITY_NAME="Clawdline Local Development"
KEYCHAIN="${CLAWDLINE_LOCAL_SIGN_KEYCHAIN:-$HOME/Library/Keychains/login.keychain-db}"

identity_hash() {
  security find-identity -p codesigning "$KEYCHAIN" 2>/dev/null \
    | awk -v name="$IDENTITY_NAME" 'index($0, "\"" name "\"") { print $2; exit }'
}

if [ -n "$(identity_hash)" ]; then
  echo "✓ local signing identity already exists: $IDENTITY_NAME"
  echo "  The first launch after switching identities still asks once; choose Always Allow so rebuilds stop asking."
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

hash=$(identity_hash)
[ -n "$hash" ] || {
  echo "!! Keychain import completed but no code-signing identity was found" >&2
  exit 1
}

echo "✓ created local signing identity: $IDENTITY_NAME ($hash)"
echo "  The first launch after switching identities still asks once; choose Always Allow so rebuilds stop asking."
