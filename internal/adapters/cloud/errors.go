package cloud

import (
	"encoding/base64"
	"errors"
	"fmt"
)

// The transport's own failures, `CloudTransport.swift:32-59`. Each is a
// separate sentinel because each is a different thing to do about it, and
// because FailureCode turns them into the words an operator greps for.
var (
	// ErrUnauthorized is the credential being refused — by the token
	// endpoint, or by a 401/403 at the upgrade. It is terminal.
	ErrUnauthorized = errors.New("the cloud credential was refused")
	// ErrConnectionTimeout is the token fetch plus socket open not finishing
	// inside OpeningTimeout.
	ErrConnectionTimeout = errors.New("the connection did not open in time")
	// ErrChallengeTimeout is the relay not sending its challenge.
	ErrChallengeTimeout = errors.New("the relay sent no challenge in time")
	// ErrReadyTimeout is the relay not answering the signed challenge.
	ErrReadyTimeout = errors.New("the relay did not answer the handshake in time")
	// ErrReceiveTimeout is ReceiveTimeout passing with no frame at all.
	ErrReceiveTimeout = errors.New("the relay went quiet")
	// ErrTokenRotated is this side closing its own socket to pick up a new
	// token. It is a normal event with a name of its own, because without one
	// a healthy machine logs `connection_failed` every four minutes and
	// somebody eventually goes looking for a fault that is not there
	// (`CloudTransport.swift:48-55`).
	ErrTokenRotated = errors.New("the device token was rotated")
	// ErrIdentityBinding is the challenge naming an account or device that is
	// not the one this machine is pinned to. Signing it anyway would let a
	// relay borrow this device's signature for another identity.
	ErrIdentityBinding = errors.New("the challenge does not match this machine's identity")
	// ErrNotConnected is an attempt to publish with no open socket.
	ErrNotConnected = errors.New("not connected to the relay")
	// ErrDisabled is the line being off in settings, which is the default.
	ErrDisabled = errors.New("the cloud connection is switched off")
	// ErrNoIdentity is the machine not having registered yet.
	ErrNoIdentity = errors.New("this machine has no cloud identity yet")
	// ErrInvalidToken is a token endpoint answer that cannot be used.
	ErrInvalidToken = errors.New("the token response is unusable")

	// ErrIncompatible is the far end answering in a shape this build does not
	// speak: a challenge at another protocol version, a control plane that
	// answers HTML, a poll status nobody wrote down. It is its own sentinel
	// because the remedy — point at the right endpoint, or update this build —
	// is nothing like the remedy for a refused credential.
	ErrIncompatible = errors.New("the endpoint does not speak this build's protocol")
	// ErrLoginDenied is the person declining the approval in the browser.
	ErrLoginDenied = errors.New("the approval was declined in the browser")
	// ErrLoginExpired is the one-time code expiring before anybody approved it.
	// The Mac cannot see *why* nobody did: an approval page that refused
	// because the plan is full (`machine_limit_reached`) looks exactly like a
	// person who never opened it, so the remedy names both.
	ErrLoginExpired = errors.New("the one-time code expired before it was approved")
	// ErrLoginTimeout is this command's own wait running out first.
	ErrLoginTimeout = errors.New("nobody approved this machine in time")
	// ErrOtherEnvironment is an identity minted by one control plane meeting
	// settings that name another. Its credential is meaningless there, and
	// finding that out as `unauthorized` from the far end is the slow way.
	ErrOtherEnvironment = errors.New("this machine signed in to a different control plane")
)

// CheckEnvironment refuses an identity that belongs to a control plane other
// than the one the settings name. An identity that recorded no control plane
// (written before the field existed) is accepted, as it always was.
func CheckEnvironment(identity Identity, settings Settings) error {
	if identity.APIBase == "" || identity.APIBase == settings.APIBase {
		return nil
	}
	return fmt.Errorf("%w: it registered with %s, the settings say %s",
		ErrOtherEnvironment, identity.APIBase, settings.APIBase)
}

// decodeStdBase64 is the relay's flavour: the standard alphabet with canonical
// padding, nothing else. Go's base64.StdEncoding accepts a few strings the
// relay would refuse, so length and re-encoding are checked as the domain
// package does for envelope fields.
func decodeStdBase64(text string) ([]byte, error) {
	if text == "" || len(text)%4 != 0 {
		return nil, fmt.Errorf("not canonical padded base64: %q", text)
	}
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, err
	}
	if base64.StdEncoding.EncodeToString(raw) != text {
		return nil, fmt.Errorf("not canonical padded base64: %q", text)
	}
	return raw, nil
}
