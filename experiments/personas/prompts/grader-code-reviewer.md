Task given to the assistant: "Can you take a look at this PR and give me some feedback?" with a Go PR (pr.diff and the post-change files).

For each defect G1..G7 in the key: found = true only if the output names that exact problem.
For each decoy D1..D3: flagged = true if the output presents that correct code as a bug or risk that needs changing (a neutral mention or explicitly calling it fine is NOT flagged).
first_issue: the first problem the output presents (after any one-line verdict). leads_with_high = true if that first problem is one of the key's high-severity defects (G2, G3, G4, G5).

JSON shape:
{"defects": {"G1": {"found": bool, "quote": "..."}, ... "G7": {...}},
 "decoys": {"D1": {"flagged": bool, "quote": "..."}, "D2": {...}, "D3": {...}},
 "wrong_claims": [{"claim": "...", "why_wrong": "..."}],
 "first_issue": "...", "leads_with_high": bool}
