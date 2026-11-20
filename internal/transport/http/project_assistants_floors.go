package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The Bots tower's floor placements are this machine's own view preference.
// Like the Project catalog, the route stands outside /v1/project-assistants/
// so that every Bot ID stays available, and it answers only for this machine.
func (s *Server) projectAssistantFloorsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var (
		answer contract.ProjectAssistantTowerFloorsResponse
		err    error
	)
	switch {
	case r.URL.Path != "/v1/project-assistant-floors":
		err = paHTTPFailure(pa.Invalid, "route")
	case r.Method == http.MethodGet:
		answer, err = s.projectAssistantFloorsRead(r)
	case r.Method == http.MethodPost:
		answer, err = s.projectAssistantFloorsWrite(w, r)
	default:
		err = paHTTPFailure(pa.Invalid, "method")
	}
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	writeJSON(w, answer)
}

func paWireTowerFloors(machine string, floors pa.TowerFloors) contract.ProjectAssistantTowerFloorsResponse {
	out := contract.ProjectAssistantTowerFloorsResponse{ContractVersion: pa.ContractVersion, MachineID: machine, Version: floors.Version, Floors: make([]contract.ProjectAssistantTowerFloor, 0, len(floors.Floors))}
	for _, f := range floors.Floors {
		out.Floors = append(out.Floors, contract.ProjectAssistantTowerFloor{ProjectID: f.ProjectID, Floor: int64(f.Floor)})
	}
	return out
}

// sameMachineAuthority holds a reply to the authority it was read under: a
// grant renewed, narrowed or revoked meanwhile is not published.
func sameMachineAuthority(before, now app.Authority) error {
	if before.PrincipalID != now.PrincipalID || before.ReaderID != now.ReaderID {
		return paHTTPFailure(pa.ScopeDenied, "effect_principal")
	}
	if before.AuthorizationRevision != now.AuthorizationRevision || before.Source.Writable != now.Source.Writable || before.Source.Access != now.Source.Access {
		return paHTTPFailure(pa.StaleVersion, "effect_authorization")
	}
	return nil
}

func (s *Server) projectAssistantFloorsStore() *projectassistantstore.Store {
	return projectassistantstore.New(s.store.ProjectAssistantDatabase())
}

func (s *Server) projectAssistantFloorsRead(r *http.Request) (contract.ProjectAssistantTowerFloorsResponse, error) {
	query, err := paHTTPQuery(r, "machine_id")
	if err != nil {
		return contract.ProjectAssistantTowerFloorsResponse{}, err
	}
	machine := query.Get("machine_id")
	before, err := s.projectAssistantMachineAuthority(r.Context(), machine, false)
	if err != nil {
		return contract.ProjectAssistantTowerFloorsResponse{}, err
	}
	floors, err := s.projectAssistantFloorsStore().TowerFloors(r.Context(), machine)
	if err != nil {
		return contract.ProjectAssistantTowerFloorsResponse{}, err
	}
	now, err := s.projectAssistantMachineAuthority(r.Context(), machine, false)
	if err == nil {
		err = sameMachineAuthority(before, now)
	}
	return paWireTowerFloors(machine, floors), err
}

func (s *Server) projectAssistantFloorsWrite(w http.ResponseWriter, r *http.Request) (contract.ProjectAssistantTowerFloorsResponse, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return contract.ProjectAssistantTowerFloorsResponse{}, err
	}
	body, err := paDecodeHTTP[contract.ProjectAssistantTowerFloorRequest](w, r)
	if err != nil {
		return contract.ProjectAssistantTowerFloorsResponse{}, err
	}
	if body.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != body.IdempotencyKey {
		return contract.ProjectAssistantTowerFloorsResponse{}, paHTTPFailure(pa.Invalid, "idempotency_key")
	}
	if body.Floor < 0 || body.Floor >= pa.MaxTowerFloors {
		return contract.ProjectAssistantTowerFloorsResponse{}, paHTTPFailure(pa.Invalid, "floor")
	}
	before, err := s.projectAssistantMachineAuthority(r.Context(), body.MachineID, true)
	if err != nil {
		return contract.ProjectAssistantTowerFloorsResponse{}, err
	}
	floors, err := s.projectAssistantFloorsStore().SaveTowerFloor(r.Context(), pa.TowerFloorWrite{MachineID: body.MachineID, ExpectedVersion: body.ExpectedVersion,
		Floor: pa.TowerFloor{ProjectID: body.ProjectID, Floor: int(body.Floor)}})
	if err != nil {
		var f *pa.Failure
		if !errors.As(err, &f) {
			// The transaction may or may not have committed.
			err = errors.Join(paHTTPFailure(pa.OutcomeUnknown, "tower_floors"), err)
		}
		return contract.ProjectAssistantTowerFloorsResponse{}, err
	}
	// The placement is stored; only the reply depends on the authority still
	// being the same, and a changed one cannot tell the writer what happened.
	now, err := s.projectAssistantMachineAuthority(context.WithoutCancel(r.Context()), body.MachineID, true)
	if err == nil {
		err = sameMachineAuthority(before, now)
	}
	if err != nil {
		return contract.ProjectAssistantTowerFloorsResponse{}, errors.Join(paHTTPFailure(pa.OutcomeUnknown, "tower_floors_publication"), err)
	}
	return paWireTowerFloors(body.MachineID, floors), nil
}
