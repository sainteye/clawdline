import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import App from "./App.js"
import "./styles.css"

const host = document.getElementById("root")
if (!host) throw new Error("no #root in the document")

createRoot(host).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
