import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { DoorGate } from "./door/Door.js"
import { keepSamePageLinksHere } from "./same-page-links.js"

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

// Both consoles can be a Home Screen app, and in one a link to the console
// itself must not leave for a browser that holds none of its keys
// (same-page-links.ts). Installed before anything draws a link.
keepSamePageLinksHere(window)

// Which console this build is. Served by the daemon it is that daemon's, and
// the door stands in front of it: until the daemon says the browser is let in,
// the console is not drawn at all (door/Door.tsx). Built for Clawdline Cloud it
// reads a machine across the relay instead, and the Cloud gate stands there
// (cloud/CloudGate.tsx). The discriminator is the build's declaration, never a
// guess from the hostname — the daemon's own page is also served from hosts
// that are not localhost, through a tunnel — which is the Swift console's rule
// (`legacy/js/net/cloud-boot.js`). The declaration is the object that console's
// build wrote into `window.__clawdlineCloud`, given here at build time:
//
//   VITE_HOSTED_CONSOLE='{"v":1,"app_origin":"https://…","api_origin":"https://…","relay_url":"wss://…"}' npm run build
//
// The gate is loaded only when declared, so the daemon's console does not
// carry the relay client.
const declared = import.meta.env.VITE_HOSTED_CONSOLE as string | undefined
if (declared) {
  void import("./cloud/CloudGate.js").then(({ CloudGate }) =>
    createRoot(host).render(
      <StrictMode>
        <CloudGate declared={declared} />
      </StrictMode>,
    ),
  )
} else {
  createRoot(host).render(
    <StrictMode>
      <DoorGate />
    </StrictMode>,
  )
}
