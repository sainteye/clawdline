package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// Body is the body of one push request: an RFC 8188 `aes128gcm` payload of
// exactly one record, keyed the RFC 8291 way.
//
// Pure, and takes both pieces of randomness as arguments. That is the only way
// this can be checked at all — the interesting assertion is not "it produced
// some bytes", it is "the subscriber's private key turns these bytes back into
// the plaintext", and that test needs both sides of a fixed key exchange.
//
// Both arguments must be **fresh per message**. RFC 8291 §3.1 has the
// application server generating a key pair and a salt when it sends, and
// discarding the pair afterwards; reusing either would reuse the AES-GCM key
// and nonce across two messages to the same subscriber, which is the one
// failure mode GCM does not survive. Sender.post makes new ones inside the
// loop, per subscriber, not per call.
func Body(payload []byte, subscription Subscription, ephemeral *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	// 16 exactly, because the header field is 16 octets wide and a longer salt
	// would silently become a shorter salt plus a corrupted `rs`.
	if len(salt) != SaltBytes {
		return nil, fmt.Errorf("%w: the salt is %d octets", ErrSaltLength, len(salt))
	}
	if len(payload) > MaxPayload {
		return nil, fmt.Errorf("%w: %d octets, and the ceiling is %d", ErrPayloadTooLarge, len(payload), MaxPayload)
	}
	subscriber, err := ecdh.P256().NewPublicKey(subscription.P256dh)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSubscriberKey, err)
	}

	ours := ephemeral.PublicKey().Bytes() // 65, uncompressed
	shared, err := ephemeral.ECDH(subscriber)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSubscriberKey, err)
	}

	// RFC 8291 §3.4. The ECDH secret alone is not enough — anyone who
	// intercepted the subscription's public key could do the same exchange —
	// so the `auth` secret the browser kept is folded in as the HKDF salt, and
	// the two public keys go into the info string in the order the RFC fixes:
	// **user agent first, application server second**. Getting that pair the
	// wrong way round produces a key that is perfectly valid and that the
	// browser will never derive.
	keyInfo := make([]byte, 0, len("WebPush: info")+1+len(subscription.P256dh)+len(ours))
	keyInfo = append(keyInfo, "WebPush: info"...)
	keyInfo = append(keyInfo, 0)
	keyInfo = append(keyInfo, subscription.P256dh...)
	keyInfo = append(keyInfo, ours...)
	ikm, err := hkdf.Key(sha256.New, shared, subscription.Auth, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}

	// RFC 8188 §2.2, now with the per-message salt as the extract salt. The
	// trailing NUL in each info string is part of the string the RFC
	// specifies, not a C artefact.
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	baseNonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	// The nonce is the base nonce XOR the record sequence number, and this is
	// record zero of one — so the XOR is with 96 zero bits and the base nonce
	// is used as it stands. Written down rather than assumed: the day this
	// grows a second record is the day the omission becomes a nonce reuse.
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// RFC 8188 §2: the plaintext of a record ends in a padding delimiter,
	// 0x01 for a record with more to come and 0x02 for the last one. This is
	// the last one and there is no padding after it — padding exists to hide a
	// message's length, and every message here is the same handful of fields,
	// so there is nothing to hide and a longer body to pay for.
	plaintext := make([]byte, 0, len(payload)+1)
	plaintext = append(plaintext, payload...)
	plaintext = append(plaintext, 0x02)

	out := make([]byte, 0, HeaderLength+len(plaintext)+gcm.Overhead())
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, RecordSize)
	out = append(out, byte(len(ours)))
	out = append(out, ours...)
	// Seal appends the ciphertext and its tag, which is exactly the order
	// RFC 8188 wants them in.
	return gcm.Seal(out, baseNonce, plaintext, nil), nil
}

// NewSalt is the 16 octets one message is keyed with.
func NewSalt() ([]byte, error) {
	salt := make([]byte, SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// NewEphemeral is the key pair RFC 8291 §3.1 has the application server make
// per message and throw away afterwards.
func NewEphemeral() (*ecdh.PrivateKey, error) { return ecdh.P256().GenerateKey(rand.Reader) }
