# `protocol-vectors.json` — where it came from

A byte-for-byte copy of the Swift app's published protocol vectors:

| | |
|---|---|
| source | `~/code/clawdline/Tests/protocol-vectors.json` |
| SHA-256 | `ca354b68c37feb1df6d26ac9cadd218e5569f65cd7f52bb2ee7aaf34a13fe5a9` |
| bytes | 186,557 |
| copied | 2026-09-18, from the checkout at commit `85cf6003c7a4356058ec1fe1901d8d396533c73c` |
| produced by | `~/code/clawdline/tools/generate-protocol-vectors.swift` |
| mirrored as | `~/code/clawdline-cloud/contracts/cloud/v1/vectors.json` (same object, different formatting and therefore a different digest) |

**Every key and nonce in it is TEST-ONLY, on purpose.** Fixed AES-GCM nonces
are what makes a cross-runtime fixture reproducible; reusing one under a real
master secret destroys AEAD security outright. Nothing here is a production
secret and nothing here may become one.

The file is the oracle for `envelope_test.go`, `keys_test.go` and
`canonicaljson_test.go`: the Go implementation has to produce these exact
bytes, not merely something that decodes to the same value. Refresh it only by
copying the source file again — never by writing what this implementation
happens to emit.
