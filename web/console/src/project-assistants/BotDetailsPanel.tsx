import type * as W from "@clawdline/contract"
import type {Words} from "./words"

export function BotDetailsPanel({bot,projectName,machineName,onSettings,onRoutines,w}:{bot:W.ProjectAssistantBot;projectName?:string;machineName?:string;onSettings:()=>void;onRoutines:()=>void;w:Words}) {
  return <div className="pa-bot-details"><section><h3>{w.instructions}</h3><div className="pa-card"><p className="pa-info-instructions">{bot.instructions.text||w.noInstructions}</p><button type="button" onClick={onSettings}>{w.editInformation}</button></div></section><section><h3>{w.project}</h3><dl className="pa-card"><dt>{w.project}</dt><dd>{projectName||w.unnamedProject}</dd><dt>{w.machine}</dt><dd>{machineName||w.thisMachine}</dd><dt>{w.agent}</dt><dd>{bot.agent==="claude"?"Claude Code":"Codex"}</dd></dl></section><section><h3>{w.routines}</h3><button type="button" onClick={onRoutines}>{w.routines}</button></section></div>
}
