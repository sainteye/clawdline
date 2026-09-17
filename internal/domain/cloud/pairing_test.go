package cloud

// The oracle for this file is `testdata/protocol-vectors.json`'s
// `pairing_handover` entry, which carries both private keys and the fixed
// nonce. That makes the whole handover reproducible: these tests compare
// **bytes**, not values, because a test that decoded both sides and compared
// the results would pass for an implementation that sealed the wrong spelling.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
)

// mustVectorOffer reads the offer the fixture carries, as bytes and as a value.
func mustVectorOffer(t *testing.T, file vectorFile) (PairingOffer, []byte) {
	t.Helper()
	offer, err := DecodePairingOffer([]byte(file.PairingHandover.Offer), file.PairingHandover.NowMilliseconds)
	if err != nil {
		t.Fatalf("the published offer does not decode: %v", err)
	}
	return offer, []byte(file.PairingHandover.Offer)
}

func TestTheOfferCanonicalizesToThePublishedBytes(t *testing.T) {
	file := loadVectors(t)
	offer, want := mustVectorOffer(t, file)
	if got := offer.CanonicalJSON(); !bytes.Equal(got, want) {
		t.Fatalf("the offer canonicalizes to\n%s\nwanted\n%s", got, want)
	}
	if got := offer.Fragment(); got != file.PairingHandover.OfferFragment {
		t.Fatalf("the offer fragment is %q, wanted %q", got, file.PairingHandover.OfferFragment)
	}
}

func TestTheOfferFragmentRoundTripsThroughBase64URL(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	decoded, err := DecodePairingOfferFragment(file.PairingHandover.OfferFragment, file.PairingHandover.NowMilliseconds)
	if err != nil {
		t.Fatalf("the published fragment does not decode: %v", err)
	}
	if decoded != offer {
		t.Fatalf("the fragment decodes to a different offer:\n%#v\n%#v", decoded, offer)
	}
}

func TestThePhaseKeyMatchesThePublishedValue(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	machinePrivate := mustBase64(t, file.PairingHandover.MachineEphemeralPrivateKey)
	key, err := pairingPhaseKeyFor(offer, machinePrivate, offer.ViewerEphemeralKey)
	if err != nil {
		t.Fatalf("deriving the phase key: %v", err)
	}
	if got := base64.StdEncoding.EncodeToString(key); got != file.PairingHandover.PhaseKey {
		t.Fatalf("the grant phase key is %q, wanted %q", got, file.PairingHandover.PhaseKey)
	}

	// The viewer derives the same key from the other side, which is the whole
	// point of the agreement and is worth one assertion of its own.
	viewerPrivate := mustBase64(t, file.PairingHandover.ViewerEphemeralPrivateKey)
	mirrored, err := pairingPhaseKeyFor(offer, viewerPrivate, file.PairingHandover.Wrapper.EphemeralKey)
	if err != nil {
		t.Fatalf("deriving the viewer's phase key: %v", err)
	}
	if !bytes.Equal(key, mirrored) {
		t.Fatal("the two halves derived different phase keys")
	}
}

func TestTheHandoverSealsToThePublishedWrapper(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	published := file.PairingHandover.Wrapper

	handover, err := decodeVectorHandover(file.PairingHandover.Handover)
	if err != nil {
		t.Fatalf("the published handover does not decode: %v", err)
	}
	if got := handover.CanonicalJSON(); string(got) != file.PairingHandover.Handover {
		t.Fatalf("the handover canonicalizes to\n%s\nwanted\n%s", got, file.PairingHandover.Handover)
	}

	wrapper, err := SealPairingHandover(handover, offer, file.PairingHandover.SenderDeviceID,
		mustBase64(t, file.PairingHandover.MachineEphemeralPrivateKey),
		mustBase64(t, published.Nonce),
		file.PairingHandover.NowMilliseconds)
	if err != nil {
		t.Fatalf("sealing the handover: %v", err)
	}
	if wrapper.Phase != published.Phase || wrapper.PairingID != published.PairingID ||
		wrapper.SenderDeviceID != published.SenderDeviceID ||
		wrapper.EphemeralKey != published.EphemeralKey ||
		wrapper.Nonce != published.Nonce {
		t.Fatalf("the wrapper's authenticated members differ:\n%#v", wrapper)
	}
	if wrapper.Ct != published.Ct {
		t.Fatalf("the ciphertext is\n%s\nwanted\n%s", wrapper.Ct, published.Ct)
	}
	aad, err := wrapper.AAD()
	if err != nil {
		t.Fatalf("building the AAD: %v", err)
	}
	if string(aad) != file.PairingHandover.AAD {
		t.Fatalf("the AAD is\n%s\nwanted\n%s", aad, file.PairingHandover.AAD)
	}
}

