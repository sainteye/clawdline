import type * as W from "@clawdline/contract"

const HEX = /^#[0-9a-f]{6}$/i

/**
 * A Project's mark, drawn as the new-Session sheet draws it (pixel cells, or a
 * supplied SVG), without the legacy canvas bridge so the module stays
 * removable. A supplied SVG only ever renders in an image context, which runs
 * no script and loads nothing external. Nothing is drawn when there is no mark.
 */
export function ProjectMark({icon,size=16}:{icon?:W.Icon|null;size?:number}) {
  if(!icon)return null
  if(icon.svg){
    let source=""
    try{
      const doc=new DOMParser().parseFromString(icon.svg,"image/svg+xml"),root=doc.documentElement
      if(root.localName!=="svg"||doc.querySelector("parsererror"))return null
      root.setAttribute("xmlns","http://www.w3.org/2000/svg")
      source=new XMLSerializer().serializeToString(root)
    }catch{return null}
    return <img className="pa-project-mark" src={"data:image/svg+xml;charset=utf-8,"+encodeURIComponent(source)} width={size} height={size} alt="" aria-hidden="true"/>
  }
  const cells=Array.isArray(icon.cells)?icon.cells.slice(0,64):[]
  const cols=Math.min(64,Math.max(0,...cells.map(r=>Array.isArray(r)?r.length:0)))
  if(!cells.length||!cols)return null
  const rects=cells.flatMap((row,y)=>Array.isArray(row)?row.slice(0,64).map((c,x)=>c&&HEX.test(c)?<rect key={`${x}.${y}`} x={x} y={y} width="1.02" height="1.02" fill={c}/>:null):[])
  return <svg className="pa-project-mark" viewBox={`0 0 ${cols} ${cells.length}`} width={size} height={size*cells.length/cols} shapeRendering="crispEdges" aria-hidden="true">{rects}</svg>
}
