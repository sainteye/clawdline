package transcript

// LiveContextReadLimit is how much of a record's end the list reads to say how
// full a conversation's window is. The newest turn carries the whole answer,
// so a short tail suffices; Facts reads recordReadLimit for the spend instead.
const LiveContextReadLimit = 512 << 10

// LiveContext reads how full a conversation's window is from the end of its
// record, for the Session list, which cannot afford the tail Facts reads. It
// answers false when the record or the window is unknown: a conversation that
// could not be read is not empty.
func LiveContext(path, assistant string, status ClaudeStatusLine) (Context, bool) {
	if path == "" {
		if assistant == "claude" && status.Found && status.HasUsedTokens && status.HasWindowTokens {
			return ClaudeContext(nil, status, "")
		}
		return Context{}, false
	}
	data, _, err := tailData(path, LiveContextReadLimit)
	if err != nil || len(data) == 0 {
		return Context{}, false
	}
	switch assistant {
	case "codex":
		if facts := codexFacts(data); facts.Context != nil {
			return *facts.Context, true
		}
	case "claude":
		return ClaudeContext(claudeFill(data), status, claudeStatedModel(data))
	}
	return Context{}, false
}