func TestTheViewerHalfOpensThePublishedWrapper(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	published := file.PairingHandover.Wrapper
	wrapper := PairingWrapper{
		Phase: published.Phase, PairingID: published.PairingID,
		SenderDeviceID: published.SenderDeviceID, EphemeralKey: published.EphemeralKey,
		Nonce: published.Nonce, Ct: published.Ct,
	}
	opened, err := OpenPairingHandover(wrapper, offer,
		mustBase64(t, file.PairingHandover.ViewerEphemeralPrivateKey),
		published.SenderDeviceID, file.PairingHandover.NowMilliseconds)
	if err != nil {
		t.Fatalf("opening the published wrapper: %v", err)
	}
	if string(opened.CanonicalJSON()) != file.PairingHandover.Handover {
		t.Fatalf("the opened handover is\n%s", opened.CanonicalJSON())
	}
	// What the browser actually keeps: an account key it can decrypt with and
	// a machine key it can verify with. Both are checked here as the bytes the
	// rest of the transport will use.
	secret, err := ContentKeyFromBytes(mustBase64(t, opened.MasterSecret))
	if err != nil || !secret.Valid() {
		t.Fatalf("the handover's master secret is not a content key: %v", err)
	}
	if _, err := PublicKeyFromBytes(mustBase64(t, opened.MachineSigningKey)); err != nil {
		t.Fatalf("the handover's signing key is not an Ed25519 public key: %v", err)
	}
}

func TestAWrapperSealedForOneViewerDoesNotOpenForAnother(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	published := file.PairingHandover.Wrapper
	wrapper := PairingWrapper{
		Phase: published.Phase, PairingID: published.PairingID,
		SenderDeviceID: published.SenderDeviceID, EphemeralKey: published.EphemeralKey,
		Nonce: published.Nonce, Ct: published.Ct,
	}
	// The machine's own ephemeral key stands in for "some other private key":
	// it is the right length and the wrong secret.
	other := mustBase64(t, file.PairingHandover.MachineEphemeralPrivateKey)
	if _, err := OpenPairingHandover(wrapper, offer, other, published.SenderDeviceID,
		file.PairingHandover.NowMilliseconds); !errors.Is(err, ErrPairingSealed) {
		t.Fatalf("a foreign ephemeral key opened the wrapper: %v", err)
	}
}

func TestAWrapperNamingAnotherSenderIsRefused(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	published := file.PairingHandover.Wrapper
	wrapper := PairingWrapper{
		Phase: published.Phase, PairingID: published.PairingID,
		SenderDeviceID: published.SenderDeviceID, EphemeralKey: published.EphemeralKey,
		Nonce: published.Nonce, Ct: published.Ct,
	}
	if _, err := OpenPairingHandover(wrapper, offer,
		mustBase64(t, file.PairingHandover.ViewerEphemeralPrivateKey),
		"mac-somebody-else", file.PairingHandover.NowMilliseconds); !errors.Is(err, ErrPairingSender) {
		t.Fatalf("the control plane's sender was not compared: %v", err)
	}
}

