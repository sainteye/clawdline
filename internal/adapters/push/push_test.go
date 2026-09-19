package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A browser, for the tests: the half of the exchange that the real one keeps.
type browser struct {
	private *ecdh.PrivateKey
	auth    []byte
}

func newBrowser(t *testing.T) browser {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("browser key: %v", err)
	}
	secret := make([]byte, AuthSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("auth secret: %v", err)
	}
	return browser{private: key, auth: secret}
}

func (b browser) subscription(id, endpoint, device string) Subscription {
	return Subscription{
		ID:       id,
		Endpoint: endpoint,
		P256dh:   b.private.PublicKey().Bytes(),
		Auth:     b.auth,
		Device:   device,
		Created:  time.Unix(1_700_000_000, 0),
	}
}

// open is the other half of Body: RFC 8291 §3.4 and RFC 8188 §2, read backwards.
// It is the only assertion worth making about the encryption — that the
// subscriber's own private key turns these bytes back into the plaintext.
func (b browser) open(t *testing.T, sealed []byte) []byte {
	t.Helper()
	if len(sealed) < HeaderLength+16 {
		t.Fatalf("a body of %d octets cannot hold a header and a tag", len(sealed))
	}
	salt := sealed[:16]
	if got := binary.BigEndian.Uint32(sealed[16:20]); got != RecordSize {
		t.Errorf("rs is %d, want %d", got, RecordSize)
	}
	if int(sealed[20]) != SubscriberKeyBytes {
		t.Fatalf("keyid length is %d, want %d", sealed[20], SubscriberKeyBytes)
	}
	theirs := sealed[21 : 21+SubscriberKeyBytes]
	ciphertext := sealed[HeaderLength:]

	server, err := ecdh.P256().NewPublicKey(theirs)
	if err != nil {
		t.Fatalf("the application server's point does not parse: %v", err)
	}
	shared, err := b.private.ECDH(server)
	if err != nil {
		t.Fatalf("ecdh: %v", err)
	}
	keyInfo := append([]byte("WebPush: info\x00"), b.private.PublicKey().Bytes()...)
	keyInfo = append(keyInfo, theirs...)
	ikm, err := hkdf.Key(sha256.New, shared, b.auth, string(keyInfo), 32)
	if err != nil {
		t.Fatalf("ikm: %v", err)
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		t.Fatalf("cek: %v", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		t.Fatalf("nonce: %v", err)
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("the browser could not open this message: %v", err)
	}
	if len(plain) == 0 || plain[len(plain)-1] != 0x02 {
		t.Fatalf("the record does not end in the last-record delimiter")
	}
	return plain[:len(plain)-1]
}

// TestTheBrowserCanReadWhatWeSealed is the one assertion the encryption is for.
func TestTheBrowserCanReadWhatWeSealed(t *testing.T) {
	t.Parallel()
	b := newBrowser(t)
	subscription := b.subscription("one", "https://fcm.googleapis.com/fcm/send/abc", "phone")
	payload := []byte(`{"title":"Clawdline","body":"一則通知"}`)

	ephemeral, err := NewEphemeral()
	if err != nil {
		t.Fatalf("ephemeral: %v", err)
	}
	salt, err := NewSalt()
	if err != nil {
		t.Fatalf("salt: %v", err)
	}
	sealed, err := Body(payload, subscription, ephemeral, salt)
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	if got := b.open(t, sealed); !bytes.Equal(got, payload) {
		t.Errorf("read back %q, want %q", got, payload)
	}
}

// TestEveryMessageIsKeyedOnItsOwn. Reusing an ephemeral key or a salt reuses
// the AES-GCM key and nonce, which is the one failure mode GCM does not
// survive, so two sends of the same bytes must not produce the same body.
func TestEveryMessageIsKeyedOnItsOwn(t *testing.T) {
	t.Parallel()
	b := newBrowser(t)
	subscription := b.subscription("one", "https://example.test/x", "phone")
	seen := map[string]bool{}
	for range 4 {
		ephemeral, _ := NewEphemeral()
		salt, _ := NewSalt()
		sealed, err := Body([]byte("same"), subscription, ephemeral, salt)
		if err != nil {
			t.Fatalf("Body: %v", err)
		}
		key := string(sealed)
		if seen[key] {
			t.Fatal("two messages came out identical, so a key and a nonce were reused")
		}
		seen[key] = true
	}
}

// TestBodyRefusesWhatAPushServiceWouldRefuseLater, where the reason is still
// in front of us.
func TestBodyRefusesWhatAPushServiceWouldRefuseLater(t *testing.T) {
	t.Parallel()
	b := newBrowser(t)
	good := b.subscription("one", "https://example.test/x", "phone")
	ephemeral, _ := NewEphemeral()
	salt, _ := NewSalt()

	if _, err := Body([]byte("x"), good, ephemeral, salt[:15]); !errors.Is(err, ErrSaltLength) {
		t.Errorf("a short salt: want ErrSaltLength, got %v", err)
	}
	if _, err := Body(bytes.Repeat([]byte("x"), MaxPayload+1), good, ephemeral, salt); !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("an oversized payload: want ErrPayloadTooLarge, got %v", err)
	}
	broken := good
	broken.P256dh = append([]byte{0x03}, good.P256dh[1:]...) // a compressed-looking point
	if _, err := Body([]byte("x"), broken, ephemeral, salt); !errors.Is(err, ErrSubscriberKey) {
		t.Errorf("a bad subscriber key: want ErrSubscriberKey, got %v", err)
	}
	// And the largest legal payload still fits, which is the other half of the
	// same boundary.
	if _, err := Body(bytes.Repeat([]byte("x"), MaxPayload), good, ephemeral, salt); err != nil {
		t.Errorf("the largest legal payload was refused: %v", err)
	}
}

