package projectassistantacp

import (
	"context"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestDispatchWorkModeChoosesChildOrRoot(t *testing.T) {
	for _, c := range []struct {
		args, mode string
		ok         bool
	}{
		{`{"idempotency_key":"k","brief":"b","claims":["a.go"]}`, "", true},
		{`{"idempotency_key":"k","brief":"b","claims":["a.go"],"mode":"child"}`, "", true},
		{`{"idempotency_key":"k","brief":"b","claims":[],"mode":"root"}`, pa.DispatchModeRoot, true},
		{`{"idempotency_key":"k","brief":"b","claims":[]}`, "", false},
		{`{"idempotency_key":"k","brief":"b","claims":[],"mode":"root","continue_on_completion":true}`, "", false},
		{`{"idempotency_key":"k","brief":"b","claims":["a.go"],"mode":"owner"}`, "", false},
	} {
		q, ctx, err := decodeDispatchWork(context.Background(), []byte(c.args))
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.args, err, c.ok)
			continue
		}
		if c.ok && (q.Mode != c.mode || pa.DispatchChoiceFrom(ctx).Mode != c.mode) {
			t.Errorf("%s: mode = %q / %q, want %q", c.args, q.Mode, pa.DispatchChoiceFrom(ctx).Mode, c.mode)
		}
	}
}
