# Defaults for new sessions

The website Settings page is the primary entry. Its Work tab reads and writes
`/v1/settings/default-models`, including over the existing paired Cloud path. The
narrow route exposes model defaults and provider model choices; it does not expose
unrelated machine settings. A local console or a paired device with send authority
may write; a read-only device may only read. Writes merge named fields atomically,
so repeating the same write is idempotent and other provider defaults survive.

The route (`defaultModelsRoute` and `defaultModelsSnapshot` in
`internal/transport/http/settings.go`; contract in `api/v1/settings.schema.json`)
answers and accepts five keys: `codex_default_model`, `codex_default_effort`,
`claude_default_model`, `antigravity_default_model` and `antigravity_default_effort`.
A write is `application/json`; any other key is refused with `bad_request`
(`not a default model key`), and an invalid value with `invalid_default_model` or
`invalid_default_effort`. A saved Antigravity model that is no longer valid is
answered as `null`; a saved Antigravity effort is answered as saved.

Antigravity uses `antigravity_default_model` and `antigravity_default_effort`.
An empty value leaves the choice to the native provider. The model uses the existing
safe model slug grammar and bound. Effort accepts `low`, `medium`, `high`, `xhigh`
and `max`; Codex retains its separate `high`/`xhigh` vocabulary. The reader helper
`nextconfig.DefaultAntigravityEffort` provides only this provider's validated effort.
An absent or malformed setting never borrows a value from another provider.

The route still answers `models` lists, built from the daemon's
[model catalog](model-catalog.md), for older consoles. This console does not read
them: both Settings pickers (the Work tab and "Defaults for new sessions") take their
options and efforts from the catalog itself (`web/console/src/model-catalog`), and
read this route only for the saved values. The website offers the agent default and
a validated saved model without inventing model names, context sizes, quota, or prices. Settings
offers no Antigravity effort control: native Antigravity models carry their own effort
(removed on 2026-11-11 in `a51cd5f70`). A saved `antigravity_default_effort` is still read
and applied at launch, and can be written through this route or the Cloud word
`default-models-update`; the schedule form keeps its own per-schedule Antigravity
effort chips. An invalid new write is refused. Saving a choice does not authorize paid
calls or paid fallback.

Startup (`internal/transport/http/start.go`) and broker composition
(`internal/transport/http/orchestrator_wiring.go`) consume the provider-specific
effort through `nextconfig.DefaultEffort`, which sends Antigravity to
`nextconfig.DefaultAntigravityEffort`. Configuration persistence alone does not
establish native launch readiness or permission support. The guarded launch path
continues to own those decisions. See [schedules](schedules.md) for scheduled values.
