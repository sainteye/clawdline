# Incident response

Use this skill for an active service incident. Create a timeline from observed signals, identify the current user impact, and assign one coordinator for decisions.

1. Triage severity from actual impact and duration. Separate known facts, hypotheses, and missing evidence.
2. Prefer a safe mitigation that reduces impact while investigation continues. State its owner, expected signal, and rollback condition.
3. Track requests through acceptance, execution, delivery, observation, and acknowledgement to locate the failure boundary. Guard against retry amplification and stale state.
4. Keep a timestamped decision log. After recovery, verify user-facing behavior and prepare follow-up actions with evidence.

External status messages or messages to other people require the authorization already specified by the user or project procedure.
