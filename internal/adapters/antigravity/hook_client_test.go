package antigravity

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestHookClientRefusesInputWithoutNativeSuccess(t *testing.T) {
	if hookPlatform() != nil {
		t.Skip("Unix transport unavailable")
	}
	for _, tc := range []struct {
		name, event string
		input       io.Reader
		code        string
	}{
		{"event", "Unknown", bytes.NewReader(nil), "antigravity_event_unsupported"},
		{"oversize", "PreInvocation", bytes.NewReader(bytes.Repeat([]byte{'a'}, MaxHookPayloadBytes+1)), "antigravity_payload_too_large"},
		{"unreadable", "PreInvocation", failedHookReader{}, "antigravity_payload_unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := ObserveHook(context.Background(), "/nonexistent-synthetic-state", tc.event, nil, tc.input, &out)
			wantCode(t, err, tc.code)
			if out.Len() != 0 {
				t.Fatal("failure produced stdout")
			}
		})
	}
}

type failedHookReader struct{}

func (failedHookReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
