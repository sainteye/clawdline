import {test} from "node:test"
import assert from "node:assert/strict"
import {readFileSync} from "node:fs"
// @ts-expect-error -- Node executes source extensions.
import {draftGroupMentions,shownGroupMentions,sourceGroupMentions} from "./group-mentions.ts"

type Fixture={members:{id:string;name:string}[];cases:{name:string;author?:string;text:string;marks:[string,string][];asked:string[]}[]}
const fixture:Fixture=JSON.parse(readFileSync(new URL("../../../../internal/domain/projectassistant/testdata/room-mentions.json",import.meta.url),"utf8"))

const marked=(text:string,spans:{start:number;end:number;id:string}[])=>shownGroupMentions(spans).map(s=>[text.slice(s.start,s.end),s.id])
const asked=(spans:{id:string}[],author?:string)=>[...new Set(spans.flatMap(s=>s.id==="*"?fixture.members.map(m=>m.id).filter(id=>id!==author):[s.id]))].sort()

for(const c of fixture.cases){
  test(`the draft marks whom the source asks: ${c.name}`,()=>{
    const spans=draftGroupMentions(c.text,fixture.members,c.author)
    assert.deepEqual(marked(c.text,spans),c.marks)
    assert.deepEqual(asked(spans,c.author),c.asked)
  })
}

test("a sent message is marked only where the source says it routes",()=>{
  const text="@小明幫我看，@al 不是 alice"
  const spans=sourceGroupMentions([{start:0,end:3,member:{bot_id:"ming"}},{start:7,end:10,everyone:true},{start:1,end:2}])
  assert.deepEqual(marked(text,spans),[["@小明","ming"],["@al","*"]],"the source's spans are drawn as sent, never re-read")
  assert.deepEqual(sourceGroupMentions(undefined),[])
})
