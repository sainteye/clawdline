/** A reply's first words as a one-line preview: Markdown marks are dropped, not shown. */
export function plainPreview(text:string){
  return text.replace(/\[([^\]]*)\]\([^)]*\)/g,"$1").replace(/^\s*(?:[-*+]|\d+\.|#{1,6}|>)\s+/gm,"").replace(/(\*\*|__|`)/g,"").replace(/\s+/g," ").trim()
}

const sessionReportLineLimit=80
/** The one sentence a Session report shows while its card is closed. The roster
 * preview uses the same sentence, so a person finds the line they saw there. */
export function sessionReportLine(text:string){
  const first=text.split("\n").map(plainPreview).find(Boolean)??""
  const sentence=/^.*?(?:[\u3002\uff01\uff1f]|[.!?](?=\s|$))/u.exec(first)?.[0]??first
  const chars=Array.from(sentence)
  return chars.length>sessionReportLineLimit?chars.slice(0,sessionReportLineLimit-1).join("").trimEnd()+"…":sentence
}
