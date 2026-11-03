package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudePastIncludesRemoteControlButNotUnrelatedSDKWork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	place := Place{ID: "fixture", Path: "/work/project"}
	dir := filepath.Join(root, "projects", Slug(place.Path))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(id, body string) string {
		t.Helper()
		path := filepath.Join(dir, id+".jsonl")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	const remoteID = "11111111-1111-4111-8111-111111111111"
	bridge := `{"type":"bridge-session","bridgeSessionId":"session_fixture"}` + "\n"
	user := `{"type":"user","entrypoint":"sdk-cli","promptSource":"sdk","message":{"content":"Review the remote task"}}` + "\n"
	write(remoteID, bridge+user)
	const aliasID = "55555555-5555-4555-8555-555555555555"
	write(aliasID, strings.Replace(bridge, "session_fixture", "cse_fixture", 1)+user)
	write("22222222-2222-4222-8222-222222222222", user)
	write("33333333-3333-4333-8333-333333333333", bridge+`{"type":"user","entrypoint":"sdk-cli","message":{"content":"You are a Clawdline CHILD agent for task fixture"}}`+"\n")
	got := ClaudePast(place, nil, PastTitles{}, 10, 10)
	found := map[string]bool{}
	for _, r := range got {
		found[r.ID] = true
	}
	if len(got) != 2 || !found[remoteID] || !found[aliasID] || !Recorded(place, AssistantClaude, remoteID) || !Recorded(place, AssistantClaude, aliasID) {
		t.Fatalf("history = %+v", got)
	}
	// An initial SDK record must not be cached forever before its bridge link arrives.
	late := write("44444444-4444-4444-8444-444444444444", user)
	if cachedFront(late).isConversation() {
		t.Fatal("ordinary SDK job was admitted")
	}
	if err := os.WriteFile(late, []byte(user+bridge), 0o600); err != nil {
		t.Fatal(err)
	}
	if !cachedFront(late).isConversation() {
		t.Fatal("late Remote Control identity stayed hidden")
	}
}
