import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import {groupMentionOptions,withGroupMention} from "./group-mentions.ts"

const members=[{name:"Alice Lee"},{name:"Bob"},{name:"小明"}]
test("group mentions filter only the current end-of-draft token and preserve full Unicode names",()=>{
  assert.deepEqual(groupMentionOptions("Please @",8,members),members)
  assert.deepEqual(groupMentionOptions("@lee",4,members),[members[0]])
  assert.deepEqual(groupMentionOptions("@明",2,members),[members[2]])
  assert.equal(withGroupMention("Please @ali","Alice Lee"),"Please @Alice Lee ")
  assert.equal(withGroupMention("@小","小明"),"@小明 ")
  for(const draft of ["mail@example.com","@Alice Lee ","@all","@everyone ","@\nBob"]){
    assert.deepEqual(groupMentionOptions(draft,draft.length,members),[],draft)
  }
  assert.deepEqual(groupMentionOptions("@Alice later",3,members),[])
  const long={name:"A name long enough to exceed the upstream query shortcut"}
  assert.deepEqual(groupMentionOptions("@"+long.name,long.name.length+1,[long]),[long])
})
