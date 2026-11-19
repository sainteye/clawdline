import {useLayoutEffect,useRef,useState} from "react"
import {useReduceMotion} from "./codync-rendering"

/** New tail messages and messages released from the queue can enter. A baseline, older pages and
 * edits at a known sequence leave the position and animation untouched. */
export function useMessageArrivals<T>({items,ready,following,seq,id,skip,deferred}:{
  items:readonly T[];ready:boolean;following:boolean;seq:(message:T)=>number;id:(message:T)=>string;skip?:(message:T)=>boolean;deferred?:ReadonlySet<string>
}) {
  const frontier=useRef<number|null>(null)
  const previousDeferred=useRef<ReadonlySet<string>|undefined>(undefined)
  const [arrivals,setArrivals]=useState<ReadonlySet<string>>(()=>new Set())
  const reduce=useReduceMotion()
  useLayoutEffect(()=>{
    const waiting=previousDeferred.current
    previousDeferred.current=deferred
    if(!ready){
      frontier.current=null
      if(arrivals.size)setArrivals(new Set())
      return
    }
    const previous=frontier.current,next=items.reduce((max,item)=>Math.max(max,seq(item)),previous??0)
    frontier.current=next
    if(reduce){if(arrivals.size)setArrivals(new Set());return}
    if(previous===null||!following||next===previous&&!waiting?.size)return
    const entered=items.filter(item=>{
      const key=id(item)
      return (seq(item)>previous||waiting?.has(key))&&!deferred?.has(key)&&!skip?.(item)
    }).map(id)
    if(entered.length)setArrivals(old=>new Set([...old,...entered]))
  },[items,ready,following,seq,id,skip,deferred,reduce,arrivals.size])
  return {
    arrivals,
    finish:(id:string)=>setArrivals(old=>{if(!old.has(id))return old;const next=new Set(old);next.delete(id);return next}),
  }
}
