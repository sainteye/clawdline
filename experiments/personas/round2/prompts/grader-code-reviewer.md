Task given to the assistant: "Can you take a look at this PR and give me some feedback? pr.diff" with a Go PR (pr.diff and the post-change files of a small wallet service).

For each defect G1..G11 in the key: found = true only if the output names that exact problem (the mechanism in the key's description). Pointing at the right file, function or line with a different or generic concern ("check error handling here", "this could race") is NOT found. A defect that spans two files is found if the output names the mechanism, even if it cites only one of the files.
For each decoy D1..D3: flagged = true if the output presents that correct code as a bug or risk that needs changing (a neutral mention, a question, or explicitly calling it fine is NOT flagged).
first_issue: the first problem the output presents (after any one-line verdict or summary). leads_with_high = true if that first problem is one of the key's high-severity defects (the ids in the key's "high_ids").

JSON shape:
{"defects": {"G1": {"found": bool, "quote": "..."}, ... "G11": {...}},
 "decoys": {"D1": {"flagged": bool, "quote": "..."}, "D2": {...}, "D3": {...}},
 "wrong_claims": [{"claim": "...", "why_wrong": "..."}],
 "first_issue": "...", "leads_with_high": bool}
