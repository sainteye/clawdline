// Package workflow defines immutable, provider-independent launch instructions.
package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
// covers canonical JSON with an empty digest, including Graphify and all stages.
// Instructions confer no authority beyond the user's existing permissions.
type Snapshot struct {
	Schema          int     `json:"schema"`
	ProfileID       string  `json:"profile_id"`
	Version         string  `json:"version"`
	Source          string  `json:"source"`
	Digest          string  `json:"digest"`
	Instructions    string  `json:"instructions"`
	Stages          []Stage `json:"stages"`
	GraphifyEnabled bool    `json:"graphify_enabled"`
}

// Seal returns an independent normalized copy. A zero schema selects schema 1;
// other schemas are refused. The caller must separately bound serialized size.
func Seal(s Snapshot) (Snapshot, error) {
	if s.Schema == 0 {
		s.Schema = 1
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
	if s.Schema != 1 || !safeID.MatchString(s.ProfileID) || strings.TrimSpace(s.Version) == "" || strings.TrimSpace(s.Source) == "" {
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
