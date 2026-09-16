import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import App from "./App.js"

// The original stylesheet, in the original's order. These are byte-for-byte
// copies (see legacy/README.md) and the load order matters: tokens before
// anything that reads them, responsive last so it can override.
import "./legacy/tokens.css"
import "./legacy/base.css"
import "./legacy/header.css"
import "./legacy/shell.css"
import "./legacy/list.css"
import "./legacy/detail.css"
import "./legacy/transcript.css"
import "./legacy/composer.css"
import "./legacy/live.css"
import "./legacy/agents.css"
import "./legacy/status-line.css"
import "./legacy/responsive.css"
import "./shell.css"

const host = document.getElementById("root")
if (!host) throw new Error("no #root in the document")

createRoot(host).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
