// Package cloud is the wire foundation for the hosted console at
// app.clawdline.com: canonical JSON, the ten-field relay envelope, the two key
// roles, signing and sealing, and the clock guard that decides whether this
// machine's time may be trusted at all.
//
// It is deliberately the bottom of the Cloud stack and nothing else. There is
// no socket here, no pairing, no command vocabulary and no push: those are
// later waves. What is here is the part where one wrong byte is invisible —
// a signature that never verifies, a ciphertext nobody can open — so every
// unit in this package is checked against the published protocol vectors in
// testdata/protocol-vectors.json rather than against its own idea of itself.
//
// The normative reading is docs/cloud-wire.md, which cites the relay, the
// contract package and the Swift app line by line. Two rules from it are worth
// repeating where the code lives:
//
//   - The envelope signature covers the pipe-joined UTF-8 text
//     v|ch|seq|ts|class|key_id|nonce|ct, using the base64 *spelling* of nonce
//     and ct rather than their bytes. Re-encoding either one, even to an
//     equivalent spelling, changes what was signed.
//   - Canonical JSON here is the RFC 8785 safe-integer subset. A JSON number
//     with a fraction or an exponent, and an integer past 2^53-1, are refused
//     rather than approximated, because the browser end cannot reproduce the
//     bytes Go would print for them.
//
// Nothing in this package reads or writes a file. Key storage is the KeyStore
// seam, implemented today by internal/adapters/cloudkeys as owner-only files
// and later by each platform's own keystore.
package cloud
