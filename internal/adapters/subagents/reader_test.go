package subagents

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const testConversation = "a0000000-0000-4000-8000-000000000001"
const testCodexAgent = "b0000000-0000-4000-8000-000000000002"

func fixture(t *testing.T) (string, session.Session, string, string) {
	t.Helper()
	home := t.TempDir()
	row := session.Session{Assistant: session.AssistantClaude, CWD: "/fixture/project", ConversationID: testConversation}
	parent := transcript.ClaudePath(home, row.CWD, row.ConversationID)
	folder := filepath.Join(parent[:len(parent)-len(filepath.Ext(parent))], "subagents")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return home, row, parent, folder
}

func writeAgent(t *testing.T, folder, id string) {
	t.Helper()
	meta := []byte(`{"description":"inspect the fixture","agentType":"Explore","model":"fixture-model","spawnDepth":1}`)
	if err := os.WriteFile(filepath.Join(folder, "agent-"+id+".meta.json"), meta, 0o600); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"tool_use","name":"Read"}]}}` + "\n")
	if err := os.WriteFile(filepath.Join(folder, "agent-"+id+".jsonl"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMissingProviderRecordIsUnknownNotZero(t *testing.T) {
	r := New(t.TempDir())
	got := r.ForSession(session.Session{Assistant: session.AssistantClaude, CWD: "/fixture", ConversationID: testConversation})
	if got.AgentReading.State != session.AgentsUnknown || got.AgentReading.Reason != session.AgentsNoRecord {
		t.Fatalf("reading %+v, want unknown/no_record", got.AgentReading)
	}
}

func TestRunningAgentIsReadAndStableBeatOpensNothing(t *testing.T) {
	home, row, _, folder := fixture(t)
	writeAgent(t, folder, "agent_1")
	r := New(home)
	got := r.ForSession(row)
	if len(got.Agents) != 1 || got.Agents[0].State != session.AgentRunning || got.Agents[0].Doing != "Read" {
		t.Fatalf("agents %+v", got.Agents)
	}
	got = r.ForSession(row)
	if cost := r.LastMeasurement(); cost.Opened != 0 {
		t.Fatalf("stable beat opened %d files, want 0 (stats=%d bytes=%d)", cost.Opened, cost.Stats, cost.Bytes)
	}
}

func TestCodexHeadNamesOtherwiseIdenticalSpawnRowsAndIsMemoized(t *testing.T) {
	home := t.TempDir()
	folder := filepath.Join(home, ".codex", "sessions", "2026", "09", "21")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	head := fmt.Sprintf(`{"type":"session_meta","payload":{"session_id":%q,"id":%q,"thread_source":"subagent","agent_nickname":"fixture scout","timestamp":"2026-09-21T03:04:05.123Z","source":{"subagent":{"thread_spawn":{}}}}}`+"\n", testConversation, testCodexAgent)
	if err := os.WriteFile(filepath.Join(folder, "rollout-fixture-"+testCodexAgent+".jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
	row := session.Session{
		Assistant: session.AssistantCodex, ConversationID: testConversation,
		Agents:       []session.Agent{{ID: testCodexAgent, What: "thread_spawn", Type: "thread_spawn", State: session.AgentRunning}},
		AgentReading: session.AgentReading{State: session.AgentsComplete},
	}
	r := New(home)
	got := r.ForSession(row)
	if len(got.Agents) != 1 || got.Agents[0].What != "fixture scout" || got.Agents[0].Type != "thread_spawn" || got.Agents[0].At.IsZero() {
		t.Fatalf("agent metadata %+v", got.Agents)
	}
	if cost := r.LastMeasurement(); cost.Opened != 1 || cost.Bytes != int64(len(head)) {
		t.Fatalf("cold reading %+v, want one measured head read", cost)
	}
	r.ForSession(row)
	if cost := r.LastMeasurement(); cost.Opened != 0 || cost.Bytes != 0 {
		t.Fatalf("stable reading %+v, want no file opens", cost)
	}
}

func TestCompletionCarriesResultAndSettlesOut(t *testing.T) {
	home, row, parent, folder := fixture(t)
	writeAgent(t, folder, "agent_2")
	notice := `{"content":"<task-notification><task-id>agent_2</task-id><status>completed</status><result>fixture result</result><subagent_tokens>12</subagent_tokens><tool_uses>3</tool_uses><duration_ms>2500</duration_ms></task-notification>"}` + "\n"
	if err := os.WriteFile(parent, []byte(notice), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r := New(home)
	r.Now = func() time.Time { return now }
	got := r.ForSession(row)
	if len(got.Agents) != 1 || got.Agents[0].State != session.AgentDone || got.Agents[0].Result != "fixture result" || got.Agents[0].Tokens != 12 {
		t.Fatalf("agents %+v", got.Agents)
	}
	r.Now = func() time.Time { return now.Add(settleWindow + time.Second) }
	got = r.ForSession(row)
	if len(got.Agents) != 0 || got.AgentReading.State != session.AgentsComplete || got.AgentReading.Truncated != 0 {
		t.Fatalf("settled agents %+v reading %+v", got.Agents, got.AgentReading)
	}
}

func TestPerSessionBoundSaysHowManyRowsWereLeftOut(t *testing.T) {
	home, row, _, folder := fixture(t)
	for i := 0; i < MaximumShown+2; i++ {
		writeAgent(t, folder, fmt.Sprintf("agent_%d", i))
	}
	got := New(home).ForSession(row)
	if len(got.Agents) != MaximumShown || got.AgentReading.Truncated != 2 {
		t.Fatalf("got %d rows and %d truncated", len(got.Agents), got.AgentReading.Truncated)
	}
}

func TestSixAgentScanCostIsBoundedAndMeasured(t *testing.T) {
	home, row, _, folder := fixture(t)
	for i := 0; i < MaximumShown; i++ {
		writeAgent(t, folder, fmt.Sprintf("measured_%d", i))
	}
	r := New(home)
	r.ForSession(row)
	cold := r.LastMeasurement()
	if cold.Stats != 14 || cold.Opened != 12 || cold.Bytes <= 0 {
		t.Fatalf("cold scan %+v, want 14 stats, 12 opens and measured bytes", cold)
	}
	r.ForSession(row)
	warm := r.LastMeasurement()
	if warm.Stats != 14 || warm.Opened != 0 || warm.Bytes != 0 {
		t.Fatalf("warm scan %+v, want 14 stats and no file opens", warm)
	}
	t.Logf("six agents: cold stats=%d opens=%d bytes=%d duration=%s; warm stats=%d opens=%d bytes=%d duration=%s",
		cold.Stats, cold.Opened, cold.Bytes, cold.Duration, warm.Stats, warm.Opened, warm.Bytes, warm.Duration)
}

func TestAgentPathNeverLetsAnIDEscapeItsSession(t *testing.T) {
	home, row, _, folder := fixture(t)
	writeAgent(t, folder, "agent_safe")
	r := New(home)
	if _, ok := r.AgentPath(row, "../../outside"); ok {
		t.Fatal("an escaping id was accepted")
	}
	resolvedFolder, err := filepath.EvalSymlinks(folder)
	if err != nil {
		t.Fatal(err)
	}
	if path, ok := r.AgentPath(row, "agent_safe"); !ok || filepath.Dir(path) != resolvedFolder {
		t.Fatalf("path %q ok=%v", path, ok)
	}
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(folder, "agent-agent_safe.jsonl")
	if err := os.Remove(inside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, inside); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.AgentPath(row, "agent_safe"); ok {
		t.Fatal("a symlink outside the session was accepted")
	}
}

func TestRowsMeasureTheFullestSessionInOneInventory(t *testing.T) {
	r := New(t.TempDir())
	r.Begin()
	r.ForSession(session.Session{Assistant: session.AssistantCodex, Agents: make([]session.Agent, 6), AgentReading: session.AgentReading{Truncated: 2}})
	r.ForSession(session.Session{Assistant: session.AssistantCodex})
	if got := r.RowsReading(); !got.Known || got.Used != 8 {
		t.Fatalf("reading %+v", got)
	}
	r.Begin()
	if got := r.RowsReading(); !got.Known || got.Used != 0 {
		t.Fatalf("new inventory reading %+v", got)
	}
}
