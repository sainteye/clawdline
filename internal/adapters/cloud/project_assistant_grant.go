package cloud

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

// AssistantViewerGrant is a snapshot of a locally paired viewer's authority.
// It contains no pairing credentials. Its revision identifies authorization
// facts, not a new reader: a scope change must not reset an account's cursor.
type AssistantViewerGrant struct {
	PrincipalID           string
	ReaderID              string
	AuthorizationRevision string
	Writable              bool
}

// ProjectAssistantGrant requires a completed local pairing; unlike ViewerScope,
// it never admits a missing pin through the legacy roster fallback.
//
// The host must supply expectedAccountID from its currently loaded Cloud
// Identity, and verifiedSender from transport/cloud.CloudViewerOf(ctx), which
// carries the authenticated envelope sender. Neither argument may come from a
// browser body or header. This adapter checks the pin, not envelope signatures.
//
// All pin facts are freshly read under the store lock, without changing the
// receiver's cache or relying on its size/mtime stamp. The grant is only a
// snapshot: callers must revalidate authority when applying a protected
// operation. Other processes can replace the file after this call.
func (s *PinnedStore) ProjectAssistantGrant(expectedAccountID, verifiedSender string) (AssistantViewerGrant, error) {
	if s == nil || expectedAccountID == "" || verifiedSender == "" {
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
	if snapshot.record.AccountID != expectedAccountID {
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
	if !found || !pin.Active() || pin.PairingPending || pin.PairedAt <= 0 {
		return AssistantViewerGrant{}, ErrViewerNotPinned
	}
	scope := pin.EffectiveScope()
	if !ValidViewerScope(scope) {
		return AssistantViewerGrant{}, ErrViewerScope
	}
	key, ok := pin.Key()
	if !ok || pin.Fingerprint == "" || domain.Fingerprint(key) != pin.Fingerprint {
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
	}, nil
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