// TestTheTokenNamesTheOriginAndVerifies. `aud` is the endpoint's origin and not
// the endpoint: the full path there is the classic way to get a token that
// verifies perfectly and is rejected anyway.
func TestTheTokenNamesTheOriginAndVerifies(t *testing.T) {
	t.Parallel()
	key, err := NewVAPIDKey()
	if err != nil {
		t.Fatalf("NewVAPIDKey: %v", err)
	}
	expires := time.Unix(1_700_043_200, 0)
	header, err := key.Authorization("https://web.push.apple.com:443/one/two?q=1", expires, Subject)
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}
	if !strings.HasPrefix(header, "vapid t=") || !strings.Contains(header, ", k=") {
		t.Fatalf("the header is not RFC 8292 shaped: %q", header)
	}
	jwt := strings.TrimPrefix(strings.SplitN(header, ", k=", 2)[0], "vapid t=")
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("a JWT has three segments, this has %d", len(parts))
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("claims: %v", err)
	}
	// The default port is written out of the origin, and the path never enters it.
	if claims.Aud != "https://web.push.apple.com" {
		t.Errorf("aud is %q", claims.Aud)
	}
	if claims.Exp != expires.Unix() {
		t.Errorf("exp is %d, want %d", claims.Exp, expires.Unix())
	}
	if claims.Sub != Subject {
		t.Errorf("sub is %q", claims.Sub)
	}
	if strings.Contains(string(raw), `\/`) {
		t.Errorf("the claims escaped their slashes: %s", raw)
	}

	// The signature is r‖s, 64 octets, and it verifies against the key the
	// header carries — which is what the push service checks.
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("signature: %d octets, %v", len(signature), err)
	}
	carried, err := base64.RawURLEncoding.DecodeString(strings.SplitN(header, ", k=", 2)[1])
	if err != nil {
		t.Fatalf("k=: %v", err)
	}
	if !bytes.Equal(carried, key.PublicBytes()) {
		t.Error("k= is not this key's public point")
	}
	point, err := ecdh.P256().NewPublicKey(carried)
	if err != nil {
		t.Fatalf("k= does not parse: %v", err)
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), point.Bytes())
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest[:],
		new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		t.Error("the signature does not verify against the key the header carries")
	}
}

func TestTheTokenRefusesASubjectAPushServiceWouldNotRead(t *testing.T) {
	t.Parallel()
	key, _ := NewVAPIDKey()
	if _, err := key.Authorization("https://example.test/x", time.Now(), "somebody@example.test"); !errors.Is(err, ErrBadSubject) {
		t.Errorf("want ErrBadSubject, got %v", err)
	}
	if _, err := key.Authorization("not a url at all", time.Now(), Subject); !errors.Is(err, ErrNoOrigin) {
		t.Errorf("want ErrNoOrigin, got %v", err)
	}
}

// TestAKeySurvivesItsSeed, which is the whole point of storing one.
func TestAKeySurvivesItsSeed(t *testing.T) {
	t.Parallel()
	key, _ := NewVAPIDKey()
	again, err := VAPIDKeyFromSeed(key.Seed())
	if err != nil {
		t.Fatalf("VAPIDKeyFromSeed: %v", err)
	}
	if again.PublicKey() != key.PublicKey() {
		t.Error("the same seed produced a different public key")
	}
	if len(key.Seed()) != VAPIDSeedBytes {
		t.Errorf("a seed is %d octets", len(key.Seed()))
	}
	for name, seed := range map[string][]byte{
		"too short": make([]byte, 31),
		"zero":      make([]byte, 32),
		"the order": elliptic.P256().Params().N.FillBytes(make([]byte, 32)),
	} {
		if _, err := VAPIDKeyFromSeed(seed); err == nil {
			t.Errorf("%s was accepted as a scalar", name)
		}
	}
}

