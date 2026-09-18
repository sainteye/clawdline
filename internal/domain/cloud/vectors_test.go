package cloud

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// The published protocol vectors, read once per test binary. testdata/README.md
// records where the file came from and why every key in it is TEST-ONLY.
//
// The whole point of this package is that its bytes match somebody else's, so
// these tests compare bytes rather than values: a test that decoded both sides
// and compared the results would pass for an implementation that signs the
// wrong spelling.

type vectorFile struct {
	Format           int    `json:"format"`
	Cipher           string `json:"cipher"`
	NonceBytes       int    `json:"nonce_bytes"`
	Ed25519Seed      string `json:"ed25519_seed"`
	Ed25519PublicKey string `json:"ed25519_public_key"`
	MasterSecret     string `json:"master_secret"`
	Envelopes        []struct {
		Name      string `json:"name"`
		Plaintext string `json:"plaintext"`
		Envelope  struct {
			V      int    `json:"v"`
			Ch     string `json:"ch"`
			Seq    uint64 `json:"seq"`
			Ts     uint64 `json:"ts"`
			Class  string `json:"class"`
			KeyID  string `json:"key_id"`
			Nonce  string `json:"nonce"`
			Ct     string `json:"ct"`
			Sender string `json:"sender"`
			Sig    string `json:"sig"`
		} `json:"envelope"`
	} `json:"envelopes"`
	ControlResponse struct {
		Channel        string   `json:"channel"`
		AllowedClasses []string `json:"allowed_classes"`
		ReplyKey       struct {
			KeyID             string `json:"key_id"`
			Key               string `json:"key"`
			DecodedByteLength int    `json:"decoded_byte_length"`
		} `json:"reply_key"`
		RequestEnvelope     canonicalBytesVector `json:"request_envelope"`
		ResponseEnvelope    canonicalBytesVector `json:"response_envelope"`
		ResponsePayload     canonicalBytesVector `json:"response_payload"`
		ResponseOpenResults []struct {
			Name     string `json:"name"`
			KeyID    string `json:"key_id"`
			Succeeds bool   `json:"succeeds"`
		} `json:"response_open_results"`
	} `json:"control_response"`
	PairingHandover struct {
		Name     string `json:"name"`
		Offer    string `json:"offer"`
		Handover string `json:"handover"`
		AAD      string `json:"aad"`
		// The fixture publishes both ephemeral private keys, the fixed nonce
		// and the clock it was built at, which is what makes the whole
		// handover reproducible rather than merely checkable.
		OfferFragment              string `json:"offer_fragment"`
		MachineEphemeralPrivateKey string `json:"machine_ephemeral_private_key"`
		ViewerEphemeralPrivateKey  string `json:"viewer_ephemeral_private_key"`
		SenderDeviceID             string `json:"sender_device_id"`
		NowMilliseconds            int64  `json:"now_milliseconds"`
		Wrapper                    struct {
			V              int    `json:"v"`
			Phase          string `json:"phase"`
			PairingID      string `json:"pairing_id"`
			SenderDeviceID string `json:"sender_device_id"`
			EphemeralKey   string `json:"ephemeral_key"`
			Nonce          string `json:"nonce"`
			Ct             string `json:"ct"`
		} `json:"wrapper"`
		PhaseKey string `json:"phase_key"`
	} `json:"pairing_handover"`
	Receipts []struct {
		Name       string `json:"name"`
		Body       string `json:"body"`
		ByteLength int    `json:"byte_length"`
		SHA256     string `json:"sha256"`
	} `json:"receipts"`
}

type canonicalBytesVector struct {
	Name       string `json:"name"`
	Body       string `json:"body"`
	ByteLength int    `json:"byte_length"`
	SHA256     string `json:"sha256"`
	FieldCount int    `json:"field_count"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/protocol-vectors.json")
	if err != nil {
		t.Fatalf("reading the protocol vectors: %v", err)
	}
	var file vectorFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decoding the protocol vectors: %v", err)
	}
	if file.Format != 1 || file.Cipher != "AES-256-GCM" || file.NonceBytes != NonceBytes {
		t.Fatalf("the vector file is not the shape this package was written against: "+
			"format=%d cipher=%q nonce_bytes=%d", file.Format, file.Cipher, file.NonceBytes)
	}
	return file
}

func mustBase64(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		t.Fatalf("the vector file holds unreadable base64 %q: %v", text, err)
	}
	return raw
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
