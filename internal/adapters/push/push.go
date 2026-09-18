// Package push makes a phone in another room buzz when a session is waiting
// for an answer.
//
// **This is the only thing in this daemon that leaves the machine on its own.**
// Everything else either reads a terminal that is already here or answers a
// question somebody asked over a line they opened deliberately. A push message
// is different in kind: it is handed to Apple — or Mozilla, or Google,
// whichever browser subscribed — and carried to a device that is not this one,
// over infrastructure nobody here controls. Two things follow from that, and
// they are the whole design. They are the Swift app's (Sources/WebPush.swift),
// kept word for word where the reasoning still holds.
//
// **The courier does not get to read it.** RFC 8291 seals the payload with a
// key derived from two secrets: an ECDH exchange with a public key the browser
// generated, and a 16-octet `auth` secret it generated alongside. Both were
// handed to *this machine* and to nothing else, so the push service sees a
// P-256 point, a salt and a lump of AES-GCM ciphertext, and can say only that a
// message went to a subscription. That is not a nicety — the alternative is
// posting the name of every repository somebody works on to a third party,
// forever, as a side effect of wanting their watch to tap them.
//
// **Encryption does not help with what is on the screen.** The entire point of
// a push message is that it lights up a locked phone lying face-up on a table
// in a room with other people in it. So the encryption settles who may read it
// in transit and settles nothing about who reads it on arrival.
//
// **The VAPID key pair is an identity, not a session, and losing it is
// silent.** A push service binds a subscription to the application server key
// it was created with; mint a new pair and every existing subscription starts
// failing — and it fails on the push service's side, with a 403 nobody is
// looking at, so what a person experiences is that their phone quietly stopped
// buzzing. So the private key is written once, at mode 0600, read back on the
// next launch, and only ever replaced when what is on disk will not parse as a
// key at all. See Store.VAPIDKey.
//
// The store lives under this daemon's own state directory and never under the
// Swift app's, for the reason internal/adapters/cloudkeys gives: two daemons
// sharing one identity fight over it. This package does not open, read or
// accept anything under ~/.config/clawdline.
package push

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The numbers the RFCs fix.
const (
	// HeaderLength is `salt(16) || rs(4) || idlen(1) || keyid(65)` — RFC 8188
	// §2.1, with the keyid length RFC 8291 §4 pins to an uncompressed P-256
	// point. 86 octets, always, for this profile.
	HeaderLength = 16 + 4 + 1 + 65

	// RecordSize is the `rs` field: an unsigned 32-bit integer, network byte
	// order, describing the **ciphertext** size of a record including its
	// 16-octet tag.
	//
	// 4096 because that is the body size RFC 8030 §7.2 forbids a push service
	// from rejecting, so it is the one number every service is known to
	// accept. Only the *final* record may be shorter than `rs`, and RFC 8291
	// §4 requires a push message to be exactly one record — which is therefore
	// the final one, so a single short record under a 4096 declaration is well
	// formed rather than a fudge.
	RecordSize uint32 = 4096

	// MaxPayload is what is left for the caller once the header, the padding
	// delimiter and the tag have been taken out of 4096: 3993 octets. Far more
	// than a notification needs, and checked anyway, because the failure it
	// prevents is a 413 from the push service for a message whose length
	// nobody was watching.
	MaxPayload = 4096 - HeaderLength - 1 - 16

	// TTL is how long a message is worth delivering. RFC 8030 §5.2 makes this
	// header mandatory.
	//
	// The claim being made is "a session is waiting for you", and a claim like
	// that goes stale. Zero would mean deliver-now-or-never, which throws away
	// the case this feature exists for — a phone asleep on a bad network for
	// ninety seconds. A day would mean a phone that spent the afternoon in a
	// drawer buzzing at midnight about a question answered at noon, which
	// teaches people to turn notifications off. An hour is longer than a lift,
	// a tunnel or a meeting, and shorter than the point at which the sentence
	// stops being true.
	TTL = 3600

	// Urgency is RFC 8030 §5.3. `high` is the level the RFC illustrates with
	// "incoming phone call or time-sensitive alert", and it is the one a
	// device in a low power state will not defer.
	//
	// A blocked agent is precisely a time-sensitive alert: it is doing nothing
	// at all until somebody answers. What makes `high` defensible rather than
	// rude is **rarity** — this is called when a session becomes `waiting`,
	// and not for every state change.
	Urgency = "high"

	// TokenLifetime: RFC 8292 §2 caps `exp` at 24 hours and encourages reusing
	// a token inside its window so the push service can cache the signature
	// check. Twelve hours is half the ceiling, which leaves room for a phone
	// and a machine whose clocks disagree by more than they should.
	TokenLifetime = 12 * time.Hour

	// Subject is the `sub` claim: a contact URI for whoever is operating the
	// application server.
	//
	// There is no operator here — the application server is somebody's laptop
	// — so the honest answer is the project a push service would end up
	// reading if it wanted to know what had been sending it messages. It is
	// not optional in practice: Apple refuses a token without a syntactically
	// valid `sub` and says only `BadJwtToken`.
	Subject = "https://github.com/sainteye/clawdline"

	// SubscriberKeyBytes is an uncompressed P-256 point.
	SubscriberKeyBytes = 65
	// AuthSecretBytes is RFC 8291 §3.2.
	AuthSecretBytes = 16
	// SaltBytes is the width of the aes128gcm header's salt field.
	SaltBytes = 16
	// VAPIDSeedBytes is a P-256 private scalar.
	VAPIDSeedBytes = 32
)

