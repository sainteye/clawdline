package http

import (
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const botLocalMachineKey = "project_assistant_local_machine"

var errBotSourceNotRegistered = errors.New("no Bot source identity exists")

func botLocalIdentity(v nextconfig.Values) (adaptercloud.Identity, bool, error) {
	if _, exists := v.Raw[botLocalMachineKey]; !exists {
		return adaptercloud.Identity{}, false, nil
	}
	id, _ := v.String(botLocalMachineKey)
	value := strings.TrimPrefix(id, "bot-local-")
	if _, err := hex.DecodeString(value); value == id || len(value) != 32 || err != nil || strings.ToLower(value) != value {
		return adaptercloud.Identity{}, false, paHTTPFailure(pa.Unpaired, "local_source_identity")
	}
	return adaptercloud.Identity{MachineID: id}, true, nil
}

func (s *Server) projectAssistantIdentity() (adaptercloud.Identity, error) {
	if s.executionMachineID != "" {
		// This seam is supplied only by a fixture. Retain a seeded Cloud
		// identity's precedence when the fixture exercises that path.
		i, found, err := adaptercloud.NewIdentityStore(filepath.Join(s.cfg.Dir, cloudkeys.DirName)).Load()
		if found || err != nil {
			return i, err
		}
		return adaptercloud.Identity{MachineID: s.executionMachineID}, nil
	}
	settings, err := s.settingsFile().Read()
	if err != nil {
		return adaptercloud.Identity{}, err
	}
	// The ID keys saved chats and workspaces. Future Cloud sign-in must not
	// displace them or let a new account inherit their local authority.
	if i, found, err := botLocalIdentity(settings); found || err != nil {
		return i, err
	}
	i, found, err := adaptercloud.NewIdentityStore(filepath.Join(s.cfg.Dir, cloudkeys.DirName)).Load()
	if err != nil {
		return i, errors.Join(paHTTPFailure(pa.Unpaired, "source_identity"), err)
	}
	if !found {
		return i, errors.Join(paHTTPFailure(pa.Unpaired, "source_identity"), errBotSourceNotRegistered)
	}
	return i, nil
}

// Startup and owner enable call this under the Bot lifecycle lock. Config
// writes share the settings coordinator, so concurrent setting changes survive.
func (s *Server) createLocalBotSource() (adaptercloud.Identity, error) {
	i := adaptercloud.Identity{MachineID: "bot-local-" + projectAssistantID()}
	if _, err := s.settingsFile().Set(map[string]any{botLocalMachineKey: i.MachineID}); err != nil {
		return adaptercloud.Identity{}, errors.Join(paHTTPFailure(pa.OutcomeUnknown, "local_source_identity"), err)
	}
	return i, nil
}
