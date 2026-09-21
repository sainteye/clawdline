package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devices"
	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// The /v1/push/* routes, which are the Swift app's (`RemoteServer.swift`, the
// four cases under "Subscribing is read-level, deliberately").
//
// **Subscribing does not go through the write gate.** That gate is about typing
// into somebody's session, and asking to be told when one needs an answer is
// the opposite of that: it is the reading half arriving by a different road. A
// phone paired read-only is exactly the device this feature exists for.
//
// Everything a browser hands over is checked before it is believed. `endpoint`
// is a URL this machine will POST to from inside its own network whenever a
// session changes state, so an unchecked one is a request-forgery primitive
// handed to whoever holds a token. The checking is in
// internal/adapters/push.FromBrowser, beside the reason for it.

const (
	// pushTestTitle and pushTestBody are the Swift app's `"Clawdline"` and
	// `L.t.pushTest` (`Copy+Chinese.swift`). They are written here rather than
	// read from the console's catalog because this is a *server* string: the
	// catalog under web/console is the page's, and nothing in this daemon
	// speaks a language yet. When it does, this is one of the strings that
	// moves.
	pushTestTitle = "Clawdline"
	pushTestBody  = "這是一則測試通知，該接的都接好了。"

	// pushSendDeadline is how long a route that owes its caller a delivery
	// result will wait for the fan-out. Every individual request already has
	// its own timeout and the retry budget bounds the rest; this is the outer
	// edge, so a push service having a very bad afternoon cannot hold a
	// browser's request open indefinitely.
	pushSendDeadline = 40 * time.Second
)

// pushState is one Store and Sender per state directory, made on first use.
//
// Per directory rather than per Server for the reason gate.go gives: the
// authority behind it is the only writer of that directory's files, and a
// second one would be a second writer.
var (
	pushStores sync.Map // dir -> *pushHold
	pushMu     sync.Mutex
)

type pushHold struct {
	store *adapterpush.Store
	err   error
}

func (s *Server) push() (*adapterpush.Store, error) {
	if held, ok := pushStores.Load(s.cfg.Dir); ok {
		h := held.(*pushHold)
		return h.store, h.err
	}
	pushMu.Lock()
	defer pushMu.Unlock()
	if held, ok := pushStores.Load(s.cfg.Dir); ok {
		h := held.(*pushHold)
		return h.store, h.err
	}
	store, err := adapterpush.Open(s.cfg.Dir, swiftDirs()...)
	if err != nil {
		log.Printf("push: the store at %s could not be opened: %v", s.cfg.Dir, err)
	} else {
		// A new subscription is refused at the register's limit.
		store.SetLimit(CapacityLimit(capacity.PushSubscriptions))
	}
	pushStores.Store(s.cfg.Dir, &pushHold{store: store, err: err})
	return store, err
}

// pushRoute is every /v1/push/ path. One handler rather than four
// registrations, so the four names are read in one place and an unknown one is
// a 404 rather than a prefix match on the nearest.
func (s *Server) pushRoute(w http.ResponseWriter, r *http.Request) {
	p := routePath(r)
	get := r.Method == http.MethodGet || r.Method == http.MethodHead
	post := r.Method == http.MethodPost
	switch {
	case get && p == "/v1/push/key":
		s.pushKeyRoute(w, r)
	case post && p == "/v1/push/subscribe":
		s.pushSubscribeRoute(w, r)
	case post && p == "/v1/push/test":
		s.pushTestRoute(w, r)
	case post && p == "/v1/push/unsubscribe":
		s.pushUnsubscribeRoute(w, r)
	default:
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "No such route")
	}
}

// pushKeyRoute hands over the application server key.
//
// Asking for it is what mints one, and that is deliberate: the key is an
// identity, and a machine nobody has ever asked to be notified by should not be
// growing key material. The page asks for it at the moment somebody presses
// "notify me", and not before.
func (s *Server) pushKeyRoute(w http.ResponseWriter, r *http.Request) {
	store, err := s.push()
	if err != nil {
		writePushStoreFailure(w, err)
		return
	}
	key, err := store.VAPIDKey()
	if err != nil {
		writePushStoreFailure(w, err)
		return
	}
	writeJSON(w, contract.PushKey{Key: key.PublicKey()})
}

func (s *Server) pushSubscribeRoute(w http.ResponseWriter, r *http.Request) {
	device, ok := pushDevice(w, r)
	if !ok {
		return
	}
	store, err := s.push()
	if err != nil {
		writePushStoreFailure(w, err)
		return
	}
	// Validated rather than stored as given. See FromBrowser.
	subscription, ok := adapterpush.FromBrowser(readBody(r), newPushID(), device,
		pushWebAppOrigin(r))
	if !ok {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That is not a usable push subscription.")
		return
	}
	if err := store.Add(subscription); err != nil {
		writePushStoreFailure(w, err)
		return
	}
	// In the audit log with everything else: a new subscription is a new
	// destination for notices about what somebody is working on, which is
	// exactly the kind of change the question "what did they do while they
	// were in" needs an answer for.
	s.audit("push.subscribe", map[string]string{
		"id": subscription.ID, "device": device, "host": subscription.Host(),
	})
	writeJSON(w, contract.PushSubscribed{OK: true, ID: subscription.ID})
}

