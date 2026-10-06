// Package release is the contract between a published release and the
// machines that install it: the manifest that names each artifact, the
// signatures that make the manifest trustworthy, and the checks an artifact
// must pass before anything installs it (docs/releasing.md, docs/updates.md).
//
// The first download of an install trusts HTTPS to the host serving it; every
// install after that trusts only a manifest signed by a key compiled into the
// binary already running. A manifest is never followed on the strength of
// where it came from.
package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Bounds of what is read from a release host. A longer answer is refused as
// not a manifest, rather than read into memory.
const (
	maxManifestBytes  = 64 << 10
	maxSignatureBytes = 16 << 10
)

// MaxManifestBytes and MaxSignatureBytes are the bounds for readers outside
// this package.
func MaxManifestBytes() int64  { return maxManifestBytes }
func MaxSignatureBytes() int64 { return maxSignatureBytes }

// Kinds of artifact. A daemon archive holds `clawdline` and `dist/`, the
// layout `current` points at; an app archive holds `Clawdline Next.app`.
const (
	KindDaemon = "daemon"
	KindApp    = "app"
)

// Manifest is one release, as manifest.json says it.
type Manifest struct {
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	CommittedAt string `json:"committed_at"`
	Channel     string `json:"channel"`
	// MinVersion is the oldest version this release may be upgraded from and
	// rolled back to. A release that writes a store format an older binary
	// cannot read raises it and sets NonAdditiveMigration.
	MinVersion           string     `json:"min_version,omitempty"`
	NonAdditiveMigration bool       `json:"non_additive_migration,omitempty"`
	NotesURL             string     `json:"notes_url,omitempty"`
	Artifacts            []Artifact `json:"artifacts"`
}

// Artifact is one downloadable file of a release.
type Artifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Signature is one key's signature over the manifest's exact bytes.
// manifest.sig.json is a list of them, so a key rotation can sign with the old
// key and the new one at once.
type Signature struct {
	KeyID string `json:"key_id"`
	Sig   string `json:"sig"`
}

// Error is a refusal with a code a person can look up in
// docs/user/troubleshooting.md.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

// The codes Error carries.
const (
	CodeManifestMalformed  = "manifest_malformed"
	CodeManifestUnsigned   = "manifest_unsigned"
	CodeSignatureInvalid   = "manifest_signature_invalid"
	CodeNoArtifact         = "no_artifact_for_platform"
	CodeArtifactSize       = "artifact_size_mismatch"
	CodeArtifactSHA256     = "artifact_sha256_mismatch"
	CodeNoTrustedKey       = "no_trusted_key"
	CodeVersionUnparseable = "version_unparseable"
)

func refuse(code, format string, a ...any) error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// KeyID names a public key: the first 8 bytes of its SHA-256, in hex.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// Sign signs manifest bytes exactly as they will be published.
func Sign(manifest []byte, priv ed25519.PrivateKey) Signature {
	pub := priv.Public().(ed25519.PublicKey)
	return Signature{KeyID: KeyID(pub), Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, manifest))}
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Open checks that sigs carries a valid signature over manifest by one of
// keys, and only then parses and validates the manifest.
func Open(manifest, sigs []byte, keys []ed25519.PublicKey) (Manifest, error) {
	if len(keys) == 0 {
		return Manifest{}, refuse(CodeNoTrustedKey, "this binary trusts no release key, so it cannot verify any release")
	}
	if len(manifest) > maxManifestBytes {
		return Manifest{}, refuse(CodeManifestMalformed, "manifest is longer than %d bytes", maxManifestBytes)
	}
	if len(bytes.TrimSpace(sigs)) == 0 {
		return Manifest{}, refuse(CodeManifestUnsigned, "the release carries no signature")
	}
	if len(sigs) > maxSignatureBytes {
		return Manifest{}, refuse(CodeSignatureInvalid, "signature file is longer than %d bytes", maxSignatureBytes)
	}
	var list []Signature
	if err := json.Unmarshal(sigs, &list); err != nil {
		return Manifest{}, refuse(CodeSignatureInvalid, "signature file is not a JSON list: %v", err)
	}
	if len(list) == 0 {
		return Manifest{}, refuse(CodeManifestUnsigned, "the signature list is empty")
	}
	byID := map[string]ed25519.PublicKey{}
	for _, k := range keys {
		byID[KeyID(k)] = k
	}
	verified, known := false, false
	for _, s := range list {
		k, ok := byID[s.KeyID]
		if !ok {
			continue
		}
		known = true
		raw, err := base64.StdEncoding.DecodeString(s.Sig)
		if err == nil && ed25519.Verify(k, manifest, raw) {
			verified = true
			break
		}
	}
	if !verified {
		if !known {
			return Manifest{}, refuse(CodeSignatureInvalid, "no signature is by a key this binary trusts")
		}
		return Manifest{}, refuse(CodeSignatureInvalid, "the signature does not match the manifest")
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(manifest))
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, refuse(CodeManifestMalformed, "manifest is not JSON: %v", err)
	}
	if err := m.validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) validate() error {
	if _, err := ParseVersion(m.Version); err != nil {
		return err
	}
	if m.MinVersion != "" {
		if _, err := ParseVersion(m.MinVersion); err != nil {
			return err
		}
	}
	if len(m.Commit) != 40 || strings.Trim(m.Commit, "0123456789abcdef") != "" {
		return refuse(CodeManifestMalformed, "commit %q is not a full lowercase commit", m.Commit)
	}
	for _, a := range m.Artifacts {
		if a.OS == "" || a.Arch == "" || a.Name == "" || a.URL == "" {
			return refuse(CodeManifestMalformed, "artifact %q lacks os, arch, name or url", a.Name)
		}
		if a.Kind != KindDaemon && a.Kind != KindApp {
			return refuse(CodeManifestMalformed, "artifact %q has unknown kind %q", a.Name, a.Kind)
		}
		if a.Size <= 0 || !sha256Hex.MatchString(a.SHA256) {
			return refuse(CodeManifestMalformed, "artifact %q lacks a size or a sha256", a.Name)
		}
		if strings.ContainsAny(a.Name, `/\`) || a.Name == "." || a.Name == ".." {
			return refuse(CodeManifestMalformed, "artifact name %q is not a plain file name", a.Name)
		}
	}
	return nil
}

// Artifact is the one artifact of kind for os and arch.
func (m Manifest) Artifact(os, arch, kind string) (Artifact, error) {
	for _, a := range m.Artifacts {
		if a.OS == os && a.Arch == arch && a.Kind == kind {
			return a, nil
		}
	}
	return Artifact{}, refuse(CodeNoArtifact, "release %s has no %s artifact for %s/%s", m.Version, kind, os, arch)
}

// Check reads r to its end, at most a.Size+1 bytes, and refuses unless it is
// exactly a.Size bytes with a.SHA256. It copies what it read to w, which may
// be nil.
func (a Artifact) Check(r io.Reader, w io.Writer) error {
	h := sha256.New()
	dst := io.Writer(h)
	if w != nil {
		dst = io.MultiWriter(h, w)
	}
	n, err := io.Copy(dst, io.LimitReader(r, a.Size+1))
	if err != nil {
		return err
	}
	if n != a.Size {
		return refuse(CodeArtifactSize, "%s is %d bytes, the manifest says %d", a.Name, n, a.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return refuse(CodeArtifactSHA256, "%s has sha256 %s, the manifest says %s", a.Name, got, a.SHA256)
	}
	return nil
}
