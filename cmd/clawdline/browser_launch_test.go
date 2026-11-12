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
