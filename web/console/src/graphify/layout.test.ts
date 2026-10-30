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
