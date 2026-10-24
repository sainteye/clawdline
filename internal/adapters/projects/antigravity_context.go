package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/squad"
)

var ErrSquadContextUnavailable = errors.New("squad_context_unavailable")

// AntigravityContext is prepared by the daemon from its original immutable
// Squad snapshot and Project memory, never from hook JSON or browser input.
// Store this exact document with the launch; resume reloads it, not the catalog.
type AntigravityContext struct {
	PromptPath string `json:"prompt_path,omitempty"`
	PromptHash string `json:"prompt_hash,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	Memory     string `json:"memory,omitempty"`
	MemoryHash string `json:"memory_hash"`
	Language   string `json:"language,omitempty"`
}

func contextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// PrepareAntigravityContext takes only already selected private snapshot bytes.
// It performs no filesystem reads and never replaces the provider base prompt.
func PrepareAntigravityContext(promptPath, prompt, memory, language string) (AntigravityContext, error) {
	c := AntigravityContext{PromptPath: promptPath, Prompt: prompt, Memory: memory, Language: language,
		MemoryHash: contextHash(memory)}
	if prompt != "" {
		c.PromptHash = contextHash(prompt)
	}
	if err := c.Validate(); err != nil {
		return AntigravityContext{}, err
	}
	return c, nil
}

func (c AntigravityContext) Validate() error {
	if len(c.Prompt) > squad.MaxSquadRequestBytes || len(c.Memory) > MemoryLaunchLimit ||
		c.MemoryHash != contextHash(c.Memory) ||
		(c.Language != "" && CodexBoardLanguage(c.Language) != c.Language) {
		return ErrSquadContextUnavailable
	}
	if c.Prompt == "" {
		if c.PromptPath != "" || c.PromptHash != "" {
			return ErrSquadContextUnavailable
		}
	} else if !usable(c.PromptPath) || c.PromptHash != contextHash(c.Prompt) {
		return ErrSquadContextUnavailable
	}
	return nil
}

// Document returns the bytes whose digest must be pinned in the launch record.
func (c AntigravityContext) Document() ([]byte, string, error) {
	if err := c.Validate(); err != nil {
		return nil, "", err
	}
	b, err := json.Marshal(c)
	if len(b) > squad.MaxSquadRequestBytes {
		return nil, "", ErrSquadContextUnavailable
	}
	return b, contextHash(string(b)), err
}

// RestoreAntigravityContext verifies the launch-record digest before restoring
// original resume bytes. Root's filesystem reader must use the existing Squad
// request byte budget as its read ceiling and refuse symlink/foreign paths.
func RestoreAntigravityContext(document []byte, digest string) (AntigravityContext, error) {
	var c AntigravityContext
	if digest == "" || len(document) > squad.MaxSquadRequestBytes || contextHash(string(document)) != digest ||
		json.Unmarshal(document, &c) != nil || c.Validate() != nil {
		return AntigravityContext{}, ErrSquadContextUnavailable
	}
	return c, nil
}

// Instructions joins the immutable role, language and shared Project memory.
func (c AntigravityContext) Instructions() string {
	var parts []string
	if c.Prompt != "" {
		parts = append(parts, c.Prompt)
	}
	if c.Language != "" {
		parts = append(parts, fmt.Sprintf("Write every Clawdline Board item title, description, and step you author in the language identified by BCP 47 tag %q.", c.Language))
	}
	if c.Memory != "" {
		parts = append(parts, c.Memory)
	}
	if len(parts) == 0 {
		return ""
	}
	parts = append(parts, codexNoteInstruction)
	return strings.Join(parts, "\n\n")
}

// AntigravityContextResponse composes with P2 ObserverResponse after local
// process/capability verification. Only PreInvocation injects, which also runs
// on exact resume. The root must persist a per-launch/process/conversation
// delivery receipt and suppress duplicate event execution before calling this.
// A response is prepared delivery, not proof the model loaded the context.
func AntigravityContextResponse(c AntigravityContext, event string, observer json.RawMessage) (json.RawMessage, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(observer, &response); err != nil || response == nil {
		return nil, ErrSquadContextUnavailable
	}
	if _, exists := response["injectSteps"]; exists {
		return nil, ErrSquadContextUnavailable
	}
	if event != "PreInvocation" || c.Instructions() == "" {
		return observer, nil
	}
	step := map[string]any{"systemMessage": map[string]string{"systemMessage": c.Instructions()}}
	encoded, err := json.Marshal([]any{step})
	if err != nil {
		return nil, err
	}
	response["injectSteps"] = encoded
	return json.Marshal(response)
}
