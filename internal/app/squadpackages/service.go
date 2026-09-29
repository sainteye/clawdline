// Package squadpackages coordinates a verified offline ZIP with the private
// catalog's atomic adoption boundary. It stores no candidate file bytes.
package squadpackages

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

type Snapshot struct {
	Version        int64
	SettingsDigest string
	Existing       []squadpack.Existing
}

// PreviewGrant is persisted only after the entire ZIP was validated. TokenHash
// is stored instead of the bearer token itself; it is useless without a
// separately authenticated write-capable actor.
type PreviewGrant struct {
	TokenHash      string
	Actor          string
	Scope          string
	ArchiveDigest  string
	CatalogVersion int64
	SettingsDigest string
	PreviewDigest  string
	ExpiresAt      time.Time
}

type Adoption struct {
	Actor           string
	Scope           string
	RequestID       string
	Fingerprint     string
	TokenHash       string
	ArchiveDigest   string
	PreviewDigest   string
	ExpectedVersion int64
	Choices         map[string]string
	PrivateScopes   []string
	ConfirmPrivate  bool
	Package         squadpack.Package
	At              time.Time
}

type Receipt struct {
	CatalogVersion int64    `json:"catalog_version"`
	ArchiveDigest  string   `json:"archive_digest"`
	AdoptedIDs     []string `json:"adopted_ids"`
	PrivateScopes  []string `json:"private_scopes"`
}

// Catalog implementation performs the adoption transaction. In that one
// transaction it first checks an existing request receipt, then the preview
// grant, catalog/settings versions, conflict choices and reference closure,
// and finally writes definitions, selected settings and the receipt.
type Catalog interface {
	Snapshot(context.Context) (Snapshot, error)
	SavePreview(context.Context, PreviewGrant) error
	Replay(context.Context, string, string, string) (Receipt, bool, error)
	Adopt(context.Context, Adoption) (Receipt, error)
	ExportData(context.Context, []string) (squadpack.Manifest, map[string][]byte, error)
}

type Service struct {
	Catalog    Catalog
	Now        func() time.Time
	Random     io.Reader
	PreviewAge time.Duration
}

type PreviewResult struct {
	squadpack.Preview
	Token     string    `json:"preview_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

const (
	ErrPreviewInvalid = "preview_invalid"
	ErrDigestChanged  = "archive_digest_changed"
	ErrRequestInvalid = "request_invalid"
)

func (s Service) clock() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) age() time.Duration {
	if s.PreviewAge > 0 && s.PreviewAge <= MaxPreviewAgeSeconds*time.Second {
		return s.PreviewAge
	}
	return MaxPreviewAgeSeconds * time.Second
}

func (s Service) random() io.Reader {
	if s.Random != nil {
		return s.Random
	}
	return rand.Reader
}

func (s Service) Preview(ctx context.Context, actor, scope string, archive []byte) (PreviewResult, error) {
	if actor == "" || scope == "" {
		return PreviewResult{}, &squadpack.Refusal{Code: ErrRequestInvalid}
	}
	pkg, err := squadpack.Parse(archive)
	if err != nil {
		return PreviewResult{}, err
	}
	snapshot, err := s.Catalog.Snapshot(ctx)
	if err != nil {
		return PreviewResult{}, err
	}
	preview, err := squadpack.Analyze(pkg, snapshot.Version, scope, snapshot.Existing)
	if err != nil {
		return PreviewResult{}, err
	}
	secret := make([]byte, 32)
	if _, err := io.ReadFull(s.random(), secret); err != nil {
		return PreviewResult{}, err
	}
	token := hex.EncodeToString(secret)
	hash := sha256.Sum256(secret)
	expires := s.clock().Add(s.age())
	grant := PreviewGrant{TokenHash: hex.EncodeToString(hash[:]), Actor: actor, Scope: scope,
		ArchiveDigest: pkg.Digest, CatalogVersion: snapshot.Version, SettingsDigest: snapshot.SettingsDigest,
		PreviewDigest: preview.Digest, ExpiresAt: expires}
	if err := s.Catalog.SavePreview(ctx, grant); err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{Preview: preview, Token: token, ExpiresAt: expires}, nil
}

type AdoptInput struct {
	Actor           string
	Scope           string
	RequestID       string
	PreviewToken    string
	ArchiveDigest   string
	PreviewDigest   string
	ExpectedVersion int64
	Choices         map[string]string
	PrivateScopes   []string
	ConfirmPrivate  bool
	Archive         []byte
}

func (s Service) Adopt(ctx context.Context, input AdoptInput) (Receipt, error) {
	if input.Actor == "" || input.Scope == "" || input.RequestID == "" || input.ExpectedVersion < 0 ||
		input.PreviewDigest == "" || len(input.PreviewToken) != 64 {
		return Receipt{}, &squadpack.Refusal{Code: ErrRequestInvalid}
	}
	secret, err := hex.DecodeString(input.PreviewToken)
	if err != nil {
		return Receipt{}, &squadpack.Refusal{Code: ErrPreviewInvalid}
	}
	hash := sha256.Sum256(secret)
	archiveHash := sha256.Sum256(input.Archive)
	fingerprintBody, _ := json.Marshal(struct {
		Actor           string
		Scope           string
		TokenHash       string
		ArchiveHash     string
		AssertedDigest  string
		PreviewDigest   string
		ExpectedVersion int64
		Choices         map[string]string
		PrivateScopes   []string
		ConfirmPrivate  bool
	}{input.Actor, input.Scope, hex.EncodeToString(hash[:]), hex.EncodeToString(archiveHash[:]),
		input.ArchiveDigest, input.PreviewDigest, input.ExpectedVersion, input.Choices,
		input.PrivateScopes, input.ConfirmPrivate})
	fingerprintHash := sha256.Sum256(fingerprintBody)
	fingerprint := hex.EncodeToString(fingerprintHash[:])
	if receipt, found, err := s.Catalog.Replay(ctx, input.Actor, input.RequestID, fingerprint); err != nil || found {
		return receipt, err
	}
	pkg, err := squadpack.Parse(input.Archive)
	if err != nil {
		return Receipt{}, err
	}
	if pkg.Digest != input.ArchiveDigest {
		return Receipt{}, &squadpack.Refusal{Code: ErrDigestChanged}
	}
	return s.Catalog.Adopt(ctx, Adoption{Actor: input.Actor, Scope: input.Scope,
		RequestID: input.RequestID, Fingerprint: fingerprint, TokenHash: hex.EncodeToString(hash[:]),
		ArchiveDigest: pkg.Digest, PreviewDigest: input.PreviewDigest,
		ExpectedVersion: input.ExpectedVersion, Choices: input.Choices,
		PrivateScopes: input.PrivateScopes, ConfirmPrivate: input.ConfirmPrivate,
		Package: pkg, At: s.clock()})
}

func (s Service) Export(ctx context.Context, selected []string, confirmed bool) ([]byte, error) {
	if len(selected) > 0 && !confirmed {
		return nil, &squadpack.Refusal{Code: squadpack.ErrPrivateConfirmation}
	}
	manifest, files, err := s.Catalog.ExportData(ctx, selected)
	if err != nil {
		return nil, err
	}
	return squadpack.Export(manifest, files, squadpack.ExportSelection{PrivateScopes: selected, ConfirmPrivate: confirmed})
}
