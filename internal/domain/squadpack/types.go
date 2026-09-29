package squadpack

import "fmt"

const Format = "clawdline.squad-package"
const Version = 1

// Manifest is the complete list of archive contents. A path not listed here
// is never silently ignored by the reader.
type Manifest struct {
	Format    string       `json:"format"`
	Version   int          `json:"version"`
	Namespace string       `json:"namespace"`
	Source    string       `json:"source"`
	License   string       `json:"license"`
	Teams     []Definition `json:"teams"`
	Personas  []Definition `json:"personas"`
	Skills    []Definition `json:"skills"`
	Private   []Private    `json:"private,omitempty"`
}

// Definition is one shareable item. Personas and skills are ordered IDs,
// not display names. Only teams use Personas, and only personas use Skills.
type Definition struct {
	ID             string   `json:"id"`
	Version        string   `json:"version"`
	Name           string   `json:"name"`
	NameZhHant     string   `json:"name_zh_hant,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	SummaryZhHant  string   `json:"summary_zh_hant,omitempty"`
	Purpose        string   `json:"purpose,omitempty"`
	PurposeZhHant  string   `json:"purpose_zh_hant,omitempty"`
	Icon           Icon     `json:"icon"`
	Source         string   `json:"source,omitempty"`
	License        string   `json:"license,omitempty"`
	Body           string   `json:"body"`
	SHA256         string   `json:"sha256"`
	Personas       []string `json:"personas,omitempty"`
	Skills         []string `json:"skills,omitempty"`
	DisabledSkills []string `json:"disabled_skills,omitempty"`
}

type Icon struct {
	Accent string      `json:"accent"`
	Cells  [][]*string `json:"cells"`
}

// Private is an explicitly selected global or Project settings document.
// Its contents remain opaque to this format package; the settings service
// validates their shape and confirms their scope before applying them.
type Private struct {
	Scope   string `json:"scope"`
	Project string `json:"project,omitempty"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

type Package struct {
	Manifest Manifest
	Files    map[string][]byte
	Digest   string // SHA-256 of the original ZIP bytes, prefixed with sha256:.
}

// Refusal is a stable, path-free rejection for an untrusted package.
type Refusal struct{ Code string }

func (e *Refusal) Error() string { return fmt.Sprintf("squad package: %s", e.Code) }

func refuse(code string) error { return &Refusal{Code: code} }

const (
	ErrArchiveTooLarge       = "archive_too_large"
	ErrArchiveInvalid        = "archive_invalid"
	ErrArchiveEntryCount     = "archive_entry_limit"
	ErrArchiveEntryType      = "archive_entry_type"
	ErrArchiveEncryption     = "archive_encrypted"
	ErrArchiveCompression    = "archive_compression"
	ErrArchiveExpansion      = "archive_expansion"
	ErrArchiveFileTooLarge   = "archive_file_too_large"
	ErrArchivePath           = "archive_path_invalid"
	ErrArchivePathCollision  = "archive_path_collision"
	ErrArchiveExtraFile      = "archive_extra_file"
	ErrArchiveMissingFile    = "archive_missing_file"
	ErrArchiveHeaderMismatch = "archive_header_mismatch"
	ErrManifestInvalid       = "manifest_invalid"
	ErrManifestTooLarge      = "manifest_too_large"
	ErrFormatUnsupported     = "format_unsupported"
	ErrTextEncoding          = "text_encoding_invalid"
	ErrDigestMismatch        = "digest_mismatch"
	ErrDuplicateID           = "duplicate_id"
	ErrNameConflict          = "name_conflict"
	ErrReservedID            = "reserved_id"
	ErrInvalidDefinition     = "definition_invalid"
	ErrMissingReference      = "missing_reference"
	ErrDuplicateReference    = "duplicate_reference"
	ErrPrivateScope          = "private_scope_invalid"
	ErrPrivateConfirmation   = "private_confirmation_required"
)
