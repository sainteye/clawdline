package push

import (
	"crypto/sha256"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Notification is what a caller asks to have delivered.
//
// **What goes in Title and Body is a decision about a lock screen, not about a
// network.** The transport is sealed end to end, so nothing here is a
// disclosure to a push service — and none of that matters for the thing that
// actually happens, which is text appearing on a phone lying on a table in a
// room with other people in it, quite possibly while its owner is in a meeting.
// Callers therefore send the useful session summary: its task title, project
// and state.
//
// URL is a deep link the service worker opens on a tap. It carries a session id
// and no prose, for the same reason.
//
// Icon is a path on this origin. It is a URL and not the picture itself for a
// reason worth stating: the picture would fit, just, and then the message would
// be a notification competing with its own decoration for the 3993 octets a
// push service is obliged to accept. A short path costs nothing and the phone
// fetches the mark once a year.
type Notification struct {
	Title string
	Body  string
	URL   string
	Tag   string
	Icon  string
	At    time.Time
}

// Message is one subscription's bytes and the content type that describes them.
type Message struct {
	Plaintext   []byte
	ContentType string
}

// Topic is a short, legal `Topic` header for a string that is neither.
//
// RFC 8030 §5.4 caps a topic at 32 characters from the URL-safe base64
// alphabet, and a session id is a 36-character UUID with hyphens in it. So it
// is hashed down rather than truncated: two sessions whose ids happen to share
// a prefix would otherwise collapse into each other, and the whole point of a
// topic is that it is the *same* session.
//
// What it buys is the case this feature exists for. A phone in a pocket for ten
// minutes has notifications waiting at the push service, not on the phone — and
// with a topic, a second message about the same session **replaces** the first
// there rather than joining a queue. The `tag` in the payload does the same job
// for the ones that already arrived.
func Topic(key string) string {
	digest := sha256.Sum256([]byte(key))
	encoded := EncodeBase64URL(digest[:])
	if len(encoded) > 32 {
		encoded = encoded[:32]
	}
	return encoded
}

// unreservedForSessionID is RFC 3986 §2.3: the characters that never need
// encoding anywhere in a URI.
func unreservedForSessionID(r byte) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '-' || r == '.' || r == '_' || r == '~':
		return true
	}
	return false
}

// SessionURL is the deep link a notification about a session carries, with the
// id encoded.
//
// **A session id is not URL text, and on this machine it usually is not even
// close.** The sessions this daemon watches are tmux panes, and a pane id is
// `%141`. Written straight into a fragment the address reads `/#session=%141`,
// and the web app answers that with `decodeURIComponent`, which does not refuse
// it: `%14` is a complete escape, so the id the page went looking for was
// U+0014 followed by `1`. No session has ever had that id, and the tap stopped
// on the list with nothing on screen to say why. iTerm ids — `w0t0p0:<UUID>` —
// carry no per-cent and went through unharmed, which is why this only ever
// happened on the machines that use tmux and never in a test.
//
// **The allowed set is the unreserved characters of RFC 3986 and nothing
// else.** A fragment is allowed to contain `&`, `=` and `#`, and the reader at
// the other end is `/(?:^|[#&])session=([^&]*)/` — so an id containing any of
// them would be cut in half by the very characters a fragment-safe set permits.
func SessionURL(id string) string {
	var out strings.Builder
	out.WriteString("/#session=")
	for i := 0; i < len(id); i++ {
		if c := id[i]; unreservedForSessionID(c) {
			out.WriteByte(c)
			continue
		}
		out.WriteString("%")
		const hex = "0123456789ABCDEF"
		out.WriteByte(hex[id[i]>>4])
		out.WriteByte(hex[id[i]&0x0f])
	}
	return out.String()
}

