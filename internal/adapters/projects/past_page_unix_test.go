//go:build !windows

package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func historyProvider(t *testing.T, response string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	exe, requests := filepath.Join(dir, "provider"), filepath.Join(dir, "requests")
	script := "#!/bin/sh\nwhile IFS= read -r line; do\nprintf '%s\\n' \"$line\" >> '" + requests + "'\ncase \"$line\" in\n*initialize*0*|*0*initialize*) echo '{\"id\":0,\"result\":{}}';;\n*thread/list*|*thread/read*) echo '" + response + "';;\nesac\ndone\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return exe, requests
}

func TestCodexPageAdvancesPastFilteredChildren(t *testing.T) {
	exe, requests := historyProvider(t, `{"id":1,"result":{"data":[{"id":"00000000-0000-4000-8000-000000000001","cwd":"/project","updatedAt":10,"preview":"`+BriefingOpening+` child"}],"nextCursor":"older"}}`)
	rows, next, err := codexPastPage(context.Background(), exe, Place{Path: "/project"}, nil, PastTitles{}, "newer")
	if err != nil || len(rows) != 0 || next != "older" {
		t.Fatalf("rows=%v next=%q err=%v", rows, next, err)
	}
	raw, _ := os.ReadFile(requests)
	for _, want := range []string{`"limit":20`, `"cursor":"newer"`, `"cwd":"/project"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
}

func TestCodexPageFailureIsNeverAnEmptySuccess(t *testing.T) {
	for _, reply := range []string{
		`{"id":1,"error":{"code":-1,"message":"unavailable"}}`,
		`{"id":1,"result":{}}`,
		`{"id":1,"result":{"data":{}}}`,
		`{"id":1,"result":{"data":[],"nextCursor":"same"}}`,
	} {
		t.Run(reply, func(t *testing.T) {
			exe, _ := historyProvider(t, reply)
			_, _, err := codexPastPage(context.Background(), exe, Place{Path: "/project"}, nil, PastTitles{}, "same")
			if !errors.Is(err, ErrCodexUnavailable) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCodexPageHonorsCancellation(t *testing.T) {
	exe, _ := historyProvider(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := codexPastPage(ctx, exe, Place{Path: "/project"}, nil, PastTitles{}, "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("provider failed to stop on cancellation: %v", err)
	}
}

func TestCodexResumeReadsOnlyTheSelectedThreadAndChecksItsProject(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000002"
	for _, cwd := range []string{"/project", "/another-project"} {
		exe, requests := historyProvider(t, `{"id":1,"result":{"thread":{"id":"`+id+`","cwd":"`+cwd+`","source":"cli","updatedAt":10,"preview":"An ordinary conversation"}}}`)
		found, err := codexConversation(context.Background(), exe, Place{Path: "/project"}, id)
		if err != nil || found != (cwd == "/project") {
			t.Fatalf("cwd=%s found=%v err=%v", cwd, found, err)
		}
		raw, _ := os.ReadFile(requests)
		if strings.Contains(string(raw), "thread/list") || !strings.Contains(string(raw), `"includeTurns":false`) || !strings.Contains(string(raw), `"threadId":"`+id+`"`) {
			t.Fatalf("wrong request %s", raw)
		}
	}
	exe, _ := historyProvider(t, `{"id":1,"error":{"code":-1,"message":"unavailable"}}`)
	if found, err := codexConversation(context.Background(), exe, Place{Path: "/project"}, id); found || !errors.Is(err, ErrCodexUnavailable) {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestCodexUserSourcesSurviveFilteredPages(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000003"
	dir := t.TempDir()
	exe := filepath.Join(dir, "provider")
	// Model the provider's source filter before Clawdline sees each page.
	script := `#!/bin/sh
while IFS= read -r line; do
case "$line" in
*initialize*0*|*0*initialize*) echo '{"id":0,"result":{}}';;
*thread/list*)
  case "$line" in
  *'"cursor":"older"'*|*'"limit":40'*)
    case "$line" in
    *vscode*) echo '{"id":1,"result":{"data":[{"id":"` + id + `","cwd":"/project","source":"vscode","updatedAt":10,"preview":"Continue the design"}],"nextCursor":null}}';;
    *) echo '{"id":1,"result":{"data":[],"nextCursor":null}}';;
    esac;;
  *) echo '{"id":1,"result":{"data":[{"id":"00000000-0000-4000-8000-000000000001","cwd":"/project","source":"cli","updatedAt":20,"preview":"` + BriefingOpening + ` child"}],"nextCursor":"older"}}';;
  esac;;
esac
done
`
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	rows, cursor, err := codexPastPage(context.Background(), exe, Place{Path: "/project"}, nil, PastTitles{}, "")
	if err != nil || len(rows) != 0 || cursor != "older" {
		t.Fatalf("filtered page: rows=%v cursor=%q err=%v", rows, cursor, err)
	}
	rows, cursor, err = codexPastPage(context.Background(), exe, Place{Path: "/project"}, nil, PastTitles{}, cursor)
	if err != nil || len(rows) != 1 || rows[0].ID != id || cursor != "" {
		t.Fatalf("older IDE conversation: rows=%v cursor=%q err=%v", rows, cursor, err)
	}
	// The older non-paginated reader must use the same user sources.
	raw, err := codexThreadList(context.Background(), exe, "/project", 40)
	legacyRows := codexListed(raw, "/project", nil, PastTitles{})
	if err != nil || len(legacyRows) != 1 || legacyRows[0].ID != id {
		t.Fatalf("legacy list: %s %v", raw, err)
	}
}

func TestCodexResumeKeepsInteractiveSourcesAndProjectBoundaries(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000003"
	for _, tc := range []struct {
		name, source, cwd, preview string
		ephemeral, want            bool
	}{
		{"CLI", `"cli"`, "/project", "Continue the design", false, true},
		{"IDE", `"vscode"`, "/project", "Continue the design", false, true},
		{"sibling", `"vscode"`, "/project-other", "Continue the design", false, false},
		{"subdirectory", `"vscode"`, "/project/sub", "Continue the design", false, false},
		{"parent", `"vscode"`, "/", "Continue the design", false, false},
		{"background", `"exec"`, "/project", "Guardian review", false, false},
		{"subagent", `{"subAgent":"review"}`, "/project", "Review", false, false},
		{"ephemeral", `"vscode"`, "/project", "Temporary review", true, false},
		{"child", `"vscode"`, "/project", BriefingOpening + " synthetic", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			thread := `{"id":"` + id + `","cwd":"` + tc.cwd + `","source":` + tc.source + `,"ephemeral":` + strconv.FormatBool(tc.ephemeral) + `,"updatedAt":10,"preview":"` + tc.preview + `"}`
			exe, _ := historyProvider(t, `{"id":1,"result":{"thread":`+thread+`}}`)
			found, err := codexConversation(context.Background(), exe, Place{Path: "/project"}, id)
			if err != nil || found != tc.want {
				t.Fatalf("resume found=%v want=%v err=%v", found, tc.want, err)
			}
			rows := codexListed([]byte(`{"result":{"data":[`+thread+`]}}`), "/project", nil, PastTitles{})
			if (len(rows) == 1) != tc.want {
				t.Fatalf("list rows=%v want visible=%v", rows, tc.want)
			}
		})
	}
}
