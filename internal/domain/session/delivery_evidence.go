package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	// DeliveryEvidenceBytesLimit bounds the exact UTF-8 evidence document, before JSON transport escaping.
	DeliveryEvidenceBytesLimit   = 256 << 10
	DeliveryEvidenceCommitsLimit = 4096
)

// DeliveryEvidence binds a short sentence to a complete adapter-verified Git receipt.
// It is an authenticated assertion, not an independent review or permission to publish.
type DeliveryEvidence struct {
	SchemaVersion  int             `json:"schema_version"`
	Summary        string          `json:"summary"`
	BaselineSHA256 string          `json:"baseline_sha256"`
	ManifestSHA256 string          `json:"manifest_sha256"`
	GitReceipt     json.RawMessage `json:"git_receipt"`
}

type DeliveryEvidenceError string

func (e DeliveryEvidenceError) Error() string { return string(e) }

// ValidateDeliveryEvidence preserves the original bytes for hashing and storage.
// The adapter owns Git/file verification; this boundary rejects incomplete or contradictory assertions.
func ValidateDeliveryEvidence(raw, summary string) (digest string, commits int, err error) {
	fail := func(code string) (string, int, error) { return "", 0, DeliveryEvidenceError(code) }
	if len(raw) > DeliveryEvidenceBytesLimit {
		return fail("delivery_evidence_too_large")
	}
	if !utf8.ValidString(raw) {
		return fail("delivery_evidence_invalid")
	}
	var evidence DeliveryEvidence
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&evidence) != nil || dec.Decode(new(any)) != io.EOF || evidence.SchemaVersion != 1 {
		return fail("delivery_evidence_invalid")
	}
	if evidence.Summary != strings.TrimSpace(summary) {
		return fail("delivery_evidence_mismatch")
	}
	hexOf := func(s string, n int) bool {
		if len(s) != n || strings.ToLower(s) != s {
			return false
		}
		_, e := hex.DecodeString(s)
		return e == nil
	}
	if !hexOf(evidence.BaselineSHA256, 64) || !hexOf(evidence.ManifestSHA256, 64) {
		return fail("delivery_evidence_invalid")
	}
	var receipt struct {
		SchemaVersion int               `json:"schemaVersion"`
		OK            *bool             `json:"ok"`
		Root          string            `json:"root"`
		BaselineHead  string            `json:"baselineHead"`
		Head          string            `json:"head"`
		Commits       []string          `json:"commits"`
		OwnedFiles    *int              `json:"ownedFiles"`
		Issues        []json.RawMessage `json:"issues"`
	}
	if json.Unmarshal(evidence.GitReceipt, &receipt) != nil || receipt.SchemaVersion != 1 || receipt.Root == "" {
		return fail("delivery_evidence_invalid")
	}
	if receipt.OK == nil || !*receipt.OK || receipt.Issues == nil || len(receipt.Issues) != 0 || receipt.OwnedFiles == nil || *receipt.OwnedFiles < 1 {
		return fail("delivery_evidence_unverified")
	}
	if len(receipt.Commits) > DeliveryEvidenceCommitsLimit {
		return fail("delivery_evidence_too_large")
	}
	if len(receipt.Commits) == 0 {
		return fail("delivery_evidence_unverified")
	}
	oid := func(v string) bool { return hexOf(v, 40) || hexOf(v, 64) }
	if !oid(receipt.Head) || !oid(receipt.BaselineHead) || len(receipt.Head) != len(receipt.BaselineHead) {
		return fail("delivery_evidence_invalid")
	}
	seen := map[string]bool{}
	for _, c := range receipt.Commits {
		if !oid(c) || len(c) != len(receipt.Head) || seen[c] || c == receipt.BaselineHead {
			return fail("delivery_evidence_invalid")
		}
		seen[c] = true
	}
	// Reject duplicate object keys too: JSON decoders otherwise silently accept the last verdict.
	if err := uniqueJSONKeys([]byte(raw)); err != nil {
		return fail("delivery_evidence_invalid")
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), len(receipt.Commits), nil
}

func uniqueJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var value func() error
	value = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch tok {
		case json.Delim('{'):
			keys := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok || keys[s] {
					return errors.New("duplicate key")
				}
				keys[s] = true
				if err := value(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case json.Delim('['):
			for dec.More() {
				if err := value(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	return value()
}