// TestASubscriptionIsCheckedBeforeItIsBelieved. `endpoint` is a URL this
// machine will POST to from inside its own network, so an unchecked one is a
// request-forgery primitive.
func TestASubscriptionIsCheckedBeforeItIsBelieved(t *testing.T) {
	t.Parallel()
	b := newBrowser(t)
	good := map[string]any{
		"endpoint": "https://fcm.googleapis.com/fcm/send/abc",
		"keys": map[string]any{
			"p256dh": EncodeBase64URL(b.private.PublicKey().Bytes()),
			"auth":   EncodeBase64URL(b.auth),
		},
	}
	if _, ok := FromBrowser(good, "id", "device", "https://console.example"); !ok {
		t.Fatal("a real subscription was refused")
	}

	bad := map[string]map[string]any{
		"plain http":            {"endpoint": "http://fcm.googleapis.com/x"},
		"a local scheme":        {"endpoint": "file:///etc/passwd"},
		"no host":               {"endpoint": "https:///x"},
		"no endpoint":           {},
		"a compressed point":    {"endpoint": "https://a.test/x", "keys": map[string]any{"p256dh": EncodeBase64URL(make([]byte, 33)), "auth": EncodeBase64URL(b.auth)}},
		"a short auth secret":   {"endpoint": "https://a.test/x", "keys": map[string]any{"p256dh": EncodeBase64URL(b.private.PublicKey().Bytes()), "auth": EncodeBase64URL(make([]byte, 8))}},
		"a point that is not a": {"endpoint": "https://a.test/x", "keys": map[string]any{"p256dh": EncodeBase64URL(make([]byte, 65)), "auth": EncodeBase64URL(b.auth)}},
	}
	for name, body := range bad {
		if name == "a compressed point" || name == "a short auth secret" || name == "a point that is not a" {
			// These reach the key checks; the first four stop at the endpoint.
		} else if body["keys"] == nil {
			body["keys"] = good["keys"]
		}
		if _, ok := FromBrowser(body, "id", "device", ""); ok {
			t.Errorf("%s was accepted", name)
		}
	}
	// A 65-octet lump that starts 0x04 but is not on the curve is refused by
	// the encryption rather than here, which is stated so the boundary is not
	// mistaken for a curve check.
	onCurveLooking := map[string]any{
		"endpoint": "https://a.test/x",
		"keys": map[string]any{
			"p256dh": EncodeBase64URL(append([]byte{0x04}, make([]byte, 64)...)),
			"auth":   EncodeBase64URL(b.auth),
		},
	}
	subscription, ok := FromBrowser(onCurveLooking, "id", "device", "")
	if !ok {
		t.Fatal("a well-shaped point was refused before the curve check")
	}
	ephemeral, _ := NewEphemeral()
	salt, _ := NewSalt()
	if _, err := Body([]byte("x"), subscription, ephemeral, salt); !errors.Is(err, ErrSubscriberKey) {
		t.Errorf("a point off the curve: want ErrSubscriberKey, got %v", err)
	}
}

