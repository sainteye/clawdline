package main

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/sainteye/clawdline/internal/adapters/graphify"
)

func graphifyCommand(args []string) {
	os.Exit(runGraphifyCheck(context.Background(), args, os.Stdout))
}

// The machine-readable command is also usable when the launch integration is
// off: it only compares local files, without running the third-party tool.
func runGraphifyCheck(ctx context.Context, args []string, out io.Writer) int {
	s := graphify.Status{State: "unverified", Reason: "graphify_invalid_arguments"}
	code := 2
	if len(args) == 2 && args[0] == "check" && args[1] != "" {
		var err error
		s, err = graphify.Inspect(ctx, args[1])
		if err != nil {
			s.State = "unverified"
			s.Reason = "graphify_check_unavailable"
		}
		if s.State == graphify.Matched && err == nil {
			code = 0
		} else {
			code = 1
		}
	}
	if json.NewEncoder(out).Encode(s) != nil {
		return 1
	}
	return code
}
