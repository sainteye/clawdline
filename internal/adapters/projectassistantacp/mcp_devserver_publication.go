package projectassistantacp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This private result cannot be supplied through JSON or a model argument.
// The actual write crosses the source's current memory/publication fence.
type serverMCPReply struct {
	value   any
	publish func(context.Context, func() error) error
}

func (s *ownedMCPServer) writeServerReply(ctx context.Context, w http.ResponseWriter, request frame, encoded []byte, reply serverMCPReply) {
	attempted := false
	err := reply.publish(ctx, func() error {
		// Human review has already finished. Hold the source fence only for one
		// bounded write, never for the peer's consumption or acknowledgement.
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.limits.WriteTimeout)); err != nil {
			return err
		}
		attempted = true
		n, err := w.Write(encoded)
		if err == nil && n != len(encoded) {
			return io.ErrShortWrite
		}
		if err != nil {
			return err
		}
		// net/http may buffer a small body until ServeHTTP returns. Flush it
		// inside the same fence rather than publishing stale bytes afterward.
		return http.NewResponseController(w).Flush()
	})
	if err != nil && !attempted {
		// No old content was written. A typed refusal contains no command/log.
		fallback := frame{JSONRPC: "2.0", ID: request.ID, Result: raw(mcpToolResult(map[string]string{"code": string(pa.Code(err))}, true))}
		body, _ := json.Marshal(fallback)
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.limits.WriteTimeout))
		_, _ = w.Write(body)
	}
	// A failed/partial write is not followed by a second frame. The caller's
	// original mutation receipt remains; an uncertain transport is not a retry.
}
