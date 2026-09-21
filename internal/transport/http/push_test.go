package http

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"image"
	_ "image/png"
	"mime"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	adapterpush "github.com/sainteye/clawdline/internal/adapters/push"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// pushServer is a Server over its own state directory, so nothing here shares a
// store with another test or with this machine.
func pushServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}}
}

// asDevice is a request the gate has already judged, which is the only way a
// handler behind it ever sees one.
func asDevice(r *http.Request, device string, local bool) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), accessKey{}, access{
		verdict: auth.Verdict{Allowed: true, Device: device, Local: local, Caps: auth.NewCaps(auth.Read)},
	}))
}

func browserSubscription(t *testing.T, endpoint string) string {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	secret := make([]byte, adapterpush.AuthSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("auth: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"endpoint": endpoint,
		"keys": map[string]any{
			"p256dh": adapterpush.EncodeBase64URL(key.PublicKey().Bytes()),
			"auth":   adapterpush.EncodeBase64URL(secret),
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

func pushCall(t *testing.T, s *Server, method, path, body, device string, local bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.pushRoute(rec, asDevice(req, device, local))
	return rec
}

// TestThePushRoutesAreTheFourTheyAre. An unknown one is a 404 rather than a
// prefix match on the nearest.
func TestThePushRoutesAreTheFourTheyAre(t *testing.T) {
	s := pushServer(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/v1/push/nope"},
		{http.MethodPost, "/v1/push/"},
		{http.MethodPost, "/v1/push/key"},
		{http.MethodGet, "/v1/push/subscribe"},
		{http.MethodGet, "/v1/push/test"},
	} {
		rec := pushCall(t, s, call.method, call.path, "", "phone", false)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d %s", call.method, call.path, rec.Code, rec.Body)
		}
	}
}

// TestAskingForTheKeyIsWhatMintsOne, and the same one comes back next time.
func TestAskingForTheKeyIsWhatMintsOne(t *testing.T) {
	s := pushServer(t)
	rec := pushCall(t, s, http.MethodGet, "/v1/push/key", "", "phone", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("key: %d %s", rec.Code, rec.Body)
	}
	var first contract.PushKey
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("key: %v", err)
	}
	if first.Key == "" {
		t.Fatal("an empty application server key")
	}
	raw, err := adapterpush.DecodeBase64URL(first.Key)
	if err != nil || len(raw) != adapterpush.SubscriberKeyBytes || raw[0] != 0x04 {
		t.Fatalf("the key is not an uncompressed point: %d octets, %v", len(raw), err)
	}

	rec = pushCall(t, s, http.MethodGet, "/v1/push/key", "", "phone", false)
	var again contract.PushKey
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if again.Key != first.Key {
		t.Error("the identity changed between two reads")
	}
}

// TestASubscriptionIsCheckedAtTheRoute, in the Swift app's words.
func TestASubscriptionIsCheckedAtTheRoute(t *testing.T) {
	s := pushServer(t)
	for name, body := range map[string]string{
		"nothing at all":  "",
		"an empty object": "{}",
		"plain http":      browserSubscription(t, "http://fcm.googleapis.com/x"),
		"a local scheme":  browserSubscription(t, "file:///etc/passwd"),
	} {
		rec := pushCall(t, s, http.MethodPost, "/v1/push/subscribe", body, "phone", false)
		if rec.Code != http.StatusBadRequest || refusalCode(rec) != "bad_request" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	rec := pushCall(t, s, http.MethodPost, "/v1/push/subscribe",
		browserSubscription(t, "https://fcm.googleapis.com/fcm/send/abc"), "phone", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("a real subscription: %d %s", rec.Code, rec.Body)
	}
	var subscribed contract.PushSubscribed
	if err := json.Unmarshal(rec.Body.Bytes(), &subscribed); err != nil || !subscribed.OK || subscribed.ID == "" {
		t.Fatalf("subscribe answered %s (%v)", rec.Body, err)
	}
}

// TestATestSendNeedsSomethingToSendTo, and says so with a code the page can
// turn into one sentence.
func TestATestSendNeedsSomethingToSendTo(t *testing.T) {
	s := pushServer(t)
	rec := pushCall(t, s, http.MethodPost, "/v1/push/test", "{}", "phone", false)
	if rec.Code != http.StatusConflict || refusalCode(rec) != "not_subscribed" {
		t.Fatalf("test: %d %s", rec.Code, rec.Body)
	}
	// And another device's subscription is not this device's reason to send.
	if code := pushCall(t, s, http.MethodPost, "/v1/push/subscribe",
		browserSubscription(t, "https://fcm.googleapis.com/fcm/send/abc"), "laptop", false).Code; code != http.StatusOK {
		t.Fatalf("subscribe: %d", code)
	}
	rec = pushCall(t, s, http.MethodPost, "/v1/push/test", "{}", "phone", false)
	if rec.Code != http.StatusConflict || refusalCode(rec) != "not_subscribed" {
		t.Fatalf("another device's row was counted: %d %s", rec.Code, rec.Body)
	}
}

// TestOneBrowserCannotUnsubscribeAnother.
//
// The id is not a secret — it is this machine's name for a row, and it comes
// back to the page that made it. What stops it being a lever is that a row is
// only removed for the device that owns it.
func TestOneBrowserCannotUnsubscribeAnother(t *testing.T) {
	s := pushServer(t)
	rec := pushCall(t, s, http.MethodPost, "/v1/push/subscribe",
		browserSubscription(t, "https://fcm.googleapis.com/fcm/send/abc"), "laptop", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body)
	}
	var subscribed contract.PushSubscribed
	_ = json.Unmarshal(rec.Body.Bytes(), &subscribed)

	store, err := s.push()
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// A body with no id at all is the one refusal here.
	rec = pushCall(t, s, http.MethodPost, "/v1/push/unsubscribe", "{}", "phone", false)
	if rec.Code != http.StatusBadRequest || refusalCode(rec) != "bad_request" {
		t.Fatalf("no id: %d %s", rec.Code, rec.Body)
	}

	// Another device naming it is answered as though it were not there.
	body, _ := json.Marshal(map[string]string{"id": subscribed.ID})
	rec = pushCall(t, s, http.MethodPost, "/v1/push/unsubscribe", string(body), "phone", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("another device: %d %s", rec.Code, rec.Body)
	}
	if rows, _ := store.Subscriptions(); len(rows) != 1 {
		t.Fatal("one browser unsubscribed another")
	}

	// Its own device takes it back, and so may this machine's own token.
	rec = pushCall(t, s, http.MethodPost, "/v1/push/unsubscribe", string(body), "laptop", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("its own device: %d %s", rec.Code, rec.Body)
	}
	if rows, _ := store.Subscriptions(); len(rows) != 0 {
		t.Fatal("a device could not take back its own subscription")
	}

	// Unsubscribing twice is what a reload of the page looks like.
	rec = pushCall(t, s, http.MethodPost, "/v1/push/unsubscribe", string(body), "laptop", false)
	if rec.Code != http.StatusOK {
		t.Errorf("a second unsubscribe: %d %s", rec.Code, rec.Body)
	}
}

// TestAnUnjudgedRequestIsRefused. Every one of these routes needs a paired
// device; the gate has already required a token, and this is the name behind it.
func TestAnUnjudgedRequestIsRefused(t *testing.T) {
	s := pushServer(t)
	for _, path := range []string{"/v1/push/subscribe", "/v1/push/test", "/v1/push/unsubscribe"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		s.pushRoute(rec, req)
		if rec.Code != http.StatusUnauthorized || refusalCode(rec) != "unauthorized" {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

// ---- the home-screen shell ----

func TestSplashPathIsAGeometryOrNothing(t *testing.T) {
	t.Parallel()
	for path, want := range map[string][2]int{
		"/splash-1179x2556.png": {1179, 2556},
		"/splash-750x1334.png":  {750, 1334},
		"/splash-.png":          {},
		"/splash-1179.png":      {},
		"/splash-1179x.png":     {},
		"/splash-axb.png":       {},
		"/splash-1x2x3.png":     {},
		"/splash-1179x2556":     {},
		"/icon-192.png":         {},
		"/":                     {},
	} {
		w, h, ok := splashPath(path)
		if want == [2]int{} {
			if ok {
				t.Errorf("%s was read as %dx%d", path, w, h)
			}
			continue
		}
		if !ok || w != want[0] || h != want[1] {
			t.Errorf("%s = %dx%d (%v), want %v", path, w, h, ok, want)
		}
	}
}

// TestTheLaunchImagesAreDrawnAndEverythingElsePassesThrough.
func TestTheLaunchImagesAreDrawnAndEverythingElsePassesThrough(t *testing.T) {
	t.Parallel()
	s := &Server{}
	reached := 0
	handler := s.withPWA(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/splash-750x1334.png", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("a launch image: %d %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("content type is %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=86400" {
		t.Errorf("cache control is %q", got)
	}
	bounds, _, err := image.DecodeConfig(rec.Body)
	if err != nil || bounds.Width != 750 || bounds.Height != 1334 {
		t.Errorf("the body is %dx%d (%v)", bounds.Width, bounds.Height, err)
	}

	// A geometry nobody has a screen of is a 404 rather than an allocation.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/splash-99999x99999.png", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("an impossible geometry: %d", rec.Code)
	}

	// And everything else is the console's.
	for _, path := range []string{"/", "/icon-192.png", "/manifest.webmanifest", "/sw.js"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusTeapot {
			t.Errorf("%s was answered here rather than by the console: %d", path, rec.Code)
		}
	}
	if reached != 4 {
		t.Errorf("the console was reached %d times, want 4", reached)
	}
}

// TestTheShellsTypesAreRegistered. A manifest sent as text/plain is a home
// screen that installs without a name, and neither extension is in Go's own
// table.
func TestTheShellsTypesAreRegistered(t *testing.T) {
	t.Parallel()
	for ext, want := range map[string]string{
		".webmanifest": "application/manifest+json",
		".ico":         "image/x-icon",
	} {
		if got := mime.TypeByExtension(ext); !strings.HasPrefix(got, want) {
			t.Errorf("%s is %q, want %s…", ext, got, want)
		}
	}
}

// TestABrowserOnThisMachinesOwnNetworkKeepsItsOwnOrigin is the road that must
// not move.
//
// A browser at `http://127.0.0.1:7727`, or at a tunnel's name, sends a real
// Origin header naming the page the person is looking at — and that page is
// the one a notification should open. Only a request dispatched in process by
// the Cloud line is answered for a console somewhere else, and only because
// nothing on that road can speak for itself. A change that read the hosted
// console's origin for every subscription would send a phone paired over the
// tunnel to `app.clawdline.com`, where it has never signed in.
func TestABrowserOnThisMachinesOwnNetworkKeepsItsOwnOrigin(t *testing.T) {
	for _, origin := range []string{
		"http://127.0.0.1:7727",
		"https://calm-river-1234.trycloudflare.com",
		// No Origin at all is no origin stored, which is what keeps an old row
		// on the service worker's payload rather than a guessed address.
		"",
	} {
		s := pushServer(t)
		req := httptest.NewRequest(http.MethodPost, "/v1/push/subscribe",
			strings.NewReader(browserSubscription(t, "https://web.push.apple.com/QWxpY2U")))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		s.pushRoute(rec, asDevice(req, "phone", false))
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: subscribe answered %d %s", origin, rec.Code, rec.Body)
		}
		store, err := s.push()
		if err != nil {
			t.Fatal(err)
		}
		rows, err := store.Subscriptions()
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("%q: %d subscriptions", origin, len(rows))
		}
		if rows[0].Origin != origin {
			t.Errorf("a browser at %q had its subscription stored as %q", origin, rows[0].Origin)
		}
	}
}
