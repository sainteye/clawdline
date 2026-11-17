package capacity

import bh "github.com/sainteye/clawdline/internal/domain/botroutinehooks"

// These source bounds mirror only the bridge-owned part of the frozen Cloud
// protocol. Cloud queue retention, leases, TTL and rates are enforced by Cloud,
// not measured or represented as available capacity by the source daemon.
func botRoutineHookBounds() []Entry {
	type bound struct {
		name, source string
		unit         Unit
		value        int64
	}
	rows := []bound{
		{"hooks", "MaxHooks", Rows, bh.MaxHooks},
		{"pending", "MaxPending", Rows, bh.MaxPending},
		{"enroll_bytes", "MaxEnrollmentBytes", Bytes, bh.MaxEnrollmentBytes},
		{"event_bytes", "MaxEventBytes", Bytes, bh.MaxEventBytes},
		{"header_bytes", "MaxHeaderBytes", Bytes, bh.MaxHeaderBytes},
		{"id_bytes", "MaxRoutineIDBytes", Bytes, bh.MaxRoutineIDBytes},
		{"response_bytes", "MaxResponseBytes", Bytes, bh.MaxResponseBytes},
		{"local_bytes", "MaxLocalResponseBytes", Bytes, bh.MaxLocalResponseBytes},
		{"http_seconds", "MaxHTTPSeconds", Seconds, bh.MaxHTTPSeconds},
		{"poll_seconds", "MaxPollSeconds", Seconds, bh.MaxPollSeconds},
		{"backoff_seconds", "MaxBackoffSeconds", Seconds, bh.MaxBackoffSeconds},
		{"revision", "MaxRevision", Rows, bh.MaxRevision},
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, Entry{Name: "bots.hook_" + r.name, Class: Buffer, Unit: r.unit, Limit: r.value, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/botroutinehooks." + r.source}})
	}
	return out
}
