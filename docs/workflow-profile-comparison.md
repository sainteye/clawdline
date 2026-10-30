# A small comparison of three workflow prompt adaptations

On 2026-10-30, three fresh native subagents implemented the same standalone Go function, `ResolveProfile([]byte) (string, error)`, in separate directories. Each inherited the same model and reasoning setting. The arms received the same specification and five-minute ceiling, with one additional method description: ordinary direct development, the selected Superpowers evidence-first adaptation, or the bounded Poteto data-shape/verifiable-unit/evidence-map adaptation.

The function parses one JSON object, resolves a main workflow with a legacy boolean fallback, validates inputs, ignores unrelated settings and never mutates the input. It does not read or write real machine settings. No external model API was called; provider-native subagents used the current session's account. Comparable billed cost and token totals were not measured.

The integrating root authored and ran the same independent 26-case acceptance suite against each returned artifact. It checked defaults, both legacy values, all three explicit profiles, precedence, unrelated fields, whitespace, malformed/non-object/trailing input, null/empty/unknown/wrong-type profile values, invalid legacy values and input preservation.

| Method condition | Common acceptance | What the returned record showed |
| --- | --- | --- |
| Ordinary direct development | 26/26 passed | Implementation and behavioral tests |
| Selected Superpowers adaptation | 26/26 passed | A failing behavioral-test run before implementation, then passing tests |
| Bounded Poteto adaptation | 26/26 passed | Data shape and verification map; an additional large-number/unrelated-field regression found and corrected |

The method-specific observations come from each worker's report; only the common acceptance outcomes were independently checked by the root. Timing and self-authored test counts are deliberately not ranked: the task is short, executions were not simultaneous, cache/scheduling conditions differ, there is one run per arm and the root knew the condition labels. The prompts were concise adaptations, not executions of the complete upstream Superpowers or pstack packages. This is not a blinded or causal benchmark of those projects, nor an end-to-end test of profile launching.

This trial establishes that all three methods can complete this bounded parsing task. It does not establish a winner, a productivity gain or general suitability for larger changes. The feature itself uses the existing selected evidence-led method plus Clawdline's normal checks and independent review. Runtime snapshot, resume, child inheritance and UI behavior require their own integration tests.

A subsequent useful evaluation would use a real, moderately complex defect or feature from the same base revision, with a fixed model/tool budget, fresh contexts, isolated working trees and independent acceptance that includes regressions and real-surface behavior. Preserve quality, rework, human intervention, time and actual usage evidence before selecting a default for broader use.

See [the profile integration contract](workflow-profiles.md).
