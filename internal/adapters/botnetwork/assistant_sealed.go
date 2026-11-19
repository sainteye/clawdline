package botnetwork

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"golang.org/x/crypto/hkdf"
)

type AssistantSealedSource struct {
	callKey, replyKey cipher.AEAD
	service           *app.AssistantService
}

const assistantSealLabel = "clawdline-assistant-seal-v1\n"

func NewAssistantSealedSource(account, machine string, secret []byte, service *app.AssistantService) (*AssistantSealedSource, error) {
	if len(secret) != 32 || account == "" || machine == "" || service == nil {
		return nil, wire.AssistantUnavailable
	}
	info := assistantSealLabel + account + "\n" + machine + "\n"
	call, err := assistantAEAD(secret, info+"call")
	if err != nil {
		return nil, err
	}
	reply, err := assistantAEAD(secret, info+"reply")
	if err != nil {
		return nil, err
	}
	return &AssistantSealedSource{callKey: call, replyKey: reply, service: service}, nil
}

func assistantAEAD(secret []byte, info string) (cipher.AEAD, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, secret, nil, []byte(info)), key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func assistantOpen(aead cipher.AEAD, raw string, aad []byte) ([]byte, error) {
	if len(raw) > base64.StdEncoding.EncodedLen(wire.MaxBodyBytes+aead.Overhead()) {
		return nil, wire.AssistantInvalid
	}
	sealed, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || base64.StdEncoding.EncodeToString(sealed) != raw ||
		len(sealed) <= aead.NonceSize()+aead.Overhead() || len(sealed) > wire.MaxBodyBytes+aead.Overhead() {
		return nil, wire.AssistantInvalid
	}
	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], aad)
}

func assistantSeal(aead cipher.AEAD, plain, aad []byte) (string, error) {
	if len(plain) == 0 || len(plain)+aead.NonceSize() > wire.MaxBodyBytes {
		return "", wire.AssistantInvalid
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, aad)), nil
}

// Handle authenticates the sealed packet before parsing it. The source service
// independently checks the dedicated gateway signature and live human consent.
func (s *AssistantSealedSource) Handle(ctx context.Context, packet string) (string, error) {
	raw, err := assistantOpen(s.callKey, packet, []byte(assistantSealLabel+"call"))
	if err != nil {
		return "", wire.AssistantUnverified
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request AssistantRequest
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return "", wire.AssistantInvalid
	}
	reply, err := s.service.Receive(ctx, request.Call, []byte(request.Body))
	var result []byte
	if err == nil {
		result, err = json.Marshal(struct {
			Result app.AssistantResponse `json:"result"`
		}{reply})
	} else {
		result, err = json.Marshal(map[string]string{"error": assistantSealedRefusal(err)})
	}
	if err != nil {
		return "", err
	}
	binding := sha256.Sum256([]byte(packet))
	return assistantSeal(s.replyKey, result, []byte(assistantSealLabel+"reply\n"+hex.EncodeToString(binding[:])))
}

func assistantSealedRefusal(err error) string {
	var wireCode wire.Refusal
	if errors.As(err, &wireCode) {
		return string(wireCode)
	}
	if code := pa.Code(err); code != "" {
		return string(code)
	}
	return string(wire.AssistantUnavailable)
}