// pushWebAppOrigin is the web app this subscription belongs to — the page that
// will be opened when somebody taps a notification sent to it, months from
// now.
//
// **That is not the same question as "where did this request come from", even
// though one header was answering both.** For a browser they coincide: the
// page asking to be notified is the page the person is looking at, and its
// Origin says so. For a Cloud viewer they do not. The request is dispatched in
// this process on the person's behalf, and `internal/transport/cloud`'s
// `LocalAuthorizer` stamps `Origin: http://127.0.0.1` on it, which is the
// truthful answer to the CSRF question and the wrong answer to this one: the
// person is looking at `app.clawdline.com` on a phone, where `127.0.0.1` opens
// nothing. Stored, that address goes into every notification this daemon later
// sends — an iPhone's endpoint is `push.apple.com`, so a row with an origin
// takes Apple's declarative envelope and its `navigate` is resolved against
// exactly this value, and WebKit never wakes `sw.js` to give the tap a second
// chance.
//
// So the Cloud line says which console it is answering for, in the request's
// context where nothing arriving over a socket can put it (`WithAppOrigin`),
// and it is the settings' `cloud_app_origin` — a person who pointed this
// machine at their own console is followed there. Every other request is a
// browser speaking for itself and is read exactly as before.
func pushWebAppOrigin(r *http.Request) string {
	if console := cloudtransport.AppOriginOf(r.Context()); console != "" {
		return console
	}
	return r.Header.Get("Origin")
}

// pushTestRoute sends one notification to the asking device, and waits for the
// receipt.
//
// Read-level, like subscribing: this reaches nobody but the person who asked,
// and it is the only way to answer "did that work" without waiting for a
// session to need you — which is a long way to go to find out whether a key was
// minted correctly.
func (s *Server) pushTestRoute(w http.ResponseWriter, r *http.Request) {
	device, ok := pushDevice(w, r)
	if !ok {
		return
	}
	store, err := s.push()
	if err != nil {
		writePushStoreFailure(w, err)
		return
	}
	mine, err := store.ForDevice(device)
	if err != nil {
		writePushStoreFailure(w, err)
		return
	}
	if len(mine) == 0 {
		writeAuthRefusal(w, http.StatusConflict, "not_subscribed",
			"This device has not asked for notifications yet.")
		return
	}
	// The words are what keeps a test a test: a phone shows `Clawdline` over
	// the test sentence, which no session has ever been called. **Only the
	// address moves.** What an optional session_id buys is the other half of
	// the question this route exists to answer — not only *did a notification
	// arrive* but *does tapping one get me back to my session* — and that half
	// was unaskable while every test push went to the list.
	body := readBody(r)
	asked, _ := body["session_id"].(string)
	url := s.pushSessionURL(r.Context(), asked)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), pushSendDeadline)
	defer cancel()
	sender := &adapterpush.Sender{Store: store}
	// No tag, and no mark. A test that arrived must never be mistaken for a
	// session that needs you, so it carries no project and nothing that would
	// replace a real notification already on the phone.
	delivery, err := sender.Send(ctx, adapterpush.Notification{
		Title: pushTestTitle, Body: pushTestBody, URL: url,
	}, device)
	if err != nil {
		writePushStoreFailure(w, err)
		return
	}
	s.audit("push.test", map[string]string{"device": device, "url": url})
	writeJSON(w, contract.PushSent{
		OK: delivery.Sent > 0, Sent: int64(delivery.Sent), Failed: int64(delivery.Failed),
	})
}

// pushUnsubscribeRoute takes a subscription back.
//
// The id names a row this machine wrote, and it is only removed when it belongs
// to the device asking — otherwise one paired browser could quietly stop
// another one's notifications by naming its id. This machine's own token is
// exempt, because it is how a script cleans up after itself.
func (s *Server) pushUnsubscribeRoute(w http.ResponseWriter, r *http.Request) {
	device, ok := pushDevice(w, r)
	if !ok {
		return
	}
	store, err := s.push()
	if err != nil {
		writePushStoreFailure(w, err)
		return
	}
	id, _ := readBody(r)["id"].(string)
	if id == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs an id.")
		return
	}
	rows, err := store.Subscriptions()
	if err != nil {
		writePushStoreFailure(w, err)
		return
	}
	local := accessOf(r).verdict.Local
	for _, row := range rows {
		if row.ID != id {
			continue
		}
		if row.Device != device && !local {
			// Answered as though it were not there, because it is not this
			// device's: telling one browser that another one's subscription
			// exists is the disclosure, not the removal.
			break
		}
		if _, _, err := store.Remove(id); err != nil {
			writePushStoreFailure(w, err)
			return
		}
		s.audit("push.unsubscribe", map[string]string{"id": id, "device": row.Device})
		break
	}
	// Ok either way. Unsubscribing twice is what a reload of the page looks
	// like, and a browser that has already dropped its own subscription must
	// not be told its cleanup failed.
	writeJSON(w, contract.PushOK{OK: true})
}

