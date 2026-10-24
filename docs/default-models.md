# Defaults for new sessions

The website Settings page is the primary entry. Its Work tab reads and writes
`/v1/settings/default-models`, including over the existing paired Cloud path. The
narrow route exposes model defaults and provider model choices; it does not expose
unrelated machine settings. A local console or a paired device with send authority
may write; a read-only device may only read. Writes merge named fields atomically,
so repeating the same write is idempotent and other provider defaults survive.

Antigravity uses `antigravity_default_model` and `antigravity_default_effort`.
An empty value leaves the choice to the native provider. The model uses the existing
safe model slug grammar and bound. Effort accepts `low`, `medium`, `high`, `xhigh`
and `max`; Codex retains its separate `high`/`xhigh` vocabulary. The reader helper
`nextconfig.DefaultAntigravityEffort` provides only this provider's validated effort.
An absent or malformed setting never borrows a value from another provider.

There is currently no installed native model-list source on this settings path.
The Antigravity catalog is an empty array, meaning unknown. The website offers the
native default and a validated saved model without inventing model names, context
sizes, quota, or prices. An unknown saved effort remains visible until explicitly
changed; an invalid new write is refused. Saving a choice does not authorize paid
calls or paid fallback.

Startup and broker composition must explicitly consume the provider-specific effort
helper before a new launch can honor it. Configuration persistence alone does not
establish native launch readiness or permission support. The guarded launch path
continues to own those decisions. See [schedules](schedules.md) for scheduled values.
