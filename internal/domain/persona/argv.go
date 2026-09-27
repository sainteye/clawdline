package persona

import "regexp"

// How a persona is carried on a command line, and read back from one.
//
// The process table is the record. A session's persona is not stored
// anywhere else: while the process runs, its argv says which persona it was
// launched with, so a badge drawn from it cannot outlive or contradict the
// session it is drawn on (a person retyping `claude` in the same terminal
// gets a row without one).

// Marker opens the Codex pointer, and is what reads it back.
const Marker = "clawdline-persona:"

// CodexInstruction is the `developer_instructions` a Codex session is given:
// one sentence pointing at the file, rather than the text itself, so the
// command line stays short. It is ASCII apart from the path, because `ps`
// renders a command line under LC_ALL=C and escapes anything else.
func CodexInstruction(p Persona, path string) string {
	return Marker + p.ID + " - Your role in this session is " + p.Name.En +
		`. Its full definition is in the file "` + path + `". ` +
		"Read that file completely before your first answer and follow it for the whole session."
}

var (
	// claudeFlag is Claude Code's `--append-system-prompt-file <dir>/personas/<id>.md`.
	// `ps` joins argv with spaces, so a directory with a space in it is
	// matched by the shortest run up to the personas directory.
	claudeFlag = regexp.MustCompile(`--append-system-prompt-file[= ](?:[^\n]*?/)?` + DirName + `/([a-z]+(?:-[a-z]+)*)\.md(?:\s|$)`)
	// codexMarker is the pointer's marker, wherever the argument puts it.
	codexMarker = regexp.MustCompile(regexp.QuoteMeta(Marker) + `([a-z]+(?:-[a-z]+)*)`)
)

// FromCommandLine is the persona a process's command line was launched
// with, or "" when it names none this catalog knows. An id the catalog does
// not have — a file somebody wrote by hand, a daemon from a later release —
// is not a persona here.
func FromCommandLine(command string) string {
	for _, re := range []*regexp.Regexp{claudeFlag, codexMarker} {
		if m := re.FindStringSubmatch(command); len(m) == 2 {
			if _, ok := Known(m[1]); ok {
				return m[1]
			}
		}
	}
	return ""
}
