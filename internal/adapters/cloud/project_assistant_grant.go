package cloud

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

// AssistantViewerGrant is a snapshot of a Cloud viewer's Bot identity and scope.
// It contains no pairing credentials. Its revision identifies authorization
// facts, not a new reader: a scope change must not reset an account's cursor.
type AssistantViewerGrant struct {
	PrincipalID           string
	ReaderID              string
	AuthorizationRevision string
	Writable              bool
	Invited               bool
}

// VerifiedProjectAssistantGrant resolves an assistant identity after the Cloud
// transport has rechecked the signed viewer's account and machine-key access.
// The verified key supports a legacy account pairing with no local pin. Every
// stored pin is freshly read, and an invalid or refused pin takes precedence.
func (s *PinnedStore) VerifiedProjectAssistantGrant(expectedAccountID, verifiedSender string, verifiedKey ed25519.PublicKey) (AssistantViewerGrant, error) {
	if s == nil || expectedAccountID == "" || verifiedSender == "" || len(verifiedKey) != ed25519.PublicKeySize ||
		strings.TrimSpace(verifiedSender) != verifiedSender {
		return AssistantViewerGrant{}, ErrViewerNotPinned
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Reuse the pin parser with an unloaded snapshot. Only this API bypasses
	// the stamp cache; existing PinnedStore readers keep their own semantics.
	snapshot := PinnedStore{dir: s.dir}
	if err := snapshot.loadLocked(); err != nil {
		return AssistantViewerGrant{}, err
	}
	if snapshot.record.AccountID != expectedAccountID && !snapshot.stamp.missing {
		return AssistantViewerGrant{}, ErrViewerNotPinned
	}
	var pin PinnedDevice
	found := false
	for _, row := range snapshot.record.Devices {
		if row.DeviceID != verifiedSender {
			continue
		}
		if found {
			return AssistantViewerGrant{}, ErrPinnedUnreadable
		}
		pin, found = row, true
	}
	if !found {
		principal, reader := ProjectAssistantOwnerIdentity(expectedAccountID)
		return AssistantViewerGrant{
			PrincipalID: principal, ReaderID: reader, Writable: true,
			AuthorizationRevision: assistantGrantDigest("legacy-authorization", expectedAccountID, verifiedSender, string(verifiedKey)),
		}, nil
	}
	if !pin.Active() || pin.PairingPending || pin.PairedAt <= 0 {
		return AssistantViewerGrant{}, ErrViewerNotPinned
	}
	scope := pin.EffectiveScope()
	if !ValidViewerScope(scope) {
		return AssistantViewerGrant{}, ErrViewerScope
	}
	key, ok := pin.Key()
	if !ok || pin.Fingerprint == "" || domain.Fingerprint(key) != pin.Fingerprint || !bytes.Equal(key, verifiedKey) {
		return AssistantViewerGrant{}, ErrPinnedUnreadable
	}
	// Only account-paired devices share the owner. Invitations have their own
	// namespace, even if a sender's spelling happens to equal an account id.
	identity := []string{"owner", snapshot.record.AccountID}
	if pin.Invited {
		identity = []string{"invited", snapshot.record.AccountID, pin.DeviceID}
	}
	return AssistantViewerGrant{
		PrincipalID: assistantGrantDigest("principal", identity...),
		ReaderID:    assistantGrantDigest("reader", identity...),
		AuthorizationRevision: assistantGrantDigest("authorization",
			snapshot.record.AccountID, pin.DeviceID, pin.PublicKey, pin.Fingerprint,
			pin.Scope, strconv.FormatInt(pin.PairedAt, 10),
			strconv.FormatInt(pin.RevokedAt, 10), strconv.FormatInt(pin.ExpiresAt, 10),
			strconv.FormatBool(pin.Invited), strconv.FormatBool(pin.PairingPending)),
		Writable: scope == ScopeOperate,
		Invited:  pin.Invited,
	}, nil
}

// ProjectAssistantOwnerIdentity derives account-owner namespaces only. It is
// not a grant: callers still need current gate proof and current source policy.
func ProjectAssistantOwnerIdentity(accountID string) (principal, reader string) {
	if accountID == "" {
		return "", ""
	}
	return assistantGrantDigest("principal", "owner", accountID), assistantGrantDigest("reader", "owner", accountID)
}

// Length framing prevents account/sender boundary ambiguity. Domains and the
// format version keep principals, cursors and authorization snapshots separate.
func assistantGrantDigest(domain string, parts ...string) string {
	h := sha256.New()
	for _, part := range append([]string{"project-assistant-grant-v1", domain}, parts...) {
		h.Write([]byte(strconv.Itoa(len(part)) + ":"))
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}
