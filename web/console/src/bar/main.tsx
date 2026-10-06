import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import Bar from "./Bar.js"
import { askShell } from "./shell.js"
import { bootLocalCatalog } from "../catalog.js"

// The palette only. The console's twenty-nine stylesheets describe a page with
// a header, a drawer and a session list in it; this window is one card, and
// `Panel.swift`'s own geometry is in `bar.css`. What is shared is the colours —
// `--accent` in `tokens.css` is `Style.accent` to the byte — so they cannot
// drift apart.
import "../legacy/tokens.css"
import "./bar.css"

const host = document.getElementById("bar")
if (!host) throw new Error("no #bar in the document")

void bootLocalCatalog().finally(() => {
  createRoot(host).render(
    <StrictMode>
      <Bar />
    </StrictMode>,
  )
  document.documentElement.classList.remove("booting")
  // Tell the shell only after React has painted the translated card.
  requestAnimationFrame(() => {
    requestAnimationFrame(() => askShell({ kind: "ready" }))
  })
})
