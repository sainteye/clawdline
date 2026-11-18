import assert from "node:assert/strict"
import {test} from "node:test"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import {matchesRosterQuery,snapshotRosterQuery} from "./roster-search.ts"

const item={bot:{key:{machine_id:"studio",bot_id:"reader"},name:"Reading helper",project:{project_id:"project"}}} as W.ProjectAssistantBotSummary
const projects=new Map([[JSON.stringify(["studio","project"]),"Clawdline"],[JSON.stringify(["notebook","project"]),"Notes"]])
test("search matches the displayed Project on the Bot's own machine",()=>{
  assert.equal(matchesRosterQuery(item,"cLaWdLiNe",projects),true)
  assert.equal(matchesRosterQuery(item,"Reading",projects),true)
  assert.equal(matchesRosterQuery(item,"Notes",projects),false)
  assert.equal(matchesRosterQuery(item,"unknown",projects),false)
  assert.equal(matchesRosterQuery(item,"",projects),true)
})
test("Project search retains source pagination while name search stays server-filtered",()=>{
  assert.equal(snapshotRosterQuery("studio","Clawdline",projects),"")
  assert.equal(snapshotRosterQuery("notebook","Clawdline",projects),"Clawdline")
  assert.equal(snapshotRosterQuery("studio","Reading",projects),"Reading")
  assert.equal(snapshotRosterQuery("studio","Clawdline",new Map()),"Clawdline")
})
