import type { HelpSection } from './copilot-help.js'

/** Shared by the call and the durable workspace; no call is required to read the guide. */
export async function loadCopilotHelp(): Promise<HelpSection[]> {
  const [{ loadHelpDocs }, { helpSections }] = await Promise.all([import('./copilot-help-docs.js'), import('./copilot-help.js')])
  return helpSections(await loadHelpDocs(), document.documentElement.lang || 'en')
}
