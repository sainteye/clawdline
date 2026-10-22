# Secret Handoff

Use **Secret Handoff** when a Session needs a password, credential, or API key for a tool. The Session asks in a Note. You provide the value in that Note's dedicated field; it is never sent as a chat message.

## Hand off a value

1. Open the Session that asked for the value. Check its name, the Note's title and reason, and the credential name immediately above the input. The requesting agent sets that name.
2. Paste the value into the field named for the requested credential. If the Agent did not name it, the label reads **Secret value**. The field always accepts one-line and multi-line values and shows the text while you enter it; check your surroundings before pasting.
3. Select **Hand off securely**. The button shows progress, then the Note moves to recently handled as soon as the machine receives the value, before the panel folds away. A page reload keeps the Note handled. Clawdline does not display the value again.
4. The named Session can use the value once, within 15 minutes. The Note is already handled; this does not prevent the Session from using the value. If the machine restarts or the time runs out first, ask the Session for a fresh Secret Handoff Note.

Do not paste the value into a suggested reply, a normal message, a document, or a command argument. A normal message becomes part of the conversation record. A Secret Handoff value stays in the target machine's memory until use or expiry; Clawdline Cloud carries the submission encrypted. The receiving tool may still store or print what it receives, so use a tool you trust and follow its own credential guidance.

If the page says the handoff did not complete, check whether the Session received it before submitting the value again. The request may have reached the machine even if the confirmation did not return.

For the technical boundary and the agent command, see [the design note](../secret-handoff.md).
