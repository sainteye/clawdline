import { useId, useState } from "react"
import type { VisitPoint } from "./visits.js"
import type { Say } from "./words.js"

/** One UTC line per metric. `unit` says whether each point is an hour or a whole day. */
export function HourlyVisitChart({ title, points, unit, duration = false, summary, say, locale }: { title: string; points: VisitPoint[]; unit: "hour" | "day"; duration?: boolean; summary?: string; say: Say; locale: string }) {
  const id=useId(), [selected,setSelected]=useState<number|null>(null)
  const number=new Intl.NumberFormat(locale,{maximumFractionDigits:1})
  const value=(v:number|null)=>v===null?"—":duration?say("visitsSeconds",{n:number.format(v)}):number.format(v)
  const dateTime=new Intl.DateTimeFormat(locale,unit==="day"?{timeZone:"UTC",month:"2-digit",day:"2-digit"}:{timeZone:"UTC",month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",hourCycle:"h23"})
  const time=(at:string)=>dateTime.format(new Date(at))
  const max=Math.max(1,...points.flatMap(p=>p.value===null?[]:[p.value]))
  const W=600, H=150, left=6, right=W-6, top=18, bottom=H-8
  const x=(i:number)=>points.length===1?W/2:left+i*(right-left)/(points.length-1)
  const y=(v:number)=>bottom-v/max*(bottom-top)
  const lines:string[][]=[]
  for (const [i,p] of points.entries()) {
    if (p.value===null) { if (lines.at(-1)?.length) lines.push([]); continue }
    if (!lines.length) lines.push([])
    lines[lines.length-1].push(`${x(i)},${y(p.value)}`)
  }
  const index=points.length?Math.min(selected??points.length-1,points.length-1):0, active=points[index]
  return <figure className="adm-panel adm-chart" aria-labelledby={id}>
    <figcaption className="adm-chart-head"><span id={id} className="adm-chart-title">{title}</span><span className="adm-chart-range">{say(unit==="day"?"visitsDaily":"visitsHourly")}</span></figcaption>
    {summary?<p className="adm-chart-summary">{summary}</p>:null}
    {!points.length?<p className="adm-fine">{say("visitsHourlyEmpty")}</p>:<>
      <div className="adm-chart-legend"><span>{time(active.at)} UTC · {value(active.value)}</span></div>
      <div className="adm-chart-body">
        <span className="adm-chart-max" aria-hidden="true">{value(max)}</span>
        <svg className="adm-svg" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={`${title} · ${say(unit==="day"?"visitsDailyHelp":"visitsHourlyHelp")}`} tabIndex={0}
          onPointerMove={e=>{const r=e.currentTarget.getBoundingClientRect();setSelected(Math.max(0,Math.min(points.length-1,Math.round(((e.clientX-r.left)/r.width*W-left)/(right-left)*(points.length-1)))))}}
          onPointerLeave={()=>setSelected(null)} onFocus={()=>setSelected(points.length-1)}
          onKeyDown={e=>{if(e.key==="ArrowLeft"||e.key==="ArrowRight"){e.preventDefault();setSelected(Math.max(0,Math.min(points.length-1,index+(e.key==="ArrowLeft"?-1:1))))}}}>
          <line x1="0" x2={W} y1={bottom} y2={bottom} className="adm-axis"/>
          <line x1="0" x2={W} y1={top} y2={top} className="adm-axis adm-axis-top"/>
          {lines.filter(line=>line.length>1).map((line,i)=><polyline key={i} points={line.join(" ")} className="adm-line-daily"/>)}
          {lines.filter(line=>line.length===1).map((line,i)=>{const [cx,cy]=line[0].split(",");return <circle key={i} cx={cx} cy={cy} r="2.3" className="adm-line-point"/>})}
          {active.value!==null?<circle cx={x(index)} cy={y(active.value)} r="3.6" className="adm-line-point adm-line-peak"/>:null}
        </svg>
      </div>
      <div className="adm-chart-scale" aria-hidden="true"><span>{time(points[0].at)}</span><span>{time(points[points.length-1].at)}</span></div>
      <details className="adm-details"><summary>{say("chartTable")}</summary><div className="adm-table-wrap"><table className="adm-table adm-table-days">
        <thead><tr><th scope="col">{say(unit==="day"?"visitsDay":"visitsHour")}</th><th scope="col" className="num">{title}</th></tr></thead>
        <tbody>{[...points].reverse().map(p=><tr key={p.at}><td>{time(p.at)} UTC</td><td className="num">{value(p.value)}</td></tr>)}</tbody>
      </table></div></details>
    </>}
  </figure>
}
