// A free standalone ACP peer for host integration rehearsals. It never calls a
// provider or network. Runtime tests additionally exercise a dedicated owned
// copy of their Go test binary, avoiding a second compiler invocation.
package main

import (
	"bufio"
	"encoding/json"
	"os"
)

func main() {
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var q struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(s.Bytes(), &q) != nil {
			return
		}
		var result any
		switch q.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]bool{"loadSession": true}}
		case "session/new", "session/load":
			result = map[string]string{"sessionId": "free-scripted-peer"}
		case "session/prompt":
			result = map[string]string{"stopReason": "end_turn", "text": "Scripted reply; no model was called."}
		default:
			continue
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}
}