// Build selects the envelope the receiving browser understands.
//
// Safari's Declarative Web Push lets the browser display and navigate without
// first waking the service worker; that is the missing boundary when an
// installed web app is already alive and WebKit never dispatches
// `notificationclick`. Other push services, and old rows without a trustworthy
// origin, keep the existing payload and worker behaviour.
//
// `shortened` reports when the body had to be cut to fit, so a caller that logs
// says it once rather than this package writing to a log it does not own.
func Build(n Notification, subscription Subscription) (Message, bool, bool) {
	at := n.At
	if at.IsZero() {
		at = time.Now()
	}
	host := subscription.Host()
	apple := host == "push.apple.com" || strings.HasSuffix(host, ".push.apple.com")
	if apple && subscription.Origin != "" {
		if destination, ok := sameOriginURL(orDefault(n.URL, "/"), subscription.Origin); ok {
			payload, shortened, ok := declarative(n, destination, subscription.Origin, at)
			if ok {
				return Message{Plaintext: payload, ContentType: "application/notification+json"}, shortened, true
			}
			return Message{}, false, false
		}
	}
	payload, shortened, ok := legacy(n, at)
	if !ok {
		return Message{}, false, false
	}
	return Message{Plaintext: payload, ContentType: "application/octet-stream"}, shortened, true
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// legacy is the payload the service worker reads: RFC-agnostic JSON this
// project's own `sw.js` knows the shape of.
func legacy(n Notification, at time.Time) ([]byte, bool, bool) {
	fields := map[string]any{"title": n.Title, "at": at.Unix()}
	if n.URL != "" {
		fields["url"] = n.URL
	}
	if n.Tag != "" {
		fields["tag"] = Topic(n.Tag)
	}
	if n.Icon != "" {
		fields["icon"] = n.Icon
	}
	return fit(fields, "body", n.Body, "icon")
}

// declarative is Apple's Declarative Web Push envelope.
func declarative(n Notification, destination, base string, at time.Time) ([]byte, bool, bool) {
	notification := map[string]any{
		"title":     n.Title,
		"navigate":  destination,
		"timestamp": at.UnixMilli(),
	}
	if n.Tag != "" {
		notification["tag"] = Topic(n.Tag)
	}
	if n.Icon != "" {
		if absolute, ok := sameOriginURL(n.Icon, base); ok {
			notification["icon"] = absolute
		}
	}
	// `fit` shortens the body inside the object it was handed, and the
	// declarative envelope wraps that object — so it is given the inner one and
	// the wrapping is done by the encoder it is handed.
	return fitWrapped(notification, "body", n.Body, "icon", func(inner map[string]any) any {
		return map[string]any{"web_push": 8030, "notification": inner}
	})
}

// fit builds the bytes and shortens what it must to stay under MaxPayload.
//
// Decorations are expendable first; if the sentence still does not fit, keep its
// beginning and discard only its tail. The binary search is over runes, so it
// never splits a character in half. (The Swift app searches over grapheme
// clusters, so a flag or a ZWJ emoji is indivisible there and can be split
// here. That is the one difference, and it costs a broken glyph at the very end
// of a body 3,900 octets long, which no notification this daemon sends is.)
func fit(fields map[string]any, bodyKey, body, decoration string) ([]byte, bool, bool) {
	return fitWrapped(fields, bodyKey, body, decoration, func(inner map[string]any) any { return inner })
}

func fitWrapped(fields map[string]any, bodyKey, body, decoration string,
	wrap func(map[string]any) any) ([]byte, bool, bool) {
	fields[bodyKey] = body
	encode := func() ([]byte, bool) {
		var out strings.Builder
		enc := json.NewEncoder(&out)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(wrap(fields)); err != nil {
			return nil, false
		}
		return []byte(strings.TrimRight(out.String(), "\n")), true
	}

	made, ok := encode()
	if !ok {
		return nil, false, false
	}
	if len(made) > MaxPayload {
		if _, has := fields[decoration]; has {
			delete(fields, decoration)
			if made, ok = encode(); !ok {
				return nil, false, false
			}
		}
	}
	shortened := false
	if len(made) > MaxPayload {
		runes := []rune(body)
		low, high := 0, len(runes)
		for low < high {
			middle := (low + high + 1) / 2
			fields[bodyKey] = string(runes[:middle])
			candidate, ok := encode()
			if ok && len(candidate) <= MaxPayload {
				low = middle
			} else {
				high = middle - 1
			}
		}
		fields[bodyKey] = string(runes[:low])
		if made, ok = encode(); !ok {
			return nil, false, false
		}
		shortened = true
	}
	if len(made) > MaxPayload {
		// The title alone does not fit. Nothing is truncated silently past
		// this point: a caller told "sent" about bytes no service would take
		// is worse than a caller told the message could not be built.
		return nil, false, false
	}
	return made, shortened, true
}

// sameOriginURL resolves a possibly relative address against the web app's
// origin and refuses one that leaves it.
func sameOriginURL(raw, base string) (string, bool) {
	if raw == "" {
		return "", false
	}
	root, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	resolved, err := root.Parse(raw)
	if err != nil {
		return "", false
	}
	if OriginOf(resolved.String()) != OriginOf(base) {
		return "", false
	}
	return resolved.String(), true
}
