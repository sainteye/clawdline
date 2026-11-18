import {readFileSync} from 'node:fs'
import {fileURLToPath} from 'node:url'

/** Only the generated entry's same-origin stylesheets are deployment inputs. */
export function stylesheetPaths(html,origin){
 const base=new URL(origin),paths=new Set()
 for(const tag of html.replace(/<!--[\s\S]*?-->/g,'').matchAll(/<link\b[^>]*>/gi)){
  const attrs=Object.fromEntries([...tag[0].matchAll(/([\w:-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/g)].map(m=>[m[1].toLowerCase(),m[2]??m[3]??m[4]]))
  if(!attrs.rel?.toLowerCase().split(/\s+/).includes('stylesheet'))continue
  if(!attrs.href)throw new Error('hosted_stylesheet_missing_href')
  const url=new URL(attrs.href,base)
  if(url.origin!==base.origin||url.username||url.password||!url.pathname.startsWith('/assets/')||!url.pathname.endsWith('.css'))throw new Error('hosted_stylesheet_unexpected_location')
  paths.add(url.pathname+url.search)
 }
 if(!paths.size)throw new Error('hosted_stylesheet_missing')
 return [...paths]
}

/** An HTTP 200 SPA fallback is not a stylesheet, even with a misleading header. */
export function verifyStylesheet(path,headers,body){
 const types=[...headers.matchAll(/^content-type:\s*([^;\r\n]+)/gim)]
 const type=types.at(-1)?.[1].trim().toLowerCase()
 if(type!=='text/css')throw new Error(`hosted_stylesheet_wrong_type: ${path}: ${type??'missing'}`)
 if(!body.trim()||body.trimStart().startsWith('<'))throw new Error(`hosted_stylesheet_invalid_body: ${path}`)
}

if(process.argv[1]===fileURLToPath(import.meta.url)){
 try{
  const [mode,...args]=process.argv.slice(2)
  if(mode==='--list'&&args.length===2)for(const path of stylesheetPaths(readFileSync(args[1],'utf8'),args[0]))console.log(path)
  else if(mode==='--style'&&args.length===3){verifyStylesheet(args[0],readFileSync(args[1],'utf8'),readFileSync(args[2],'utf8'));console.log(`hosted stylesheet: ${args[0]} is CSS`)}
  else throw new Error('usage: --list <origin> <index-file> or --style <path> <headers-file> <body-file>')
 }catch(error){console.error(`FAILED: ${error.message}`);process.exitCode=1}
}
