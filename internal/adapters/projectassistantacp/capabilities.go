// Package projectassistantacp implements a bounded ACP subprocess transport.
// The pending/incoming split is adapted from Codync host/src/agent/acp.rs at
// 234219cef2d3f991f0f7225ba89e3c99cdd34177; see third_party/codync/NOTICE.
package projectassistantacp

import (
	"context"
	"os"
	"path/filepath"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Configuration comes exclusively from the trusted host. There is no registry
// resolution, shell expansion, automatic installation or npx fallback.
type Configuration struct {
	Executable  string
	Arguments   []string
	Environment []string
}

type ProviderOptions struct {
	Configurations map[pa.Agent]Configuration
	Limits         Limits
	// Verify supplies exact local version/auth/sandbox/memory evidence. It must
	// not initiate a model request. Missing evidence is a typed refusal.
	Verify func(context.Context, pa.Bot, Configuration) (pa.Capabilities, error)
	// ReadOnlyPermission classifies the actual native tool payload. A missing
	// classifier denies allow options; option labels are never authority.
	ReadOnlyPermission func([]byte) bool
}
type Provider struct{ o ProviderOptions }

var _ app.Provider = (*Provider)(nil)

func refusal(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }

func NewProvider(o ProviderOptions) (*Provider, error) {
	if err := o.Limits.Validate(); err != nil {
		return nil, err
	}
	configs := make(map[pa.Agent]Configuration, len(o.Configurations))
	for agent, c := range o.Configurations {
		if agent != pa.ClaudeCode && agent != pa.Codex {
			return nil, refusal(pa.Unsupported, "agent")
		}
		if !filepath.IsAbs(c.Executable) {
			return nil, refusal(pa.Invalid, "trusted_executable")
		}
		c.Arguments = append([]string(nil), c.Arguments...)
		c.Environment = append([]string(nil), c.Environment...)
		configs[agent] = c
	}
	o.Configurations = configs
	return &Provider{o: o}, nil
}

func (p *Provider) Capabilities(ctx context.Context, bot pa.Bot) (pa.Capabilities, error) {
	caps := pa.Capabilities{Key: bot.Key, Project: bot.Project, Agent: bot.Agent, NativeMemory: pa.NativeMemoryUnknown}
	c, ok := p.o.Configurations[bot.Agent]
	if !ok {
		return caps, refusal(pa.AdapterNotFound, "trusted_executable")
	}
	info, err := os.Stat(c.Executable)
	if err != nil || !info.Mode().IsRegular() {
		return caps, refusal(pa.AdapterNotFound, "trusted_executable")
	}
	if p.o.Verify == nil {
		return caps, refusal(pa.ReadOnlyUnverified, "native_evidence")
	}
	caps, err = p.o.Verify(ctx, bot, c)
	if err != nil {
		return caps, err
	}
	if caps.Agent != bot.Agent || caps.Key != bot.Key || caps.Project != bot.Project {
		return caps, refusal(pa.ScopeDenied, "capability_scope")
	}
	if err = pa.CheckCapabilities(caps); err != nil {
		return caps, err
	}
	return caps, nil
}

func (p *Provider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	caps, err := p.Capabilities(ctx, q.Bot)
	if err != nil {
		return nil, err
	}
	if err = pa.CheckExecutionCapabilities(caps, q.Binding.Fence); err != nil {
		return nil, err
	}
	if q.Resume && (!caps.Resume || q.Binding.NativeSessionID == "") {
		return nil, refusal(pa.ResumeUnsupported, "native_session")
	}
	c := p.o.Configurations[q.Bot.Agent]
	proc, err := StartProcess(ctx, ProcessOptions{Configuration: c, Directory: q.Directory, Limits: p.o.Limits})
	if err != nil {
		return nil, err
	}
	client, err := NewClient(proc, p.o.Limits, q.Binding.Fence, sink, p.o.ReadOnlyPermission)
	if err != nil {
		_ = proc.Close(ctx)
		return nil, err
	}
	if err = client.initialize(ctx, q); err != nil {
		_ = client.Close(ctx)
		return nil, err
	}
	return client, nil
}
