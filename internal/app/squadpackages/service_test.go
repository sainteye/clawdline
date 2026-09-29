package squadpackages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

type fakeCatalog struct {
	grant              PreviewGrant
	grants             int
	adoption           Adoption
	adoptions          int
	receiptFingerprint string
	receipt            Receipt
}

func (f *fakeCatalog) Snapshot(context.Context) (Snapshot, error) {
	return Snapshot{Version: 4, Existing: []squadpack.Existing{}}, nil
}
func (f *fakeCatalog) SavePreview(_ context.Context, grant PreviewGrant) error {
	f.grant, f.grants = grant, f.grants+1
	return nil
}
func (f *fakeCatalog) Replay(_ context.Context, _, _, fingerprint string) (Receipt, bool, error) {
	if f.receiptFingerprint == "" {
		return Receipt{}, false, nil
	}
	if f.receiptFingerprint != fingerprint {
		return Receipt{}, false, &squadpack.Refusal{Code: "idempotency_conflict"}
	}
	return f.receipt, true, nil
}
func (f *fakeCatalog) Adopt(_ context.Context, a Adoption) (Receipt, error) {
	f.adoption, f.adoptions = a, f.adoptions+1
	f.receiptFingerprint = a.Fingerprint
	f.receipt = Receipt{CatalogVersion: 5, ArchiveDigest: a.ArchiveDigest}
	return f.receipt, nil
}
func (f *fakeCatalog) ExportData(context.Context, []string) (squadpack.Manifest, map[string][]byte, error) {
	return squadpack.Manifest{Namespace: "example.squad", Source: "test", License: "CC0-1.0"}, map[string][]byte{}, nil
}

func smallArchive(t *testing.T) []byte {
	t.Helper()
	b, err := squadpack.Build(squadpack.Manifest{Namespace: "example.squad", Source: "test", License: "CC0-1.0"}, map[string][]byte{})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPreviewValidatesBeforeSavingHashOfBoundToken(t *testing.T) {
	f := &fakeCatalog{}
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	s := Service{Catalog: f, Now: func() time.Time { return now }, Random: strings.NewReader(strings.Repeat("r", 32))}
	if _, err := s.Preview(context.Background(), "person-1", "global", []byte("bad")); err == nil || f.grants != 0 {
		t.Fatal("an invalid ZIP minted a preview")
	}
	result, err := s.Preview(context.Background(), "person-1", "global", smallArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := hex.DecodeString(result.Token)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(secret)
	if f.grants != 1 || f.grant.Actor != "person-1" || f.grant.Scope != "global" ||
		f.grant.TokenHash != hex.EncodeToString(hash[:]) || f.grant.TokenHash == result.Token ||
		f.grant.ArchiveDigest != result.ArchiveDigest || f.grant.PreviewDigest != result.Digest ||
		!f.grant.ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("preview grant was not correctly bound: %+v", f.grant)
	}
}

func TestAdoptionReparsesBytesAndRejectsChangedDigestBeforeStore(t *testing.T) {
	f := &fakeCatalog{}
	s := Service{Catalog: f, Random: strings.NewReader(strings.Repeat("r", 32))}
	archive := smallArchive(t)
	preview, err := s.Preview(context.Background(), "person-1", "global", archive)
	if err != nil {
		t.Fatal(err)
	}
	in := AdoptInput{Actor: "person-1", Scope: "global", RequestID: "req-1", PreviewToken: preview.Token,
		ArchiveDigest: "sha256:" + strings.Repeat("0", 64), PreviewDigest: preview.Digest, ExpectedVersion: 4, Archive: archive}
	_, err = s.Adopt(context.Background(), in)
	var refusal *squadpack.Refusal
	if !errors.As(err, &refusal) || refusal.Code != ErrDigestChanged || f.adoptions != 0 {
		t.Fatalf("changed digest reached store: %v", err)
	}
	in.ArchiveDigest = preview.ArchiveDigest
	if _, err = s.Adopt(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if f.adoptions != 1 || f.adoption.TokenHash != f.grant.TokenHash ||
		f.adoption.Package.Digest != preview.ArchiveDigest || f.adoption.ExpectedVersion != 4 {
		t.Fatalf("adoption lost preview binding: %+v", f.adoption)
	}
	if _, err := s.Adopt(context.Background(), in); err != nil || f.adoptions != 1 {
		t.Fatalf("successful retry did not replay original receipt: %v", err)
	}
	in.Archive = []byte("corrupt")
	if _, err := s.Adopt(context.Background(), in); !errors.As(err, &refusal) || refusal.Code != "idempotency_conflict" {
		t.Fatalf("changed retry reached archive parser: %v", err)
	}
}
