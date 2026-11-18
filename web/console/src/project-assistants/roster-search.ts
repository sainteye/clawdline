import type * as W from "@clawdline/contract"

export function matchesRosterQuery(item:W.ProjectAssistantBotSummary,query:string,projects:ReadonlyMap<string,string>):boolean {
  const needle=query.toLocaleLowerCase()
  return [item.bot.name,projects.get(JSON.stringify([item.bot.key.machine_id,item.bot.project.project_id]))??""].some(value=>value.toLocaleLowerCase().includes(needle))
}

/** Snapshot list queries match Bot names. Project labels belong to this viewer,
 * so keep that source's ordinary pagination and filter its rows locally. */
export function snapshotRosterQuery(machine:string,query:string,projects:ReadonlyMap<string,string>):string {
  if(!query)return ""
  const needle=query.toLocaleLowerCase()
  for(const [key,label] of projects)if(label.toLocaleLowerCase().includes(needle)&&JSON.parse(key)[0]===machine)return ""
  return query
}
