import type { HelpDoc } from './copilot-help.js'

/**
 * The user guide as the copilot reads it, bundled as its own chunk and loaded
 * the first time a call asks how something is used.
 */
const GUIDE = import.meta.glob('../../../../docs/user/*.md', { query: '?raw', import: 'default' })

export async function loadHelpDocs(): Promise<HelpDoc[]> {
  const docs = await Promise.all(Object.entries(GUIDE).map(async ([path, load]) => ({
    name: (path.split('/').pop() ?? path).replace(/\.md$/, ''),
    text: String(await load()),
  })))
  return docs.filter((d) => d.name !== 'README')
}
