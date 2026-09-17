package cloud

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// pinnedOnly answers one sender's key and nothing else, which is what a Mac's
// paired-device roster does.
func pinnedOnly(sender string, key ed25519.PublicKey) PublicKeyFor {
	return func(who string) (ed25519.PublicKey, bool) {
		if who != sender {
			return nil, false
		}
		return key, true
	}
}

// TestSealReproducesEveryPublishedEnvelope is the test this whole package
// exists for: given the vectors' master secret, device seed, nonce and header
// fields, Go must produce the same ciphertext and the same signature the Swift
// app published — not an equivalent one.
func TestSealReproducesEveryPublishedEnvelope(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	secret, err := ContentKeyFromBytes(mustBase64(t, vectors.MasterSecret))
	if err != nil {
		t.Fatalf("master secret: %v", err)
	}
	signer, err := DeviceKeyFromSeed(mustBase64(t, vectors.Ed25519Seed))
	if err != nil {
		t.Fatalf("device seed: %v", err)
	}

	if len(vectors.Envelopes) == 0 {
		t.Fatal("the vector file carries no envelopes")
	}
	for _, vector := range vectors.Envelopes {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			want := vector.Envelope
			got, err := Seal(mustBase64(t, vector.Plaintext), SealParams{
				Ch:     want.Ch,
				Seq:    want.Seq,
				Ts:     want.Ts,
				Class:  Class(want.Class),
				KeyID:  want.KeyID,
				Sender: want.Sender,
				Key:    secret,
				Signer: signer,
				Nonce:  mustBase64(t, want.Nonce),
			})
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			if got.Ct != want.Ct {
				t.Errorf("ct differs\n got %.64s…\nwant %.64s…", got.Ct, want.Ct)
			}
			if got.Sig != want.Sig {
				t.Errorf("sig differs\n got %s\nwant %s", got.Sig, want.Sig)
			}
			if got.Nonce != want.Nonce || got.V != want.V || got.Ch != want.Ch ||
				got.Seq != want.Seq || got.Ts != want.Ts || string(got.Class) != want.Class ||
				got.KeyID != want.KeyID || got.Sender != want.Sender {
				t.Errorf("a header field differs: %+v", got)
			}

			// And the far end of the same rule: the published envelope opens
			// back to the published plaintext.
			opened, err := got.Open(secret, pinnedOnly(want.Sender, signer.PublicKey()))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if base64.StdEncoding.EncodeToString(opened) != vector.Plaintext {
				t.Errorf("the plaintext did not survive the round trip")
			}
		})
	}
}

// TestSigningStringIsTheDocumentedOrder pins the one string everything else
// rests on, for a vector whose fields are all short enough to read.
func TestSigningStringIsTheDocumentedOrder(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	stream := vectors.Envelopes[0]
	if stream.Name != "stream-empty" {
		t.Fatalf("the vector order changed; first entry is %q", stream.Name)
	}
	e := Envelope{
		V: stream.Envelope.V, Ch: stream.Envelope.Ch, Seq: stream.Envelope.Seq,
		Ts: stream.Envelope.Ts, Class: Class(stream.Envelope.Class),
		KeyID: stream.Envelope.KeyID, Nonce: stream.Envelope.Nonce,
		Ct: stream.Envelope.Ct, Sender: stream.Envelope.Sender, Sig: stream.Envelope.Sig,
	}
	want := "1|s/mac-01/session-01|1|1787817600000|stream|ms-1|AAAAAAAAAAAAAAAB|XMkcF68Slm6Z1e2GdfAxrA=="
	if got := e.SigningString(); got != want {
		t.Errorf("signing string\n got %q\nwant %q", got, want)
	}
	if strings.Count(want, "|") != 7 {
		t.Errorf("the signing string must carry exactly seven separators")
	}
}

