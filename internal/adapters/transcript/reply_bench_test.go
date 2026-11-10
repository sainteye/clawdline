package transcript

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A working Claude record's 512 KB tail after a long tool-only stretch: what
// one reply reading parsed on every pass the record moved (whole_tail), and
// what it parses now (appended: the one row the pass added).
func BenchmarkReplyAtOnAToolOnlyPass(b *testing.B) {
	result := `{"type":"user","timestamp":"2026-11-10T08:00:%02d.100Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"` + strings.Repeat("x", 1800) + `"}]}}` + "\n"
	use := `{"type":"assistant","timestamp":"2026-11-10T08:00:%02d.100Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"Bash","input":{"command":"ls"}}]}}` + "\n"
	var tail strings.Builder
	for i := 0; tail.Len() < activityTail; i++ {
		tail.WriteString(fmt.Sprintf(use, i%60))
		tail.WriteString(fmt.Sprintf(result, i%60))
	}
	whole := []byte(tail.String())
	row := []byte(fmt.Sprintf(result, 1))
	b.Run("whole_tail", func(b *testing.B) {
		b.SetBytes(int64(len(whole)))
		for range b.N {
			if !lastReplyAt(whole, session.AssistantClaude).IsZero() {
				b.Fatal("a tool-only tail has no reply")
			}
		}
	})
	b.Run("appended", func(b *testing.B) {
		b.SetBytes(int64(len(row)))
		for range b.N {
			if !lastReplyAt(row, session.AssistantClaude).IsZero() {
				b.Fatal("a tool row is not a reply")
			}
		}
	})
}
