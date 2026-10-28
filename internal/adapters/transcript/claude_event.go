package transcript

// ClaudeEventEntries projects one native stream-json payload with the same
// text, tool and private-reasoning rules as a local Claude JSONL record.
func ClaudeEventEntries(payload []byte) []Entry { return claudeEntries(payload, false) }