// TestVerifyRefusesWhatItShould covers the three ways a signature check can be
// asked to say yes and must not.
func TestVerifyRefusesWhatItShould(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	signer, err := DeviceKeyFromSeed(mustBase64(t, vectors.Ed25519Seed))
	if err != nil {
		t.Fatalf("device seed: %v", err)
	}
	vector := vectors.Envelopes[1] // control-command
	base := Envelope{
		V: vector.Envelope.V, Ch: vector.Envelope.Ch, Seq: vector.Envelope.Seq,
		Ts: vector.Envelope.Ts, Class: Class(vector.Envelope.Class),
		KeyID: vector.Envelope.KeyID, Nonce: vector.Envelope.Nonce,
		Ct: vector.Envelope.Ct, Sender: vector.Envelope.Sender, Sig: vector.Envelope.Sig,
	}
	pinned := pinnedOnly(base.Sender, signer.PublicKey())
	if !base.Verify(pinned) {
		t.Fatal("the published envelope must verify against its own pinned key")
	}

	t.Run("an unpinned sender", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Sender = "device-vector-02"
		if other.Verify(pinned) {
			t.Error("a sender with no pinned key must not verify")
		}
	})
	t.Run("sender is outside the signed bytes, so the pin is the binding", func(t *testing.T) {
		t.Parallel()
		// sender is outside the signed bytes on purpose: swapping it must
		// select another pinned key or none, never reuse this one.
		other := base
		other.Sender = "device-vector-02"
		anyKey := func(string) (ed25519.PublicKey, bool) { return signer.PublicKey(), true }
		if !other.Verify(anyKey) {
			t.Error("the signature itself does not cover sender; this documents that")
		}
	})
	t.Run("one flipped class", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Class = ClassDispatch
		if other.Verify(pinned) {
			t.Error("class is inside the signed bytes and must break the signature")
		}
	})
	t.Run("a re-spelled ciphertext", func(t *testing.T) {
		t.Parallel()
		other := base
		raw, err := base64.StdEncoding.DecodeString(other.Ct)
		if err != nil {
			t.Fatalf("decoding ct: %v", err)
		}
		other.Ct = base64.RawStdEncoding.EncodeToString(raw) // same bytes, no padding
		if other.Verify(pinned) {
			t.Error("an unpadded spelling of the same bytes must not verify")
		}
	})
}

// TestOpenRefusesAnUnpairedSenderBeforeItDecrypts is D1 in one assertion: an
// attacker holding the account session can attach a device, but a Mac opens
// nothing for a device it did not pin.
func TestOpenRefusesAnUnpairedSenderBeforeItDecrypts(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	secret, err := ContentKeyFromBytes(mustBase64(t, vectors.MasterSecret))
	if err != nil {
		t.Fatalf("master secret: %v", err)
	}
	vector := vectors.Envelopes[1]
	e := Envelope{
		V: vector.Envelope.V, Ch: vector.Envelope.Ch, Seq: vector.Envelope.Seq,
		Ts: vector.Envelope.Ts, Class: Class(vector.Envelope.Class),
		KeyID: vector.Envelope.KeyID, Nonce: vector.Envelope.Nonce,
		Ct: vector.Envelope.Ct, Sender: vector.Envelope.Sender, Sig: vector.Envelope.Sig,
	}
	nobody := func(string) (ed25519.PublicKey, bool) { return nil, false }
	if _, err := e.Open(secret, nobody); !errors.Is(err, ErrUnknownSender) {
		t.Errorf("want ErrUnknownSender, got %v", err)
	}
}

// TestCanonicalEnvelopeBytes checks the exact ten-field JSON spelling against
// the two envelopes the vector file pins by digest.
func TestCanonicalEnvelopeBytes(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	for _, pinned := range []canonicalBytesVector{
		vectors.ControlResponse.RequestEnvelope,
		vectors.ControlResponse.ResponseEnvelope,
	} {
		t.Run(pinned.Name, func(t *testing.T) {
			t.Parallel()
			e, err := DecodeEnvelope([]byte(pinned.Body))
			if err != nil {
				t.Fatalf("DecodeEnvelope: %v", err)
			}
			got, err := e.CanonicalJSON()
			if err != nil {
				t.Fatalf("CanonicalJSON: %v", err)
			}
			if string(got) != pinned.Body {
				t.Errorf("canonical bytes differ\n got %s\nwant %s", got, pinned.Body)
			}
			if len(got) != pinned.ByteLength {
				t.Errorf("byte length %d, want %d", len(got), pinned.ByteLength)
			}
			if sha256Hex(got) != pinned.SHA256 {
				t.Errorf("sha256 %s, want %s", sha256Hex(got), pinned.SHA256)
			}
			if pinned.FieldCount != len(envelopeFields) {
				t.Errorf("the vector pins %d fields, this package knows %d",
					pinned.FieldCount, len(envelopeFields))
			}
		})
	}
}

