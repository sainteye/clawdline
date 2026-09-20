//go:build darwin

package process

import "testing"

// lsof's `-F` output is parsed rather than its columns, for the reason Scan
// does not count `ps` columns: a table meant for a person changes width. This
// is one real block, as `lsof -w -n -P -F pn -p <pid>` prints it.
func TestParseLsofKeepsEachProcessesOwnFiles(t *testing.T) {
	out := "p74653\n" +
		"n/Users/x/.codex/state_5.sqlite\n" +
		"n" + rolloutA + "\n" +
		"p17378\n" +
		"n" + rolloutB + "\n"
	files := parseLsof(out)
	if len(files[74653]) != 2 || files[74653][1] != rolloutA {
		t.Fatalf("got %v", files[74653])
	}
	if len(files[17378]) != 1 || files[17378][0] != rolloutB {
		t.Fatalf("got %v", files[17378])
	}
	// A name before any process block belongs to nobody, and a short or
	// unnumbered line is not a record.
	stray := parseLsof("n/tmp/orphan\np\npx\nn/tmp/also\n")
	if len(stray) != 0 {
		t.Fatalf("got %v, want nothing claimed by nobody", stray)
	}
}
