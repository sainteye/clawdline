/**
 * The start and resume routes, as the daemon reads them (api/v1/projects.schema.json).
 *
 * A persona is the route's last two segments, `/as/{persona}`, which the
 * daemon reads only after an assistant is named (docs/personas.md), so a
 * persona brings the assistant with it: `claude` when none was chosen, the
 * daemon's own default. Clawdline Cloud reads the same paths
 * (`cloud/relay-writer.ts` `writeRoute`), so one spelling serves both.
 */

export function startPath(place: string, assistant?: string | null, model?: string, persona?: string): string {
  let path = "/v1/places/" + encodeURIComponent(place) + "/start"
  if (assistant || model || persona) path += "/" + encodeURIComponent(assistant || "claude")
  if (model) path += "/" + encodeURIComponent(model)
  if (persona) path += "/as/" + encodeURIComponent(persona)
  return path
}

export function resumePath(place: string, session: string, assistant?: string | null, persona?: string): string {
  let path = "/v1/places/" + encodeURIComponent(place) + "/resume/"
  if (assistant || persona) path += encodeURIComponent(assistant || "claude") + "/"
  path += encodeURIComponent(session)
  if (persona) path += "/as/" + encodeURIComponent(persona)
  return path
}