// TestControlResponseUsesTheRequestReplyKey is the one place where the master
// secret is deliberately the wrong key: a ctlr response is sealed under the
// 32 bytes the request carried, so a viewer that did not ask cannot read the
// answer even though it holds the account secret.
func TestControlResponseUsesTheRequestReplyKey(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	control := vectors.ControlResponse

	master, err := ContentKeyFromBytes(mustBase64(t, vectors.MasterSecret))
	if err != nil {
		t.Fatalf("master secret: %v", err)
	}
	reply, err := ContentKeyFromBytes(mustBase64(t, control.ReplyKey.Key))
	if err != nil {
		t.Fatalf("reply key: %v", err)
	}
	signer, err := DeviceKeyFromSeed(mustBase64(t, vectors.Ed25519Seed))
	if err != nil {
		t.Fatalf("device seed: %v", err)
	}

	response, err := DecodeEnvelope([]byte(control.ResponseEnvelope.Body))
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if response.Ch != control.Channel {
		t.Errorf("channel %q, want %q", response.Ch, control.Channel)
	}
	if !IsReplyKeyID(response.KeyID) || response.KeyID != control.ReplyKey.KeyID {
		t.Errorf("key id %q is not the request's reply key", response.KeyID)
	}
	pinned := pinnedOnly(response.Sender, signer.PublicKey())

	opened, err := response.Open(reply, pinned)
	if err != nil {
		t.Fatalf("the reply key must open the response: %v", err)
	}
	if string(opened) != control.ResponsePayload.Body {
		t.Errorf("payload differs\n got %s\nwant %s", opened, control.ResponsePayload.Body)
	}
	if _, err := response.Open(master, pinned); !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("the master secret must not open a ctlr response, got %v", err)
	}

	// The vector states both outcomes itself; read them rather than trusting
	// the two assertions above to stay in step with it.
	for _, outcome := range control.ResponseOpenResults {
		key := master
		if outcome.KeyID == control.ReplyKey.KeyID {
			key = reply
		}
		_, err := response.Open(key, pinned)
		if (err == nil) != outcome.Succeeds {
			t.Errorf("%s: succeeds=%v, err=%v", outcome.Name, outcome.Succeeds, err)
		}
	}

	// The request rides the ordinary command channel under the account secret.
	request, err := DecodeEnvelope([]byte(control.RequestEnvelope.Body))
	if err != nil {
		t.Fatalf("DecodeEnvelope(request): %v", err)
	}
	payload, err := request.Open(master, pinnedOnly(request.Sender, signer.PublicKey()))
	if err != nil {
		t.Fatalf("the master secret must open the request: %v", err)
	}
	value, err := ParseStrict(payload)
	if err != nil {
		t.Fatalf("the request payload must be canonical JSON: %v", err)
	}
	for _, name := range []string{"v", "type", "request_id", "deadline_at", "reply"} {
		if _, ok := value.Member(name); !ok {
			t.Errorf("the request payload has no %q", name)
		}
	}
}

// TestChannelGrammar walks the whole vocabulary and the refusals around it.
func TestChannelGrammar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		channel string
		class   Class
		ok      bool
		why     string
	}{
		{"s/mac-01/session-01", ClassStream, true, ""},
		{"t/mac-01/session-01", ClassStream, true, ""},
		{"orch/mac-01", ClassStream, true, ""},
		{"ctl/mac-01", ClassCtl, true, ""},
		{"ctl/mac-01", ClassDispatch, true, "a dispatch is a command on the command rail"},
		{"ctlr/mac-01/viewer-01", ClassCtl, true, "the response rail"},
		{"ho/account-01/handoff-01", ClassHo, true, ""},

		{"s/mac-01", ClassStream, false, "a session channel needs two segments"},
		{"orch/mac-01/extra", ClassStream, false, "an orchestrator channel takes one"},
		{"ctl/mac-01", ClassStream, false, "stream may not ride the command rail"},
		{"ctlr/mac-01/viewer-01", ClassDispatch, false, "a response is never a dispatch"},
		{"s/mac-01/session-01", ClassCtl, false, "a command may not ride a snapshot channel"},
		{"ho/account-01/handoff-01", ClassStream, false, "handoff carries only ho"},
		{"wh/anything/at-all", ClassStream, false, "wh/ is reserved"},
		{"s/mac 01/session-01", ClassStream, false, "a space is not in the token alphabet"},
		{"s/mac|01/session-01", ClassStream, false, "a pipe would collide with the signing separator"},
		{"s//session-01", ClassStream, false, "an empty segment"},
		{"", ClassStream, false, "an empty channel"},
	}
	for _, c := range cases {
		name := c.channel + " as " + string(c.class)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := validEnvelopeFor(t, c.channel, c.class)
			err := e.validateUnsigned()
			if c.ok && err != nil {
				t.Fatalf("%s: %v", c.why, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("accepted, and should not have been: %s", c.why)
			}
		})
	}
}