// TestTheStoredOriginIsAnOriginAndNothingElse. A path, query or fragment in a
// stored base would let persistence change where a later notification opens.
func TestTheStoredOriginIsAnOriginAndNothingElse(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"https://console.example/app?x=1#y": "https://console.example",
		"https://console.example:8443/":     "https://console.example:8443",
		"https://CONSOLE.example":           "https://console.example",
		// The console is read at loopback over http, which every browser
		// treats as a secure context; the Swift app never had this origin.
		"http://127.0.0.1:7727/":  "http://127.0.0.1:7727",
		"http://localhost:5273/x": "http://localhost:5273",
		// And nothing else over plain http.
		"http://192.0.2.4:7727/": "",
		"ftp://console.example":  "",
		"":                       "",
		"not a url":              "",
	} {
		if got := WebAppOrigin(raw); got != want {
			t.Errorf("WebAppOrigin(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestASessionAddressSurvivesATmuxPaneID. `%14` written straight into a
// fragment reads as U+0014 at the other end, and the tap lands
// on a list with nothing on screen to say why.
func TestASessionAddressSurvivesATmuxPaneID(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]string{
		"%14":            "/#session=%2514",
		"w0t0p0:9F2A":    "/#session=w0t0p0%3A9F2A",
		"plain-id_1.2~3": "/#session=plain-id_1.2~3",
		"a&b=c#d":        "/#session=a%26b%3Dc%23d",
		"":               "/#session=",
	} {
		if got := SessionURL(id); got != want {
			t.Errorf("SessionURL(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestATopicIsShortAndIsNotAPrefix. RFC 8030 §5.4 caps a topic at 32 URL-safe
// base64 characters, and two ids that share a prefix must not collapse.
func TestATopicIsShortAndIsNotAPrefix(t *testing.T) {
	t.Parallel()
	one, two := Topic("session-aaaaaaaaaaaaaaaaaaaaaaaaaaaa1"), Topic("session-aaaaaaaaaaaaaaaaaaaaaaaaaaaa2")
	if len(one) != 32 || len(two) != 32 {
		t.Fatalf("topics are %d and %d characters", len(one), len(two))
	}
	if one == two {
		t.Error("two sessions collapsed into one topic")
	}
	if strings.ContainsAny(one, "+/=") {
		t.Errorf("a topic must be URL-safe base64: %q", one)
	}
}

// TestAppleGetsADeclarativeMessageAndEverybodyElseGetsTheWorkerOne.
func TestAppleGetsADeclarativeMessageAndEverybodyElseGetsTheWorkerOne(t *testing.T) {
	t.Parallel()
	b := newBrowser(t)
	n := Notification{Title: "Clawdline", Body: "一個 session 在等你", URL: "/#session=%2514",
		Tag: "session-1", Icon: "/icon-192.png", At: time.Unix(1_700_000_000, 0)}

	apple := b.subscription("a", "https://web.push.apple.com/one", "phone")
	apple.Origin = "https://console.example"
	message, shortened, ok := Build(n, apple)
	if !ok || shortened {
		t.Fatalf("Build: ok=%v shortened=%v", ok, shortened)
	}
	if message.ContentType != "application/notification+json" {
		t.Errorf("content type is %q", message.ContentType)
	}
	var declared struct {
		WebPush      int `json:"web_push"`
		Notification struct {
			Title     string `json:"title"`
			Body      string `json:"body"`
			Navigate  string `json:"navigate"`
			Timestamp int64  `json:"timestamp"`
			Tag       string `json:"tag"`
			Icon      string `json:"icon"`
		} `json:"notification"`
	}
	if err := json.Unmarshal(message.Plaintext, &declared); err != nil {
		t.Fatalf("the declarative payload does not parse: %v", err)
	}
	if declared.WebPush != 8030 {
		t.Errorf("web_push is %d", declared.WebPush)
	}
	if declared.Notification.Navigate != "https://console.example/#session=%2514" {
		t.Errorf("navigate is %q", declared.Notification.Navigate)
	}
	if declared.Notification.Icon != "https://console.example/icon-192.png" {
		t.Errorf("icon is %q", declared.Notification.Icon)
	}
	if declared.Notification.Timestamp != n.At.UnixMilli() {
		t.Errorf("timestamp is %d, want %d", declared.Notification.Timestamp, n.At.UnixMilli())
	}
	if declared.Notification.Tag != Topic("session-1") {
		t.Errorf("tag is %q", declared.Notification.Tag)
	}

	// An Apple row with no trustworthy origin keeps the worker payload, as does
	// everybody else.
	for name, subscription := range map[string]Subscription{
		"apple without an origin": b.subscription("a", "https://web.push.apple.com/one", "phone"),
		"a mozilla endpoint":      b.subscription("b", "https://updates.push.services.mozilla.com/x", "laptop"),
		"a google endpoint":       b.subscription("c", "https://fcm.googleapis.com/fcm/send/x", "laptop"),
	} {
		message, _, ok := Build(n, subscription)
		if !ok {
			t.Fatalf("%s: Build refused", name)
		}
		if message.ContentType != "application/octet-stream" {
			t.Errorf("%s: content type is %q", name, message.ContentType)
		}
		var worker struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			URL   string `json:"url"`
			Tag   string `json:"tag"`
			Icon  string `json:"icon"`
			At    int64  `json:"at"`
		}
		if err := json.Unmarshal(message.Plaintext, &worker); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if worker.URL != n.URL || worker.Title != n.Title || worker.Body != n.Body {
			t.Errorf("%s: %+v", name, worker)
		}
		if worker.At != n.At.Unix() {
			t.Errorf("%s: at is %d", name, worker.At)
		}
	}
}

// TestALongBodyLosesItsTailAndNotItsTitle. Decorations go first; the sentence
// keeps its beginning.
func TestALongBodyLosesItsTailAndNotItsTitle(t *testing.T) {
	t.Parallel()
	b := newBrowser(t)
	subscription := b.subscription("a", "https://fcm.googleapis.com/x", "phone")
	long := strings.Repeat("重", 4000)
	message, shortened, ok := Build(Notification{Title: "Clawdline", Body: long,
		Icon: "/icon-192.png", At: time.Unix(1, 0)}, subscription)
	if !ok || !shortened {
		t.Fatalf("Build: ok=%v shortened=%v", ok, shortened)
	}
	if len(message.Plaintext) > MaxPayload {
		t.Errorf("the payload is %d octets, and the ceiling is %d", len(message.Plaintext), MaxPayload)
	}
	var worker struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Icon  string `json:"icon"`
	}
	if err := json.Unmarshal(message.Plaintext, &worker); err != nil {
		t.Fatalf("the shortened payload does not parse: %v", err)
	}
	if worker.Title != "Clawdline" {
		t.Errorf("the title was touched: %q", worker.Title)
	}
	if worker.Icon != "" {
		t.Error("the decoration was kept over the sentence")
	}
	if !strings.HasPrefix(long, worker.Body) || worker.Body == "" {
		t.Errorf("the body is not a prefix of what was asked for: %d characters", len([]rune(worker.Body)))
	}
	// Cut on a character boundary, never inside one.
	for _, r := range worker.Body {
		if r == '�' {
			t.Fatal("a character was cut in half")
		}
	}
}

// ---- the store ----

func openStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store.Log = func(string, ...any) {}
	return store
}

// TestAnEmptyStoreMintsNothingUntilItIsAsked.
func TestAnEmptyStoreMintsNothingUntilItIsAsked(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	if store.HasVAPIDKey() {
		t.Error("a fresh store already claims an identity")
	}
	rows, err := store.Subscriptions()
	if err != nil || len(rows) != 0 {
		t.Fatalf("subscriptions: %d rows, %v", len(rows), err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), VAPIDKeyFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("reading an empty store wrote a key")
	}
	key, err := store.VAPIDKey()
	if err != nil || !key.Valid() {
		t.Fatalf("VAPIDKey: %v", err)
	}
	if !store.HasVAPIDKey() {
		t.Error("the minted key was not written through")
	}
}

// TestTheIdentityIsTheSameTomorrow, because a new one silently unsubscribes
// every device.
func TestTheIdentityIsTheSameTomorrow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first.Log = func(string, ...any) {}
	key, err := first.VAPIDKey()
	if err != nil {
		t.Fatalf("VAPIDKey: %v", err)
	}
	again, err := Open(root)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	again.Log = func(string, ...any) {}
	read, err := again.VAPIDKey()
	if err != nil {
		t.Fatalf("VAPIDKey: %v", err)
	}
	if read.PublicKey() != key.PublicKey() {
		t.Error("the identity changed between two runs")
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(first.Dir(), VAPIDKeyFile))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("the key file is %v, want 0600 (%v)", info.Mode().Perm(), err)
		}
		dir, err := os.Stat(first.Dir())
		if err != nil || dir.Mode().Perm() != 0o700 {
			t.Errorf("the directory is %v, want 0700 (%v)", dir.Mode().Perm(), err)
		}
	}
}

