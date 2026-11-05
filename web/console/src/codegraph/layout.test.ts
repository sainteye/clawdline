import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's behavior runner loads source extensions.
import { layoutGraph, graphSelection } from "./layout.ts"
const nodes = [
 {id:"a",label:"Caller",file:"a.ts",location:"L1",community:"1"},
 {id:"b",label:"Callee",file:"b.ts",location:"L2",community:"2"},
]
const links = [{source:"a",target:"b",relation:"calls",confidence:"EXTRACTED"}]
test("a graph keeps every node in bounds with stable community positions", () => {
 const placed=layoutGraph(nodes)
 assert.deepEqual(placed,layoutGraph(nodes));assert.equal(placed.length,2)
 for(const node of placed) {assert.ok(node.x>=0 && node.x<=900 && node.y>=0 && node.y<=600)}
 assert.deepEqual(layoutGraph([]),[])
})
test("node and file searches keep only links whose two endpoints are shown", () => {
 const placed=layoutGraph(nodes)
 assert.equal(graphSelection(placed,links,"","*").links.length,1)
 assert.deepEqual(graphSelection(placed,links,"a.ts","*").nodes.map(n=>n.id),["a"])
 assert.equal(graphSelection(placed,links,"a.ts","*").links.length,0)
 assert.equal(graphSelection(placed,links,"","2").nodes[0]?.id,"b")
 assert.equal(graphSelection(placed,links,"not found","*").nodes.length,0)
})
test("a full overview keeps distinct community islands and finite node positions", () => {
 const overview=Array.from({length:300},(_,i)=>({...nodes[0],id:String(i),community:String(i%16)}))
 const placed=layoutGraph(overview)
 assert.equal(new Set(placed.map(n=>`${n.x},${n.y}`)).size,overview.length)
 for(const n of placed)assert.ok(Number.isFinite(n.x)&&Number.isFinite(n.y)&&n.x>=0&&n.x<=900&&n.y>=0&&n.y<=600)
 assert.deepEqual(new Set(placed.map(n=>n.id)),new Set(overview.map(n=>n.id)))
})
test("a populous community has room to spread instead of sharing a tiny island", () => {
 const overview=Array.from({length:300},(_,i)=>({...nodes[0],id:String(i),community:i<220?"core":String(i%16)}))
 const placed=layoutGraph(overview)
 const span=(group:string)=>{const ns=placed.filter(n=>n.community===group);return Math.hypot(Math.max(...ns.map(n=>n.x))-Math.min(...ns.map(n=>n.x)),Math.max(...ns.map(n=>n.y))-Math.min(...ns.map(n=>n.y)))}
 assert.ok(span("core")>span("1")*2)
})
