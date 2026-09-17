// Package artifacts keeps this daemon's own pictures: the ones a browser sends
// with a message, and the ones a session stores to show in its own reply.
//
// Two stores, because the Swift app has two and they answer different
// questions. `Drops` is where a picture sent from the page is written so that a
// terminal program can read it — a file with a path, kept for a while after the
// prompt was accepted, because accepting the prompt is not reading the file.
// `Store` is the opaque-id image store behind `<clawdline-image id="…">` and
// `GET /v1/artifacts/images/:id`: bytes, metadata and expiry, never a path on
// the wire.
//
// Both live under this daemon's own directory (`CLAWDLINE_NEXT_DIR`), with the
// directory 0700 and every file 0600. Nothing here writes the Swift app's
// caches; reading those is `internal/adapters/swiftstore`'s job and only that.
package artifacts

import (
	"fmt"
	"regexp"
)

// Artifact is the only image metadata allowed onto a wire: `SessionImageArtifact`.
// Bytes and file locations stay in the store.
type Artifact struct {
	ID        string `json:"id"`
	MediaType string `json:"mediaType"`
	ByteCount int    `json:"byteCount"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ExpiresAt int64  `json:"expiresAt"`
}

// Absence is why a reference describes no bytes: the store held the image and
// no longer does, or it never held that id at all. Collapsing the two is what
// made the Swift app once say "Image expired" about a picture nobody had.
type Absence string

const (
	AbsenceExpired Absence = "expired"
	AbsenceUnknown Absence = "unknown"
)

// Policy is every bound the store keeps. The production values are the Swift
// app's `SessionImageArtifactStore.productionPolicy`, so a picture one app
// accepts the other accepts too.
type Policy struct {
	TTLSeconds          int64
	MaxCount            int
	MaxTotalBytes       int
	MaxInputBytes       int
	MaxEncodedBytes     int
	MaxDimension        int
	MaxPixels           int
	TombstoneTTLSeconds int64
	MaxMetadataCount    int
	MaxImagesPerMessage int
}

var ProductionPolicy = Policy{
	TTLSeconds:          24 * 60 * 60,
	MaxCount:            64,
	MaxTotalBytes:       64 << 20,
	MaxInputBytes:       12 << 20,
	MaxEncodedBytes:     12 << 20,
	MaxDimension:        12_000,
	MaxPixels:           40_000_000,
	TombstoneTTLSeconds: 7 * 24 * 60 * 60,
	MaxMetadataCount:    512,
	MaxImagesPerMessage: 6,
}

// Refusal is a typed no with the HTTP status that describes it. Every bound in
// this package answers with one, so the routes that use it surface exactly what
// it said rather than a sentence of their own.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

func (r Refusal) Error() string { return fmt.Sprintf("%s: %s", r.Code, r.Message) }

var artifactID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsID is `SessionImageArtifactStore.isArtifactID`: a lowercase UUID, spelled
// exactly as Foundation spells one. It is the whole namespace of removable
// names in the store, so it is checked before any file name is built from it.
func IsID(id string) bool { return artifactID.MatchString(id) }

// Valid is `SessionImageArtifact.isValidReference` under a policy.
func (a Artifact) Valid(p Policy) bool {
	return IsID(a.ID) &&
		a.MediaType == "image/png" &&
		a.ByteCount > 0 && a.ByteCount <= p.MaxEncodedBytes &&
		a.Width > 0 && a.Height > 0 &&
		a.Width <= p.MaxDimension && a.Height <= p.MaxDimension &&
		a.Width <= p.MaxPixels/a.Height &&
		a.ExpiresAt > 0
}
