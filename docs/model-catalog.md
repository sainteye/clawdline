# The model catalog

Every screen that offers a model for an agent (Claude Code `claude`, Codex `codex`, Gemini
`antigravity`, Grok `grok`) reads one list per agent from the daemon, so the same agent on the
same machine offers the same models everywhere. Decided 2026-11-25: one source of truth in the
daemon, two tiers, every screen reads the same result.

## Shape

`GET /v1/model-catalog` answers every agent in the order above; `?agent=<name>` answers one. A
paired phone sends the Cloud word `model-catalog` (optional field `agent`), which the bridge turns
into the same request. Arrived at API level 120. The types are `ModelCatalogAnswer`,
`ModelCatalogAgent` and `ModelCatalogModel` in `api/v1/model-catalog.schema.json`.

```json
{"agents": [{"agent": "codex", "models": [{"value": "model-sol", "name": "6.2 Sol", "description": "…"}],
  "default_name": "6.2 Sol", "efforts": ["high", "xhigh"], "source": "agent",
  "observed_at": "2026-11-25T06:00:00Z", "refreshing": false}]}
```

- `value` always passes the launcher's model rule (`modelcatalog.ModelName`, the one rule
  `projects.ModelName` and saved Bot dispatch choices use). The agent's own default is never an
  entry: a picker offers "Agent default (`default_name`)" first.
- `efforts` comes from the one effort table in `internal/domain/modelcatalog`, which the
  launchers, the default-model settings and the Bots shared settings also read.
- `refreshing` is true while the daemon is asking the agent; a reader polls. `problem` (and
  `problem_field`) name why the last agent discovery failed while the list shown is the last good
  one.

## Tiers

1. **Native** (`source: "native"`): the agent's own files or CLI, read without starting an agent
   session — Claude Code's account state and executable (cached by their file identity, so the
   executable is no longer scanned on every read), Codex's `~/.codex/models_cache.json`,
   `agy models` for Gemini. Grok has none. A first read with nothing cached answers this tier at
   once.
2. **Agent** (`source: "agent"`): the agent's own ACP configuration answer, from the disposable
   discovery session the Bots model dialog already uses. It needs no Bot: it runs under the
   machine's own `model-catalog` identity (`projectassistant.ModelCatalogBot`) in that identity's
   empty personal workspace, with the person's own login, and takes the Bots discovery slot, so it
   never runs beside a Bot's own discovery. It runs only while Bots are enabled; it never sets
   Bots up. Its answer is mapped by `modelcatalog.FromConfiguration`: the Default entry is left
   out and names the default, Codex's per-effort entries collapse to one model each, and a value
   the launcher refuses is dropped.

A read never waits for the agent tier. It starts one background discovery per agent (single
flight), at most one at a time on the machine, when the agent's answer is missing or older than
`cache.model_catalog_refresh_seconds` (600 s). A failed discovery keeps the last good list and is
retried no sooner than `cache.model_catalog_retry_seconds` (60 s); one discovery is abandoned
after `cache.model_catalog_probe_seconds` (90 s). At most `cache.model_catalog_models` (64)
models are kept per agent.

## Console

`web/console/src/model-catalog/` is the only console code that reads a model list. It holds:

- `useModelCatalog.ts` / `reader.ts`: the catalog for one captured machine source, local or
  Cloud. It polls while any agent is refreshing (bounded, never an endless spinner) and exposes
  loading, ready (with `waiting`), failed (with retry) and unsupported.
- `machine.ts` / `ui.tsx` `useMachineModelCatalog`: the page's own machine as a source,
  `GET /v1/model-catalog` locally or the Cloud word `model-catalog` through the relay writer.
  An older daemon's 404, or a relay or machine that does not carry the word, is unsupported.
- `picker.ts` `catalogPicker`: one agent's options (the agent default first, a saved model kept
  with a "not in the current list" hint) and its one status line. `CatalogStatus`,
  `CatalogModelMenu` and `CatalogEffortMenu` draw it outside the Bots drop-down.
- `match.ts` `currentModelRow` / `isCurrentModel`: the one rule for which listed model a session
  runs: the same word, a dated release (`-20250805`) or a bracketed variant (`[1m]`), never a
  shorter version.
- `one-catalog.test.ts`: fails when a hard-coded alias list, default-models model options, or a
  read of `/v1/model-catalog` reappears elsewhere in console sources.

A daemon older than API 120, or a relay that does not carry the word, is unsupported: the
picker offers only the agent default and says why.

Consumers:

- Bots settings (`project-assistants/BotsSettingsModal.tsx` `ModelSelect`), through the
  captured Bot source.
- Settings, Work tab (`pages/settings/window/SettingsWindow.tsx`) and "Defaults for new
  sessions" (`pages/settings/DefaultModelsBlock.tsx`): models and the Codex effort. The saved
  values still come from and go to `/v1/settings/default-models`.
- The Board's per-launch row (`pages/work/WorkModelChoice.tsx`, used by `pages/work/WorkV2.tsx`
  and the Start sheet `session/Start.tsx`), Grok included. The Start sheet offers no row for a
  start on another machine: the start is carried there with its model, but the row reads the
  page machine's catalog.
- Schedules (`pages/schedules.tsx`): models for Claude Code, Codex and Gemini, and the catalog
  efforts for Codex and Gemini.
- The voice copilot's `list_projects` (`realtime/copilot-tools.ts`): the choices per assistant;
  the saved defaults still come from default-models.
- Session info (`overlays/info.ts`) and the status line (`session/StatusLine.tsx`) match the
  current model with `currentModelRow`. `GET /v1/sessions/{id}/info` lists the catalog for a
  session no live adapter speaks for (`catalogSessionModels` in
  `internal/transport/http/transcript.go`); a live owned Gemini or Grok session lists its own
  offer, which is the truth for that session.
- Bot runtime pickers (`project-assistants/AgentModelPicker.tsx`, `ChatModel.tsx`,
  `BotSettings.tsx`, `agent-model-summary.ts`) for Claude Code, Codex and Grok. The pick is a
  catalog model plus an effort, saved as `model` or `model/effort`;
  `modelcatalog.ToConfiguration`, the inverse of `FromConfiguration`, maps it to the agent's ACP
  value where the daemon validates and applies it, and `FromChoice` reads a saved value back as
  its catalog entry. Gemini stays on the Bot's own ACP list. See [Bots](bots.md).

## Not pickers

These daemon lists name models but offer nothing to choose from, so they deliberately do not
read the catalog:

- `internal/adapters/planner/planner.go` `planner.Models`: the classifier's output vocabulary
  (haiku/sonnet/opus as a size tier); anything else it answers is dropped to the default.
- `internal/adapters/transcript/pricing.go` `ClaudeModels`: Claude Code's moving aliases mapped to
  the IDs a transcript reports, for parsing and for matching a session's current model.
- The effort suffix in `internal/transport/http/start.go` and `antigravity_owned.go`
  (`hasKnownEffortSuffix`): composes a launch from a saved default model and effort; it already
  reads the one effort table (`modelcatalog.Efforts`).
- Voice copilot realtime models (`internal/domain/copilot`) and usage's `WorkCursorModel` are not
  agent models.
