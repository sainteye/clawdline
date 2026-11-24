import type * as W from "@clawdline/contract"

type Definition=Pick<W.ProjectAssistantRoutineDefinition,"id"|"version"|"name"|"instruction"|"triggers"|"timeout_seconds"|"enabled">

/** The fields the routine editor writes. Run state (next due, last run) is not among them. */
export function sameRoutineEdit(a:Definition,b:Definition):boolean{
  return a.id===b.id&&a.name===b.name&&a.instruction===b.instruction&&a.timeout_seconds===b.timeout_seconds&&
    a.enabled===b.enabled&&JSON.stringify(a.triggers)===JSON.stringify(b.triggers)
}

/**
 * The revision an editor save sends. A scheduled fire advances the
 * definition's version without touching what the person edits, so the save
 * rebases on the live definition while its edited fields still equal the
 * ones the editor opened with; otherwise it keeps the opened revision and the
 * source refuses a real conflict.
 */
export function routineEditBase(opened:Definition|null|undefined,live:readonly Definition[]|undefined):number{
  if(!opened)return 0
  const fresh=live?.find(d=>d.id===opened.id)
  return fresh&&sameRoutineEdit(fresh,opened)?fresh.version:opened.version
}
