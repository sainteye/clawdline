package http

import (
	"encoding/json"
	"log"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/browserbridge"
)

func (s *Server) browsers() *browserbridge.Profiles {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	return s.browsersLocked()
}

func (s *Server) browsersLocked() *browserbridge.Profiles {
	if s.browserProfiles == nil {
		profiles, err := browserbridge.OpenProfiles(s.cfg.Dir, s.announceBrowserSite, s.browserBridge)
		if err != nil {
			log.Printf("browser: profile state could not be opened: %v", err)
		}
		s.browserProfiles = profiles
		s.browserBridge = profiles.Legacy()
	}
	return s.browserProfiles
}

func (s *Server) browserPolicy() (nextconfig.BrowserPolicy, error) {
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		return nextconfig.BrowserPolicy{}, err
	}
	return values.BrowserPolicy()
}

func (s *Server) forgetClosedBrowser(conversation string) {
	if conversation == "" {
		return
	}
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	// Closing an unrelated Session must not initialize any browser state.
	if s.browserProfiles == nil {
		return
	}
	if err := s.browserProfiles.ForgetClosedSession(conversation); err != nil {
		log.Printf("browser: closed Session choice retained: %v", err)
	}
}

// A settings write changes automatic selection only. Existing Session choices,
// person control and other Chrome channels are not switched or discarded.
func (s *Server) setBrowserPolicySettings(f *nextconfig.File, changes map[string]any) (nextconfig.Values, error) {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	current, err := f.Read()
	if err != nil {
		return current, err
	}
	before, err := current.BrowserPolicy()
	if err != nil {
		return current, err
	}
	if value, ok := changes["browser_policy"]; ok {
		raw, err := json.Marshal(value)
		if err != nil {
			return current, nextconfig.ErrBrowserPolicy
		}
		policy, err := nextconfig.DecodeBrowserPolicy(raw)
		if err != nil {
			return current, err
		}
		profiles := s.browsersLocked()
		for _, mode := range []string{nextconfig.BrowserModeIsolated, nextconfig.BrowserModeUserChrome} {
			if policy.Automatic[mode] && !before.Automatic[mode] && !profiles.Ready(mode) {
				return current, browserbridge.SetupRequired
			}
		}
	} else if value, modeChanged := changes["browser_mode"]; modeChanged || changes["browser_enabled"] != nil {
		if _, exists := current.Raw["browser_policy"]; exists {
			return current, nextconfig.ErrBrowserPolicy
		}
		mode := current.BrowserMode()
		if modeChanged {
			var ok bool
			mode, ok = value.(string)
			if !ok {
				return current, nextconfig.ErrBrowserMode
			}
		} else if enabled, ok := changes["browser_enabled"].(bool); ok {
			if !enabled {
				mode = nextconfig.BrowserModeNative
			} else if mode == nextconfig.BrowserModeNative {
				mode = nextconfig.BrowserModeIsolated
			}
		} else {
			return current, nextconfig.ErrBrowserMode
		}
		if mode != nextconfig.BrowserModeNative && !before.Automatic[mode] && !s.browsersLocked().Ready(mode) {
			return current, browserbridge.SetupRequired
		}
	}
	return f.Set(changes)
}
