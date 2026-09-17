import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import Bar from "./Bar.js"
import { askShell } from "./shell.js"

// The palette only. The console's twenty-nine stylesheets describe a page with
// a header, a drawer and a session list in it; this window is one card, and
// `Panel.swift`'s own geometry is in `bar.css`. What is shared is the colours —
// `--accent` in `tokens.css` is `Style.accent` to the byte — so they cannot
// drift apart.
import "../legacy/tokens.css"
import "./bar.css"

const host = document.getElementById("bar")
if (!host) throw new Error("no #bar in the document")

createRoot(host).render(
  <StrictMode>
    <Bar />
  </StrictMode>,
)

// The shell keeps the window hidden until the card exists, so that a summon
// never shows an empty rectangle while React mounts. Said after the first paint
// rather than after the render call, which returns before anything is drawn.
requestAnimationFrame(() => {
  requestAnimationFrame(() => askShell({ kind: "ready" }))
})
