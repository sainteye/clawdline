package projects

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/persona"
)

// shellWords splits a line the way a POSIX shell's word splitting and quote
// removal do for the three quotings a launch line can hold: '…', "…" and a
// backslash outside quotes. Anything it cannot read fails the test rather than
// guessing.
func shellWords(t *testing.T, line string) []string {
	t.Helper()
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\'':
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				t.Fatalf("unterminated single quote in %q", line)
			}
			cur.WriteString(line[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == '"':
			i++
			for ; i < len(line) && line[i] != '"'; i++ {
				if line[i] == '\\' && i+1 < len(line) && strings.IndexByte("$`\"\\\n", line[i+1]) >= 0 {
					i++
				} else if line[i] == '$' || line[i] == '`' {
					t.Fatalf("an expansion inside double quotes in %q", line)
				}
				cur.WriteByte(line[i])
			}
			if i >= len(line) {
				t.Fatalf("unterminated double quote in %q", line)
			}
			inWord = true
		case c == '\\':
			if i+1 < len(line) {
				i++
				cur.WriteByte(line[i])
			}
			inWord = true
		case strings.IndexByte("$`;&|<>(){}*?[#~", c) >= 0:
			t.Fatalf("an unquoted %q the shell would read in %q", c, line)
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// tomlBasic decodes one TOML basic string, the whole of raw, by the spec's
// escapes; anything else is an error.
func tomlBasic(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", errors.New("not a quoted basic string")
	}
	body := raw[1 : len(raw)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == '"' {
			return "", errors.New("an unescaped quote inside")
		}
		if c < 0x20 && c != '\t' || c == 0x7f {
			return "", fmt.Errorf("an unescaped control character %#x", c)
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", errors.New("a trailing backslash")
		}
		switch body[i] {
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'u':
			if i+5 > len(body) {
				return "", errors.New("a short \\u escape")
			}
			n, err := strconv.ParseUint(body[i+1:i+5], 16, 32)
			if err != nil {
				return "", err
			}
			b.WriteRune(rune(n))
			i += 4
		default:
			return "", fmt.Errorf("an unknown escape \\%c", body[i])
		}
	}
	return b.String(), nil
}

func TestTOMLStringDecodesToItsValue(t *testing.T) {
	for _, v := range []string{
		"plain", `a "quoted" word`, `back\slash`, "tab\there", "new\nline", "bell\x07", "del\x7f",
		"it's", "unicode 角色", `C:\path\"x"`,
	} {
		got, err := tomlBasic(TOMLString(v))
		if err != nil || got != v {
			t.Errorf("%q → %s → %q (%v)", v, TOMLString(v), got, err)
		}
	}
	if got := TOMLString(`say "hi"`); got != `"say \"hi\""` {
		t.Errorf("hand-computed: %s", got)
	}
}

// A persona reaches each assistant as exactly one argument, whatever the path
// holds, and a Codex one decodes to exactly the sentence intended.
func TestAdmitCarriesAPersona(t *testing.T) {
	architect, _ := persona.Known("architect")
	for _, dir := range []string{
		"/home/a/.config/clawdline-next/personas",
		"/Users/a/with space/personas",
		"/Users/a/it's here/personas",
		`/Users/a/say "hi"/personas`,
	} {
		path := filepath.Join(dir, "architect.md")

		claude, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: AssistantClaude, Model: "opus",
			Persona: "architect", PersonaDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		words := shellWords(t, claude.ShellCommand())
		if i := index(words, "--append-system-prompt-file"); i < 0 || i+1 >= len(words) || words[i+1] != path {
			t.Errorf("claude %q: %q", dir, words)
		}
		if words[len(words)-1] != path {
			t.Errorf("claude %q: the path is not one whole argument at the end: %q", dir, words)
		}

		codex, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: AssistantCodex,
			Resume: "0f1e2d3c-0000-4000-8000-000000000001", Persona: "architect", PersonaDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		words = shellWords(t, codex.ShellCommand())
		i := index(words, "-c")
		if i < 0 || i+1 != len(words)-1 {
			t.Fatalf("codex %q: %q", dir, words)
		}
		value, ok := strings.CutPrefix(words[i+1], "developer_instructions=")
		if !ok {
			t.Fatalf("codex %q: %q", dir, words[i+1])
		}
		got, err := tomlBasic(value)
		if want := persona.CodexInstruction(architect, path); err != nil || got != want {
			t.Errorf("codex %q: decoded %q (%v), want %q", dir, got, err, want)
		}
		// The resume stays ahead of it, where its optional value needs it.
		if j := index(words, "resume"); j < 0 || j > i || words[j+1] != "0f1e2d3c-0000-4000-8000-000000000001" {
			t.Errorf("codex: %q", words)
		}
		if persona.FromCommandLine(strings.Join(words, " ")) != "architect" {
			t.Errorf("codex %q: the command line does not read back as architect", dir)
		}
	}
}

func TestCodexBoardLanguageAndPersonaShareOneDeveloperInstruction(t *testing.T) {
	dir := "/personas"
	architect, _ := persona.Known("architect")
	l, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: AssistantCodex, Language: "zh-TW",
		Persona: architect.ID, PersonaDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	words := shellWords(t, l.ShellCommand())
	if count := strings.Count(strings.Join(words, "\n"), "developer_instructions="); count != 1 {
		t.Fatalf("got %d developer instructions in %q", count, words)
	}
	i := index(words, "-c")
	value, ok := strings.CutPrefix(words[i+1], "developer_instructions=")
	if !ok {
		t.Fatalf("Codex config is %q", words[i+1])
	}
	got, err := tomlBasic(value)
	if err != nil {
		t.Fatal(err)
	}
	if want := persona.CodexInstruction(architect, filepath.Join(dir, "architect.md")); !strings.HasPrefix(got, want+"\n\n") {
		t.Fatalf("persona is absent from %q", got)
	}
	if !strings.HasSuffix(got,
		`Write every Clawdline Board item title, description, and step you author in the language identified by BCP 47 tag "zh-TW".`) {
		t.Fatalf("Board language is absent from %q", got)
	}
}

func TestAdmitRefusesAPersonaItCannotCarry(t *testing.T) {
	for name, req := range map[string]LaunchRequest{
		"unknown":      {ProjectRoot: "/p", Assistant: AssistantClaude, Persona: "wizard", PersonaDir: "/d"},
		"path-shaped":  {ProjectRoot: "/p", Assistant: AssistantClaude, Persona: "../architect", PersonaDir: "/d"},
		"no directory": {ProjectRoot: "/p", Assistant: AssistantClaude, Persona: "architect"},
		"relative":     {ProjectRoot: "/p", Assistant: AssistantCodex, Persona: "architect", PersonaDir: "d"},
	} {
		_, err := Admit(req)
		if !errors.Is(err, ErrInvalidLaunch) {
			t.Errorf("%s: %v", name, err)
		}
		if name == "unknown" && !errors.Is(err, ErrUnknownPersona) {
			t.Errorf("unknown: %v is not ErrUnknownPersona", err)
		}
	}
	// No persona is no argument.
	l, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: AssistantClaude, PersonaDir: "/d"})
	if err != nil || len(l.Arguments) != 0 {
		t.Errorf("%v %v", l.Arguments, err)
	}
}

func index(words []string, w string) int {
	for i, x := range words {
		if x == w {
			return i
		}
	}
	return -1
}
