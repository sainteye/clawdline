package push

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sender posts one notification to every subscription that should have it.
type Sender struct {
	Store *Store
	// Client is the one that reaches the push services. A nil one is the
	// package default, which has the per-request timeout below.
	Client *http.Client
	// Subject is the `sub` claim. Empty means the package's own.
	Subject string
	// Attempts is how many times one subscription is tried before a retryable
	// failure is given up on. Zero means DefaultAttempts.
	Attempts int
	// Now and Sleep exist so the backoff can be exercised without the test
	// taking as long as the thing it is testing.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) bool
	Log   func(format string, args ...any)
}

const (
	// RequestTimeout is shorter than Go's default of none at all. A push
	// service that has not answered in fifteen seconds is not going to make
	// this notification timely, and the request is holding a session.
	RequestTimeout = 15 * time.Second
	// DefaultAttempts is one try and two retries.
	//
	// **Retrying at all is a departure from the Swift app, which sends once.**
	// The thing it buys is the failure this feature actually has on a laptop:
	// a push service answering 503 or a Wi-Fi handover swallowing one request
	// is not evidence about the subscription, and the Swift app's answer to it
	// is a notification that simply never arrives with a line in a log nobody
	// reads. The thing it must not buy is a retry storm, so it is bounded
	// three ways: a small fixed count, an exponential delay with jitter, and a
	// whole-send budget past which nothing is tried again.
	DefaultAttempts = 3
	// RetryBase is the first backoff. It doubles, with full jitter.
	RetryBase = time.Second
	// RetryCeiling caps one wait.
	RetryCeiling = 8 * time.Second
	// RetryBudget caps every wait in one subscription's send put together, so
	// a route that waits for a receipt has a bound that does not depend on
	// what a push service felt like answering.
	RetryBudget = 30 * time.Second
	// RetryAfterCeiling is the longest `Retry-After` this will honour. Past
	// that the message is stale before the retry lands, so it is given up on
	// rather than queued.
	RetryAfterCeiling = 60 * time.Second
)

// ErrNoIdentity is a send asked for before any browser has subscribed. It is
// not a failure to report to a person: it is the state a machine is in before
// anybody has asked to be told anything.
var ErrNoIdentity = errors.New("no VAPID identity, because nothing has subscribed")

func (s *Sender) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sender) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (s *Sender) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: RequestTimeout}
}

func (s *Sender) subject() string {
	if s.Subject != "" {
		return s.Subject
	}
	return Subject
}

func (s *Sender) attempts() int {
	if s.Attempts > 0 {
		return s.Attempts
	}
	return DefaultAttempts
}

