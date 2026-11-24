package projectassistant

import (
	"regexp"
	"time"
)

// Profiles are source-owned identities. A wire value is never a folder or an
// environment dictionary. The empty selection keeps the existing login in place.
const MaxLoginProfiles = 32

// Pending targets contain scope metadata only and expire with Secret Handoff.
const MaxLoginSecretTargets = 128

// A sign-in check reads native status or the key vault at most this many times
// before it reports the outcome as unknown. A failed read never changes the
// stored state, so the next check that reads successfully settles it.
const MaxLoginProbeAttempts = 3

// LoginProbeBackoff is the wait before the second read; each later wait doubles.
const LoginProbeBackoff = 200 * time.Millisecond

// MaxLoginProbeBackoffMilliseconds is the longest single wait between reads.
const MaxLoginProbeBackoffMilliseconds = 400

var loginProfileID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func ValidLoginProfileID(id string) bool { return id == "" || loginProfileID.MatchString(id) }

type LoginProfile struct {
	ID        string             `json:"id"`
	Agent     Agent              `json:"agent"`
	Kind      string             `json:"kind"`
	Name      string             `json:"name"`
	State     string             `json:"state"`
	Revision  int64              `json:"revision"`
	Variables []string           `json:"variables,omitempty"`
	Handoff   *LoginSecretTarget `json:"handoff,omitempty"`
}

// LoginSecretTarget is an expiring source-owned delivery address, not a token
// that authorizes a caller. Every submission still checks the current principal.
type LoginSecretTarget struct {
	ID      string    `json:"id"`
	Expires time.Time `json:"expires"`
}

func LoginKeyVariable(agent Agent) string {
	switch agent {
	case ClaudeCode:
		return "ANTHROPIC_API_KEY"
	case Codex:
		return "CODEX_API_KEY"
	}
	return ""
}

type LoginProfileAction struct {
	Command         Command `json:"command"`
	Fence           Fence   `json:"fence"`
	ExpectedVersion int64   `json:"expected_version"`
	ProfileID       string  `json:"profile_id"`
	Revision        int64   `json:"revision"`
	Agent           Agent   `json:"agent"`
	Action          string  `json:"action"`
	Name            string  `json:"name"`
	Kind            string  `json:"kind,omitempty"`
	Target          string  `json:"target,omitempty"`
	Authority       string  `json:"-"`
}

const LoginProfileOperation Operation = "login-profile"
