package antigravitysession

import (
	"context"
	"encoding/json"
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
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil {
		return nil, nil, refusal("closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hidden || c.client == nil {
		return nil, nil, refusal("closed")
	}
	if c.busy || c.row.State != session.StateIdle {
		return nil, nil, refusal("busy")
	}
	c.busy = true
	h.jobs.Add(1)
	return c.client, func() { c.mu.Lock(); c.busy = false; c.mu.Unlock(); h.jobs.Done() }, nil
}

func (h *Host) selectModel(ctx context.Context, c *conversation, modelID string, before func() error) error {
	c.mu.Lock()
	structured := c.structuredConfig
	c.mu.Unlock()
	if structured {
		return h.selectConfig(ctx, c, "model", modelID, before)
	}
	client, done, err := h.beginMetadata(c)
	if err != nil {
		return err
	}
	defer done()
	c.mu.Lock()
	offered := false
	for _, m := range c.models {
		offered = offered || m.ID == modelID
	}
	c.mu.Unlock()
	if !offered {
		return refusal("model_not_offered")
	}
	if err := client.SetModelChecked(ctx, modelID, before); err != nil {
		return err
	}
	c.mu.Lock()
	c.model = modelID // Ack selects the next turn; it does not execute a model.
	c.mu.Unlock()
	return nil
}

func (h *Host) selectConfig(ctx context.Context, c *conversation, id, value string, before func() error) error {
	client, done, err := h.beginMetadata(c)
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
	return client.SetConfigOptionChecked(ctx, id, value, func() error {
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		return validate()
	}, func(raw json.RawMessage) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.readMetadata(raw, false)
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
	var model, effort string
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
		if key.ID != "model" && key.ID != "thinking_level" {
			continue
		}
		if seen[key.ID] {
			return refusal("invalid_config")
		}
		seen[key.ID] = true
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
	return nil
}

// RefreshQuota performs at most one metadata-only read. Summary/list/send paths
// never call it, and a busy session refuses rather than queuing or retrying.
func (h *Host) RefreshQuota(ctx context.Context, s session.Session) error {
	c, err := h.lookup(s)
	if err != nil {
		return err
	}
	if _, err = h.Resume(ctx, s.CWD, s.ConversationID); err != nil {
		return err
	}
	client, done, err := h.beginMetadata(c)
	if err != nil {
		return err
	}
	defer done()
	text, err := client.Usage(ctx)
	reading := unknownQuota(contract.AssistantUnknownReasonUnreadable)
	if err == nil {
		reading = parseQuota(text, time.Now())
	}
	c.mu.Lock()
	c.quota = reading
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
	client, err := antigravityprocess.Start(h.ctx, antigravityprocess.Options{Executable: h.opts.Executable, Args: h.opts.Args, AccountDir: h.opts.AccountDir, WorkingDir: cwd, MaxFrameBytes: MaxFrameBytes, CallTimeout: MaxCallTime})
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

// ACP 1.4.1 emits English with LC_ALL=C. Only numeric Gemini quota windows
// are understood; session tokens and other model groups are ignored.
var quotaRemaining = regexp.MustCompile(`^- \*\*(Weekly|Five Hour) Limit Remaining:\*\* ([0-9]+(?:\.[0-9]+)?)% remaining(?: \(resets (.+ UTC)\))?$`)

func parseQuota(text string, at time.Time) *contract.SessionLimits {
	out := unknownQuota(contract.AssistantUnknownReasonNoReading)
	inQuota, inGemini := false, false
	found := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			inQuota = trimmed == "### Quota Limits"
			inGemini = false
			continue
		}
		if !inQuota {
			continue
		}
		if strings.HasPrefix(line, "- **") {
			inGemini = trimmed == "- **Gemini Models**"
			continue
		}
		if !inGemini {
			continue
		}
		match := quotaRemaining.FindStringSubmatch(trimmed)
		if match == nil {
			if strings.HasPrefix(trimmed, "- **Weekly Limit Remaining:") || strings.HasPrefix(trimmed, "- **Five Hour Limit Remaining:") {
				return unknownQuota(contract.AssistantUnknownReasonNoReading)
			}
			continue
		}
		name := map[string]string{"Weekly": "7d", "Five Hour": "5h"}[match[1]]
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
