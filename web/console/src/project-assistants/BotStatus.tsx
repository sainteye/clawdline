import type { ProjectAssistantSourceStatus } from "@clawdline/contract"
import type { Words } from "./words"
export function BotStatus({sources,w}:{sources:readonly ProjectAssistantSourceStatus[];w:Words}) {
  return <section className="pa-status"><h2>{w.status}</h2>{sources.map(s=><article className="pa-card" key={s.machine_id}><h3>{s.machine_id}</h3><p>{w[s.state]} · {w[s.access]}</p>{!s.fresh&&<p>{w.stale}</p>}{!s.capabilities.length&&<p>{w.noCapabilities}</p>}{s.capabilities.map((c,i)=><div key={i}><p>{c.agent} · {w.version}: {c.cli_version||w.unavailable} / {c.adapter_version||w.unavailable}</p><p>{w.readOnly}: {c.read_only_enforced?w.yes:w.no}</p><p>{w.memory}: {w[`memory_${c.native_memory}`]} · {c.native_memory_verified?w.yes:w.no}</p>{c.unavailable&&<p>{w.unavailable} ({c.unavailable.code})</p>}</div>)}</article>)}</section>
}
