# Use Clawdline

Start with the job you want to finish. These pages describe what to look for in the console and what to do when the expected result is missing. You do not need an API route, a Session identifier, or an Agent command to follow them.

| I want to… | Read | I will know it worked when… |
| --- | --- | --- |
| Install Clawdline and see my first Session | [First Session](first-session.md) | The Session appears in the list and opens to its conversation. |
| Keep track of work and answer an Agent | [Sessions](sessions.md) · [Board](board.md) | The answer appears in the conversation or the Board item shows its next phase. |
| Check my machines from a phone or another computer | [See another machine](cloud-first-look.md) | The selected machine and its Sessions appear with a fresh connection state. |
| Understand a missing, offline, or unreadable state | [Recover from a problem](recovery.md) | The state changes after the named action, or you know which machine needs attention. |
| Control who can read or act | [Remote access and permissions](remote-access.md) · [Notifications](notifications.md) | The device or machine shows the intended permission and a test reaches you. |
| Set up more work | [Projects](projects.md) · [Schedules](schedules.md) | The project or scheduled task appears in its list. |
| Get a second machine ready to work | [Set up another machine with an Agent](new-machine.md) | The new machine lists your projects with their icons and skills. |
| Operate or develop the daemon | [Installation details](install.md) · [Technical reference](../README.md#how-it-is-designed) | The command reports the expected result. |

Clawdline runs on your machine. Clawdline Cloud is optional and relays encrypted data when you enable it. A machine that is **offline** cannot provide a fresh Session view or accept a new command until it reconnects. [What the states mean](../contract-model.md#shared-terms-and-states).

For Agent actions, use the guide included in the **current machine's** Clawdline binary: `clawdline guide`. The [Agent contract map](../agent-contract.md) explains where its technical facts live. Human pages explain goals and visible outcomes; they are not instructions for an Agent to infer a route or authorization.
