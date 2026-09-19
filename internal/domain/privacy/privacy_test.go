package privacy

import (
	"strings"
	"testing"
)

// j joins its pieces. Every private-shaped sample below is spelled in pieces,
// because this file is published and scanned like any other: a sample written
// whole would be exactly the thing the checker exists to stop.
func j(pieces ...string) string { return strings.Join(pieces, "") }

// TestEachRuleCatchesTheRealShape is the rule's reason to exist: every line
// here is shaped like something that really leaked, and each must be caught
// by the rule named, and only reported once.
func TestEachRuleCatchesTheRealShape(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ rule, line string }{
		{"home-path", j("cd /Users/", "dana/code/app")},
		{"home-path", j(`"cwd": "/home/`, `dana/src"`)},
		{"home-path", j(`C:\`, `Users\`, `dana\AppData`)},
		{"home-path", j("~/.claude/projects/-Users-", "dana-code-app/x.jsonl")},
		{"home-path", j("link -> /Users/", "Dana/code")},
		{"private-repo", j("see ~/code/clawdline", "-cloud/contracts")},
		{"private-repo", j("Clawdline_", "Cloud docs")},
		{"uuid", j("task ", "7c9e6679-7425-40de-", "944b-e07fc1f90ae7 landed")},
		{"uuid", j("tab ", "D379E32C-9201-45F1-", "B889-25D015FB56B5")},
		{"uuid", j("11111111-2222-3333-", "4444-555555555555")},
		{"task-id", j("evidence is in task `", "1f9ca362`'s artifacts")},
		{"task-id", j("<!-- section:x owner:task-", "1d861805 -->")},
		{"task-id", j("# branch.head clawdline/task/", "2fc3e801")},
		{"uuid", j("worktrees/x/7c9e6679-7425-40de-", "944b-e07fc1f90ae7")},
		{"uuid", j("task/", "7c9e6679-7425-40de-", "944b-e07fc1f90ae7")},
		{"pane-id", j("the child in `%", "905` wrote it")},
		{"pane-id", j("/v1/sessions/%25", "905/info")},
		{"pane-id", j("clawdline:@", "800.")},
		{"pane-id", j("（%", "896）")},
		{"email", j("mail dana", "@corp.co")},
		{"credential", j("-----BEGIN OPENSSH ", "PRIVATE KEY-----")},
		{"credential", j("key sk-ant-", "api03-Zq8vT2mR7pL0xW4nB6yC")},
		{"credential", j("ghp_", "Zq8vT2mR7pL0xW4nB6yCa9dF3gH5jK1lQ7sU")},
		{"credential", j("Authorization: Bearer ", "Zq8vT2mR7pL0xW4nB6yCa9dF")},
		{"credential", j(`"vapid_private_key": "`, `Zq8vT2mR7pL0xW4nB6yCa9dF3gH5jK1lQ7sU0e"`)},
		{"credential", j(`token := "`, `Zq8vT2mR7pL0xW4nB6yCa9dF3gH5"`)},
		{"credential", j("https://dana:Zq8vT2mR7pL0", "@git.corp.co/x")},
		{"hostname", j("ssh Danas-", "MacBook-Pro")},
		{"hostname", j("http://studio", ".local:7727")},
		{"hostname", j("ping dana-", "mbp.local")},
		{"ip-address", j("http://192.", "168.1.37:7727")},
		{"ip-address", j("tailnet 100.", "101.7.9")},
	} {
		got := New(nil).Scan("x.go", []byte(c.line))
		if len(got) != 1 || got[0].Rule != c.rule || got[0].Line != 1 {
			t.Errorf("%q: got %+v, want one %s on line 1", c.line, got, c.rule)
		}
	}
}

// TestFixturesPass is the other half: the shapes tests and documents are told
// to use must pass, or the rule message is a lie and people stop reading it.
func TestFixturesPass(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		"/Users/you/code/thing", "/Users/a/code/clawdline-go", "/home/sean/x",
		"/Users/<name>/code", "/home/$USER", "/home/...", "/Users/Shared/x",
		"-Users-a-code-app", "/api/v1/home/feed", "/Users/%s/code",
		"c6000001-0000-4000-8000-000000000001", "22222222-2222-4222-8222-222222222222",
		"0F0F0F0F-1234-4000-8000-00000000ABCD",
		"clawdline-task-aaaaaaaa-other", "task/c6000001", "task 70d00010", "subtask 1f9ca362x",
		"%1", "%12", `"%12": "%2512"`, "@7", "n%1000", "fmt %100d", "react@1000",
		"t@example.invalid", "somebody@example.test", "icon@2x.png", "git@github.com",
		"https://console.example", "u@mail.example.com", "wss://u:p@relay.clawdline.com/v1/connect",
		"https://user:<password>@host", "https://ci:${TOKEN}@git.corp.co",
		`taskSecret = "5ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2"`,
		`"master_secret" : "oKGio6SlpqeoqaqrrK2ur7CxsrO0tba3uLm6u7y9vr8="`,
		`token := "test-token-that-is-long-enough"`, "Bearer <token>", `"Bearer " + token`,
		"f.local, err = g.auth.LocalToken()", "dispatch-policy.local.md", "settings.local.json",
		"http://127.0.0.1:7727", "0.0.0.0", "192.0.2.4", "RFC 8785 §3.2.2.3", "v1.2.3.4", "1.2.3.4.5",
		"258EAFA5-E914-47DA-95CA-C5AB0DC85B11",
	} {
		if got := New(nil).Scan("x.go", []byte(line)); len(got) != 0 {
			t.Errorf("%q: got %+v, want nothing", line, got)
		}
	}
}

// TestPrivateWordsAreWholeWordsAnyCase: the person's own list flags whole words,
// in any case, and nothing when there is no list.
func TestPrivateWordsAreWholeWordsAnyCase(t *testing.T) {
	t.Parallel()
	s := New([]string{"# a comment", "", "Acme", "night shift"})
	for line, want := range map[string]int{
		"deploy acme today":      1,
		"ACME-app":               1,
		"acmeish":                0,
		"the Night Shift report": 1,
		"night shifts":           0,
	} {
		if got := s.Scan("x.md", []byte(line)); len(got) != want {
			t.Errorf("%q: got %+v, want %d", line, got, want)
		}
	}
	if got := New(nil).Scan("x.md", []byte("deploy acme today")); len(got) != 0 {
		t.Errorf("no list: got %+v", got)
	}
}

// TestFindingsNameTheirLineAndThePath: line numbers are 1-based and the path
// itself is scanned, reported as line 0.
func TestFindingsNameTheirLineAndThePath(t *testing.T) {
	t.Parallel()
	data := []byte(j("ok\nok\nsee %", "905\n"))
	got := New(nil).Scan(j("notes/-Users-", "dana-x.md"), data)
	if len(got) != 2 || got[0].Line != 0 || got[0].Rule != "home-path" || got[1].Line != 3 || got[1].Rule != "pane-id" {
		t.Fatalf("got %+v", got)
	}
}

// TestACredentialIsNeverPrinted: the finding is printed, and a build log is
// the last place a caught key should be copied to.
func TestACredentialIsNeverPrinted(t *testing.T) {
	t.Parallel()
	key := j("Zq8vT2mR7pL0", "xW4nB6yCa9dF3gH5")
	got := New(nil).Scan("x.go", []byte(`api_key = "`+key+`"`))
	if len(got) != 1 || strings.Contains(got[0].Match, key[4:]) || !strings.HasPrefix(got[0].Match, key[:4]) {
		t.Fatalf("got %+v", got)
	}
}

// TestAnAllowanceIsExact: an allowance names one text in one file, so the
// same file still fails on anything else, and another file on the same text.
func TestAnAllowanceIsExact(t *testing.T) {
	t.Parallel()
	const copied = "web/console/src/legacy/js/net/cloud-crypto.js"
	keys := j(`open("clawdline-`, `cloud-keys", 1)`)
	repo := j("see clawdline-", "cloud/docs")
	if got := New(nil).Scan(copied, []byte(keys)); len(got) != 0 {
		t.Errorf("allowed text in its file: %+v", got)
	}
	if got := New(nil).Scan(copied, []byte(repo)); len(got) != 1 {
		t.Errorf("other text in the same file: %+v", got)
	}
	if got := New(nil).Scan("other.js", []byte(keys)); len(got) != 1 {
		t.Errorf("allowed text in another file: %+v", got)
	}
}
