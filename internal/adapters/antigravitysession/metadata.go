package antigravitysession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// beginMetadata shares the sender's single in-flight operation, without
// manufacturing a working model turn. Shutdown owns cancellation and joins jobs.
func (h *Host) beginMetadata(c *conversation) (*antigravityprocess.Client, func(), error) {
	client, _, done, err := h.beginMetadataOp(h.ctx, c, true, false)
	return client, done, err
}

func (h *Host) beginMetadataOp(ctx context.Context, c *conversation, requireClient, waitOnStop bool) (*antigravityprocess.Client, context.Context, func(), error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, nil, nil, refusal("cancelled")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil {
		return nil, nil, nil, refusal("closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hidden || (requireClient && c.client == nil) {
		return nil, nil, nil, refusal("closed")
	}
	if c.busy || c.stopping || c.row.State != session.StateIdle {
		return nil, nil, nil, refusal("busy")
	}
	opCtx, opCancel := context.WithCancel(ctx)
	var opDone chan struct{}
	if waitOnStop {
		opDone = make(chan struct{})
	}
	c.busy = true
	c.opCancel = opCancel
	c.opDone = opDone
	h.jobs.Add(1)
	return c.client, opCtx, func() {
		opCancel()
		c.mu.Lock()
		c.busy = false
		c.opCancel = nil
		c.opDone = nil
		c.mu.Unlock()
		if opDone != nil {
			close(opDone)
		}
		h.jobs.Done()
	}, nil
}

func (h *Host) acquireConfigOp(ctx context.Context, c *conversation) (*antigravityprocess.Client, context.Context, func(), error) {
	for {
		client, opCtx, done, err := h.beginMetadataOp(ctx, c, true, true)
		if err == nil {
			return client, opCtx, done, nil
		}
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "busy" {
			return nil, nil, nil, err
		}
		c.mu.Lock()
		idle := c.row.State == session.StateIdle && !c.stopping
		opDone := c.opDone
		c.mu.Unlock()
		if !idle || opDone == nil {
			return nil, nil, nil, err
		}
		select {
		case <-ctx.Done():
			return nil, nil, nil, refusal("cancelled")
		case <-h.ctx.Done():
			return nil, nil, nil, refusal("closed")
		case <-opDone:
		}
	}
}

func (h *Host) selectModel(ctx context.Context, c *conversation, modelID string, before func() error) error {
	c.mu.Lock()
	structured := c.structuredConfig
	models := c.models
	efforts := c.efforts
	c.mu.Unlock()
	targetModel, targetEffort, ok := resolveModelChoice(modelID, models, efforts)
	if !ok {
		return refusal("model_not_offered")
	}
	if structured {
		if err := h.selectConfig(ctx, c, "model", targetModel, before); err != nil {
			return err
		}
		if targetEffort != "" {
			c.mu.Lock()
			effortConfigID := c.effortConfigID()
			c.mu.Unlock()
			if effortConfigID != "" {
				if err := h.selectConfig(ctx, c, effortConfigID, targetEffort, nil); err != nil {
					return err
				}
			}
		}
		return nil
	}
	client, opCtx, done, err := h.acquireConfigOp(ctx, c)
	if err != nil {
		return err
	}
	defer done()
	if err := client.SetModelChecked(opCtx, targetModel, before); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = targetModel // Ack selects the next turn; it does not execute a model.
	return c.rememberChoice("model", targetModel)
}

func (h *Host) selectConfig(ctx context.Context, c *conversation, id, value string, before func() error) error {
	client, opCtx, done, err := h.acquireConfigOp(ctx, c)
	if err != nil {
		return err
	}
	defer done()
	validate := func() error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.structuredConfig {
			if id == "model" {
				for _, m := range c.models {
					if m.ID == value {
						return nil
					}
				}
			} else {
				for _, effort := range c.efforts {
					if effort == value {
						return nil
					}
				}
			}
		}
		if id == "model" {
			return refusal("model_not_offered")
		}
		return refusal("effort_not_offered")
	}
	if err := validate(); err != nil {
		return err
	}
	return client.SetConfigOptionChecked(opCtx, id, value, func() error {
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		return validate()
	}, func(raw json.RawMessage) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := c.readMetadata(raw, false); err != nil {
			return err
		}
		return c.rememberChoice(id, value)
	})
}

