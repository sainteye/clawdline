package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

const squadCapabilityEnv = "CLAWDLINE_SQUAD_CAPABILITY_FILE"

func readSquadCapability(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%s is not set for this Session", squadCapabilityEnv)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("session capability is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, brokerTokenLimit+1))
	if err != nil {
		return "", err
	}
	capability := strings.TrimSpace(string(data))
	if len(data) > brokerTokenLimit || capability == "" || strings.ContainsAny(capability, " \t\r\n") {
		return "", fmt.Errorf("session capability file is invalid")
	}
	return capability, nil
}

func squadCommand(args []string) {
	if len(args) == 0 || args[0] != "skill-event" {
		fmt.Fprintln(os.Stderr, "usage: clawdline squad skill-event --skill ID --version VERSION --status read|applied|failed [--event-id ID] [--failure-code CODE]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("squad skill-event", flag.ExitOnError)
	skillID := fs.String("skill", "", "snapshot skill ID")
	version := fs.String("version", "", "snapshot skill version")
	status := fs.String("status", "", "read, applied, or failed")
	eventID := fs.String("event-id", "", "stable ID for a retry")
	failure := fs.String("failure-code", "", "reason for failed use")
	_ = fs.Parse(args[1:])
	if fs.NArg() != 0 || *skillID == "" || *version == "" ||
		(*status != "read" && *status != "applied" && *status != "failed") {
		fmt.Fprintln(os.Stderr, "clawdline: invalid squad skill event arguments")
		os.Exit(2)
	}
	if *eventID == "" {
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			fail(err)
		}
		*eventID = base64.RawURLEncoding.EncodeToString(bytes)
	}
	capability, err := readSquadCapability(os.Getenv(squadCapabilityEnv))
	if err != nil {
		fail(err)
	}
	event := store.SquadSkillEvent{SkillID: *skillID, SkillVersion: *version,
		ClientEventID: *eventID, Status: *status, FailureCode: *failure}
	req, err := daemonRequest(http.MethodPost, "/v1/squad/session-events", event)
	if err != nil {
		fail(err)
	}
	req.Header.Set("X-Clawdline-Session-Capability", capability)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		fail(err)
	}
	defer res.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		fail(err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		fmt.Fprintln(os.Stderr, "clawdline:", res.StatusCode, string(answer))
		os.Exit(1)
	}
	fmt.Print(string(answer))
}