// TestAKeyThatWillNotParseIsReplacedAndAKeyThatCannotBeReadIsNot.
//
// The two halves are different failures. A file full of the wrong bytes has
// already lost every subscription it could have signed for, so minting is the
// only way back; a file that cannot be read at all may be a transient fault,
// and minting over it would throw away an identity that is still there.
func TestAKeyThatWillNotParseIsReplacedAndAKeyThatCannotBeReadIsNot(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"not base64":                 "not a key at all\n",
		"base64 of the wrong length": "AAAA\n",
		"a scalar of zero":           base64.StdEncoding.EncodeToString(make([]byte, 32)) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			said := 0
			store.Log = func(string, ...any) { said++ }
			path := filepath.Join(store.Dir(), VAPIDKeyFile)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("writing the broken file: %v", err)
			}
			key, err := store.VAPIDKey()
			if err != nil || !key.Valid() {
				t.Fatalf("VAPIDKey: %v", err)
			}
			if said == 0 {
				t.Error("an identity was replaced without saying so")
			}
			written, err := os.ReadFile(path)
			if err != nil || strings.TrimSpace(string(written)) == strings.TrimSpace(body) {
				t.Error("the broken key was left on disk")
			}
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("a symlink", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			store, err := Open(root)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			store.Log = func(string, ...any) {}
			elsewhere := filepath.Join(root, "elsewhere")
			if err := os.WriteFile(elsewhere, []byte("someone else's\n"), 0o644); err != nil {
				t.Fatalf("writing the other file: %v", err)
			}
			if err := os.Symlink(elsewhere, filepath.Join(store.Dir(), VAPIDKeyFile)); err != nil {
				t.Fatalf("symlink: %v", err)
			}
			if _, err := store.VAPIDKey(); !errors.Is(err, ErrNotRegular) {
				t.Errorf("want ErrNotRegular, got %v", err)
			}
			body, err := os.ReadFile(elsewhere)
			if err != nil || string(body) != "someone else's\n" {
				t.Errorf("the linked-to file was written through: %q %v", body, err)
			}
		})
	}
}

// TestRefusesTheSwiftAppsDirectory. The two apps keep separate subscriptions on
// purpose: a browser paired with one has not agreed to be told things by the
// other.
func TestRefusesTheSwiftAppsDirectory(t *testing.T) {
	t.Parallel()
	swift := t.TempDir()
	if _, err := Open(swift, swift); !errors.Is(err, ErrForeignDir) {
		t.Errorf("want ErrForeignDir, got %v", err)
	}
	if _, err := Open(filepath.Join(swift, "inside"), swift); !errors.Is(err, ErrForeignDir) {
		t.Errorf("a directory inside it: want ErrForeignDir, got %v", err)
	}
	if _, err := Open(t.TempDir(), swift); err != nil {
		t.Errorf("an unrelated directory must be fine: %v", err)
	}
}