// The negative vectors of `contracts/cloud/v1/crypto-negative-vectors.json`,
// whose expectation for every row is `reject_before_hkdf`: a low-order peer key
// agrees to an all-zero secret, and an all-zero secret is the same secret for
// every peer, so deriving from it hands an attacker the key.
func TestLowOrderPeerKeysAreRefusedBeforeDerivation(t *testing.T) {
	file := loadVectors(t)
	private := mustBase64(t, file.PairingHandover.MachineEphemeralPrivateKey)
	for _, row := range []struct {
		name string
		peer string
	}{
		{"x25519-low-order-zero", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		{"x25519-low-order-one", "AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
	} {
		t.Run(row.name, func(t *testing.T) {
			if _, err := PairingSharedSecret(private, mustBase64(t, row.peer)); !errors.Is(err, ErrPairingAgreement) {
				t.Fatalf("a low-order peer key agreed: %v", err)
			}
		})
	}
	// The same refusal one layer up, for an implementation that computed the
	// secret some other way and handed it here.
	for _, row := range []struct {
		name   string
		shared string
	}{
		{"x25519-all-zero-result", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		{"x25519-short-result", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="},
	} {
		t.Run(row.name, func(t *testing.T) {
			nonce := make([]byte, PairingNonceBytes)
			claim := make([]byte, PairingNonceBytes)
			claim[0] = 1
			if _, err := DerivePairingPhaseKey(mustBase64(t, row.shared), nonce, "pairing-vector-01", claim, PairingPhaseGrant); !errors.Is(err, ErrPairingAgreement) {
				t.Fatalf("an unusable shared secret reached the derivation: %v", err)
			}
		})
	}
}

func TestAnOfferIsRefusedWhenItsFingerprintDoesNotMatchItsKey(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	offer.ViewerFingerprint = "AAAA-BBBB-CCCC-DDDD"
	if err := offer.Validate(file.PairingHandover.NowMilliseconds); !errors.Is(err, ErrPairingFingerprint) {
		t.Fatalf("a substituted fingerprint was accepted: %v", err)
	}
}

func TestAnExpiredOrOverlongOfferIsRefused(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	if err := offer.Validate(offer.ExpiresAt + 1); !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("an expired offer was accepted: %v", err)
	}
	if err := offer.Validate(offer.ExpiresAt - PairingOfferLifetimeMS - 1); !errors.Is(err, ErrPairingLifetime) {
		t.Fatalf("an offer claiming more than its lifetime was accepted: %v", err)
	}
}

func TestAnOfferWithTheSameNonceTwiceIsRefused(t *testing.T) {
	file := loadVectors(t)
	offer, _ := mustVectorOffer(t, file)
	offer.PairingNonce = offer.ClaimNonce
	if err := offer.Validate(file.PairingHandover.NowMilliseconds); !errors.Is(err, ErrPairingNonceReuse) {
		t.Fatalf("one nonce spelled twice was accepted: %v", err)
	}
}

func TestOnlyOneSpellingOfAFragmentIsAccepted(t *testing.T) {
	file := loadVectors(t)
	fragment := file.PairingHandover.OfferFragment
	if _, err := DecodeCanonicalBase64URL(fragment); err != nil {
		t.Fatalf("the published fragment is not canonical: %v", err)
	}
	// Bytes 0xfb 0xff encode as "-_8" in base64url and as "+/8" in the
	// standard alphabet. Spelling the same bytes either of the second two
	// ways, or with the padding a URL fragment does not carry, is refused.
	canonical := EncodeCanonicalBase64URL([]byte{0xfb, 0xff})
	if canonical != "-_8" {
		t.Fatalf("base64url of fb ff is %q, wanted %q", canonical, "-_8")
	}
	for _, spelling := range []string{
		"-_8=",                            // padded
		"+_8",                             // the standard alphabet's 62
		"-/8",                             // the standard alphabet's 63
		"-_9",                             // non-zero unused bits
		fragment[:len(fragment)-1] + "\n", // a byte outside the alphabet
		fragment + "=",                    // padding on a real fragment
		"A",                               // a length no base64 quantum has
		"",                                // nothing at all
	} {
		if _, err := DecodeCanonicalBase64URL(spelling); err == nil {
			t.Fatalf("a non-canonical fragment spelling was accepted: %q", spelling)
		}
	}
}

// The invitation half: the machine shows a secret, the browser seals its offer
// under it, and only this invitation's id opens the result.
func TestTheInvitationCarriesAnOfferAndOnlyItsOwnID(t *testing.T) {
	file := loadVectors(t)
	secret := make([]byte, PairingSecretBytes)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	invitation := PairingInvitation{
		InvitationID: "inv-vector-01",
		Secret:       secret,
		ExpiresAt:    file.PairingHandover.NowMilliseconds + 300_000,
	}
	fragment, err := invitation.Fragment()
	if err != nil {
		t.Fatalf("encoding the invitation: %v", err)
	}
	decoded, err := DecodePairingInvitation(fragment, file.PairingHandover.NowMilliseconds)
	if err != nil {
		t.Fatalf("decoding the invitation: %v", err)
	}
	if decoded.InvitationID != invitation.InvitationID || !bytes.Equal(decoded.Secret, secret) ||
		decoded.ExpiresAt != invitation.ExpiresAt {
		t.Fatalf("the invitation did not round trip: %#v", decoded)
	}

	nonce := make([]byte, NonceBytes)
	nonce[NonceBytes-1] = 7
	blob, err := invitation.SealEncryptedOffer(file.PairingHandover.OfferFragment, nonce)
	if err != nil {
		t.Fatalf("sealing the offer for the invitation: %v", err)
	}
	opened, err := invitation.OpenEncryptedOffer(blob, file.PairingHandover.NowMilliseconds)
	if err != nil {
		t.Fatalf("opening the encrypted offer: %v", err)
	}
	if opened != file.PairingHandover.OfferFragment {
		t.Fatal("the invitation carried a different offer fragment")
	}

	// The invitation id is inside the AAD, so a second invitation holding the
	// same secret still cannot open this one's slot.
	other := invitation
	other.InvitationID = "inv-vector-02"
	if _, err := other.OpenEncryptedOffer(blob, file.PairingHandover.NowMilliseconds); !errors.Is(err, ErrPairingSealed) {
		t.Fatalf("another invitation opened this one's offer: %v", err)
	}
	if _, err := invitation.OpenEncryptedOffer(blob, invitation.ExpiresAt+1); !errors.Is(err, ErrInvitationExpired) {
		t.Fatalf("an expired invitation still opened its offer: %v", err)
	}
}

func TestTheInvitationLinkIsTheFragmentOnAnAppOrigin(t *testing.T) {
	invitation := PairingInvitation{
		InvitationID: "inv-vector-01",
		Secret:       make([]byte, PairingSecretBytes),
		ExpiresAt:    1787817900000,
	}
	fragment, err := invitation.Fragment()
	if err != nil {
		t.Fatalf("encoding the invitation: %v", err)
	}
	link, err := invitation.URL("https://app.clawdline.com/")
	if err != nil {
		t.Fatalf("building the link: %v", err)
	}
	if want := "https://app.clawdline.com/#pair=" + fragment; link != want {
		t.Fatalf("the link is %q, wanted %q", link, want)
	}
	if _, err := invitation.URL(""); err == nil {
		t.Fatal("a link with no origin was produced")
	}
}

// A full machine→viewer round trip with keys neither side published, so the
// agreement is exercised rather than replayed.
func TestAFreshHandoverRoundTrips(t *testing.T) {
	const now = int64(1787817600000)
	viewerSigning, err := NewDeviceKey(nil)
	if err != nil {
		t.Fatalf("drawing the viewer's signing key: %v", err)
	}
	machineSigning, err := NewDeviceKey(nil)
	if err != nil {
		t.Fatalf("drawing the machine's signing key: %v", err)
	}
	master, err := NewContentKey(nil)
	if err != nil {
		t.Fatalf("drawing the master secret: %v", err)
	}
	viewerEphemeral, err := NewX25519PrivateKey(nil)
	if err != nil {
		t.Fatalf("drawing the viewer's ephemeral key: %v", err)
	}
	viewerEphemeralPublic, err := X25519PublicKey(viewerEphemeral)
	if err != nil {
		t.Fatalf("deriving the viewer's ephemeral public key: %v", err)
	}
	claim := make([]byte, PairingNonceBytes)
	pairing := make([]byte, PairingNonceBytes)
	claim[0], pairing[0] = 1, 2

	offer := PairingOffer{
		PairingID:          "pairing-roundtrip",
		ClaimNonce:         base64.StdEncoding.EncodeToString(claim),
		PairingNonce:       base64.StdEncoding.EncodeToString(pairing),
		AccountID:          "usr_roundtrip",
		ViewerDeviceID:     "dev_roundtrip",
		ViewerSigningKey:   base64.StdEncoding.EncodeToString(viewerSigning.PublicKey()),
		ViewerEphemeralKey: base64.StdEncoding.EncodeToString(viewerEphemeralPublic),
		ViewerFingerprint:  viewerSigning.Fingerprint(),
		ExpiresAt:          now + 300_000,
	}
	handover := PairingHandover{
		AccountID:          offer.AccountID,
		MachineID:          "mac_roundtrip",
		MachineSigningKey:  base64.StdEncoding.EncodeToString(machineSigning.PublicKey()),
		MachineFingerprint: machineSigning.Fingerprint(),
		KeyID:              "ms-1",
		MasterSecret:       base64.StdEncoding.EncodeToString(master.Bytes()),
	}
	machineEphemeral, err := NewX25519PrivateKey(nil)
	if err != nil {
		t.Fatalf("drawing the machine's ephemeral key: %v", err)
	}
	nonce := make([]byte, NonceBytes)
	nonce[0] = 9
	wrapper, err := SealPairingHandover(handover, offer, handover.MachineID, machineEphemeral, nonce, now)
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}
	encoded := wrapper.CanonicalJSON()
	decoded, err := DecodePairingWrapper(encoded)
	if err != nil {
		t.Fatalf("decoding the wrapper: %v", err)
	}
	opened, err := OpenPairingHandover(decoded, offer, viewerEphemeral, handover.MachineID, now)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	if opened != handover {
		t.Fatalf("the handover did not round trip:\n%#v\n%#v", opened, handover)
	}
	// The wrapper is what the viewer verifies signatures against afterwards,
	// so the key inside it has to be the machine's actual signing key.
	if !ed25519.Verify(mustBase64(t, opened.MachineSigningKey), []byte("x"), machineSigning.Sign([]byte("x"))) {
		t.Fatal("the handover carried a signing key that does not verify this machine")
	}

	// An account that does not match the offer is refused before anything is
	// sealed: a handover for another account is not a handover, it is a leak.
	wrong := handover
	wrong.AccountID = "usr_somebody_else"
	if _, err := SealPairingHandover(wrong, offer, handover.MachineID, machineEphemeral, nonce, now); !errors.Is(err, ErrPairingAccount) {
		t.Fatalf("a handover for another account was sealed: %v", err)
	}
}

// decodeVectorHandover reads the published handover document. It is here
// rather than in the package because a machine never receives one.
func decodeVectorHandover(canonical string) (PairingHandover, error) {
	value, err := ParseStrict([]byte(canonical))
	if err != nil {
		return PairingHandover{}, err
	}
	if err := exactMembers(value, pairingHandoverMembers); err != nil {
		return PairingHandover{}, err
	}
	if err := requireVersionAndType(value, pairingHandoverType); err != nil {
		return PairingHandover{}, err
	}
	handover := PairingHandover{
		AccountID:          memberString(value, "account_id"),
		MachineID:          memberString(value, "machine_id"),
		MachineSigningKey:  memberString(value, "machine_signing_key"),
		MachineFingerprint: memberString(value, "machine_fingerprint"),
		KeyID:              memberString(value, "key_id"),
		MasterSecret:       memberString(value, "master_secret"),
	}
	return handover, handover.Validate()
}
