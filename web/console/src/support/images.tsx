import { useEffect, useRef, useState, type ChangeEvent, type ClipboardEvent } from "react"
import { SUPPORT_UI_LIMITS as L, requestKey, SupportFailure } from "./api.js"
import { supportWord as w } from "./words.js"
import type { SupportImage } from "./model.js"

type Call = <T>(path: string, method?: string, body?: unknown, key?: string) => Promise<T>
type Item = { key: string; preview: string; blob?: Blob; image?: SupportImage }
export function useSupportImages(call: Call, origin: string, scope: string, publicDiscussion = false) {
  const base = publicDiscussion ? "/v1/community/images" : "/v1/support/images"
  const storageKey = "clawdline-support-image-draft:" + (publicDiscussion ? "public:" : "") + scope
  const [items,setItems] = useState<Item[]>(() => {
    try {
      const saved = JSON.parse(sessionStorage.getItem(storageKey) ?? "[]") as {key:string;image:SupportImage}[]
      return saved.filter(v => typeof v.key === "string" && /^si_[a-f0-9]{40}$/.test(v.image?.id)).slice(0,L.imageCount).map(v=>({...v,preview:origin+base+"/"+v.image.id}))
    } catch { return [] }
  })
  const [busy,setBusy] = useState(false),[error,setError] = useState<string|null>(null)
  const busyRef=useRef(false)
  const urls = useRef(new Set<string>())
  useEffect(()=>()=>{for(const url of urls.current)URL.revokeObjectURL(url)},[])
  useEffect(()=>{try{sessionStorage.setItem(storageKey,JSON.stringify(items.filter(v=>v.image).map(({key,image})=>({key,image}))))}catch{}},[items,storageKey])
  const add = async (files: File[]) => {
    if(busyRef.current)return
    if(items.length+files.length>L.imageCount){setError("support_image_limit");return}
    busyRef.current=true;setBusy(true);setError(null)
    const added:Item[]=[]
    try {
      for(const file of files){
        if(!["image/jpeg","image/png","image/webp"].includes(file.type)||file.size>L.imageRawBytes)throw new SupportFailure("support_image_invalid",true)
        const header=new Uint8Array(await file.slice(0,12).arrayBuffer())
        const jpeg=header[0]===255&&header[1]===216&&header[2]===255
        const png=[137,80,78,71,13,10,26,10].every((v,i)=>header[i]===v)
        const webp=String.fromCharCode(...header.slice(0,4))==="RIFF"&&String.fromCharCode(...header.slice(8,12))==="WEBP"
        if(!(file.type==="image/jpeg"&&jpeg||file.type==="image/png"&&png||file.type==="image/webp"&&webp))throw new SupportFailure("support_image_invalid",true)
        const bitmap=await createImageBitmap(file)
        const scale=Math.min(1,L.imageEdge/Math.max(bitmap.width,bitmap.height))
        const canvas=document.createElement("canvas");canvas.width=Math.max(1,Math.round(bitmap.width*scale));canvas.height=Math.max(1,Math.round(bitmap.height*scale))
        const context=canvas.getContext("2d");if(!context){bitmap.close();throw new Error("canvas")}
        context.fillStyle="#ffffff";context.fillRect(0,0,canvas.width,canvas.height);context.drawImage(bitmap,0,0,canvas.width,canvas.height);bitmap.close()
        const encode=()=>new Promise<Blob>((resolve,reject)=>canvas.toBlob(b=>b?resolve(b):reject(new Error("encoding")),"image/jpeg",0.85))
        let blob=await encode()
        if(blob.size>L.imageBytes){
          const smaller=document.createElement("canvas");smaller.width=Math.max(1,Math.floor(canvas.width/2));smaller.height=Math.max(1,Math.floor(canvas.height/2))
          smaller.getContext("2d")!.drawImage(canvas,0,0,smaller.width,smaller.height)
          blob=await new Promise<Blob>((resolve,reject)=>smaller.toBlob(b=>b?resolve(b):reject(new Error("encoding")),"image/jpeg",0.8))
        }
        if(blob.size>L.imageBytes)throw new SupportFailure("support_image_too_large",true)
        const preview=URL.createObjectURL(blob);urls.current.add(preview);added.push({key:requestKey(),preview,blob})
      }
      setItems(v=>[...v,...added])
    } catch(e){for(const item of added){URL.revokeObjectURL(item.preview);urls.current.delete(item.preview)}setError(e instanceof SupportFailure?e.code:"support_image_invalid")}
    finally{busyRef.current=false;setBusy(false)}
  }
  const upload=async()=>{
    if(busyRef.current)throw new SupportFailure("support_image_busy",true)
    busyRef.current=true;setBusy(true);setError(null)
    try {
      const images:SupportImage[]=[]
      for(const item of items){
        if(item.image){images.push(item.image);continue}
        const data=await new Promise<string>((resolve,reject)=>{const r=new FileReader();r.onload=()=>resolve(String(r.result).split(",")[1]!);r.onerror=()=>reject(new Error("file"));r.readAsDataURL(item.blob!)})
        const result=await call<{image:SupportImage}>(base,"POST",{mime:"image/jpeg",data,...publicDiscussion?{public_consent:true}:{}},item.key)
        if(!/^si_[a-f0-9]{40}$/.test(result.image?.id))throw new SupportFailure("support_response_unreadable",false)
        images.push(result.image);setItems(values=>values.map(v=>v.key===item.key?{...v,image:result.image}:v))
      }
      return images.map(v=>v.id)
    } catch(e){setError(e instanceof SupportFailure?e.code:"support_image_unavailable");throw e}
    finally{busyRef.current=false;setBusy(false)}
  }
  const remove=(key:string)=>{
    if(busyRef.current)return
    const item=items.find(v=>v.key===key);if(!item)return
    if(item.image)void call(base+"/"+item.image.id,"DELETE").catch(()=>{})
    if(urls.current.delete(item.preview))URL.revokeObjectURL(item.preview)
    setItems(values=>values.filter(v=>v.key!==key));setError(null)
  }
  const clear=()=>{setItems([]);try{sessionStorage.removeItem(storageKey)}catch{}}
  const paste=(event:ClipboardEvent)=>{const files=Array.from(event.clipboardData.files);if(files.length){event.preventDefault();void add(files)}}
  return {items,busy,error,add,upload,remove,clear,paste}
}
export function SupportImagePicker({images,disabled}:{images:ReturnType<typeof useSupportImages>;disabled:boolean}){
  return <div className="help-image-picker"><label className="help-image-pick" aria-disabled={disabled||images.busy}><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true" focusable="false"><path d="m8 12 6-6a3 3 0 0 1 4 4l-8 8a5 5 0 0 1-7-7l9-9"/></svg>{w(images.busy?"imageWorking":"attachImages")}<input aria-label={w("attachImages")} type="file" accept="image/jpeg,image/png,image/webp" multiple disabled={disabled||images.busy} onChange={(e:ChangeEvent<HTMLInputElement>)=>{void images.add(Array.from(e.target.files??[]));e.target.value=""}}/></label><small>{w("imageHint")}</small>{images.items.length?<ul className="help-image-previews">{images.items.map((item,index)=><li key={item.key}><img src={item.preview} alt={w("imageNumber",{number:String(index+1)})}/><button className="help-btn" type="button" disabled={disabled||images.busy} aria-label={w("removeImage",{number:String(index+1)})} onClick={()=>images.remove(item.key)}>×</button></li>)}</ul>:null}{images.error?<p role="alert">{w(images.error==="support_image_limit"?"imageLimit":images.error==="support_image_expired"?"imageExpired":"imageFailed")}</p>:null}</div>
}
export function SupportImageGallery({images,origin,question,admin,community=false}:{images:SupportImage[];origin:string;question:string;admin:boolean;community?:boolean}){
  const [open,setOpen]=useState<string|null>(null),dialog=useRef<HTMLDialogElement>(null)
  useEffect(()=>{const node=dialog.current;if(open&&node){node.showModal();return()=>node.close()}},[open])
  if(!images.length)return null
  return <><div className="help-message-images">{images.map((image,index)=>{const src=origin+(community?"/v1/community/topics/":(admin?"/su":"")+"/v1/support/questions/")+encodeURIComponent(question)+"/images/"+encodeURIComponent(image.id);return <a key={image.id} href={src} target="_blank" rel="noopener noreferrer" onClick={e=>{e.preventDefault();setOpen(src)}}><img src={src} alt={w("imageNumber",{number:String(index+1)})} loading="lazy"/></a>})}</div>{open?<dialog ref={dialog} className="help-image-overlay" aria-label={w("imagePreview")} onCancel={e=>{e.preventDefault();setOpen(null)}} onClick={e=>{if(e.target===e.currentTarget)setOpen(null)}}><button autoFocus className="help-btn" type="button" onClick={()=>setOpen(null)}>{w("closeImage")}</button><img src={open} alt={w("imagePreview")}/></dialog>:null}</>
}
