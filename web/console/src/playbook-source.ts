import type { PlaybookSource } from "./legacy/snippets-bridge.js"
import type { ProjectFile } from "./pages/projects/project-files-api.js"
import { playbookWord, playbookWordFor } from "./playbook-words.js"

import type { CatalogTag } from "./catalog.js"
const commandName = (file: ProjectFile) => file.name.replace(/\.md$/i, "")

export function playbookSourceTitle(file: ProjectFile): string {
  return file.kind === "command" && (!file.title || file.title === commandName(file))
    ? commandName(file).replace(/[-_]+/g, " ").replace(/\b[a-z]/g, (letter) => letter.toUpperCase())
    : file.title || file.location.split("/").filter(Boolean).at(-2) || file.name
}

export function playbookSourceText(file: ProjectFile, language?: CatalogTag): { title: string; body: string } {
  const title = playbookSourceTitle(file)
  const format = language ? (key: "commandPrompt" | "skillPrompt", holes: Record<string, string>) => playbookWordFor(language, key, holes) : playbookWord
  return { title, body: file.kind === "command"
    ? format("commandPrompt", { name: commandName(file) })
    : format("skillPrompt", { name: title }) }
}

export function playbookSourceMatches(saved: { source: PlaybookSource }, file: ProjectFile): boolean {
  return file.id === saved.source.file_id && file.kind === saved.source.kind && file.status === "ready"
}
