import { useState, type RefObject } from "react"
import * as L from "../legacy/bridge.js"
import type { MarkupPicture } from "../session/ImageMarkup.js"
import { groupWord } from "./Groups"

// The room carries as many pictures per line as a one-to-one send (MaxRoomPictures).
export const MaxRoomPictures=4
const readPicture=(file:File)=>new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result));reader.onerror=()=>reject(reader.error);reader.readAsDataURL(file)})

// A picture-only line sends no text; a line without pictures sends no images field.
export const pictureField=(images:readonly MarkupPicture[])=>images.length?{images:images.map(picture=>picture.url)}:{}

// The pictures a room draft carries. Any change drops the held retry request,
// so it is not resent with a different set of pictures; sent() also drops it
// once the line is written.
export function useRoomPictures<T>(unavailable:string,held:RefObject<T|null>){
  const invalidate=()=>{held.current=null}
  const [images,setImages]=useState<MarkupPicture[]>([]),[error,setError]=useState("")
  const attach=(files:readonly File[])=>{
    const pictures=files.filter(file=>/^image\/(png|jpeg|gif|webp)$/.test(file.type))
    if(!pictures.length)return
    if(images.length+pictures.length>MaxRoomPictures){setError(groupWord("groupPicturesLimit").replace("{count}",String(MaxRoomPictures)));return}
    setError("");invalidate()
    void Promise.all(pictures.map(readPicture)).then(urls=>setImages(old=>[...old,...urls.map(url=>({id:crypto.randomUUID(),url}))].slice(0,MaxRoomPictures))).catch(failure=>setError(L.failureSentence(failure,unavailable)))
  }
  return {
    images,error,
    attachWhen:(enabled:boolean)=>enabled?attach:undefined,
    remove:(id:MarkupPicture["id"])=>{setImages(old=>old.filter(picture=>picture.id!==id));invalidate()},
    replace:(id:MarkupPicture["id"],url:string)=>{setImages(old=>old.map(picture=>picture.id===id?{...picture,url}:picture));invalidate()},
    sent:()=>{setImages([]);setError("");invalidate()},
  }
}
