# See a machine from another device

Clawdline Cloud lets a paired browser see Sessions on your machines through an encrypted relay. Your machine needs to be connected for a fresh view. This walkthrough starts after [your first local Session](first-session.md) is visible.

1. On the machine you want to reach, open Clawdline's Cloud settings and sign in, turn Cloud on, then restart Clawdline so the setting takes effect.
2. In the browser or phone at app.clawdline.com, sign in to the same account and pair that browser with the machine. The pairing instructions appear on the machine or in its settings.
3. Select that machine in the hosted console, then open its Session list.

**Success:** the machine shows connected and its Session rows appear. Pairing lets the browser read. To send messages or act from Cloud, enable Cloud commands for that machine in its Remote settings. A read-only view is still useful when you want to check progress.

**If the machine is offline:** the hosted console may retain an older view. Do not treat it as a fresh result. Return to the machine and restore its connection, then check the machine's connection state again. **If reading works but sending does not:** check that the selected machine is paired and Cloud commands are enabled there. [Recovery steps](recovery.md).

## Use Sessions from another local machine

The CLI uses a separate Cloud viewer identity. On the machine where you will read, run `clawdline cloud viewer login`, approve the displayed device and capabilities in your Cloud account, then run `clawdline cloud viewer machines`. For each target machine, compare its fingerprint on that machine before running `clawdline cloud viewer pair --machine MACHINE_ID --fingerprint FINGERPRINT`. Run the printed one-time `clawdline cloud pair --offer` command on the target machine. `clawdline cloud viewer sessions` lists paired machines and their Session status. To use a browser, pair that browser separately; the hosted Console shows machines in its existing Session list.

An actionable Session has three values: machine ID, Session ID, and execution generation. Select the current row in the hosted Console or pass all three to `clawdline cloud viewer read --machine MACHINE_ID --session SESSION_ID --generation GENERATION`. The first read shows the newest transcript page. If it returns `next_before`, use `clawdline cloud viewer read --machine MACHINE_ID --session SESSION_ID --generation GENERATION --before NEXT_BEFORE` to fetch one older page. Repeat with the new cursor until none remains. Each page checks the same current execution and permission again. `send`, `answer`, `interrupt`, and `end` use the same target. A CLI action prints its request ID before sending. If its outcome is uncertain, query `clawdline cloud viewer receipt --machine MACHINE_ID --session SESSION_ID --generation GENERATION --action ACTION --request REQUEST_ID` before acting again. An offline or stale machine does not queue an action.

[Pairing, permissions, and revocation](remote-access.md) · [Cloud trust boundary](../cloud.md) · [Agent contract for Cloud actions](../agent-contract.md)
