import { readCloudConfig } from "../src/legacy/js/net/cloud-boot.js"

// A production build command owns these public endpoints. Callers do not
// assemble a declaration or inherit an ambient boolean build switch.
export const productionCloud = Object.freeze({
  v: 1,
  app_origin: "https://app.clawdline.com",
  api_origin: "https://api.clawdline.com",
  relay_url: "wss://relay.clawdline.com/v1/connect",
})

export function validateHostedDeclaration(declared) {
  if (declared === undefined || declared === "") return null
  try {
    const config = readCloudConfig({ __clawdlineCloud: JSON.parse(declared) })
    if (!config) throw new Error("a complete configuration object is required")
    return config
  } catch (error) {
    throw new Error(`VITE_HOSTED_CONSOLE is invalid: ${error.message}. Use npm run build:cloud for production.`)
  }
}
