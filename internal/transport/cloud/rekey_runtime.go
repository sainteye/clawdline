package cloud

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

const RawRekeyIntervalSecondsLimit = 2

// runtimeAuthority orders key disclosure, final pairing pins and revocation.
// Network callbacks execute under mu, so no overall latency bound is claimed.
type runtimeAuthority struct {
	mu         sync.Mutex
	generation atomic.Uint64
	admission  sync.Mutex
	last       time.Time
}

func (l *Link) retireContent(old, next domaincloud.ContentKeyState) error {
	if err := l.relay.Spool.RetireForCutover(old, next); err != nil {
		return err
	}
	// Closing peers retires SCTP's accepted-but-not-delivered bytes too.
	l.directMu.Lock()
	peers := make([]*directPeer, 0, len(l.directPeers))
	for viewer, peer := range l.directPeers {
		if peer != nil {
			peer.mu.Lock()
			peer.closed = true
			peer.open = false
			peer.mu.Unlock()
			peers = append(peers, peer)
		}
		delete(l.directPeers, viewer)
	}
	l.directMu.Unlock()
	// Do not return cutover success while the direct stack still owns old
	// queued data. No terminal lock is held; callbacks see already-closed peers.
	var closeErr error
	for _, peer := range peers {
		if peer.pc != nil {
			if err := peer.pc.Close(); err != nil && closeErr == nil {
				closeErr = err
			}
		}
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func (l *Link) resumeRevocation(r cloudkeys.RevocationIntent) error {
	recoverRotation := func() (domaincloud.ContentKeyState, error) {
		if _, err := l.pinned.Revoke(r.Viewer, l.opts.Now()); err != nil {
			return domaincloud.ContentKeyState{}, err
		}
		if _, err := l.keys.RecoverContentKey(); err != nil {
			return domaincloud.ContentKeyState{}, err
		}
		return l.keys.RotateContentKey(r.Operation, r.ExpectedEpoch, rand.Reader)
	}
	_, err := l.contentEpoch.Recover(recoverRotation, l.retireContent)
	if err != nil {
		return err
	}
	if err := l.revokeTerminalViewer(r.Viewer); err != nil {
		return err
	}
	l.closeDirectPeerOf(r.Viewer, "its local pin was revoked")
	if err := l.keys.CompleteRevocation(r); err != nil {
		return err
	}
	l.restateContentKey()
	return nil
}

func (l *Link) revokeContentViewer(viewer string) (bool, error) {
	return l.revokeContentViewerWithJournal(viewer, l.keys.BeginRevocation)
}

// The journal callback permits deterministic persistence-failure tests across
// the adapter boundary. Production always uses Files.BeginRevocation.
func (l *Link) revokeContentViewerWithJournal(viewer string, begin func(string, uint64) (cloudkeys.RevocationIntent, error)) (bool, error) {
	l.contentAuthority.mu.Lock()
	defer l.contentAuthority.mu.Unlock()
	if r, pending, err := l.keys.PendingRevocation(); err != nil {
		// An unreadable journal may already contain a durable deny intent.
		_, halted := l.contentEpoch.Recover(func() (domaincloud.ContentKeyState, error) {
			return domaincloud.ContentKeyState{}, err
		}, l.retireContent)
		return false, halted
	} else if pending {
		if err := l.resumeRevocation(r); err != nil {
			return false, err
		}
		if r.Viewer == viewer {
			return true, nil
		}
	}
	rows, err := l.pinned.Devices()
	if err != nil {
		return false, err
	}
	active := false
	for _, row := range rows {
		if row.DeviceID == viewer && row.Active() {
			active = true
		}
	}
	// Even an unpinned viewer may have an approved handover in flight.
	// A revoke attempt invalidates that generation before returning no active pin.
	l.contentAuthority.generation.Add(1)
	if !active {
		return false, nil
	}
	var r cloudkeys.RevocationIntent
	// Fence journal creation too: a failed directory sync can follow rename.
	// Recover also permits a retry when creation failed before any intent existed.
	_, err = l.contentEpoch.Recover(func() (domaincloud.ContentKeyState, error) {
		state, err := l.keys.RecoverContentKey()
		if err != nil {
			return domaincloud.ContentKeyState{}, err
		}
		r, err = begin(viewer, state.Epoch)
		if err != nil {
			return domaincloud.ContentKeyState{}, err
		}
		if _, err := l.pinned.Revoke(viewer, l.opts.Now()); err != nil {
			return domaincloud.ContentKeyState{}, err
		}
		return l.keys.RotateContentKey(r.Operation, r.ExpectedEpoch, rand.Reader)
	}, l.retireContent)
	if err != nil {
		return true, err
	}
	if err := l.revokeTerminalViewer(viewer); err != nil {
		return true, err
	}
	l.closeDirectPeerOf(viewer, "its local pin was revoked")
	if err := l.keys.CompleteRevocation(r); err != nil {
		return true, err
	}
	l.restateContentKey()
	return true, nil
}

// enqueueRekey is called by the adapter's bounded raw worker, never the reader.
// The global interval bounds fresh roster I/O even for duplicate signed offers.
func (l *Link) enqueueRekey(raw []byte) error {
	if l.contentAuthority == nil || len(raw) > domaincloud.MaxRekeyBodyBytes {
		return domaincloud.ErrRekey
	}
	a := l.contentAuthority
	a.admission.Lock()
	if !a.last.IsZero() && l.opts.Now().Sub(a.last) < RawRekeyIntervalSecondsLimit*time.Second {
		a.admission.Unlock()
		return adaptercloud.ErrRekeyOverload
	}
	a.last = l.opts.Now()
	a.admission.Unlock()
	return l.answerRekey(raw, l.transport.PublishRekeyAnswer)
}

func (l *Link) answerRekey(raw []byte, send func([]byte) error) error {
	a := l.contentAuthority
	if a == nil {
		return domaincloud.ErrRekey
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var offer domaincloud.RekeyOffer
	if len(raw) > domaincloud.MaxRekeyBodyBytes || json.Unmarshal(raw, &offer) != nil {
		return domaincloud.ErrRekey
	}
	key, err := l.pinned.ActiveRekeyPin(l.identity.AccountID, offer.ViewerDeviceID)
	if err != nil {
		return err
	}
	authority := domaincloud.RekeyAuthority{AccountID: l.identity.AccountID, MachineID: l.identity.MachineID, ViewerDeviceID: offer.ViewerDeviceID, ActiveViewerKey: key}
	if _, err := domaincloud.VerifyRekeyOffer(raw, authority, l.opts.Now().UnixMilli()); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), CloudTerminalRosterDeadlineLimit*time.Second)
	defer cancel()
	if err := l.roster.Refresh(ctx); err != nil {
		return err
	}
	allowed := false
	for _, row := range l.roster.Devices() {
		if row.ID != offer.ViewerDeviceID || (row.RevokedAt != nil && *row.RevokedAt != "") {
			continue
		}
		key, ok := l.roster.PublicKeyFor(row.ID)
		if !ok || !bytes.Equal(key, authority.ActiveViewerKey) {
			continue
		}
		for _, cap := range row.Caps {
			if cap == "read_transcript" {
				allowed = true
			}
		}
	}
	if !allowed {
		return domaincloud.ErrRekey
	}
	machine, err := domaincloud.LoadOrCreateDeviceKey(l.keys, rand.Reader)
	if err != nil {
		return err
	}
	ephemeral, err := domaincloud.NewX25519PrivateKey(rand.Reader)
	if err != nil {
		return err
	}
	nonce := make([]byte, domaincloud.NonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	return l.contentEpoch.Use(func(state domaincloud.ContentKeyState) error {
		// Re-read the durable pin immediately before disclosure, including edits
		// made outside this daemon's authority sequence.
		key, err := l.pinned.ActiveRekeyPin(l.identity.AccountID, offer.ViewerDeviceID)
		if err != nil || !bytes.Equal(key, authority.ActiveViewerKey) {
			return domaincloud.ErrRekey
		}
		answer, err := domaincloud.SealRekeyAnswer(raw, authority, state, machine, ephemeral, nonce, l.opts.Now().UnixMilli())
		if err != nil {
			return err
		}
		if send == nil {
			return errors.New("raw rekey sender unavailable")
		}
		return send(answer)
	})
}

func (l *Link) rawRekeyCallback() func([]byte) error {
	if l.contentEpoch == nil {
		return nil
	}
	return l.enqueueRekey
}

func (l *Link) RekeyPendingReading() capacity.Reading {
	if l.contentAuthority == nil {
		return capacity.Reading{Known: true, Note: "content-key runtime is disabled"}
	}
	return capacity.Reading{Known: true, Used: int64(l.transport.RekeyPending())}
}

// Publisher owns its change maps; only request its existing full-restate path.
// This debt is consumed by its next normal pass, without a second worker/queue.
func (l *Link) restateContentKey() {
	if l.publisher == nil {
		return
	}
	l.publisher.mu.Lock()
	l.publisher.restate = true
	l.publisher.viewerAt = l.opts.Now()
	l.publisher.mu.Unlock()
}
