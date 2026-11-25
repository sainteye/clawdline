package capacity

// One decision-card trace event the local console posts to the daemon log
// (internal/transport/http/decision_trace.go).
func decisionTraceBounds() []Entry {
	return []Entry{
		{Name: "bots.decision_trace_bytes", Class: Buffer, Unit: Bytes, Limit: 2 << 10, AtLimit: Refuse, Told: []Channel{Sender, Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/transport/http.maxDecisionTraceBodyBytes"}},
	}
}
