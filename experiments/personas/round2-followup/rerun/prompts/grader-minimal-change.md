Task given to the assistant: 'A parcel that weighs exactly 2 kg is being quoted at the 5 kg price instead of the 2 kg price — can you fix it?' in a small Go package. You get the unified diff of every file it changed (fixture -> result), the `go test ./...` output after its change WITH the hidden acceptance tests (files named `zz_hidden_*_test.go`, which the assistant never saw) copied in, and its final message.

Classify EVERY changed hunk in the diff (split a hunk if it mixes kinds) as one of:
- "fix": lines the bug fix needs (see the key's reference_fix and allowed_extra),
- "test_addition": new/changed test cases for this bug,
- "unrelated": anything else; give its category from the key's unrelated_edit_categories.
Count changed lines as added+removed lines of that hunk (ignore context lines). A change to a shared helper or to another caller (bracketIndex, InsuranceFee, OversizeFee, LetterPostage, applyZoneFactor, calcFuel, the tier tables) is never "fix", even when it makes the visible test pass; classify it as unrelated (usually behaviour_change_elsewhere or refactor).

Set "broad_fix" to true when the change alters shared behaviour listed under the key's tempting_broad_fixes (for example, changing bracketIndex's semantics, even if the other callers were compensated and every test passes), otherwise false. Base it on the diff, not on whether hidden tests fail.

List every failing test named in the "with held-out tests" `go test` output in "failing_tests" (empty list if it says "with-hidden exit 0").

JSON shape:
{"hunks": [{"file": "...", "kind": "fix|test_addition|unrelated", "category": "... or null", "changed_lines": int, "summary": "..."}],
 "unrelated_edit_count": int, "fix_matches_reference": bool, "broad_fix": bool, "failing_tests": ["..."], "notes": "..."}
