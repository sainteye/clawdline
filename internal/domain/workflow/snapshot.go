// Package workflow defines immutable, provider-independent launch instructions.
package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidSnapshot = errors.New("invalid_workflow_snapshot")
	ErrDigestMismatch  = errors.New("workflow_snapshot_digest_mismatch")
	safeID             = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// Stage describes an interchange boundary, not an automatically executed gate.
type Stage struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Output     string `json:"output"`
	Acceptance string `json:"acceptance"`
}

// Snapshot is the complete workflow choice retained by a conversation. Digest
// covers canonical JSON with an empty digest, including CodeGraph and all stages.
// Instructions confer no authority beyond the user's existing permissions.
type Snapshot struct {
	Schema           int     `json:"schema"`
	ProfileID        string  `json:"profile_id"`
	Version          string  `json:"version"`
	Source           string  `json:"source"`
	Digest           string  `json:"digest"`
	Instructions     string  `json:"instructions"`
	Stages           []Stage `json:"stages"`
	CodeGraphEnabled bool    `json:"codegraph_enabled"`
	legacyGraphify   *bool
}

// Seal returns an independent normalized copy. A zero schema selects schema 2;
// other schemas are refused. The caller must separately bound serialized size.
func Seal(s Snapshot) (Snapshot, error) {
	if s.Schema == 0 {
		s.Schema = 2
	}
	s.Stages = append([]Stage{}, s.Stages...)
	if err := s.validateFields(); err != nil {
		return Snapshot{}, err
	}
	s.Digest = s.canonicalDigest()
	return s, nil
}

// Validate rejects changed or unsupported snapshots without resealing them.
func (s Snapshot) Validate() error {
	if err := s.validateFields(); err != nil {
		return err
	}
	if s.Digest == "" || s.Digest != s.canonicalDigest() {
		return ErrDigestMismatch
	}
	return nil
}

func (s Snapshot) validateFields() error {
	if (s.Schema != 2 && (s.Schema != 1 || s.legacyGraphify == nil || s.CodeGraphEnabled)) || !safeID.MatchString(s.ProfileID) || strings.TrimSpace(s.Version) == "" || strings.TrimSpace(s.Source) == "" {
		return ErrInvalidSnapshot
	}
	for _, v := range []string{s.ProfileID, s.Version, s.Source, s.Instructions} {
		if !utf8.ValidString(v) {
			return ErrInvalidSnapshot
		}
	}
	seen := make(map[string]bool, len(s.Stages))
	for _, stage := range s.Stages {
		if !safeID.MatchString(stage.ID) || seen[stage.ID] || strings.TrimSpace(stage.Title) == "" || strings.TrimSpace(stage.Output) == "" || strings.TrimSpace(stage.Acceptance) == "" {
			return ErrInvalidSnapshot
		}
		for _, v := range []string{stage.Title, stage.Output, stage.Acceptance} {
			if !utf8.ValidString(v) {
				return ErrInvalidSnapshot
			}
		}
		seen[stage.ID] = true
	}
	return nil
}

func (s Snapshot) canonicalDigest() string {
	s.Digest = ""
	s.Stages = append([]Stage{}, s.Stages...)
	document, _ := json.Marshal(s)
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:])
}

// Schema 1 is retained only to verify and replay immutable historical receipts.
// Its retired flag never enables CodeGraph or adds retired launch instructions.
type legacySnapshot struct {
	Schema          int     `json:"schema"`
	ProfileID       string  `json:"profile_id"`
	Version         string  `json:"version"`
	Source          string  `json:"source"`
	Digest          string  `json:"digest"`
	Instructions    string  `json:"instructions"`
	Stages          []Stage `json:"stages"`
	GraphifyEnabled bool    `json:"graphify_enabled"`
}

func (s Snapshot) MarshalJSON() ([]byte, error) {
	if s.Schema == 1 && s.legacyGraphify != nil {
		return json.Marshal(legacySnapshot{s.Schema, s.ProfileID, s.Version, s.Source, s.Digest, s.Instructions, s.Stages, *s.legacyGraphify})
	}
	type plain Snapshot
	return json.Marshal(plain(s))
}
func (s *Snapshot) UnmarshalJSON(raw []byte) error {
	var header struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	decode := func(target any) error {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(target); err != nil {
			return err
		}
		if d.Decode(new(any)) != io.EOF {
			return ErrInvalidSnapshot
		}
		return nil
	}
	if header.Schema == 1 {
		var old legacySnapshot
		if err := decode(&old); err != nil {
			return err
		}
		*s = Snapshot{Schema: old.Schema, ProfileID: old.ProfileID, Version: old.Version, Source: old.Source, Digest: old.Digest, Instructions: old.Instructions, Stages: old.Stages, legacyGraphify: &old.GraphifyEnabled}
		return nil
	}
	type plain Snapshot
	var current plain
	if err := decode(&current); err != nil {
		return err
	}
	*s = Snapshot(current)
	return nil
}
