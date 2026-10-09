// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../../catalog.ts"
import type { NewSquadSkill } from "./api.js"
import { SKILL_BODY_BYTES } from "./skill-create.js"

export interface ImportedSkill {
  name: string
  purpose: string
  content: string
  files: NonNullable<NewSquadSkill["files"]>
}

const encodedLength = (text: string) => new TextEncoder().encode(text).byteLength
const safePath = (value: string) => value !== "" && !value.startsWith("/") && !/[\\:\r\n\0]/.test(value) &&
  value.split("/").every((part) => part !== "" && part !== "." && part !== "..")

function frontmatter(body: string, field: string): string {
  const match = body.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/)
  if (!match) return ""
  const lines = match[1].split(/\r?\n/)
  const index = lines.findIndex((row) => row.startsWith(field + ":"))
  if (index < 0) return ""
  const value = lines[index].slice(field.length + 1).trim()
  if (value === ">" || value === "|" || value === ">-" || value === "|-") {
    const parts: string[] = []
    for (const line of lines.slice(index + 1)) {
      if (line && !/^\s/.test(line)) break
      if (line.trim()) parts.push(line.trim())
    }
    return parts.join(" ")
  }
  return value.replace(/^['"]|['"]$/g, "")
}

/** Capture exact files once; a later change to the selected folder cannot alter a retry. */
export async function importSkillFiles(input: FileList | File[], folder: boolean): Promise<ImportedSkill> {
  const files = Array.from(input)
  if (!files.length) throw new Error(catalogWord("literal", "711deea02238"))
  const rows = files.map((file) => {
    const raw = folder ? file.webkitRelativePath || file.name : file.name
    const parts = raw.split("/")
    const relative = folder ? parts.slice(1).join("/") : raw
    return { file, relative }
  }).filter((row) => row.relative !== "")
  if (rows.some((row) => !safePath(row.relative))) throw new Error(catalogWord("literal", "de95647166ea"))
  const seen = new Set<string>()
  for (const row of rows) {
    const key = row.relative.toLocaleLowerCase()
    if (seen.has(key)) throw new Error(catalogWord("literal", "e861ebcd3d2a"))
    seen.add(key)
  }
  const main = rows.find((row) => row.relative === "SKILL.md")
  if (!main || (!folder && rows.length !== 1)) throw new Error(catalogWord("literal", "157f5f468213"))
  if (rows.reduce((sum, row) => sum + row.file.size, 0) > SKILL_BODY_BYTES) throw new Error(catalogWord("literal", "71a96c33c44f"))
  const content = await main.file.text()
  if (!content.trim() || encodedLength(content) > SKILL_BODY_BYTES) throw new Error(catalogWord("literal", "6d418d44972b"))
  const attachments: ImportedSkill["files"] = []
  for (const row of rows.filter((entry) => entry !== main).sort((a, b) => a.relative.localeCompare(b.relative))) {
    const bytes = new Uint8Array(await row.file.arrayBuffer())
    let binary = ""
    for (let offset = 0; offset < bytes.length; offset += 8192) binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
    attachments.push({ path: row.relative, content_base64: btoa(binary) })
  }
  const fallback = folder ? files[0].webkitRelativePath.split("/")[0] : "SKILL.md"
  return { name: frontmatter(content, "name") || fallback, purpose: frontmatter(content, "description") || catalogWord("literal", "751b770b11a9"), content, files: attachments }
}
