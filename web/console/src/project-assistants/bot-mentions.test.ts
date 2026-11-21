import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import {botMentionQuery,botMentionSegments,decodeBotMentions,encodeBotMentions,insertBotMention} from "./bot-mentions.ts"

test("a picked Bot reads as @Name in the editor and keeps its ID in the saved text",()=>{
  const saved="請 @[Lynn](bot:b2) 查信，@[研究員 A](bot:b9)。"
  const {display,picks}=decodeBotMentions(saved)
  assert.equal(display,"請 @Lynn 查信，@研究員 A。")
  assert.deepEqual(picks,[{name:"Lynn",id:"b2"},{name:"研究員 A",id:"b9"}])
  assert.equal(encodeBotMentions(display,picks),saved)
  assert.equal(encodeBotMentions("請 @Lyn 查信",picks),"請 @Lyn 查信","an edited name becomes plain text")
  assert.equal(encodeBotMentions("x@Lynn",picks),"x@Lynn")
  assert.equal(encodeBotMentions("@Lynnette",picks),"@Lynnette")
  assert.deepEqual(botMentionSegments("hi @[Lynn](bot:b2)!"),["hi ",{name:"Lynn",id:"b2"},"!"])
})

test("the @ list opens only while an @word is being typed at the caret",()=>{
  assert.deepEqual(botMentionQuery("請 @ly",5),{start:2,query:"ly"})
  assert.deepEqual(botMentionQuery("@",1),{start:0,query:""})
  assert.deepEqual(botMentionQuery("，@林",3),{start:1,query:"林"})
  for(const [draft,caret] of [["x@ly",4],["@ly x",5],["none",4]] as const)assert.equal(botMentionQuery(draft,caret),null,draft)
  assert.deepEqual(insertBotMention("請 @ly 查信",2,5,"Lynn"),{display:"請 @Lynn 查信",caret:7})
  assert.deepEqual(insertBotMention("@l",0,2,"Lynn"),{display:"@Lynn ",caret:6})
})
