package cloud

// What a person does about a failure.
//
// FailureCode is the closed vocabulary an operator greps for — forty-odd words,
// one per thing that can go wrong. This file is the layer above it: every one
// of those words belongs to exactly one of seven kinds, and the kind is chosen
// by **what somebody has to do next**, not by which component said no. A relay
// `forbidden` and a login code that expired are both "this machine is not
// approved": the answer to either is to approve it again.
//
// The five named for the production cutover (docs/cloud-cutover.md) are the
// first five below. `unavailable` is kept apart from them on purpose — "could
// not reach it" and "it said no" call for opposite responses, waiting versus
// changing something — and `unknown` is what a word nobody wrote down gets,
// rather than a guess (docs/design-guidelines.md DG-7).
//
// There is one mapping, KindOfCode, and DescribeFailure goes through it. The
// CLI, the status route and the settings page therefore cannot disagree about
// what a failure was: they all read the word the transport recorded.

import "strings"

// FailureKind is one of the seven answers to "what now".
type FailureKind string

const (
	// KindNotSignedIn: this machine holds no credential the control plane it
	// is pointed at will accept. Sign in.
	KindNotSignedIn FailureKind = "not_signed_in"
	// KindNotApproved: the account has not approved this machine, or has
	// withdrawn the approval. Approve it (again).
	KindNotApproved FailureKind = "device_not_approved"
	// KindEntitlement: the plan has no room — for another Mac, or another
	// connection. Free a slot or change the plan.
	KindEntitlement FailureKind = "entitlement"
	// KindVersionMismatch: the far end speaks another protocol, or is not a
	// Clawdline endpoint at all. Fix the setting or update this build.
	KindVersionMismatch FailureKind = "version_mismatch"
	// KindRelayRefused: the relay would not take this machine's credential or
	// handshake. Usually the api and the relay are different environments.
	KindRelayRefused FailureKind = "relay_refused"
	// KindUnavailable: nothing said no; nothing answered. The line retries.
	KindUnavailable FailureKind = "unavailable"
	// KindUnknown: a word this build has no row for.
	KindUnknown FailureKind = "unknown"
)

// LineFailure is one failure, named.
type LineFailure struct {
	Kind FailureKind `json:"kind"`
	// Code is FailureCode's word: `relay_over_capacity`, `api_no_session`.
	Code string `json:"code"`
	// Detail is the error's own text, which carries the far end's message
	// when it sent one ("this plan connects 1 machine at a time").
	Detail string `json:"detail,omitempty"`
}

// DescribeFailure names err. A nil error, and the two events that are not
// failures at all (a token rotation, the switch being off), answer a zero
// LineFailure.
func DescribeFailure(err error) LineFailure {
	code := FailureCode(err)
	kind := KindOfCode(code)
	if kind == "" {
		return LineFailure{}
	}
	return LineFailure{Kind: kind, Code: code, Detail: err.Error()}
}

// String is the one line the CLI prints and the status route carries.
func (f LineFailure) String() string {
	if f.Kind == "" {
		return ""
	}
	if f.Detail == "" {
		return string(f.Kind) + " (" + f.Code + ")"
	}
	return string(f.Kind) + " (" + f.Code + "): " + f.Detail
}

// Remedy is what to do, in one sentence a person can follow without reading
// the code. The runbook, docs/cloud-cutover.md §6, has the same rows in the
// person's own language.
func (f LineFailure) Remedy() string {
	switch f.Code {
	case "identity_other_environment":
		return "This machine signed in to a different control plane. Run `clawdline cloud login` again against the one the settings name."
	case "login_denied":
		return "The approval was declined in the browser. Run `clawdline cloud login` again if that was a mistake."
	case "relay_forbidden", "relay_revoked", "relay_device_revoked", "relay_account_revoked", "closed_4403":
		return "The account no longer accepts this machine (it was removed or revoked). Run `clawdline cloud login` again and approve it."
	case "identity_binding":
		return "The relay named another account or machine, so nothing was signed. Check that cloud_relay_url is the relay the control plane issues tokens for."
	case "tls_untrusted":
		return "The server's certificate did not verify: a proxy in the middle, a wrong host, or a system clock that is far off. Nothing was sent."
	}
	switch f.Kind {
	case KindNotSignedIn:
		return "Run `clawdline cloud login` and approve the code in the browser (docs/cloud-cutover.md step 2)."
	case KindNotApproved:
		return "Run `clawdline cloud login` again and finish the approval page. If that page said the plan allows no more Macs, this is the entitlement row of docs/cloud-cutover.md, not a fault."
	case KindEntitlement:
		return "The account's plan has no room for this machine; the machine already holding the slot is not affected. Free a slot or change the plan at app.clawdline.com → Plan."
	case KindVersionMismatch:
		return "The endpoint does not speak this build's protocol. Leave cloud_api_base and cloud_relay_url unset for production (or fix them), then update this build if it persists."
	case KindRelayRefused:
		return "The relay refused this machine's credential. Check that cloud_api_base and cloud_relay_url name the same environment and that the system clock is right; the line keeps retrying."
	case KindUnavailable:
		return "The control plane or the relay could not be reached. The line retries by itself; check the network, any proxy, and the system clock."
	case KindUnknown:
		return "This build has no name for this failure; the detail is the far end's own words."
	}
	return ""
}

