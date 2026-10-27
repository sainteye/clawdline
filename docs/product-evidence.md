# Product evidence map for support answers

Use this map to locate evidence, not as a substitute for reading the implementation. Source URLs,
exact revisions and hashes live in the installed `docs/manifest.json`. An installed snapshot proves
behavior at that revision; it does not alone prove what is deployed on every member's machine.

## Identify the entry point before answering

- **Board / ticket / assignment / dispatch / model choice:** read `docs/user/board.md`,
  `docs/user/model-assignment.zh-TW.md`, `docs/source/web/console/src/pages/work/WorkV2.tsx`,
  `WorkModelChoice.tsx` in the same directory, and that directory's `api.ts`.
  The Board has a per-launch model choice after choosing Claude or Codex. Its compact **Model**
  section starts collapsed. The final start button confirms and sends the model. Role recommendation
  is separate: `askAIForPersona` recommends a role, not a model.
- **General new Session:** `docs/user/sessions.md` and
  `docs/source/web/console/src/session/Start.tsx`. This is a different entry point; absence here
  says nothing about Board assignment.
- **Machine defaults:** `docs/source/web/console/src/pages/settings/DefaultModelsBlock.tsx`
  and the settings `api.ts`. Defaults are different from a per-launch override.
- **My questions / FAQ / support:** `docs/user/help-center.md`,
  `docs/source/web/console/src/support/HelpCenter.tsx` and `model.ts` in that directory.

Before saying a feature is missing, search the relevant UI directory and its request path; trace
both the control and the value sent to the server. Do not infer global absence from one page or
outdated guide. If the entry point or version remains ambiguous, ask for it. For a feature request,
explain the part already supported with concrete steps and distinguish any remaining suggestion.
Do not promise implementation or mark a request delivered because an answer was written.
