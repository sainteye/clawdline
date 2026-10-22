package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"time"
)

// ObserveHook reads stdin once and connects once, with no retry. Only an
// approved native JSON object reaches stdout; errors contain stable codes only.
// A general io.Reader/Writer must honor its owner's cancellation. The CLI
// process is additionally bounded by the installed native hook timeout.
func ObserveHook(ctx context.Context, stateDir, event string, launch *HookLaunch, stdin io.Reader, stdout io.Writer) error {
	if err := hookPlatform(); err != nil {
		return err
	}
	if !eventAllowed(event) {
		return refuse("antigravity_event_unsupported")
	}
	if launch != nil && (len(launch.ID) > MaxHookPayloadBytes || len(launch.Capability) > MaxHookPayloadBytes) {
		return refuse("antigravity_launch_invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, MaxHookRequestTime)
	defer cancel()
	if stdin == nil || stdout == nil {
		return refuse("antigravity_hook_configuration_invalid")
	}
	// Native hooks supply a pipe on stdin. Make a stalled producer obey the
	// same deadline without abandoning a goroutine blocked in Read. Regular
	// files and in-memory readers remain supported; custom readers must honor
	// their owner's cancellation as stated by this API's contract.
	if f, ok := stdin.(*os.File); ok {
		deadline, _ := ctx.Deadline()
		if err := f.SetReadDeadline(deadline); err == nil {
			interrupted := make(chan struct{})
			stop := context.AfterFunc(ctx, func() { defer close(interrupted); _ = f.SetReadDeadline(time.Now()) })
			defer func() {
				if !stop() {
					<-interrupted
				}
				_ = f.SetReadDeadline(time.Time{})
			}()
		} else if info, e := f.Stat(); e != nil || !info.Mode().IsRegular() {
			return refuse("antigravity_hook_input_deadline_unsupported")
		}
	}
	data, err := io.ReadAll(io.LimitReader(stdin, MaxHookPayloadBytes+1))
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, os.ErrDeadlineExceeded) {
			return refuse("antigravity_hook_timeout")
		}
		return refuse("antigravity_payload_unreadable")
	}
	if len(data) > MaxHookPayloadBytes {
		return refuse("antigravity_payload_too_large")
	}
	if ctx.Err() != nil {
		return refuse("antigravity_hook_timeout")
	}
	if err = validateHookSocket(stateDir); err != nil {
		return err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", HookSocketPath(stateDir))
	if err != nil {
		return refuse("antigravity_hook_connect_failed")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err = writeHookFrame(conn, hookRequest{Version: 1, Event: event, Payload: json.RawMessage(data), Launch: launch}); err != nil {
		return err
	}
	frame, err := readHookFrame(conn)
	if err != nil {
		if ctx.Err() != nil {
			return refuse("antigravity_hook_timeout")
		}
		return err
	}
	var reply hookReply
	if decodeHookEnvelope(frame, &reply) != nil || reply.Version != 1 {
		return refuse("antigravity_hook_response_invalid")
	}
	if reply.Code != "" {
		// Never echo arbitrary bytes supplied by a fake server as an error.
		if len(reply.Response) != 0 {
			return refuse("antigravity_hook_response_invalid")
		}
		return hookRemoteError(reply.Code)
	}
	if len(reply.Response) > MaxHookPayloadBytes {
		return refuse("antigravity_hook_response_invalid")
	}
	if _, err = rawObject(reply.Response); err != nil {
		return refuse("antigravity_hook_response_invalid")
	}
	if ctx.Err() != nil || time.Now().After(deadline) {
		return refuse("antigravity_hook_timeout")
	}
	output := append(reply.Response, '\n')
	if n, err := stdout.Write(output); err != nil || n != len(output) {
		return refuse("antigravity_hook_output_failed")
	}
	return nil
}

// Only known protocol codes cross from the server into CLI errors. A fake
// local server cannot smuggle paths, capabilities or payload text into stderr.
func hookRemoteError(code string) error {
	switch code {
	case "antigravity_hook_busy", "antigravity_hook_admission_failed", "antigravity_hook_peer_unverified", "antigravity_hook_request_invalid", "antigravity_hook_timeout", "antigravity_hook_sequence_exhausted", "antigravity_hook_response_failed", "antigravity_hook_response_invalid", "antigravity_hook_frame_invalid", "antigravity_hook_frame_too_large", "antigravity_hook_disconnected", "antigravity_hook_failed",
		"antigravity_event_unsupported", "antigravity_payload_unreadable", "antigravity_payload_too_large", "antigravity_payload_invalid", "antigravity_model_invalid", "antigravity_bridge_busy", "antigravity_observation_stale", "antigravity_observation_replayed", "antigravity_observation_cancelled", "antigravity_helper_unverified", "antigravity_helper_changed", "antigravity_process_unreadable", "antigravity_ancestry_unverified", "antigravity_ancestry_ambiguous", "antigravity_terminal_mismatch", "antigravity_launch_unverified", "antigravity_launch_invalid", "antigravity_capability_invalid", "antigravity_transcript_unverified", "antigravity_bindings_full", "antigravity_store_full", "antigravity_store_write_failed":
		return refuse(code)
	}
	return refuse("antigravity_hook_rejected")
}