// KindOfCode is the one mapping from FailureCode's word to its kind. An empty
// answer means "not a failure".
func KindOfCode(code string) FailureKind {
	if code == "" {
		return ""
	}
	if kind, ok := kindByCode[code]; ok {
		return kind
	}
	switch {
	case strings.HasPrefix(code, "api_http_"):
		return kindOfHTTPStatus(strings.TrimPrefix(code, "api_http_"), KindNotSignedIn)
	case strings.HasPrefix(code, "upgrade_refused_"):
		// A plain HTTP answer to the upgrade is never the relay refusing — it
		// refuses in-band, after a 101 (docs/cloud-wire.md §9.1) — so a 401
		// here is a proxy or another server, which is the relay_refused row.
		return kindOfHTTPStatus(strings.TrimPrefix(code, "upgrade_refused_"), KindRelayRefused)
	case strings.HasPrefix(code, "relay_"):
		// The relay said no with a word this table has no row for.
		return KindRelayRefused
	case strings.HasPrefix(code, "closed_4"):
		return KindRelayRefused
	case strings.HasPrefix(code, "closed_"):
		return KindUnavailable
	}
	return KindUnknown
}

// kindOfHTTPStatus names a bare HTTP status. auth is what a 401/403 means at
// that site.
func kindOfHTTPStatus(status string, auth FailureKind) FailureKind {
	switch status {
	case "401", "403":
		return auth
	case "400", "404", "405", "410", "415", "426":
		return KindVersionMismatch
	case "408", "429":
		return KindUnavailable
	}
	if strings.HasPrefix(status, "5") {
		return KindUnavailable
	}
	return KindUnknown
}

// kindByCode is every word FailureCode can answer that has a fixed kind, plus
// the control plane's own codes on the routes this machine calls
// (the Cloud service's auth, token and guard routes,
// services/devices.ts). Two words are deliberately absent: `token_rotation`
// and `switched_off` are events, not failures, and answer "".
var kindByCode = map[string]FailureKind{
	"token_rotation": "",
	"switched_off":   "",

	"no_identity":                KindNotSignedIn,
	"identity_other_environment": KindNotSignedIn,
	"unauthorized":               KindNotSignedIn,
	"api_no_session":             KindNotSignedIn,
	"api_no_machine_credential":  KindNotSignedIn,

	"login_denied":          KindNotApproved,
	"login_expired":         KindNotApproved,
	"login_timeout":         KindNotApproved,
	"relay_forbidden":       KindNotApproved,
	"relay_revoked":         KindNotApproved,
	"relay_device_revoked":  KindNotApproved,
	"relay_account_revoked": KindNotApproved,
	"closed_4403":           KindNotApproved,

	"relay_over_capacity":       KindEntitlement,
	"relay_rate_limited":        KindEntitlement,
	"closed_4429":               KindEntitlement,
	"api_machine_limit_reached": KindEntitlement,
	"api_device_limit_reached":  KindEntitlement,

	"incompatible":          KindVersionMismatch,
	"invalid_token":         KindVersionMismatch,
	"unexpected_frame":      KindVersionMismatch,
	"relay_bad_request":     KindVersionMismatch,
	"relay_malformed":       KindVersionMismatch,
	"closed_4400":           KindVersionMismatch,
	"api_not_found":         KindVersionMismatch,
	"api_bad_machine":       KindVersionMismatch,
	"api_bad_public_key":    KindVersionMismatch,
	"api_bad_field":         KindVersionMismatch,
	"api_bad_request":       KindVersionMismatch,
	"api_unknown_field":     KindVersionMismatch,
	"api_payload_too_large": KindVersionMismatch,

	"relay_unauthorized":      KindRelayRefused,
	"relay_token_superseded":  KindRelayRefused,
	"relay_token_expired":     KindRelayRefused,
	"relay_handshake_timeout": KindRelayRefused,
	"relay_clock_skew":        KindRelayRefused,
	"relay_too_large":         KindRelayRefused,
	"identity_binding":        KindRelayRefused,

	"unreachable":                 KindUnavailable,
	"tls_untrusted":               KindUnavailable,
	"connection_failed":           KindUnavailable,
	"connection_timeout":          KindUnavailable,
	"challenge_timeout":           KindUnavailable,
	"ready_timeout":               KindUnavailable,
	"receive_timeout":             KindUnavailable,
	"not_connected":               KindUnavailable,
	"relay_bad_gateway":           KindUnavailable,
	"relay_internal":              KindUnavailable,
	"relay_unavailable":           KindUnavailable,
	"api_internal":                KindUnavailable,
	"api_temporarily_unavailable": KindUnavailable,

	"relay_unrecognized": KindRelayRefused,
	"api_unrecognized":   KindUnknown,
}
