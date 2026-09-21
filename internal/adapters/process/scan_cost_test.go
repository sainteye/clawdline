//go:build darwin || linux

package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// BenchmarkCodexScanAmplification keeps the tradeoff in bindCodex measurable:
// discovering work on every row costs more than the former incomplete-row
// lookup, while the per-scan memo must prevent identity and work discovery
// from opening the same rollout twice.
func BenchmarkCodexScanAmplification(b *testing.B) {
	dir := b.TempDir()
	rootA, size := benchmarkRollout(b, dir, "c0de0001-0000-4000-8000-000000000001", "c0de0001-0000-4000-8000-000000000001", "")
	agentA, _ := benchmarkRollout(b, dir, "c0de0002-0000-4000-8000-000000000002", "c0de0001-0000-4000-8000-000000000001", "explorer")
	rootB, _ := benchmarkRollout(b, dir, "c0de0003-0000-4000-8000-000000000003", "c0de0003-0000-4000-8000-000000000003", "")
	agentB, _ := benchmarkRollout(b, dir, "c0de0004-0000-4000-8000-000000000004", "c0de0003-0000-4000-8000-000000000003", "worker")
	files := map[int][]string{22: {rootA, agentA}, 41: {rootB, agentB}}
	rows := func() []session.Session {
		return []session.Session{
			{Assistant: session.AssistantCodex, PID: 22, ConversationID: "c0de0001-0000-4000-8000-000000000001", CWD: "/code/known"},
			{Assistant: session.AssistantCodex, PID: 41},
			{Assistant: session.AssistantCodex, PID: 42},
		}
	}

	for _, tc := range []struct {
		name string
		run  func([]session.Session, RolloutHead)
	}{
		{name: "narrowed", run: func(rows []session.Session, head RolloutHead) {
			for i, row := range rows {
				if row.Assistant != session.AssistantCodex || row.PID == 0 || row.ConversationID != "" && row.CWD != "" {
					continue
				}
				rows[i].ConversationID, _, _, rows[i].CWD = codexConversation(files[row.PID], true, head)
			}
		}},
		{name: "widened_without_memo", run: func(rows []session.Session, head RolloutHead) {
			for i, row := range rows {
				if row.Assistant != session.AssistantCodex || row.PID == 0 {
					continue
				}
				if row.ConversationID == "" {
					rows[i].ConversationID, _, _, rows[i].CWD = codexConversation(files[row.PID], true, head)
				}
				rows[i].Agents, rows[i].AgentReading = codexAgents(files[row.PID], true, rows[i].ConversationID, head)
			}
		}},
		{name: "widened_with_memo", run: func(rows []session.Session, head RolloutHead) {
			p := &PS{Head: head, Open: func(context.Context, []int) (map[int][]string, map[int]string, bool) {
				return files, nil, true
			}}
			p.bindCodex(context.Background(), rows)
		}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var opens, bytesRead int64
			head := func(path string) (RolloutMeta, bool) {
				opens++
				bytesRead += int64(size)
				return readRolloutHead(path)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tc.run(rows(), head)
			}
			b.StopTimer()
			b.ReportMetric(float64(opens)/float64(b.N), "rollout-opens/scan")
			b.ReportMetric(float64(bytesRead)/float64(b.N), "rollout-bytes/scan")
		})
	}
}

func benchmarkRollout(b *testing.B, dir, thread, conversation, agentType string) (string, int) {
	b.Helper()
	source := ""
	if agentType != "" {
		source = fmt.Sprintf(`,"thread_source":"subagent","source":{"subagent":{"role":%q}}`, agentType)
	}
	line := []byte(fmt.Sprintf(`{"type":"session_meta","payload":{"session_id":%q,"id":%q,"cwd":"/code/project"%s}}`, conversation, thread, source))
	const recordSize = 32 << 10
	if len(line)+1 > recordSize {
		b.Fatalf("benchmark record is %d bytes", len(line)+1)
	}
	record := make([]byte, recordSize)
	copy(record, line)
	for i := len(line); i < len(record)-1; i++ {
		record[i] = ' '
	}
	record[len(record)-1] = '\n'
	path := filepath.Join(dir, "rollout-2026-09-21T12-00-00-"+thread+".jsonl")
	if err := os.WriteFile(path, record, 0o600); err != nil {
		b.Fatal(err)
	}
	return path, len(record)
}