// TestOneRowPerDeviceAndPerEndpoint. A browser can replace an endpoint across a
// reinstall; keeping the old row sends the same phone two notifications and
// leaves the test button reporting two sends.
func TestOneRowPerDeviceAndPerEndpoint(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	if err := store.Add(b.subscription("one", "https://a.test/1", "phone")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.Add(b.subscription("two", "https://a.test/2", "phone")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rows, _ := store.Subscriptions()
	if len(rows) != 1 || rows[0].ID != "two" {
		t.Fatalf("after a re-subscribe: %+v", rows)
	}
	if err := store.Add(b.subscription("three", "https://a.test/2", "laptop")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rows, _ = store.Subscriptions()
	if len(rows) != 1 || rows[0].ID != "three" {
		t.Fatalf("after the same endpoint on another device: %+v", rows)
	}
	if err := store.Add(b.subscription("four", "https://a.test/4", "phone")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rows, _ := store.Subscriptions(); len(rows) != 2 {
		t.Fatalf("two devices should be two rows: %+v", rows)
	}
	if mine, _ := store.ForDevice("phone"); len(mine) != 1 || mine[0].ID != "four" {
		t.Errorf("ForDevice: %+v", mine)
	}
	if removed, err := store.RemoveDevice("laptop"); err != nil || removed != 1 {
		t.Errorf("RemoveDevice: %d, %v", removed, err)
	}
	if rows, _ := store.Subscriptions(); len(rows) != 1 {
		t.Errorf("revoking a device left its subscription: %+v", rows)
	}
}

// TestSubscriptionsSurviveARestart, keys and origin and all.
func TestSubscriptionsSurviveARestart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first.Log = func(string, ...any) {}
	b := newBrowser(t)
	want := b.subscription("one", "https://fcm.googleapis.com/fcm/send/abc", "phone")
	want.Origin = "https://console.example"
	if err := first.Add(want); err != nil {
		t.Fatalf("Add: %v", err)
	}

	again, err := Open(root)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	again.Log = func(string, ...any) {}
	rows, err := again.Subscriptions()
	if err != nil || len(rows) != 1 {
		t.Fatalf("subscriptions: %d rows, %v", len(rows), err)
	}
	got := rows[0]
	if got.ID != want.ID || got.Endpoint != want.Endpoint || got.Device != want.Device ||
		got.Origin != want.Origin || !bytes.Equal(got.P256dh, want.P256dh) || !bytes.Equal(got.Auth, want.Auth) {
		t.Errorf("a row changed on the way to disk:\n got %+v\nwant %+v", got, want)
	}
	if !got.Created.Equal(want.Created) {
		t.Errorf("created is %v, want %v", got.Created, want.Created)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(again.Dir(), SubscriptionsFile))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("the subscriptions file is %v, want 0600 (%v)", info.Mode().Perm(), err)
		}
	}
}

// TestARowOfTheWrongShapeIsDroppedOnTheWayIn, because finding that out at send
// time means a log line an hour after whatever wrote it has been forgotten.
func TestARowOfTheWrongShapeIsDroppedOnTheWayIn(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	body := `{"version":1,"subscriptions":[
      {"id":"good","endpoint":"https://a.test/1","p256dh":"` + EncodeBase64URL(make([]byte, 65)) +
		`","auth":"` + EncodeBase64URL(make([]byte, 16)) + `","device":"phone","created":1},
      {"id":"short-key","endpoint":"https://a.test/2","p256dh":"AAAA","auth":"` +
		EncodeBase64URL(make([]byte, 16)) + `","device":"laptop","created":2},
      {"id":"no-endpoint","p256dh":"` + EncodeBase64URL(make([]byte, 65)) + `","auth":"` +
		EncodeBase64URL(make([]byte, 16)) + `","device":"x","created":3}]}`
	if err := os.WriteFile(filepath.Join(store.Dir(), SubscriptionsFile), []byte(body), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	rows, err := store.Subscriptions()
	if err != nil {
		t.Fatalf("Subscriptions: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "good" {
		t.Fatalf("kept %+v", rows)
	}
}

// ---- sending ----

// service is a push service that answers whatever it is told to, and remembers
// what it was asked.
type service struct {
	*httptest.Server
	statuses   []int
	retryAfter string
	calls      atomic.Int64
	last       atomic.Pointer[http.Header]
	bodies     atomic.Int64
}

func newService(t *testing.T, statuses ...int) *service {
	t.Helper()
	s := &service{statuses: statuses}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(s.calls.Add(1))
		header := r.Header.Clone()
		s.last.Store(&header)
		if r.ContentLength > 0 {
			s.bodies.Add(r.ContentLength)
		}
		status := s.statuses[min(n-1, len(s.statuses)-1)]
		if status == http.StatusTooManyRequests && s.retryAfter != "" {
			w.Header().Set("Retry-After", s.retryAfter)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(s.Close)
	return s
}

func senderFor(t *testing.T, store *Store, slept *[]time.Duration) *Sender {
	t.Helper()
	return &Sender{
		Store: store,
		Log:   func(string, ...any) {},
		Sleep: func(ctx context.Context, d time.Duration) bool {
			*slept = append(*slept, d)
			return ctx.Err() == nil
		},
	}
}

func TestOneAcceptedSendIsOneSent(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	service := newService(t, http.StatusCreated)
	if err := store.Add(b.subscription("one", service.URL+"/send", "phone")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	var slept []time.Duration
	sender := senderFor(t, store, &slept)
	delivery, err := sender.Send(context.Background(), Notification{
		Title: "Clawdline", Body: "一個 session 在等你", URL: "/#session=%2514", Tag: "session-1",
	}, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Sent != 1 || delivery.Failed != 0 {
		t.Errorf("delivery is %+v", delivery)
	}
	if len(slept) != 0 {
		t.Errorf("an accepted send waited: %v", slept)
	}
	header := *service.last.Load()
	for name, want := range map[string]string{
		"Content-Encoding": "aes128gcm",
		"Content-Type":     "application/octet-stream",
		"TTL":              "3600",
		"Urgency":          "high",
		"Topic":            Topic("session-1"),
	} {
		if got := header.Get(name); got != want {
			t.Errorf("%s is %q, want %q", name, got, want)
		}
	}
	if !strings.HasPrefix(header.Get("Authorization"), "vapid t=") {
		t.Errorf("Authorization is %q", header.Get("Authorization"))
	}
	if service.bodies.Load() == 0 {
		t.Error("nothing was actually posted")
	}
}

// TestAGoneSubscriptionIsDropped and nothing else is.
func TestAGoneSubscriptionIsDropped(t *testing.T) {
	t.Parallel()
	for name, status := range map[string]int{"404": http.StatusNotFound, "410": http.StatusGone} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			b := newBrowser(t)
			service := newService(t, status)
			_ = store.Add(b.subscription("one", service.URL+"/send", "phone"))
			var slept []time.Duration
			delivery, err := senderFor(t, store, &slept).Send(context.Background(),
				Notification{Title: "x", Body: "y"}, "")
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if delivery.Sent != 0 || delivery.Failed != 1 {
				t.Errorf("delivery is %+v", delivery)
			}
			if rows, _ := store.Subscriptions(); len(rows) != 0 {
				t.Errorf("a gone subscription was kept: %+v", rows)
			}
			if service.calls.Load() != 1 {
				t.Errorf("a gone subscription was retried %d times", service.calls.Load())
			}
		})
	}
}

// TestARefusalThatIsNotAboutTheSubscriptionLeavesItAlone. The one thing worse
// than a missed notification is quietly unsubscribing somebody because a
// service was having a bad afternoon.
func TestARefusalThatIsNotAboutTheSubscriptionLeavesItAlone(t *testing.T) {
	t.Parallel()
	for name, status := range map[string]int{
		"400": http.StatusBadRequest,
		"401": http.StatusUnauthorized,
		"403": http.StatusForbidden,
		"413": http.StatusRequestEntityTooLarge,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			b := newBrowser(t)
			service := newService(t, status)
			_ = store.Add(b.subscription("one", service.URL+"/send", "phone"))
			var slept []time.Duration
			delivery, _ := senderFor(t, store, &slept).Send(context.Background(),
				Notification{Title: "x", Body: "y"}, "")
			if delivery.Failed != 1 {
				t.Errorf("delivery is %+v", delivery)
			}
			if rows, _ := store.Subscriptions(); len(rows) != 1 {
				t.Errorf("the subscription was dropped over a %s", name)
			}
			if service.calls.Load() != 1 {
				t.Errorf("a permanent refusal was retried %d times", service.calls.Load())
			}
		})
	}
}

// TestABadAfternoonIsRetriedAndThenGivenUpOn.
func TestABadAfternoonIsRetriedAndThenGivenUpOn(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	service := newService(t, http.StatusServiceUnavailable)
	_ = store.Add(b.subscription("one", service.URL+"/send", "phone"))
	var slept []time.Duration
	delivery, err := senderFor(t, store, &slept).Send(context.Background(),
		Notification{Title: "x", Body: "y"}, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Failed != 1 {
		t.Errorf("delivery is %+v", delivery)
	}
	if service.calls.Load() != int64(DefaultAttempts) {
		t.Errorf("tried %d times, want %d", service.calls.Load(), DefaultAttempts)
	}
	if len(slept) != DefaultAttempts-1 {
		t.Fatalf("waited %d times, want %d", len(slept), DefaultAttempts-1)
	}
	for i, d := range slept {
		if d <= 0 || d > RetryCeiling {
			t.Errorf("wait %d is %v", i, d)
		}
	}
	if slept[1] < slept[0] {
		t.Errorf("the backoff did not grow: %v", slept)
	}
	if rows, _ := store.Subscriptions(); len(rows) != 1 {
		t.Error("a retryable failure dropped the subscription")
	}
}

// TestARetryThatSucceedsIsASend.
func TestARetryThatSucceedsIsASend(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	service := newService(t, http.StatusBadGateway, http.StatusCreated)
	_ = store.Add(b.subscription("one", service.URL+"/send", "phone"))
	var slept []time.Duration
	delivery, _ := senderFor(t, store, &slept).Send(context.Background(),
		Notification{Title: "x", Body: "y"}, "")
	if delivery.Sent != 1 || delivery.Failed != 0 {
		t.Errorf("delivery is %+v", delivery)
	}
	if service.calls.Load() != 2 {
		t.Errorf("tried %d times", service.calls.Load())
	}
}

// TestRetryAfterIsHonouredAndItsCeilingIsARefusalToQueue.
func TestRetryAfterIsHonouredAndItsCeilingIsARefusalToQueue(t *testing.T) {
	t.Parallel()
	t.Run("seconds", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		b := newBrowser(t)
		service := newService(t, http.StatusTooManyRequests, http.StatusCreated)
		service.retryAfter = "7"
		_ = store.Add(b.subscription("one", service.URL+"/send", "phone"))
		var slept []time.Duration
		delivery, _ := senderFor(t, store, &slept).Send(context.Background(),
			Notification{Title: "x", Body: "y"}, "")
		if delivery.Sent != 1 {
			t.Errorf("delivery is %+v", delivery)
		}
		if len(slept) != 1 || slept[0] != 7*time.Second {
			t.Errorf("waited %v, want one wait of 7s", slept)
		}
	})
	t.Run("past the ceiling", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		b := newBrowser(t)
		service := newService(t, http.StatusTooManyRequests)
		service.retryAfter = "600"
		_ = store.Add(b.subscription("one", service.URL+"/send", "phone"))
		var slept []time.Duration
		delivery, _ := senderFor(t, store, &slept).Send(context.Background(),
			Notification{Title: "x", Body: "y"}, "")
		if delivery.Failed != 1 {
			t.Errorf("delivery is %+v", delivery)
		}
		if len(slept) != 0 {
			t.Errorf("a message that would be stale on arrival was queued: %v", slept)
		}
		if service.calls.Load() != 1 {
			t.Errorf("tried %d times", service.calls.Load())
		}
	})
}

