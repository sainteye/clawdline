package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// diagnosticReportRoute is where the console's layout-diagnostics panel sends
// a report when somebody presses "Send to Mac"
// (web/console/src/legacy/js/core/layout-diagnostics.js REPORT_ROUTE).
const diagnosticReportRoute = "/v1/diagnostics/report"

// The two fixed names a report is kept under, beside the daemon's own log.
// A fixed name is the feature: whoever debugs opens one known file, and two
// names are also the whole of its growth (docs/diagnostics.md).
const (
	diagnosticReportFile   = "report.json"
	diagnosticPreviousFile = "previous.json"
)

// diagnosticReportMu keeps the rotation of the two files one step at a time.
var diagnosticReportMu sync.Mutex

type diagnosticReportEnvelope struct {
	Version      int             `json:"clawdline_diagnostic_report"`
	WrittenAt    string          `json:"written_at"`
	WrittenBy    string          `json:"written_by"`
	Received     int             `json:"received_bytes"`
	Completeness json.RawMessage `json:"completeness"`
	Report       json.RawMessage `json:"report"`
}

type diagnosticReportReceipt struct {
	OK                 bool   `json:"ok"`
	Path               string `json:"path"`
	Previous           string `json:"previous,omitempty"`
	Bytes              int64  `json:"bytes"`
	Limit              int64  `json:"limit"`
	CompletenessStated bool   `json:"completeness_stated"`
}

// Refusals use the auth error shape, {"error":{"code","message"}}, because
// that is what the panel prints as `code — message`.
//
// diagnosticsReport stores what a page recorded about itself. Any paired
// device may send it, at read level: it names no path, writes two fixed files
// and is bounded by capacity.DiagnosticsReportBytes. Nothing is truncated to
// fit; a larger report is refused and the last good one stays.
func (s *Server) diagnosticsReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A diagnostic report is sent with POST.")
		return
	}
	limit := CapacityLimit(capacity.DiagnosticsReportBytes)
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "The report could not be read.")
		return
	}
	if int64(len(raw)) > limit {
		writeAuthRefusal(w, http.StatusRequestEntityTooLarge, "report_too_large", "The diagnostic report is larger than this machine keeps.")
		return
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		writeAuthRefusal(w, http.StatusBadRequest, "empty_report", "The diagnostic report was empty.")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		writeAuthRefusal(w, http.StatusBadRequest, "report_not_json", "A diagnostic report must be a JSON object.")
		return
	}
	completeness, stated := fields["completeness"]
	if !stated || len(bytes.TrimSpace(completeness)) == 0 || string(bytes.TrimSpace(completeness)) == "null" {
		completeness, stated = json.RawMessage(`{"stated":false}`), false
	}
	writer := actorOf(r)
	if writer == "" {
		writer = "this machine"
	}
	body, err := json.MarshalIndent(diagnosticReportEnvelope{
		Version: 1, WrittenAt: time.Now().UTC().Format(time.RFC3339), WrittenBy: writer,
		Received: len(raw), Completeness: completeness, Report: raw,
	}, "", "  ")
	if err != nil {
		writeAuthRefusal(w, http.StatusBadRequest, "report_not_json", "A diagnostic report must be a JSON object.")
		return
	}
	receipt, err := saveDiagnosticReport(filepath.Join(s.cfg.Dir, "logs", "diagnostics"), append(body, '\n'))
	if err != nil {
		writeAuthRefusal(w, http.StatusInternalServerError, "report_write_failed", "The diagnostic report could not be written.")
		return
	}
	receipt.Limit, receipt.CompletenessStated = limit, stated
	writeJSON(w, receipt)
}

// saveDiagnosticReport moves the current report to previous.json and writes
// the new one beside it, mode 0600: a report carries Session ids and paths.
// The answer names the size that is on disk, not the size that was meant to be.
func saveDiagnosticReport(dir string, body []byte) (diagnosticReportReceipt, error) {
	diagnosticReportMu.Lock()
	defer diagnosticReportMu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return diagnosticReportReceipt{}, err
	}
	current, previous := filepath.Join(dir, diagnosticReportFile), filepath.Join(dir, diagnosticPreviousFile)
	tmp, err := os.CreateTemp(dir, ".report-*.json")
	if err != nil {
		return diagnosticReportReceipt{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return diagnosticReportReceipt{}, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return diagnosticReportReceipt{}, err
	}
	if err := tmp.Close(); err != nil {
		return diagnosticReportReceipt{}, err
	}
	kept := ""
	if err := os.Rename(current, previous); err == nil {
		kept = previous
	} else if !errors.Is(err, os.ErrNotExist) {
		return diagnosticReportReceipt{}, err
	}
	if err := os.Rename(tmp.Name(), current); err != nil {
		return diagnosticReportReceipt{}, err
	}
	info, err := os.Stat(current)
	if err != nil || info.Size() != int64(len(body)) {
		return diagnosticReportReceipt{}, errors.New("diagnostic report size mismatch")
	}
	if kept == "" {
		if _, err := os.Stat(previous); err == nil {
			kept = previous
		}
	}
	return diagnosticReportReceipt{OK: true, Path: current, Previous: kept, Bytes: info.Size()}, nil
}
