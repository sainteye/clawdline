package clauderemote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

type credentials struct{ token, organization string }

// Credentials are read only when the person reads or sends to a native worker.
// Never refresh, persist, log, or expose the CLI's OAuth secrets to the console.
func localCredentials(ctx context.Context, home string) (credentials, error) {
	root := transcript.ClaudeConfigDir(home)
	custom := filepath.IsAbs(os.Getenv("CLAUDE_CONFIG_DIR"))
	raw, err := readSmall(filepath.Join(root, ".credentials.json"))
	if errors.Is(err, os.ErrNotExist) && !custom && root == filepath.Join(home, ".claude") {
		raw, err = keychainCredentials(ctx)
	}
	if err != nil {
		return credentials{}, errors.New("Claude sign-in could not be read; sign in with Claude Code on this machine")
	}
	var auth struct {
		OAuth struct {
			AccessToken string   `json:"accessToken"`
			ExpiresAt   int64    `json:"expiresAt"`
			Scopes      []string `json:"scopes"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(raw, &auth) != nil || auth.OAuth.AccessToken == "" || auth.OAuth.ExpiresAt <= time.Now().UnixMilli() {
		return credentials{}, errors.New("Claude sign-in is missing or expired; sign in with Claude Code on this machine")
	}
	scope := false
	for _, s := range auth.OAuth.Scopes {
		if s == "user:sessions:claude_code" {
			scope = true
		}
	}
	if !scope {
		return credentials{}, errors.New("Claude sign-in does not grant Remote Control access")
	}
	configPath := filepath.Join(home, ".claude.json")
	if custom {
		configPath = filepath.Join(root, ".claude.json")
	}
	raw, err = readSmall(configPath)
	if err != nil {
		return credentials{}, errors.New("Claude account organization could not be read")
	}
	var config struct {
		Account struct {
			Organization string `json:"organizationUuid"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(raw, &config) != nil || config.Account.Organization == "" {
		return credentials{}, errors.New("Claude account organization is missing")
	}
	return credentials{auth.OAuth.AccessToken, config.Account.Organization}, nil
}
func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, ResponseBytesLimit+1))
	if err != nil || len(raw) > ResponseBytesLimit {
		return nil, errors.New("Claude credential data is unreadable")
	}
	return raw, nil
}
