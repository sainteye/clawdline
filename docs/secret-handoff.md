# Secret Handoff

Secret Handoff is the person-to-Session path for a password, credential, or API key needed by a local tool. The Note is an invitation and status surface, never the value's storage. A normal Session message and its transcript are also unsuitable: they retain what was sent. The receiving agent names the credential in the Note summary, shown as the label immediately above the dedicated multi-line input. The summary is a non-secret label, never the value.

## Contract

1. The Note identifies a live target conversation and the purpose of the requested secret. The request must not contain a secret value. Only the person may submit the value.
2. The dedicated input does not copy the value into the Session composer, a draft, a browser URL, browser storage, a notification, or a Note field. Its request travels through the existing authenticated local HTTP path or Cloud's end-to-end encrypted action channel. Cloud may queue ciphertext but must never see the plaintext.
3. The machine holds the value only in memory with a short deadline. It returns an opaque receipt and status, never the value, to the browser. A successful submission marks the Note handled immediately, in the same transaction as its durable receipt. Reloading keeps the Note handled. Completion does not consume the value: the target Session can still claim it once until a restart or deadline discards it. A rejected submission leaves the Note pending. A completed Note rejects new submissions; request a fresh Note when another handoff is needed.
4. The receiving live Session retrieves the value once through a local command that feeds it to a tool on standard input. The command opens a short-lived loopback receiver with a random nonce; the daemon sends the value to that receiver. No HTTP response contains the value, and the command never prints it. A different Session, a stale Session, a browser read, and a repeated claim are refused.
5. The value is excluded from durable receipts, audit events, request logs, and diagnostic error strings. Limits and error codes mention size or state, never content. The agent must keep the tool's own output and logs from repeating the value.

## Why this boundary

`human_interventions.go` persists Note title, summary, action, reason and detail. `Interventions.tsx` sends an answer Note's draft through `deliverUntilSeen`, which becomes a normal Session message. Neither path can carry a secret. The Note can safely provide the request, the input, and a non-sensitive delivery status. The separate payload needs a short-lived, single-use machine-side handoff.

The protection is against incidental retention and disclosure by Clawdline's normal collaboration surfaces and Cloud. It cannot protect a secret from the tool or service to which the person intentionally gives it, or from a compromised receiving machine or Session. The local machine credential remains the trust boundary: another local process holding it could assert the target conversation's identity. The UI should name that Session before enabling submission.

## Person-facing steps

1. Open the **Secret Handoff** request in the intended Session and check its purpose and Session name.
2. Paste the value into the dedicated field and submit. The button shows progress, then the Note moves to recently handled as soon as the machine receives the value, before the panel folds away. Clawdline never shows the value again.
3. The Session uses the one-time handoff. If it expires or the machine restarts before use, ask the Session for a fresh Secret Handoff Note.
