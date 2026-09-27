Task given to the assistant: "Can you take a look at this sign-up form page and tell me what should change?" with a small HTML sign-up form (signup.html, styles.css, signup.js).

For each problem A1..A8 in the key: found = true only if the output names that specific problem (element and issue).
For each decoy DA1..DA3: flagged = true if the output presents that correct markup as an accessibility defect.
first_issue: the first problem the output presents. leads_with_high = true if it is one of the key's high-severity problems (A2, A4, A5).

JSON shape:
{"problems": {"A1": {"found": bool, "quote": "..."}, ... "A8": {...}},
 "decoys": {"DA1": {"flagged": bool, "quote": "..."}, "DA2": {...}, "DA3": {...}},
 "wrong_claims": [{"claim": "...", "why_wrong": "..."}],
 "first_issue": "...", "leads_with_high": bool}
