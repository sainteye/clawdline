package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

// CodexNames reads ~/.codex/session_index.jsonl, an append-only log of thread
// names. A later row for the same id supersedes an earlier one, so the file is
// read forward and the last write wins.
func CodexNames(home string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(filepath.Join(home, ".codex", "session_index.jsonl"))
	if err != nil {
		return out
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var rec struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil || rec.ID == "" {
			continue
		}
		if rec.Name != "" {
			out[rec.ID] = rec.Name
		}
	}
	return out
}