// endpointLimit is the longest endpoint a subscription may name. The push
// services' own run to a few hundred characters; the bound is what keeps the
// register's `push.subscriptions` limit, every row at its longest, under half
// of what the subscriptions file is read up to (a test holds that).
const endpointLimit = 2048

// The ways one message can be refused before it ever reaches the network. Each
// of these is a thing a push service answers with a 400 and an opaque body, so
// it is worth catching here where the reason is still in front of us.
var (
	// ErrSubscriberKey is a p256dh that is not an uncompressed P-256 point.
	ErrSubscriberKey = errors.New("the subscription's p256dh is not an uncompressed P-256 point")
	// ErrSaltLength is a salt that is not 16 octets, which would silently
	// become a shorter salt plus a corrupted `rs`.
	ErrSaltLength = errors.New("the salt must be 16 octets, which is what the aes128gcm header has room for")
	// ErrPayloadTooLarge is a body past what a push service is obliged to take.
	ErrPayloadTooLarge = errors.New("the payload is larger than a push service is obliged to accept")
	// ErrNoOrigin is an endpoint with no origin to put in `aud`.
	ErrNoOrigin = errors.New("no origin to put in aud")
	// ErrBadSubject is a `sub` that is neither a mailto: nor an https: URI.
	ErrBadSubject = errors.New("sub must be a mailto: or https: URI")
)

// Subscription is one `PushSubscription`, as `pushManager.subscribe()`
// produced it.
//
// P256dh and Auth are the browser's half of the encryption and are useless to
// anyone else: P256dh is a public key, and Auth is a shared secret this machine
// cannot use to read anything, only to write to that one device.
type Subscription struct {
	ID string
	// Endpoint is where the message is POSTed — e.g.
	// `https://web.push.apple.com/…`.
	Endpoint string
	// P256dh is the browser's public key: an uncompressed P-256 point, 65
	// octets starting 0x04.
	P256dh []byte
	// Auth is 16 octets, per RFC 8291 §3.2.
	Auth []byte
	// Device is which paired device this belongs to, so revoking the device
	// can take the subscription with it.
	Device string
	// Origin is the secure web-app origin that created this subscription.
	// Safari needs it to turn a root-relative session address into Declarative
	// Web Push's absolute `navigate` URL. Older stored rows have no origin and
	// deliberately keep the service-worker path.
	Origin  string
	Created time.Time
}

// Delivery is what the push services actually accepted. A route that promised
// content to a person must not confuse "the send was started" with "a service
// accepted it", so partial fan-out is kept explicit and its caller can report
// both halves without inviting a blind retry.
type Delivery struct {
	Sent   int
	Failed int
}

// Total is how many subscriptions were attempted.
func (d Delivery) Total() int { return d.Sent + d.Failed }