// sleep waits, and reports whether the wait finished rather than the context
// ending underneath it.
func (s *Sender) sleep(ctx context.Context, d time.Duration) bool {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Send tells every subscribed browser something, or — with device — only what
// that one paired device subscribed with.
//
// The filter exists for the test button. **Pressing "send me one" on a phone
// should buzz that phone**, not every device anybody has ever paired: a test
// whose blast radius is larger than the thing being tested teaches you to be
// careful with it, which is the opposite of what a test button is for.
func (s *Sender) Send(ctx context.Context, n Notification, device string) (Delivery, error) {
	var (
		targets []Subscription
		err     error
	)
	if device == "" {
		targets, err = s.Store.Subscriptions()
	} else {
		targets, err = s.Store.ForDevice(device)
	}
	if err != nil {
		return Delivery{}, err
	}
	if len(targets) == 0 {
		// Nothing to do, and in particular no VAPID key minted: a machine that
		// has never had a browser subscribe should not be growing key material
		// because a session changed state.
		return Delivery{}, nil
	}
	key, err := s.Store.VAPIDKey()
	if err != nil {
		return Delivery{}, err
	}
	// One expiry for the whole fan-out. Endpoints on different hosts need
	// different `aud` claims, so the token is re-signed per subscriber — but
	// the expiry is taken once, so a slow first request cannot leave the last
	// one with a token that has already lapsed.
	expires := s.now().Add(TokenLifetime)

	var (
		mu       sync.Mutex
		delivery Delivery
		wait     sync.WaitGroup
	)
	for _, target := range targets {
		wait.Add(1)
		go func(subscription Subscription) {
			defer wait.Done()
			accepted := s.one(ctx, n, subscription, key, expires)
			mu.Lock()
			if accepted {
				delivery.Sent++
			} else {
				delivery.Failed++
			}
			mu.Unlock()
		}(target)
	}
	wait.Wait()
	s.logf("push: %s → %d sent, %d failed", n.Title, delivery.Sent, delivery.Failed)
	return delivery, nil
}

// one is a single subscription's whole send, retries and all.
func (s *Sender) one(ctx context.Context, n Notification, subscription Subscription,
	key VAPIDKey, expires time.Time) bool {
	message, shortened, ok := Build(n, subscription)
	if !ok {
		s.logf("push: could not serialise the payload for %s", subscription.Device)
		return false
	}
	if shortened {
		s.logf("push: the body was shortened to fit %d octets for %s", MaxPayload, subscription.Device)
	}
	credential, err := key.Authorization(subscription.Endpoint, expires, s.subject())
	if err != nil {
		s.logf("push: %s — %v", subscription.Device, err)
		return false
	}
	var topic string
	if n.Tag != "" {
		topic = Topic(n.Tag)
	}

	spent := time.Duration(0)
	for attempt := 1; ; attempt++ {
		outcome, retryAfter := s.post(ctx, message, subscription, credential, topic)
		switch outcome {
		case accepted:
			return true
		case gone:
			// RFC 8030 §7.3: the subscription resource is gone. The browser was
			// uninstalled, the permission was revoked, or the service expired
			// it — and none of those come back. Retrying it forever means a
			// request per state change, for the life of the daemon, to a URL
			// that will answer 410 every time.
			if _, removed, err := s.Store.Remove(subscription.ID); err != nil {
				s.logf("push: %s is gone but could not be dropped: %v", subscription.Device, err)
			} else if removed {
				s.logf("push: %s is gone — dropping it", subscription.Device)
			}
			return false
		case permanent:
			// Everything else is left exactly where it is. A 400, a 403 or a
			// 413 from a service having a bad afternoon is not evidence about
			// the subscription, and the one thing worse than a missed
			// notification is quietly unsubscribing somebody because of it.
			return false
		}
		// Retryable.
		if attempt >= s.attempts() {
			s.logf("push: %s — giving up after %d attempt(s)", subscription.Device, attempt)
			return false
		}
		wait := backoff(attempt, retryAfter)
		if wait < 0 || spent+wait > RetryBudget {
			s.logf("push: %s — not retrying, the message would be stale before it landed",
				subscription.Device)
			return false
		}
		if !s.sleep(ctx, wait) {
			return false
		}
		spent += wait
	}
}

// What one POST came back as.
type outcome int

const (
	accepted outcome = iota
	gone
	permanent
	retryable
)

// post is one request. It is the receipt boundary, and it is deliberately the
// only place a status becomes a verdict: 4xx and 5xx must not quietly drift
// back into the successful count while the network half remains hard to
// exercise in unit tests.
func (s *Sender) post(ctx context.Context, message Message, subscription Subscription,
	credential, topic string) (outcome, time.Duration) {
	salt, err := NewSalt()
	if err != nil {
		s.logf("push: %s — %v", subscription.Device, err)
		return permanent, 0
	}
	// Fresh per message and per subscriber, both of them, and fresh per
	// *attempt* as well: a retry re-seals rather than re-posting the same
	// ciphertext, which keeps the one-key-one-nonce rule true even for bytes a
	// push service may have half received.
	ephemeral, err := NewEphemeral()
	if err != nil {
		s.logf("push: %s — %v", subscription.Device, err)
		return permanent, 0
	}
	sealed, err := Body(message.Plaintext, subscription, ephemeral, salt)
	if err != nil {
		s.logf("push: %s — %v", subscription.Device, err)
		return permanent, 0
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, subscription.Endpoint, bytes.NewReader(sealed))
	if err != nil {
		s.logf("push: %s — %v", subscription.Device, err)
		return permanent, 0
	}
	request.Header.Set("Authorization", credential)
	request.Header.Set("Content-Encoding", "aes128gcm")
	request.Header.Set("Content-Type", message.ContentType)
	request.Header.Set("TTL", strconv.Itoa(TTL))
	request.Header.Set("Urgency", Urgency)
	// Replaces an undelivered message about the same session rather than
	// joining it in the queue — so a phone that was away for ten minutes finds
	// one notification per session, not one per transition. RFC 8030 §5.4.
	if topic != "" {
		request.Header.Set("Topic", topic)
	}
	request.ContentLength = int64(len(sealed))

	response, err := s.client().Do(request)
	if err != nil {
		// The network being down is not the subscription being dead, so
		// nothing is removed here — a phone on a train would otherwise
		// unsubscribe itself.
		if ctx.Err() != nil {
			s.logf("push: %s — the send was cut short: %v", subscription.Device, ctx.Err())
			return permanent, 0
		}
		s.logf("push: %s — %v", subscription.Device, err)
		return retryable, 0
	}
	defer response.Body.Close()
	// Drained, bounded, so the connection can be reused and a service that
	// answers with prose cannot make this hold a megabyte of it.
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8<<10))

	switch {
	case ServiceAccepted(response.StatusCode):
		return accepted, 0
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return gone, 0
	case Retryable(response.StatusCode):
		s.logf("push: %s answered %d — trying again", subscription.Device, response.StatusCode)
		return retryable, retryAfter(response.Header.Get("Retry-After"), s.now())
	default:
		s.logf("push: %s refused with %d — left alone", subscription.Device, response.StatusCode)
		return permanent, 0
	}
}

// ServiceAccepted is the push service's receipt boundary.
func ServiceAccepted(status int) bool { return status >= 200 && status < 300 }

// Retryable is a status that says something about this moment rather than
// about this subscription.
func Retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter reads RFC 9110 §10.2.3, in either of its two spellings. A value
// past the ceiling is -1, which the caller reads as "do not queue this".
func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil {
		if seconds < 0 {
			return 0
		}
		d := time.Duration(seconds) * time.Second
		if d > RetryAfterCeiling {
			return -1
		}
		return d
	}
	when, err := http.ParseTime(header)
	if err != nil {
		return 0
	}
	d := when.Sub(now)
	switch {
	case d <= 0:
		return 0
	case d > RetryAfterCeiling:
		return -1
	}
	return d
}

// backoff is the wait before attempt+1: what the service asked for when it
// asked, and otherwise an exponential delay with full jitter so that a fleet of
// these does not come back in step.
func backoff(attempt int, asked time.Duration) time.Duration {
	if asked < 0 {
		return -1
	}
	if asked > 0 {
		return asked
	}
	ceiling := RetryBase << (attempt - 1)
	if ceiling > RetryCeiling {
		ceiling = RetryCeiling
	}
	// Full jitter, and never zero: a retry in the same millisecond is a second
	// request, not a second chance.
	return ceiling/2 + time.Duration(rand.Int64N(int64(ceiling/2)+1))
}
