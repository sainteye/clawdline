import { ClawdlineClient } from "@clawdline/core"

/**
 * One client for the whole console.
 *
 * It takes no base URL: the console is served by the daemon it talks to, and in
 * development Vite proxies /v1 to the same place. The code that runs in the
 * browser is therefore the same either way, and no build flag decides where the
 * data comes from.
 */
export const client = new ClawdlineClient()