// PushSend is the seam for everything that is not the test button: a session
// that started waiting, a schedule that failed, a task that was delivered.
//
// It is here rather than in the adapter because the decision "who is told" is
// this daemon's and not the transport's, and because nothing else may mint the
// store. Callers pass a session id when the notification is about one, so the
// tap lands on that session rather than on the list.
func (s *Server) PushSend(ctx context.Context, title, body, sessionID, tag, icon string) (adapterpush.Delivery, error) {
	store, err := s.push()
	if err != nil {
		return adapterpush.Delivery{}, err
	}
	url := "/"
	if sessionID != "" {
		url = s.pushSessionURL(ctx, sessionID)
	}
	sender := &adapterpush.Sender{Store: store}
	return sender.Send(ctx, adapterpush.Notification{
		Title: title, Body: body, URL: url, Tag: tag, Icon: icon,
	}, "")
}

// pushSessionURL is the Swift app's `Orchestrator.pushURL(forSessionID:watching:)`:
// the address of a session this machine is actually watching, and otherwise the
// list.
//
// Checked before it is promised. The id comes from a browser naming what it
// happens to have open, and an address that opens nothing would be a worse
// answer than the list.
func (s *Server) pushSessionURL(ctx context.Context, id string) string {
	if id == "" {
		return "/"
	}
	read, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, row := range s.screen(read) {
		if row.TerminalID == id {
			return adapterpush.SessionURL(id)
		}
	}
	return "/"
}

// pushDevice is the paired device asking, or a refusal in the Swift app's
// words. Every one of these routes needs one: the gate has already required a
// token, and this is the name behind it.
func pushDevice(w http.ResponseWriter, r *http.Request) (string, bool) {
	v := accessOf(r).verdict
	if !v.Allowed || v.Device == "" {
		writeAuthRefusal(w, http.StatusUnauthorized, "unauthorized", "This needs a paired device.")
		return "", false
	}
	return v.Device, true
}

// audit records one event in whichever of the two records it belongs to
// (devices.SecurityEvent, design-decisions D25): the security audit, which is
// the file the device routes write, or the operational journal, which is the
// store's `events`. An event on neither list is kept in the security audit,
// where nothing is deleted. A record that could not be written is not a reason
// to fail the request that already happened; each writer counts its own
// failures.
func (s *Server) audit(event string, fields map[string]string) {
	if devices.JournalEvent(event) {
		payload, err := json.Marshal(fields)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = s.store.Append(ctx, store.Event{Kind: event, Subject: fields["schedule"], Payload: payload})
		}
		if err != nil {
			log.Printf("journal: could not record %s: %v", event, err)
		}
		return
	}
	if g := s.gate(); g != nil && g.files != nil {
		g.files.Audit(event, fields)
	}
}

// newPushID names one stored subscription. It is this machine's name for a row
// and never a secret: the capability is the endpoint, which stays on this disk.
func newPushID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// A machine whose randomness has failed must not fall back to a
		// guessable name; there is nothing useful left to do here.
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b)
}

// writePushStoreFailure is the refusal for a push store that could not be read
// or written. It never repeats the underlying path or error to a browser — that
// is what the log is for — and it is deliberately not a 404: "there is nothing
// subscribed" and "this machine cannot tell" are different answers.
func writePushStoreFailure(w http.ResponseWriter, err error) {
	log.Printf("push: %v", err)
	switch {
	case errors.Is(err, adapterpush.ErrSubscriptionsFull), errors.Is(err, adapterpush.ErrSubscriptionsTooLarge):
		// Full, not broken: the register's `push.subscriptions` row, which
		// only a person makes room in.
		writeAuthRefusal(w, http.StatusInsufficientStorage, "subscriptions_full",
			"This machine already notifies as many devices as it keeps. Turn notifications off on one you no longer use, then try again.")
	case errors.Is(err, adapterpush.ErrNotRegular), errors.Is(err, adapterpush.ErrUnreadable),
		errors.Is(err, adapterpush.ErrForeignDir):
		writeAuthRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
			"The notification store could not be read.")
	default:
		writeAuthRefusal(w, http.StatusInternalServerError, "internal",
			"That could not be done.")
	}
}
