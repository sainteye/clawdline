import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { SettingsWindow } from "./SettingsWindow.js"

// Only this window's stylesheet. The console's twenty-nine copied sheets are
// the Swift app's *web page*; this document is the Swift app's *settings
// window*, which was never one, so loading them here would style a window
// against rules written for a different surface.
import "./window.css"

const host = document.getElementById("root")
if (!host) throw new Error("no #root in the settings window")

createRoot(host).render(
  <StrictMode>
    <SettingsWindow />
  </StrictMode>,
)
