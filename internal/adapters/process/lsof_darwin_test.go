//go:build darwin

package process

import "testing"

// lsof's `-F` output is parsed rather than its columns, for the reason Scan
// does not count `ps` columns: a table meant for a person changes width. This
// is one real block, as `lsof -w -n -P -F pfn -p <pid>` prints it.
func TestParseLsofKeepsEachProcessesOwnFiles(t *testing.T) {
	out := "p74653\n" +
		"fcwd\nn/Users/x/code/alpha\n" +
		"f11\nn/Users/x/.codex/state_5.sqlite\n" +
		"f12\nn" + rolloutA + "\n" +
		"p17378\n" +
		"fcwd\nn/Users/x/code/beta\n" +
		"f9\nn" + rolloutB + "\n"
	files, cwds := parseLsof(out)
	if len(files[74653]) != 2 || files[74653][1] != rolloutA {
		t.Fatalf("got %v", files[74653])
	}
	if len(files[17378]) != 1 || files[17378][0] != rolloutB {
		t.Fatalf("got %v", files[17378])
	}
	if cwds[74653] != "/Users/x/code/alpha" || cwds[17378] != "/Users/x/code/beta" {
		t.Fatalf("cwd = %v", cwds)
	}
	// A name before any process block belongs to nobody, and a short or
	// unnumbered line is not a record.
	stray, strayCWD := parseLsof("n/tmp/orphan\np\npx\nn/tmp/also\n")
	if len(stray) != 0 {
		t.Fatalf("got %v, want nothing claimed by nobody", stray)
	}
	if len(strayCWD) != 0 {
		t.Fatalf("got cwd %v, want nothing claimed by nobody", strayCWD)
	}
}
