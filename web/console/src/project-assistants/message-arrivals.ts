import {useLayoutEffect,useRef,useState} from "react"
import {useReduceMotion} from "./codync-rendering"

/** Only new tail messages can enter. A snapshot's baseline, older pages and
 * edits at a known sequence leave the position and animation untouched. */
export function useMessageArrivals<T>({items,ready,following,seq,id,skip}:{
  items:readonly T[];ready:boolean;following:boolean;seq:(message:T)=>number;id:(message:T)=>string;skip?:(message:T)=>boolean
}) {
  const frontier=useRef<number|null>(null)
  const [arrivals,setArrivals]=useState<ReadonlySet<string>>(()=>new Set())
  const reduce=useReduceMotion()
  useLayoutEffect(()=>{
    if(!ready){
      frontier.current=null
      if(arrivals.size)setArrivals(new Set())
      return
    }
    const previous=frontier.current,next=items.reduce((max,item)=>Math.max(max,seq(item)),previous??0)
    frontier.current=next
    if(reduce){if(arrivals.size)setArrivals(new Set());return}
    if(previous===null||!following||next===previous)return
    const entered=items.filter(item=>seq(item)>previous&&!skip?.(item)).map(id)
    if(entered.length)setArrivals(old=>new Set([...old,...entered]))
  },[items,ready,following,seq,id,skip,reduce,arrivals.size])
  return {
    arrivals,
    finish:(id:string)=>setArrivals(old=>{if(!old.has(id))return old;const next=new Set(old);next.delete(id);return next}),
  }
}
