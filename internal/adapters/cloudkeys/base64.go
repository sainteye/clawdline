package cloudkeys

import "encoding/base64"

// encodeBase64 writes key material in the one spelling this package reads back:
// standard alphabet, padded. cloud.DecodeCanonicalBase64 refuses every other
// spelling of the same bytes, so a file written here and a file written by hand
// cannot differ silently.
func encodeBase64(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }
