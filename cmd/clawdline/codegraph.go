package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/codegraph"
)

func codegraphCommand(args []string) {
	if len(args) == 2 && args[0] == "query" {
		b, err := openBroker(0)
		if err != nil {
			fail(err)
		}
		root, err := os.Getwd()
		if err != nil {
			fail(err)
		}
		os.Exit(runCodeGraphQuery(b, root, args[1], os.Stdout, os.Stderr))
	}
	os.Exit(runCodeGraphCheck(context.Background(), args, os.Stdout))
}

// The machine-readable command is also usable when the launch integration is
// off: it only compares local files, without running the third-party tool.
func runCodeGraphCheck(ctx context.Context, args []string, out io.Writer) int {
	s := codegraph.Status{State: "unverified", Reason: "codegraph_invalid_arguments"}
	code := 2
	if len(args) == 2 && args[0] == "check" && args[1] != "" {
		var err error
		s, err = codegraph.Inspect(ctx, args[1])
		if err != nil {
			s.State = "unverified"
			s.Reason = "codegraph_check_unavailable"
		}
		if s.State == codegraph.Matched && err == nil {
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

func runCodeGraphQuery(b *broker, root, question string, out, errOut io.Writer) int {
	if strings.TrimSpace(question) == "" || len(question) > codegraph.QueryBytesLimit {
		fmt.Fprintln(errOut, "codegraph_invalid_arguments")
		return 2
	}
	answer, err := b.request(http.MethodPost, "/v1/orchestrator/codegraph/query", nil, map[string]string{"directory": root, "query": question}, "")
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return report(out, errOut, "codegraph query", answer)
}
