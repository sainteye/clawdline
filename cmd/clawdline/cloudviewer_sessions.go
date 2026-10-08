package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloud"
)

type viewerDaemonRefusal struct{ Code string }

func (r viewerDaemonRefusal) Error() string { return r.Code }

func viewerDaemonRequest(method, path string, input, output any) error {
	req, err := daemonRequest(method, path, input)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 35 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 400 {
		var refusal struct {
			Code  string `json:"code"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &refusal) == nil {
			if refusal.Error.Code != "" {
				return viewerDaemonRefusal{refusal.Error.Code}
			}
			if refusal.Code != "" {
				return viewerDaemonRefusal{refusal.Code}
			}
		}
		return viewerDaemonRefusal{fmt.Sprintf("http_%d", response.StatusCode)}
	}
	if output != nil && json.Unmarshal(data, output) != nil {
		return errors.New("malformed_viewer_reply")
	}
	return nil
}

func viewerQuery(path string, destination cloud.ViewerDestination) string {
	q := url.Values{"machine": {destination.MachineID}, "session": {destination.SessionID},
		"generation": {destination.ExecutionGeneration}}
	return path + "?" + q.Encode()
}

func viewerNewRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func viewerCLIRefusal(err error) string {
	var refusal viewerDaemonRefusal
	if !errors.As(err, &refusal) {
		return err.Error()
	}
	switch refusal.Code {
	case "cloud_disabled":
		return cliCopy("viewer", "error.disabled", "cloud_disabled: turn Cloud on before using remote Sessions")
	case "viewer_login_required":
		return cliCopy("viewer", "error.login", "viewer_login_required: run `clawdline cloud viewer login`")
	case "viewer_machine_not_paired":
		return cliCopy("viewer", "error.pair", "viewer_machine_not_paired: pair this viewer with the selected machine")
	case "viewer_machine_revoked":
		return cliCopy("viewer", "error.revoked", "viewer_machine_revoked: access to this machine was revoked")
	case "viewer_machine_offline":
		return cliCopy("viewer", "error.offline", "viewer_machine_offline: this machine is offline; nothing was queued")
	case "execution_generation_changed":
		return cliCopy("viewer", "error.changed", "execution_generation_changed: select the Session's current execution again")
	case "viewer_status_unavailable", "stale", "event_gap", "unknown":
		return cliCopy("viewer", "error.status", "viewer_status_unavailable: a current Session target could not be proved")
	case "receipt_outcome_unknown":
		return cliCopy("viewer", "error.uncertain", "receipt_outcome_unknown: query the request receipt before acting again")
	default:
		return refusal.Code
	}
}

func viewerPrintJSON(out io.Writer, value any) {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func cloudViewerSessionCommand(out, errs io.Writer, args []string) int {
	word := args[0]
	fs := flag.NewFlagSet("cloud viewer "+word, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	machine := fs.String("machine", "", "Cloud machine ID")
	session := fs.String("session", "", "Session ID")
	generation := fs.String("generation", "", "execution generation")
	text := fs.String("text", "", "prompt text")
	answer := fs.String("answer", "", "answer key")
	expect := fs.String("expect", "", "question fingerprint")
	action := fs.String("action", "", "action for receipt lookup")
	request := fs.String("request", "", "durable request ID")
	before := fs.String("before", "", "next older transcript cursor")
	asJSON := fs.Bool("json", false, "print JSON")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 {
		fmt.Fprintln(errs, cliCopy("viewer", "sessions.usage", "usage: clawdline cloud viewer <sessions|read|send|answer|interrupt|end|receipt> [--machine ID] [--session ID --generation HEX] [--before CURSOR for read]"))
		return 2
	}
	destination := cloud.ViewerDestination{MachineID: *machine, SessionID: *session, ExecutionGeneration: *generation}
	if word == "sessions" {
		if *session != "" || *generation != "" {
			return viewerCLIUsage(errs)
		}
		return viewerListSessions(out, errs, *machine, *asJSON)
	}
	if !destination.Valid() {
		return viewerCLIUsage(errs)
	}
	if word == "read" {
		path := viewerQuery("/v1/cloud/viewer/detail", destination)
		if *before != "" {
			cursor, err := strconv.ParseInt(*before, 10, 64)
			if err != nil || cursor <= 0 {
				return viewerCLIUsage(errs)
			}
			path += "&before=" + strconv.FormatInt(cursor, 10)
			var page cloud.ViewerTranscriptPage
			if err := viewerDaemonRequest(http.MethodGet, path, nil, &page); err != nil {
				fmt.Fprintln(errs, viewerCLIRefusal(err))
				return 1
			}
			viewerPrintJSON(out, page)
			return 0
		}
		var detail cloud.ViewerDetail
		if err := viewerDaemonRequest(http.MethodGet, path, nil, &detail); err != nil {
			fmt.Fprintln(errs, viewerCLIRefusal(err))
			return 1
		}
		viewerPrintJSON(out, detail)
		return 0
	}
	if *before != "" {
		return viewerCLIUsage(errs)
	}
	if word == "receipt" {
		if *request == "" || *action == "" {
			return viewerCLIUsage(errs)
		}
		query, err := viewerNewRequestID()
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		path := viewerQuery("/v1/cloud/viewer/receipts", destination) + "&" + url.Values{
			"action": {*action}, "request": {*request}, "query": {query}}.Encode()
		var receipt cloud.ViewerReceipt
		if err := viewerDaemonRequest(http.MethodGet, path, nil, &receipt); err != nil {
			fmt.Fprintln(errs, viewerCLIRefusal(err))
			return 1
		}
		viewerPrintJSON(out, receipt)
		return 0
	}
	if word != "send" && word != "answer" && word != "interrupt" && word != "end" {
		return viewerCLIUsage(errs)
	}
	if word == "send" && strings.TrimSpace(*text) == "" || word == "answer" && (*answer == "" || *expect == "") {
		return viewerCLIUsage(errs)
	}
	id := *request
	if id == "" {
		var err error
		id, err = viewerNewRequestID()
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
	}
	// This line is emitted before the request: a lost HTTP answer or CLI crash
	// cannot erase the lookup pointer the person needs for an uncertain effect.
	pointer := out
	if *asJSON {
		pointer = errs
	}
	fmt.Fprintf(pointer, cliCopy("viewer", "action.request", "request %s\n"), id)
	input := cloud.ViewerMutation{Destination: destination, Action: word, Request: id,
		Text: *text, Answer: *answer, Expect: *expect}
	var result cloud.ViewerMutationResult
	if err := viewerDaemonRequest(http.MethodPost, "/v1/cloud/viewer/actions", input, &result); err != nil {
		fmt.Fprintln(errs, viewerCLIRefusal(err))
		fmt.Fprintf(errs, cliCopy("viewer", "action.lookup", "Check: clawdline cloud viewer receipt --machine %s --session %s --generation %s --action %s --request %s\n"),
			*machine, *session, *generation, word, id)
		return 1
	}
	viewerPrintJSON(out, result)
	return 0
}

func viewerCLIUsage(errs io.Writer) int {
	fmt.Fprintln(errs, cliCopy("viewer", "sessions.usage", "usage: clawdline cloud viewer <sessions|read|send|answer|interrupt|end|receipt> [--machine ID] [--session ID --generation HEX] [--before CURSOR for read]"))
	return 2
}

type viewerSessionGroup struct {
	Machine    string                 `json:"machine"`
	Projection cloud.ViewerProjection `json:"projection"`
	Error      string                 `json:"error,omitempty"`
}

func viewerListSessions(out, errs io.Writer, machine string, asJSON bool) int {
	var machines []cloud.ViewerMachineState
	if err := viewerDaemonRequest(http.MethodGet, "/v1/cloud/viewer/machines", nil, &machines); err != nil {
		fmt.Fprintln(errs, viewerCLIRefusal(err))
		return 1
	}
	groups := make([]viewerSessionGroup, 0, len(machines))
	for _, item := range machines {
		if machine != "" && machine != item.ID {
			continue
		}
		group := viewerSessionGroup{Machine: item.ID}
		if item.Pairing != "paired" {
			group.Projection = cloud.ViewerProjection{Kind: "unavailable", Reason: item.Pairing}
		} else {
			err := viewerDaemonRequest(http.MethodGet, viewerQuery("/v1/cloud/viewer/sessions", cloud.ViewerDestination{MachineID: item.ID}), nil, &group.Projection)
			if err != nil {
				group.Projection = cloud.ViewerProjection{Kind: "unavailable", Reason: "unknown"}
				group.Error = viewerCLIRefusal(err)
			}
		}
		groups = append(groups, group)
	}
	if machine != "" && len(groups) == 0 {
		fmt.Fprintln(errs, cliCopy("viewer", "error.machine", "viewer_machine_not_found: no such Cloud machine is visible"))
		return 1
	}
	if asJSON {
		viewerPrintJSON(out, groups)
		return 0
	}
	for _, group := range groups {
		if group.Projection.Kind != "ready" {
			fmt.Fprintf(out, "%s\t%s\t%s\n", group.Machine, group.Projection.Kind, group.Projection.Reason)
			if group.Error != "" {
				fmt.Fprintln(errs, group.Error)
			}
			continue
		}
		for _, row := range group.Projection.Rows {
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", group.Machine, row.Destination.SessionID,
				row.Destination.ExecutionGeneration, row.State, row.Freshness)
		}
		if len(group.Projection.Rows) == 0 {
			fmt.Fprintf(out, "%s\t%s\n", group.Machine, cliCopy("viewer", "sessions.empty", "no Sessions"))
		}
	}
	return 0
}