// TestProducingCtlrWaitsForItsReaders records the contract gap rather than
// hiding it: this build reads a ctlr envelope and refuses to make one.
func TestProducingCtlrWaitsForItsReaders(t *testing.T) {
	t.Parallel()
	if err := ProducibleChannel("ctl/mac-01"); err != nil {
		t.Errorf("an ordinary command channel must be producible: %v", err)
	}
	if err := ProducibleChannel("ctlr/mac-01/viewer-01"); !errors.Is(err, ErrChannelUnsupported) {
		t.Errorf("want ErrChannelUnsupported for ctlr, got %v", err)
	}
	if err := ProducibleChannel("wh/x/y"); !errors.Is(err, ErrReservedChannel) {
		t.Errorf("want ErrReservedChannel for wh/, got %v", err)
	}
}

// TestDecodeEnvelopeWantsExactlyTenFields — an unknown member and a missing one
// are both refusals, because a reader that ignores extra fields cannot be told
// apart from one that never saw them.
func TestDecodeEnvelopeWantsExactlyTenFields(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	body := vectors.ControlResponse.RequestEnvelope.Body

	if _, err := DecodeEnvelope([]byte(body)); err != nil {
		t.Fatalf("the pinned body must decode: %v", err)
	}
	extra := strings.Replace(body, `{"ch"`, `{"extra":1,"ch"`, 1)
	if _, err := DecodeEnvelope([]byte(extra)); !errors.Is(err, ErrEnvelopeFields) {
		t.Errorf("an extra member: want ErrEnvelopeFields, got %v", err)
	}
	missing := strings.Replace(body, `"v":1`, `"V":1`, 1)
	if _, err := DecodeEnvelope([]byte(missing)); !errors.Is(err, ErrEnvelopeFields) {
		t.Errorf("a renamed member: want ErrEnvelopeFields, got %v", err)
	}
	duplicate := strings.Replace(body, `{"ch"`, `{"ch":"ctl/other","ch"`, 1)
	if _, err := DecodeEnvelope([]byte(duplicate)); !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("a duplicate member: want ErrDuplicateKey, got %v", err)
	}
	// Member order is not part of the wire contract: a browser's JSON.stringify
	// writes insertion order, not this order.
	reordered := `{"v":1,` + strings.TrimPrefix(strings.TrimSuffix(body, `,"v":1}`), `{`) + `}`
	if _, err := DecodeEnvelope([]byte(reordered)); err != nil {
		t.Errorf("an out-of-order envelope must still decode: %v", err)
	}
}

// TestBase64MustBeTheCanonicalSpelling — the signature is over the text, so
// every other spelling of the same bytes is a different envelope.
func TestBase64MustBeTheCanonicalSpelling(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"unpadded":              "AAAAAAAAAAAAAAAB"[:15],
		"base64url":             "-_AAAAAAAAAAAAAA",
		"a newline inside":      "AAAAAAAA\nAAAAAAAB",
		"non-zero unused bits":  "AAAAAAAAAAAAAAB=",
		"whitespace at the end": "AAAAAAAAAAAAAAAB ",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeCanonicalBase64(text); !errors.Is(err, ErrInvalidBase64) {
				t.Errorf("%q was accepted: %v", text, err)
			}
		})
	}
	if _, err := DecodeCanonicalBase64("AAAAAAAAAAAAAAAB"); err != nil {
		t.Errorf("the canonical spelling must be accepted: %v", err)
	}
}

// validEnvelopeFor builds a structurally complete envelope on a channel, so
// that the only thing a grammar case can fail on is its channel and class.
func validEnvelopeFor(t *testing.T, channel string, class Class) Envelope {
	t.Helper()
	keyID := "ms-1"
	if strings.HasPrefix(channel, "ctlr/") {
		keyID = "rk-zF3jN8rQ4Wm2pV6sT0uYxA"
	}
	return Envelope{
		V:      EnvelopeVersion,
		Ch:     channel,
		Seq:    1,
		Ts:     1787817600000,
		Class:  class,
		KeyID:  keyID,
		Nonce:  "AAAAAAAAAAAAAAAB",
		Ct:     "XMkcF68Slm6Z1e2GdfAxrA==",
		Sender: "device-vector-01",
	}
}
