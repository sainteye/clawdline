package main

import (
	"strings"
	"testing"
)

func TestTypeFromStdinIsMarkedSecretAndLosesOneNewline(t *testing.T) {
	var errOut strings.Builder
	args, _, code := browserArgs("type", []string{"--stdin"}, strings.NewReader("hunter2\n"), &errOut)
	if code != 0 || args["text"] != "hunter2" || args["secret"] != true {
		t.Fatalf("args %v code %d %s", args, code, errOut.String())
	}
	if _, _, code := browserArgs("type", []string{"--stdin"}, strings.NewReader(""), &errOut); code != 2 {
		t.Fatal("empty standard input was accepted")
	}
}

func TestViewportCommandOnlySendsTheChosenPreset(t *testing.T) {
	for _, mode := range []string{"mobile", "desktop"} {
		var errOut strings.Builder
		args, _, code := browserArgs("viewport", []string{mode}, strings.NewReader(""), &errOut)
		if code != 0 || args["mode"] != mode {
			t.Fatalf("%s: %v code %d %s", mode, args, code, errOut.String())
		}
	}
	var errOut strings.Builder
	if _, _, code := browserArgs("viewport", []string{"100000x100000"}, strings.NewReader(""), &errOut); code != 2 {
		t.Fatal("an arbitrary viewport was accepted")
	}
}