// Host is the endpoint's host, lowercased, or "".
func (s Subscription) Host() string {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// FromBrowser reads a `PushSubscription.toJSON()` from the page and checks it
// before it is believed.
//
// The checking is not tidiness. `endpoint` is a URL this machine will be told
// to POST to, from inside the network the machine is on, whenever a session
// changes state — so an unchecked one is a request-forgery primitive handed to
// whoever can reach the route. `https` only, and a real host. It is
// deliberately not an allowlist of push services: Apple, Mozilla, Google and
// anything self-hosted all have to work, and the property worth keeping is
// "not plaintext, not a scheme that reaches something local", not "a vendor we
// recognise".
//
// The key lengths are checked here too, because a compressed 33-octet point is
// a perfectly plausible thing for a client to send and the resulting failure is
// otherwise a crypto error thrown an hour later with no mention of where the
// data came from.
//
// And its size and spelling: at most endpointLimit bytes, and only the
// characters a URL is written in — no spaces, controls, quotes, backslashes or
// angle brackets, and nothing outside ASCII — so a stored row is as long on
// disk as it was on the wire.
func FromBrowser(body map[string]any, id, device, rawOrigin string) (Subscription, bool) {
	raw, _ := body["endpoint"].(string)
	if len(raw) > endpointLimit || !urlText(raw) {
		return Subscription{}, false
	}
	endpoint, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(endpoint.Scheme, "https") || endpoint.Hostname() == "" {
		return Subscription{}, false
	}
	keys, _ := body["keys"].(map[string]any)
	p256dhText, _ := keys["p256dh"].(string)
	authText, _ := keys["auth"].(string)
	p256dh, err := DecodeBase64URL(p256dhText)
	if err != nil || len(p256dh) != SubscriberKeyBytes || p256dh[0] != 0x04 {
		return Subscription{}, false
	}
	secret, err := DecodeBase64URL(authText)
	if err != nil || len(secret) != AuthSecretBytes {
		return Subscription{}, false
	}
	return Subscription{
		ID:       id,
		Endpoint: raw,
		P256dh:   p256dh,
		Auth:     secret,
		Device:   device,
		Origin:   WebAppOrigin(rawOrigin),
		Created:  time.Now(),
	}, true
}

// WebAppOrigin keeps only the RFC 6454 origin a browser supplied. A path,
// query, credentials or fragment in a stored base would let persistence change
// where a later notification opens.
//
// Unlike the Swift app this also accepts an `http://` origin whose host is
// loopback. The Swift page is only ever reached over https or through a tunnel;
// this console is reached at `http://127.0.0.1:7727` at the desk, which every
// browser treats as a secure context and which is therefore a real web-app
// origin here. Refusing it would silently drop the one origin this daemon is
// usually read from.
func WebAppOrigin(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	switch scheme {
	case "https":
	case "http":
		if !isLoopbackHost(host) {
			return ""
		}
	default:
		return ""
	}
	return OriginOf(raw)
}

func isLoopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1" ||
		strings.HasSuffix(host, ".localhost")
}

// OriginOf is the RFC 6454 §6.1 serialisation: scheme, host, and the port only
// when it is not the default one. A `https://web.push.apple.com:443` in `aud`
// is a different string to the one the push service compares against, and the
// failure is a flat 401.
func OriginOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if scheme == "" || host == "" {
		return ""
	}
	origin := scheme + "://" + host
	port := u.Port()
	if port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		origin += ":" + port
	}
	return origin
}

// EncodeBase64URL is base64url, unpadded — RFC 7515 §2, which is what a JWS
// wants and what `applicationServerKey` on the page will be decoding.
func EncodeBase64URL(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeBase64URL is lenient in both directions on purpose. A browser produces
// base64url without padding, a hand-written client produces plain base64 with
// it, and refusing one of those would be a bug report about a subscription that
// "just does not work" with no visible cause.
func DecodeBase64URL(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty")
	}
	swapped := strings.NewReplacer("-", "+", "_", "/").Replace(text)
	if pad := len(swapped) % 4; pad != 0 {
		swapped += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(swapped)
}

// urlText reports whether s is written only in characters a URL is: printable
// ASCII other than the space, the quote, the backslash and the angle brackets.
func urlText(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || c >= 0x7f || c == '"' || c == '\\' || c == '<' || c == '>' {
			return false
		}
	}
	return true
}