// readMetadata is called under c.mu. Structured options are authoritative and
// replace both controls atomically; legacy composite IDs are never split.
func (c *conversation) readMetadata(raw json.RawMessage, allowLegacy bool) error {
	var result struct {
		ConfigOptions json.RawMessage `json:"configOptions"`
		Models        struct {
			CurrentModelID  string `json:"currentModelId"`
			AvailableModels []struct {
				ModelID string `json:"modelId"`
				Name    string `json:"name"`
			} `json:"availableModels"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return refusal("invalid_config")
	}
	validValue := func(value string) bool { return value != "" }
	if result.ConfigOptions == nil {
		if !allowLegacy {
			return refusal("invalid_config")
		}
		c.model = result.Models.CurrentModelID
		for _, m := range result.Models.AvailableModels {
			if validValue(m.ModelID) {
				c.models = append(c.models, contract.SessionModel{ID: m.ModelID, Name: m.Name, Command: m.ModelID})
			}
		}
		return nil
	}
	var options []json.RawMessage
	if string(result.ConfigOptions) == "null" || json.Unmarshal(result.ConfigOptions, &options) != nil {
		return refusal("invalid_config")
	}
	var model, effort, effortID string
	models := []contract.SessionModel{}
	var efforts []string
	seen := map[string]bool{}
	for _, rawOption := range options {
		var key struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(rawOption, &key) != nil {
			return refusal("invalid_config")
		}
		if key.ID != "model" && !isEffortConfig(key.ID) {
			continue
		}
		category := key.ID
		if isEffortConfig(key.ID) {
			category, effortID = "effort", key.ID
		}
		if seen[category] {
			return refusal("invalid_config")
		}
		seen[category] = true
		var option struct {
			Type         string `json:"type"`
			CurrentValue string `json:"currentValue"`
			Options      []struct {
				Value string `json:"value"`
				Name  string `json:"name"`
			} `json:"options"`
		}
		if json.Unmarshal(rawOption, &option) != nil || option.Type != "select" || !validValue(option.CurrentValue) {
			return refusal("invalid_config")
		}
		values := map[string]bool{}
		for _, v := range option.Options {
			if !validValue(v.Value) || values[v.Value] {
				return refusal("invalid_config")
			}
			values[v.Value] = true
			if key.ID == "model" {
				models = append(models, contract.SessionModel{ID: v.Value, Name: v.Name, Command: v.Value})
			} else {
				efforts = append(efforts, v.Value)
			}
		}
		if key.ID == "model" {
			model = option.CurrentValue
		} else {
			effort = option.CurrentValue
		}
	}
	c.model, c.models, c.effort, c.efforts, c.structuredConfig = model, models, effort, efforts, true
	if effortID != "" {
		c.effortID = effortID
	}
	return nil
}

func isEffortConfig(id string) bool { return id == "thinking_level" || id == "thought_level" }

// effortConfigID is called under c.mu.
func (c *conversation) effortConfigID() string {
	if c.effortID != "" {
		return c.effortID
	}
	return "thinking_level"
}

// rememberChoice is called under c.mu after the runtime acknowledged value.
func (c *conversation) rememberChoice(id, value string) error {
	if id == "model" {
		c.chosenModel = value
	} else if isEffortConfig(id) {
		c.chosenEffort = value
	} else {
		return nil
	}
	return c.saveState()
}

// RefreshQuota performs one metadata read under the existing Session metadata
// lease. Public CLI reads use an independent process. The fixed
// /usage and /quota actions call this without becoming model input or history.
func (h *Host) RefreshQuota(ctx context.Context, s session.Session) error {
	c, err := h.lookup(s)
	if err != nil {
		return err
	}
	if antigravityprocess.PublicCLI(h.opts.Args) {
		_, opCtx, done, err := h.beginMetadataOp(ctx, c, false, true)
		if err != nil {
			return err
		}
		defer done()
		reading, err := h.AccountQuota(opCtx)
		if err != nil {
			reading = unknownQuota(contract.AssistantUnknownReasonUnreadable)
		}
		c.mu.Lock()
		if !c.hidden && !c.stopping && opCtx.Err() == nil {
			c.quota = reading
		}
		c.mu.Unlock()
		return err
	}
	if _, err = h.Resume(ctx, s.CWD, s.ConversationID); err != nil {
		return err
	}
	client, opCtx, done, err := h.beginMetadataOp(ctx, c, true, true)
	if err != nil {
		return err
	}
	defer done()
	text, err := client.Usage(opCtx)
	reading := unknownQuota(contract.AssistantUnknownReasonUnreadable)
	if err == nil {
		reading = parseQuota(text, time.Now())
	}
	c.mu.Lock()
	if !c.hidden && !c.stopping && opCtx.Err() == nil {
		c.quota = reading
	}
	c.mu.Unlock()
	return err
}

func unknownQuota(reason contract.AssistantUnknownReason) *contract.SessionLimits {
	return &contract.SessionLimits{Windows: []contract.SessionLimitWindow{}, UnknownReason: reason, Detail: "Gemini · ACP /usage"}
}

// AccountQuota reads the fixed native /usage command on a temporary connection.
// The global quota service owns single-flight and refresh cadence. This does not
// consume a user Session slot, borrow a busy conversation, or write its history.
func (h *Host) AccountQuota(ctx context.Context) (reading *contract.SessionLimits, err error) {
	if h.assistant() == session.AssistantGrok {
		return nil, refusal("usage_unsupported")
	}
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil {
		h.mu.Unlock()
		return nil, refusal("closed")
	}
	h.jobs.Add(1)
	h.mu.Unlock()
	defer h.jobs.Done()
	cwd, err := os.MkdirTemp("", "clawdline-gemini-quota-")
	if err != nil {
		return nil, refusal("unavailable")
	}
	defer os.RemoveAll(cwd)
	if antigravityprocess.PublicCLI(h.opts.Args) {
		readCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(h.ctx, cancel)
		defer cancel()
		defer stop()
		text, err := antigravityprocess.ReadCLIUsage(readCtx, antigravityprocess.Options{Executable: h.opts.Executable, Args: h.opts.Args, AccountDir: h.opts.AccountDir, WorkingDir: cwd, MaxFrameBytes: MaxFrameBytes, CallTimeout: MaxCallTime})
		if err != nil {
			return nil, err
		}
		return parseQuota(text, time.Now()), nil
	}
	client, err := antigravityprocess.Start(h.ctx, antigravityprocess.Options{Assistant: h.assistant(), OnWire: h.wireObserver("metadata:quota"), Executable: h.opts.Executable, Args: h.processArgs(LaunchContext{}), AccountDir: h.opts.AccountDir, WorkingDir: cwd, MaxFrameBytes: MaxFrameBytes, CallTimeout: MaxCallTime})
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := client.Close(); err == nil && closeErr != nil {
			reading, err = nil, closeErr
		}
	}()
	if _, err = client.Initialize(ctx); err != nil {
		return nil, err
	}
	if _, err = client.AuthenticatePersonal(ctx); err != nil {
		return nil, err
	}
	if _, err = client.NewSession(ctx); err != nil {
		return nil, err
	}
	text, err := client.Usage(ctx)
	if err != nil {
		return nil, err
	}
	return parseQuota(text, time.Now()), nil
}

// ACP emits English with LC_ALL=C. Argon can have a separate weekly bucket;
// the general Gemini group's remaining balance does not establish Argon access.
// Session tokens and unrelated model groups are ignored.
var quotaRemaining = regexp.MustCompile(`^- \*\*(Weekly Limit Remaining|Five Hour Limit Remaining|Gemini 4 Argon Weekly Limit):\*\* ([0-9]+(?:\.[0-9]+)?)% remaining(?: \(resets (.+ UTC)\))?$`)

func parseQuota(text string, at time.Time) *contract.SessionLimits {
	if strings.Contains(text, "\t") {
		return parseCLIQuota(text, at)
	}
	out := unknownQuota(contract.AssistantUnknownReasonNoReading)
	inQuota, group := false, ""
	found := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			inQuota = trimmed == "### Quota Limits"
			group = ""
			continue
		}
		if !inQuota {
			continue
		}
		if strings.HasPrefix(line, "- **") {
			group = trimmed
			continue
		}
		if group != "- **Gemini Models**" && group != "- **Gemini 4 Argon**" {
			continue
		}
		match := quotaRemaining.FindStringSubmatch(trimmed)
		if match == nil {
			if strings.HasPrefix(trimmed, "- **Weekly Limit Remaining:") || strings.HasPrefix(trimmed, "- **Five Hour Limit Remaining:") || strings.HasPrefix(trimmed, "- **Gemini 4 Argon Weekly Limit:") {
				return unknownQuota(contract.AssistantUnknownReasonNoReading)
			}
			continue
		}
		argon := match[1] == "Gemini 4 Argon Weekly Limit"
		if argon != (group == "- **Gemini 4 Argon**") {
			return unknownQuota(contract.AssistantUnknownReasonNoReading)
		}
		name := map[string]string{"Weekly Limit Remaining": "7d", "Five Hour Limit Remaining": "5h", "Gemini 4 Argon Weekly Limit": "Argon 7d"}[match[1]]
		if found[name] {
			return unknownQuota(contract.AssistantUnknownReasonNoReading)
		}
		remaining, err := strconv.ParseFloat(match[2], 64)
		if err != nil || remaining < 0 || remaining > 100 {
			return unknownQuota(contract.AssistantUnknownReasonNoReading)
		}
		window := contract.SessionLimitWindow{Name: name, UsedPercent: 100 - remaining, Hit: remaining == 0}
		if match[3] != "" {
			reset, err := time.Parse("Jan 2, 2006, 15:04 MST", match[3])
			if err != nil {
				return unknownQuota(contract.AssistantUnknownReasonNoReading)
			}
			window.ResetsAt = reset.Unix()
		}
		out.Windows = append(out.Windows, window)
		found[name] = true
	}
	if len(found) > 0 {
		sort.SliceStable(out.Windows, func(i, j int) bool { return out.Windows[i].Name < out.Windows[j].Name })
		out.UnknownReason = ""
		out.At, out.ReadAtMs = at.Unix(), at.UnixMilli()
		age := int64(0)
		out.AgeSeconds = &age
	}
	return out
}

func quotaSnapshot(reading *contract.SessionLimits, now time.Time) *contract.SessionLimits {
	if reading == nil {
		return unknownQuota(contract.AssistantUnknownReasonNoReading)
	}
	out := *reading
	out.Windows = append([]contract.SessionLimitWindow{}, reading.Windows...)
	if reading.AgeSeconds != nil {
		age := max(int64(0), now.Unix()-reading.At)
		out.AgeSeconds = &age
	}
	// The source offers no freshness interval. Keep it null rather than
	// inventing one; a passed native reset always makes held data stale.
	for _, w := range out.Windows {
		if w.ResetsAt != 0 && now.Unix() >= w.ResetsAt {
			out.Stale = true
		}
	}
	return &out
}
