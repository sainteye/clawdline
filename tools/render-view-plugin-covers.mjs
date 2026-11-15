/** Render authored SVG covers into embedded RGBA8 PNGs using an existing local Playwright. */
import {readFile,writeFile} from "node:fs/promises"
import {resolve} from "node:path"
import {pathToFileURL} from "node:url"
import {deflateSync,crc32} from "node:zlib"
if (!process.env.PLAYWRIGHT_MODULE || !process.argv[2]) throw Error("Set PLAYWRIGHT_MODULE and pass one or more plugin source directories")
const {chromium}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const browser=await chromium.launch({headless:true})
const chunk=(name,bytes)=>{const type=Buffer.from(name),head=Buffer.alloc(4),tail=Buffer.alloc(4);head.writeUInt32BE(bytes.length);tail.writeUInt32BE(crc32(Buffer.concat([type,bytes])));return Buffer.concat([head,type,bytes,tail])}
try {
 for(const directory of process.argv.slice(2)) {
  const source=resolve(directory),svg=await readFile(source+"/cover.svg","utf8"),page=await browser.newPage()
  try {
   await page.route("**/*",route=>route.abort())
   const pixels=await page.evaluate(async svg=>{
    const image=new Image();image.src="data:image/svg+xml;base64,"+btoa(svg);await image.decode()
    const canvas=document.createElement("canvas");canvas.width=canvas.height=128
    const context=canvas.getContext("2d");context.drawImage(image,0,0,128,128)
    return Array.from(context.getImageData(0,0,128,128).data)
   },svg)
   const rgba=Buffer.from(pixels),scanlines=Buffer.alloc(128*(128*4+1))
   for(let y=0;y<128;y++)rgba.copy(scanlines,y*(128*4+1)+1,y*128*4,(y+1)*128*4)
   const header=Buffer.alloc(13);header.writeUInt32BE(128);header.writeUInt32BE(128,4);header[8]=8;header[9]=6
   const png=Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]),chunk("IHDR",header),chunk("IDAT",deflateSync(scanlines,{level:9})),chunk("IEND",Buffer.alloc(0))])
   const manifest=JSON.parse(await readFile(source+"/manifest.json","utf8"));manifest.cover="data:image/png;base64,"+png.toString("base64")
   await writeFile(source+"/manifest.json",JSON.stringify(manifest,null,2)+"\n")
   console.log(directory+": "+png.length+" cover bytes")
  } finally {await page.close()}
 }
} finally {await browser.close()}
