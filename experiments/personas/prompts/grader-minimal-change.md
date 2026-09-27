Task given to the assistant: 'Refunds show up on printed invoices as "$-12.-50" instead of "-$12.50" — can you fix it?' in a small Go package. You get the unified diff of every file it changed (fixture -> result), the `go test ./...` output after its change, and its final message.

Classify EVERY changed hunk in the diff (split a hunk if it mixes kinds) as one of:
- "fix": lines the bug fix needs,
- "test_addition": new/changed test cases for this bug,
- "unrelated": anything else; give its category from the key's unrelated_edit_categories.
Count changed lines as added+removed lines of that hunk (ignore context lines).

JSON shape:
{"hunks": [{"file": "...", "kind": "fix|test_addition|unrelated", "category": "... or null", "changed_lines": int, "summary": "..."}],
 "unrelated_edit_count": int, "fix_matches_reference": bool, "notes": "..."}
