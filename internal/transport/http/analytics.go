package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/analytics"
)

// How long a Usage read waits for the records to be scanned. The scan carries
// on past it; the page is told the service is busy and asks again, and the
// session list is never held up behind it.
const analyticsWait = 20 * time.Second

// usageAnalyticsRoute is GET /v1/orchestrator/usage/analytics, and its `.csv`
// and `.json` exports: the Swift app's Project Portfolio, with the Swift app's
// query language, envelope and refusals, over rows read from the assistants'
// own records (see internal/adapters/analytics for what that changes).
func (s *Server) usageAnalyticsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeUsageRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET.")
		return
	}
	q, err := analytics.Parse(r.URL.Query())
	if err != nil {
		writeUsageRefusal(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	rows, ok := s.analyticsRows(w, r, q.ScanFrom())
	if !ok {
		return
	}
	res := analytics.Run(q, rows, time.Now())
	switch {
	case strings.HasSuffix(r.URL.Path, ".csv"):
		if res.Truncated {
			writeUsageRefusal(w, http.StatusRequestEntityTooLarge, "export_too_large", exportTooLarge())
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="clawdline-usage.csv"`)
		_, _ = w.Write([]byte(res.ExportCSV()))
	case strings.HasSuffix(r.URL.Path, ".json"):
		if res.Truncated {
			writeUsageRefusal(w, http.StatusRequestEntityTooLarge, "export_too_large", exportTooLarge())
			return
		}
		body, err := unescapedJSON(res.ExportJSON())
		if err != nil {
			writeUsageRefusal(w, http.StatusInternalServerError, "json_serialization_failed",
				"The lossless usage export could not be serialized.")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="clawdline-usage.json"`)
		_, _ = w.Write(body)
	default:
		writeUsageJSON(w, http.StatusOK, map[string]any{"usage": res.Payload()})
	}
}

// usageWorktreesRoute is GET /v1/orchestrator/usage/project-worktrees.
func (s *Server) usageWorktreesRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeUsageRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET.")
		return
	}
	q, err := analytics.ParseWorktrees(r.URL.Query())
	if err != nil {
		writeUsageRefusal(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	rows, ok := s.analyticsRows(w, r, q.Usage.ScanFrom())
	if !ok {
		return
	}
	body, err := analytics.Worktrees(q, rows, time.Now())
	var refusal *analytics.Refusal
	if errors.As(err, &refusal) {
		writeUsageRefusal(w, refusal.Status, refusal.Code, refusal.Message)
		return
	}
	writeUsageJSON(w, http.StatusOK, map[string]any{"projectWorktrees": body})
}

func (s *Server) analyticsRows(w http.ResponseWriter, r *http.Request, since time.Time) ([]analytics.Row, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), analyticsWait)
	defer cancel()
	rows, err := s.usageRows().Rows(ctx, since)
	if errors.Is(err, analytics.ErrBusy) {
		w.Header().Set("Retry-After", "5")
		writeUsageRefusal(w, http.StatusServiceUnavailable, "usage_analytics_busy",
			"Usage Analytics is still reading the assistants' records; sessions remain available.")
		return nil, false
	}
	if err != nil {
		writeUsageRefusal(w, http.StatusInternalServerError, "usage_error", err.Error())
		return nil, false
	}
	return rows, true
}

// The collector lives beside the Server rather than in it, so this route adds
// lines to Handler() and nothing else to server.go. There is one Server.
var (
	collectorOnce sync.Once
	collector     *analytics.Collector
)

func (s *Server) usageRows() *analytics.Collector {
	collectorOnce.Do(func() {
		home, _ := os.UserHomeDir()
		collector = analytics.NewCollector(home, s.swift)
	})
	return collector
}

func exportTooLarge() string {
	return fmt.Sprintf("The matching export exceeds %d rows; narrow the range.", analytics.MaxScannedRows)
}

// writeUsageRefusal is the Swift app's refusal envelope, which is what the
// Usage page reads a code from (`view/usage.js`'s `errorFrom`).
func writeUsageRefusal(w http.ResponseWriter, status int, code, message string) {
	writeUsageJSON(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": message, "request_id": requestID(),
	}})
}

func writeUsageJSON(w http.ResponseWriter, status int, v any) {
	body, err := unescapedJSON(v)
	if err != nil {
		status, body = http.StatusInternalServerError, []byte(`{"error":{"code":"json_serialization_failed"}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// unescapedJSON leaves `<`, `>` and `&` as they are, as the Swift encoder does.
func unescapedJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func requestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
