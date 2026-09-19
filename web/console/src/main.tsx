import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { DoorGate } from "./door/Door.js"

// The original stylesheet, all of it, in the order index.html links it. These
// are byte-for-byte copies (see legacy/README.md). The first cut copied only the
// twelve the session list obviously needed, and the reviewer found the drawer
// unstyled as a result: its rules live in pages.css, which is not a file anyone
// would guess from the word "sidebar". So nothing is left out now. The last two
// are the ones the original loads from script after the rest, and they stay last.
import "./legacy/tokens.css"
import "./legacy/base.css"
import "./legacy/header.css"
import "./legacy/shell.css"
import "./legacy/list.css"
import "./legacy/schedules.css"
import "./legacy/detail.css"
import "./legacy/transcript.css"
import "./legacy/composer.css"
import "./legacy/live.css"
import "./legacy/agents.css"
import "./legacy/status-line.css"
import "./legacy/sheets.css"
import "./legacy/coordinator.css"
import "./legacy/door.css"
import "./legacy/responsive.css"
import "./legacy/pages.css"
import "./legacy/devices.css"
import "./legacy/projects.css"
import "./legacy/worktrees.css"
import "./legacy/usage.css"
import "./legacy/plan.css"
import "./legacy/ledger.css"
import "./legacy/board.css"
import "./legacy/timeline.css"
import "./legacy/session-board.css"
import "./legacy/documents.css"
import "./legacy/user-messages.css"
import "./legacy/snippets.css"

import "./shell.css"

const host = document.getElementById("root")
if (!host) throw new Error("no #root in the document")

// The door stands in front of the console: until this daemon says the browser
// is let in, the console is not drawn at all (door/Door.tsx).
createRoot(host).render(
  <StrictMode>
    <DoorGate />
  </StrictMode>,
)
