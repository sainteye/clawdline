package release

import (
	"crypto/ed25519"
	"encoding/base64"
)

// releaseKeys are the public halves of the keys that sign releases, base64.
// A rotation adds the new key here, ships a release signed by both, and only
// a later release drops the old one (docs/releasing.md). The private halves
// are never in this repository.
var releaseKeys = []string{
	// b8d959431f1ad836, created 2026-10-06; the seed is the release
	// environment's CLAWDLINE_RELEASE_KEY.
	"2Du3yZ4jZF8zoMcZ2ANHFNbxZuz0wgAZdR0nZJ/g/O0=",
}

// extraKey is one more trusted key stamped in with
// `-ldflags "-X github.com/sainteye/clawdline/internal/adapters/release.extraKey=<base64>"`
// for a test release. tools/release/build.sh refuses to set it.
var extraKey string

// TrustedKeys is every key a release may be signed by, for this binary.
func TrustedKeys() []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, s := range append(append([]string{}, releaseKeys...), extraKey) {
		if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) == ed25519.PublicKeySize {
			out = append(out, ed25519.PublicKey(raw))
		}
	}
	return out
}
