import { SHARE_LIMITS, shareMetadata, metadataHTML } from "../src/community/share-metadata.ts"
const API = "https://api.clawdline.com"
export function publicResource(path, locale) {
  const topic = /^\/community\/topics\/(sq_[a-f0-9]{40})\/?$/.exec(path)
  if (topic) return { url: API + "/v1/community/topics/" + topic[1], key: "topic", id: topic[1] }
  const faq = /^\/help\/faq\/([a-zA-Z0-9_-]+)\/?$/.exec(path)
  if (faq && faq[1].length <= SHARE_LIMITS.textChars) return { url: API + "/v1/faq/" + faq[1] + "?locale=" + locale, key: "article", id: faq[1] }
  return null
}
export function requestLocale(header = "") {
  const choices = header.split(",").map(value=>{const [tag,...rest]=value.trim().split(";");const q=rest.find(v=>v.trim().startsWith("q="));return{tag,q:q?Number(q.trim().slice(2)):1}}).filter(v=>v.q>0&&v.q<=1).sort((a,b)=>b.q-a.q)
  return /^(zh-Hant|zh-TW|zh-HK)(?:-|$)/i.test(choices[0]?.tag??"") ? "zh-Hant" : "en"
}
async function readPublic(response) {
  if (!response.ok || !response.headers.get("content-type")?.includes("application/json") || !response.body) return null
  const reader=response.body.getReader(),decoder=new TextDecoder();let text="",bytes=0
  try {
    while (true) {const {done,value}=await reader.read();if(done)break;bytes+=value.byteLength;if(bytes>SHARE_LIMITS.responseBytes)throw new Error("share_response_too_large");text+=decoder.decode(value,{stream:true})}
    return JSON.parse(text+decoder.decode())
  } finally {await reader.cancel().catch(()=>{})}
}
export async function resolveMetadata(request, get = fetch) {
  const url=new URL(request.url),locale=requestLocale(request.headers.get("accept-language")??""),resource=publicResource(url.pathname,locale)
  let content
  if (resource) {
    const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),SHARE_LIMITS.fetchMs)
    try {
      // Never forward cookies, authorization, query parameters, or member/Admin endpoints.
      const response=await get(resource.url,{method:"GET",headers:{Accept:"application/json"},credentials:"omit",redirect:"error",cache:"no-store",signal:controller.signal})
      const data=await readPublic(response),row=data?.[resource.key]
      if (row && row.id===resource.id && typeof row.title==="string") {
        const body=resource.key==="topic"?row.messages?.[0]?.body:row.body
        if(typeof body==="string")content={title:row.title,body,locale:typeof (resource.key==="topic"?row.source_language:row.locale)==="string"?(resource.key==="topic"?row.source_language:row.locale):locale}
      }
    } catch { /* A preview failure must not prevent the console from opening. */ }
    finally {clearTimeout(timer)}
  }
  return shareMetadata(url.href,locale,content)
}
export default {
  async fetch(request, env) {
    const url=new URL(request.url)
    if (!/^(GET|HEAD)$/.test(request.method) || !/^\/(community|help)(\/|$)/.test(url.pathname)) return env.ASSETS.fetch(request)
    const [response,metadata]=await Promise.all([env.ASSETS.fetch(request),resolveMetadata(request)])
    if(!response.ok || !response.headers.get("content-type")?.includes("text/html"))return response
    const rewriter=new HTMLRewriter().on("title",{element(e){e.remove()}}).on('link[rel="canonical"]',{element(e){e.remove()}})
      .on('meta[name="description"],meta[name="robots"],meta[property^="og:"],meta[name^="twitter:"]',{element(e){e.remove()}})
      .on("head",{element(e){e.append(metadataHTML(metadata),{html:true})}})
    const result=rewriter.transform(response),headers=new Headers(result.headers)
    headers.delete("etag");headers.delete("content-length");headers.set("cache-control","no-store");headers.set("x-robots-tag",metadata.robots)
    return new Response(request.method==="HEAD"?null:result.body,{status:result.status,headers})
  },
}
