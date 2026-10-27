import { readFileSync } from "node:fs"
import { validateHostedBundle } from "./hosted-bundle.mjs"

try {
  validateHostedBundle(readFileSync(0, "utf8"))
  console.log("hosted declaration: the served bundle contains the complete production configuration")
} catch (error) {
  console.error(`FAILED: ${error.message}`)
  process.exitCode = 1
}
