package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func resourcesCommand(args []string) {
	b, err := openBroker(0)
	if err != nil {
		fail(err)
	}
	os.Exit(runResources(os.Stdout, os.Stderr, b, args))
}

// runResources never turns a reviewed plan into a fresh plan behind the user's
// back. Apply needs the exact plan file and a caller-retained execution UUID.
func runResources(stdout, stderr io.Writer, b *broker, args []string) int {
	usage := func() int {
		fmt.Fprintln(stderr, cliCopy("misc", "resources.usage", "usage: clawdline resources list [--after ID] | allocate --owner-kind task|root --owner-id UUID --class cache|scratch|execution_log | plan UUID... | apply --plan FILE --execution UUID | publish UUID | pin --value=true|false UUID | measure | admit"))
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	fs := flag.NewFlagSet("resources", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var after, kind, owner, class, file, execution string
	var pin bool
	fs.StringVar(&after, "after", "", "")
	fs.StringVar(&kind, "owner-kind", "", "")
	fs.StringVar(&owner, "owner-id", "", "")
	fs.StringVar(&class, "class", "", "")
	fs.StringVar(&file, "plan", "", "")
	fs.StringVar(&execution, "execution", "", "")
	fs.BoolVar(&pin, "value", true, "")
	if fs.Parse(args[1:]) != nil {
		return usage()
	}
	method, path, query := http.MethodPost, "/v1/orchestrator/resources/"+args[0], url.Values{}
	var body any = struct{}{}
	rest := fs.Args()
	switch args[0] {
	case "list":
		if len(rest) > 0 {
			return usage()
		}
		method = http.MethodGet
		path = "/v1/orchestrator/resources"
		query.Set("after", after)
	case "allocate":
		if len(rest) > 0 || (kind != "task" && kind != "root") || owner == "" || (class != "cache" && class != "scratch" && class != "execution_log") {
			return usage()
		}
		body = map[string]string{"owner_kind": kind, "owner_id": owner, "class": class}
	case "plan":
		if len(rest) == 0 || len(rest) > taskresource.PageRowsLimit {
			return usage()
		}
		body = map[string]any{"resources": rest}
	case "apply":
		if len(rest) > 0 || file == "" || execution == "" {
			return usage()
		}
		f, err := os.Open(file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		decoder := json.NewDecoder(io.LimitReader(f, brokerAnswerLimit+1))
		decoder.DisallowUnknownFields()
		var plan taskresources.Plan
		err = decoder.Decode(&plan)
		if err == nil {
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				err = errors.New("resource_plan_invalid")
			}
		}
		f.Close()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		body = map[string]any{"plan": plan, "execution": execution}
	case "publish":
		if len(rest) != 1 {
			return usage()
		}
		body = map[string]string{"resource": rest[0]}
	case "pin":
		if len(rest) != 1 {
			return usage()
		}
		body = map[string]any{"resource": rest[0], "pinned": pin}
	case "measure", "admit":
		if len(rest) > 0 {
			return usage()
		}
	default:
		return usage()
	}
	answer, err := b.request(method, path, query, body, "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return report(stdout, stderr, "resources", answer)
}
