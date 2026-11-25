package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// machineSharedSettingsReply is the shared-settings document under the Bots
// envelope, so the Cloud relay can carry it like every other Bots answer.
type machineSharedSettingsReply struct {
	ContractVersion string `json:"contract_version"`
	MachineID       string `json:"machine_id"`
	sharedSettingsReply
}

// projectAssistantMachineSettingsRoute serves one machine's dispatch rules
// and shared Bot memory to any paired console, including Cloud and phones.
// Unlike /v1/project-assistants/shared-settings it names the machine and
// checks that machine's Bots authority, as the tower floors do.
func (s *Server) projectAssistantMachineSettingsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var (
		answer machineSharedSettingsReply
		err    error
	)
	switch {
	case r.URL.Path != "/v1/project-assistant-shared-settings":
		err = paHTTPFailure(pa.Invalid, "route")
	case r.Method == http.MethodGet:
		answer, err = s.projectAssistantMachineSettingsRead(r)
	case r.Method == http.MethodPut:
		answer, err = s.projectAssistantMachineSettingsWrite(r)
	default:
		err = paHTTPFailure(pa.Invalid, "method")
	}
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	writeJSON(w, answer)
}

func (s *Server) machineSharedSettingsAnswer(machine string, settings pa.SharedSettings) machineSharedSettingsReply {
	agents := s.projectAssistantDispatchAgents(time.Now())
	if agents == nil {
		agents = []pa.DispatchAgentOption{}
	}
	return machineSharedSettingsReply{pa.ContractVersion, machine, sharedSettingsReply{settings, agents, sharedSettingsLimits()}}
}

func (s *Server) projectAssistantMachineSettingsRead(r *http.Request) (machineSharedSettingsReply, error) {
	query, err := paHTTPQuery(r, "machine_id")
	if err != nil {
		return machineSharedSettingsReply{}, err
	}
	machine := query.Get("machine_id")
	before, err := s.projectAssistantMachineAuthority(r.Context(), machine, false)
	if err != nil {
		return machineSharedSettingsReply{}, err
	}
	current, err := s.projectAssistantSharedSettings().Load()
	if err != nil {
		return machineSharedSettingsReply{}, err
	}
	now, err := s.projectAssistantMachineAuthority(r.Context(), machine, false)
	if err == nil {
		err = sameMachineAuthority(before, now)
	}
	return s.machineSharedSettingsAnswer(machine, current), err
}

type machineSharedSettingsWrite struct {
	MachineID       string            `json:"machine_id"`
	IdempotencyKey  string            `json:"idempotency_key"`
	ExpectedVersion *int64            `json:"expected_version"`
	Settings        pa.SharedSettings `json:"settings"`
}

func (s *Server) projectAssistantMachineSettingsWrite(r *http.Request) (machineSharedSettingsReply, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return machineSharedSettingsReply{}, err
	}
	var body machineSharedSettingsWrite
	dec := json.NewDecoder(io.LimitReader(r.Body, pa.MaxSharedSettingsBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.ExpectedVersion == nil {
		return machineSharedSettingsReply{}, paHTTPFailure(pa.Invalid, "body")
	}
	if body.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != body.IdempotencyKey {
		return machineSharedSettingsReply{}, paHTTPFailure(pa.Invalid, "idempotency_key")
	}
	before, err := s.projectAssistantMachineAuthority(r.Context(), body.MachineID, true)
	if err != nil {
		return machineSharedSettingsReply{}, err
	}
	saved, err := s.projectAssistantSharedSettings().Save(*body.ExpectedVersion, body.Settings)
	if err != nil {
		var f *pa.Failure
		if !errors.As(err, &f) {
			// The file may or may not have been replaced.
			err = errors.Join(paHTTPFailure(pa.OutcomeUnknown, "shared_settings"), err)
		}
		return machineSharedSettingsReply{}, err
	}
	s.projectAssistantSharedSettingsChanged(r.Context())
	now, err := s.projectAssistantMachineAuthority(context.WithoutCancel(r.Context()), body.MachineID, true)
	if err == nil {
		err = sameMachineAuthority(before, now)
	}
	if err != nil {
		return machineSharedSettingsReply{}, errors.Join(paHTTPFailure(pa.OutcomeUnknown, "shared_settings_publication"), err)
	}
	return s.machineSharedSettingsAnswer(body.MachineID, saved), nil
}
