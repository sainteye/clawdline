package projectassistant

import "regexp"

// Profiles are source-owned identities. A wire value is never a folder or an
// environment dictionary. The empty selection keeps the existing login in place.
const MaxLoginProfiles = 32

var loginProfileID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func ValidLoginProfileID(id string) bool { return id == "" || loginProfileID.MatchString(id) }

type LoginProfile struct {
	ID       string `json:"id"`
	Agent    Agent  `json:"agent"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Revision int64  `json:"revision"`
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
}

const LoginProfileOperation Operation = "login-profile"
