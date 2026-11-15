/** Embed the authored painting and cover; the public packer still writes exactly two files. */
import {readFile,writeFile} from "node:fs/promises"
const root=new URL("./",import.meta.url)
const painting=await readFile(new URL("painting.jpg",root))
const path=new URL("index.html",root),html=await readFile(path,"utf8")
const pattern=/(<img id="paintingImage" src=")[^"]*(")/
if(!pattern.test(html))throw Error("Painting slot is missing")
await writeFile(path,html.replace(pattern,(_,before,after)=>before+"data:image/jpeg;base64,"+painting.toString("base64")+after))
const manifestPath=new URL("manifest.json",root),manifest=JSON.parse(await readFile(manifestPath,"utf8"))
manifest.cover="data:image/png;base64,"+(await readFile(new URL("cover.png",root))).toString("base64")
await writeFile(manifestPath,JSON.stringify(manifest,null,2)+"\n")
