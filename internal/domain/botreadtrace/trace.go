package botreadtrace

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"time"
)

type key struct{}

var traceID = regexp.MustCompile(`^bot-view-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Record joins machine timings to the request a hosted browser recorded.
type Record struct {
	ID               string           `json:"trace_id"`
	Mode             string           `json:"mode"`
	Stages           map[string]int64 `json:"stages_ms"`
	ResponseSequence *uint64          `json:"response_sequence,omitempty"`
}

func FromCommand(body []byte) *Record {
	if !bytes.Contains(body, []byte("bot-view-")) {
		return nil
	}
	var request struct {
		Type string `json:"type"`
		ID   string `json:"request"`
		Mode string `json:"mode"`
	}
	if json.Unmarshal(body, &request) != nil || request.Type != "project-assistants.view" ||
		(request.Mode != "snapshot" && request.Mode != "history") || !traceID.MatchString(request.ID) {
		return nil
	}
	return &Record{ID: request.ID, Mode: request.Mode, Stages: make(map[string]int64)}
}

func With(ctx context.Context, record *Record) context.Context {
	if record == nil {
		return ctx
	}
	return context.WithValue(ctx, key{}, record)
}

func (r *Record) Between(stage string, start, end time.Time) {
	if r != nil && !start.IsZero() && !end.Before(start) {
		r.Stages[stage] = end.Sub(start).Milliseconds()
	}
}

func SetResponseSequence(ctx context.Context, seq uint64) {
	if record, _ := ctx.Value(key{}).(*Record); record != nil {
		record.ResponseSequence = &seq
	}
}

// Measure marks only stages a request reached. Timers use the machine's
// monotonic clock, independent of the browser and relay.
func Measure(ctx context.Context, stage string) func() {
	record, _ := ctx.Value(key{}).(*Record)
	if record == nil {
		return func() {}
	}
	start := time.Now()
	return func() { record.Between(stage, start, time.Now()) }
}
