You are grading ONE output produced by an AI assistant on a fixed test task, against an answer key written before the task was run. You do not know how the output was produced; grade only what it says or did.

Rules:
- Be strict and literal. Credit a planted item only when the output identifies the actual problem (the specific mechanism in the key), not merely the same area of code or a generic concern. Quote the words from the output that earn the credit.
- A "wrong claim" is something the output presents as a problem/bug/defect that is actually false for this code (not merely minor, stylistic, or a question). Items listed in the key under "acceptable_other_findings" are never wrong claims. Planted items and decoys are counted in their own fields, not as wrong claims.
- Output ONLY one JSON object, no prose before or after, matching the shape given below.
