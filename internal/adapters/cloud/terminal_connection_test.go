package cloud

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

func TestTerminalConnectionAckBindsEveryIdentityField(t *testing.T) {
	request := terminalControlFrame{Action: "register", RequestID: strings.Join([]string{"11111111", "1111", "4111", "8111", "111111111111"}, "-"), Machine: "machine", Viewer: "viewer", Connection: "AAAAAAAAAAAAAAAAAAAAAA"}
	answer := make(chan terminalControlResult, 1)
	transport := &Transport{terminalExpected: request, terminalPending: answer}
	base := terminalControlResult{Type: FrameTerminalConnectionRegistered, Action: request.Action,
		RequestID: request.RequestID, Machine: request.Machine, Viewer: request.Viewer, Connection: request.Connection}
	for _, mutate := range []func(*terminalControlResult){
		func(r *terminalControlResult) { r.RequestID = "other" },
		func(r *terminalControlResult) { r.Action = "retire" },
		func(r *terminalControlResult) { r.Machine = "other" },
		func(r *terminalControlResult) { r.Viewer = "other" },
		func(r *terminalControlResult) { r.Connection = "other" },
		func(r *terminalControlResult) { r.Type = FrameTerminalConnectionRetired },
	} {
		got := base
		mutate(&got)
		data, _ := json.Marshal(got)
		transport.handleTerminalConnection(data)
		select {
		case <-answer:
			t.Fatal("accepted uncorrelated metadata answer")
		default:
		}
	}
	data, _ := json.Marshal(base)
	transport.handleTerminalConnection(data)
	select {
	case <-answer:
	default:
		t.Fatal("matching metadata answer was lost")
	}
}

func TestTerminalDepartureAcceptsOnlyThisMachinesRelayMetadata(t *testing.T) {
	var got []string
	transport := &Transport{opts: Options{Identity: Identity{MachineID: "machine"},
		TerminalConnectionGone: func(viewer, connection string) { got = append(got, viewer+"/"+connection) }}}
	for _, input := range []string{
		`{"type":"terminal_connection_gone","machine":"other","viewer":"viewer","connection":"c"}`,
		`{"type":"terminal_connection_gone","machine":"machine","connection":"c"}`,
		`{"type":"terminal_connection_gone","machine":"machine","viewer":"viewer","connection":"c"}`,
	} {
		if err := transport.handleFrame(nil, []byte(input), connectionhealth.Handle{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 1 || got[0] != "viewer/c" {
		t.Fatalf("departures: %v", got)
	}
}
