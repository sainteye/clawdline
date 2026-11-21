package projectassistant

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	MaxOutputBytes        = 100 << 20
	MaxArtifactBytes      = 5 << 20
	MaxOutputChunkBytes   = 384 << 10
	MaxArtifactTitleBytes = 200
	// MaxInlineImageBytes is the largest shared picture a chat fetches whole
	// to show in place (web/console project-assistants/outputs.ts
	// INLINE_IMAGE_BYTES, as Codync's INLINE_BYTES); a larger one is a file card.
	MaxInlineImageBytes = 20 << 20
)

// Output describes an explicit, immutable Bot publication, separate from input pictures.
// ArtifactID stays stable when a whole HTML snapshot is replaced.
type ArtifactDisplayTitle struct {
	ArtifactID string `json:"artifact_id"`
	OutputID   string `json:"output_id"`
	Title      string `json:"title"`
	Version    int64  `json:"version"`
}

type Output struct {
	Quiet        bool   `json:"quiet,omitempty"`
	TitleVersion int64  `json:"title_version,omitempty"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	ArtifactID   string `json:"artifact_id"`
	RequestHash  string `json:"request_hash,omitempty"`
	SnapshotID   string `json:"snapshot_id,omitempty"`
}

func ValidateOutput(o Output) error {
	digest, err := hex.DecodeString(o.SHA256)
	if err != nil || len(digest) != 32 || strings.ToLower(o.SHA256) != o.SHA256 || o.ID == "" || len(o.ID) > MaxNameRunes || o.Bytes < 0 || o.Bytes > MaxOutputBytes {
		return fail(Invalid, "output")
	}
	if o.ArtifactID != "" && (o.Bytes > MaxArtifactBytes || !OutputName(o.ArtifactID) || !ArtifactTitle(o.Name)) {
		return fail(Invalid, "artifact")
	}
	if o.ArtifactID == "" && (o.Quiet || !OutputName(o.Name)) {
		return fail(Invalid, "output_name")
	}
	if o.SnapshotID != "" && (!OutputName(o.SnapshotID) || !strings.HasPrefix(o.SnapshotID, "snapshot-")) {
		return fail(Invalid, "output_snapshot")
	}
	return nil
}

func ArtifactTitle(title string) bool {
	return title != "" && strings.TrimSpace(title) == title && len(title) <= MaxArtifactTitleBytes && !strings.ContainsFunc(title, unicode.IsControl)
}

func OutputName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= MaxNameRunes && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`) && !strings.ContainsFunc(name, unicode.IsControl)
}