func TestRetryAfterReadsBothSpellings(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for header, want := range map[string]time.Duration{
		"":                              0,
		"  ":                            0,
		"5":                             5 * time.Second,
		"0":                             0,
		"-3":                            0,
		"9999":                          -1,
		"not a number":                  0,
		"Fri, 18 Sep 2026 12:00:20 GMT": 20 * time.Second,
		"Fri, 18 Sep 2026 11:59:00 GMT": 0,
		"Fri, 18 Sep 2026 13:00:00 GMT": -1,
	} {
		if got := retryAfter(header, now); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", header, got, want)
		}
	}
}

// TestASendWithNothingSubscribedMintsNothing. A machine that has never had a
// browser subscribe should not grow key material because a session changed
// state.
func TestASendWithNothingSubscribedMintsNothing(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	var slept []time.Duration
	delivery, err := senderFor(t, store, &slept).Send(context.Background(),
		Notification{Title: "x", Body: "y"}, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Total() != 0 {
		t.Errorf("delivery is %+v", delivery)
	}
	if store.HasVAPIDKey() {
		t.Error("a send with no targets minted an identity")
	}
}

// TestSendingToOneDeviceReachesThatDeviceOnly. A test whose blast radius is
// larger than the thing being tested teaches you to be careful with it.
func TestSendingToOneDeviceReachesThatDeviceOnly(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	mine := newService(t, http.StatusCreated)
	theirs := newService(t, http.StatusCreated)
	_ = store.Add(b.subscription("one", mine.URL+"/send", "phone"))
	_ = store.Add(b.subscription("two", theirs.URL+"/send", "somebody-else"))

	var slept []time.Duration
	delivery, err := senderFor(t, store, &slept).Send(context.Background(),
		Notification{Title: "x", Body: "y"}, "phone")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Sent != 1 {
		t.Errorf("delivery is %+v", delivery)
	}
	if theirs.calls.Load() != 0 {
		t.Errorf("the other device was sent %d message(s)", theirs.calls.Load())
	}
	if mine.calls.Load() != 1 {
		t.Errorf("this device was sent %d message(s)", mine.calls.Load())
	}
}

// TestACancelledSendStopsRatherThanRetrying.
func TestACancelledSendStopsRatherThanRetrying(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	b := newBrowser(t)
	service := newService(t, http.StatusServiceUnavailable)
	_ = store.Add(b.subscription("one", service.URL+"/send", "phone"))
	ctx, cancel := context.WithCancel(context.Background())
	sender := &Sender{
		Store: store,
		Log:   func(string, ...any) {},
		Sleep: func(context.Context, time.Duration) bool { cancel(); return false },
	}
	delivery, err := sender.Send(ctx, Notification{Title: "x", Body: "y"}, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Failed != 1 {
		t.Errorf("delivery is %+v", delivery)
	}
	if service.calls.Load() != 1 {
		t.Errorf("a cancelled send made %d requests", service.calls.Load())
	}
}

func TestServiceAcceptedIsTwoHundredsOnly(t *testing.T) {
	t.Parallel()
	for status, want := range map[int]bool{
		200: true, 201: true, 204: true, 299: true,
		100: false, 301: false, 400: false, 404: false, 410: false, 500: false, 0: false,
	} {
		if got := ServiceAccepted(status); got != want {
			t.Errorf("ServiceAccepted(%d) = %v", status, got)
		}
	}
}
