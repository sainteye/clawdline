package antigravity

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
)

func TestHookFramingBoundsAndPartialReads(t *testing.T) {
	for _, n := range []uint32{0, MaxHookFrameBytes + 1, ^uint32(0)} {
		var frame [4]byte
		binary.BigEndian.PutUint32(frame[:], n)
		_, err := readHookFrame(bytes.NewReader(frame[:]))
		wantCode(t, err, "antigravity_hook_frame_too_large")
	}
	for _, data := range [][]byte{nil, {0, 0}, {0, 0, 0, 5, 'a'}} {
		_, err := readHookFrame(bytes.NewReader(data))
		wantCode(t, err, "antigravity_hook_disconnected")
	}
	var buf bytes.Buffer
	if err := writeHookFrame(&buf, hookReply{Version: 1, Response: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	data, err := readHookFrame(&oneByteReader{r: &buf})
	if err != nil {
		t.Fatal(err)
	}
	var reply hookReply
	if decodeHookEnvelope(data, &reply) != nil || string(reply.Response) != "{}" {
		t.Fatal("partial frame did not round trip")
	}
	for _, data := range []string{`{"version":1,"version":2}`, `{} {}`, `[]`, `{"version":1,"pid":123}`} {
		if decodeHookEnvelope([]byte(data), &reply) == nil {
			t.Fatalf("accepted invalid envelope %q", data)
		}
	}
	for _, data := range []string{`{"Version":1}`, `{"version":1,"launch":{"id":"one","id":"two"}}`, `{"version":1,"launch":{"ID":"one"}}`} {
		var r hookRequest
		if decodeHookEnvelope([]byte(data), &r) == nil {
			t.Fatal("case alias or duplicated launch metadata accepted")
		}
	}
}

func TestHookTransportReportsUnsupportedPlatform(t *testing.T) {
	if hookPlatform() == nil {
		t.Skip("Unix transport supported here")
	}
	_, err := ListenHookService(HookServiceConfig{})
	wantCode(t, err, "antigravity_hook_transport_unsupported")
}

type oneByteReader struct{ r io.Reader }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}

type brokenHookWriter struct{}

func (brokenHookWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic write failure") }

func TestHookFrameWriterRefusesFailedOrOversizedOutput(t *testing.T) {
	wantCode(t, writeHookFrame(brokenHookWriter{}, hookReply{Version: 1}), "antigravity_hook_disconnected")
	wantCode(t, writeHookFrame(io.Discard, string(bytes.Repeat([]byte{'x'}, MaxHookFrameBytes))), "antigravity_hook_frame_too_large")
}

type timeoutHookIO struct{}

func (timeoutHookIO) Read([]byte) (int, error)  { return 0, &net.DNSError{IsTimeout: true} }
func (timeoutHookIO) Write([]byte) (int, error) { return 0, &net.DNSError{IsTimeout: true} }

func TestHookIOTimeoutRemainsDistinctFromDisconnect(t *testing.T) {
	_, err := readHookFrame(timeoutHookIO{})
	wantCode(t, err, "antigravity_hook_timeout")
	wantCode(t, writeHookFrame(timeoutHookIO{}, hookReply{Version: 1}), "antigravity_hook_timeout")
}
