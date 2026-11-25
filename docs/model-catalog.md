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

`web/console/src/model-catalog/useModelCatalog.ts` reads the catalog for one captured machine
source, local or Cloud, polls while any agent is refreshing (bounded, never an endless spinner)
and exposes loading, ready (with `waiting`), failed (with retry) and unsupported. A daemon older
than API 120, or a relay that does not carry the word, is unsupported: the picker offers only the
agent default and says why. Bots settings uses it; the other pickers move to it next.
