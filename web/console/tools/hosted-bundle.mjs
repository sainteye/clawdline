import { createRequire } from "node:module"
import { productionCloud, validateHostedDeclaration } from "./hosted-declaration.mjs"

const require = createRequire(new URL("../../package.json", import.meta.url))
const ts = require("typescript")

// Inspect syntax only; never execute code downloaded from the site. CloudGate
// text by itself also appears in a broken build, so require the actual complete
// production declaration among the bundle's compiled string literals.
export function validateHostedBundle(source) {
  const tree = ts.createSourceFile("main.js", source, ts.ScriptTarget.Latest, false, ts.ScriptKind.JS)
  if (tree.parseDiagnostics.length) throw new Error("The served main bundle is not valid JavaScript.")
  let found = false
  function visit(node) {
    if (found) return
    if (ts.isStringLiteral(node) && node.text.startsWith("{")) {
      try {
        const config = validateHostedDeclaration(node.text)
        found = config?.appOrigin === productionCloud.app_origin &&
          config?.apiOrigin === productionCloud.api_origin && config?.relayURL === productionCloud.relay_url
      } catch { /* Most string literals are ordinary UI text, not a declaration. */ }
    }
    if (!found) ts.forEachChild(node, visit)
  }
  visit(tree)
  if (!found) throw new Error("The served bundle has no usable production Cloud declaration.")
  return true
}
