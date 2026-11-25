import { ContextMenu, type CompactBlock } from "./ContextMenu"
import { ContextActionStatusRow, useContextActionStatus } from "./NewSessionStatusRow"
import { compactRefusalCode } from "./compact-status"
import { ChatModel, modelEdit } from "./ChatModel"
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type DragEvent } from "react"
import "../session/jump.css"
import {BACKOFF} from "../session/transcript-follow"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import type { BotReadObservation } from "./read-observation"
import { authorized, command, errorCode, failure, pictureDataURL, provablyNotSent, sendBlockedByInterruptedTurn, steeringRefused, uncertainAfterSend } from "./client"
import { botIdentity, canRead, canWrite, draftIdentity, permissionDeadline, sameBot, sameConversation, sameFence, type HeldOutcome, type ViewState } from "./view-state"
import { isSendingAdmitted, partitionMessages, placeTurn, queuedLabel, queuedAttachments, workingSubject } from "./chat-items"
import {admittedReceipt,admittedMessage,admittedRow,sendAcknowledgement,ChatObservation} from "./chat-observation"
import {useMessageArrivals} from "./message-arrivals"
import {browserViewingPage, localViewingReport, watchBotViewing} from "./bot-viewing"
import {hostedConsole} from "../pages/terminal/api"
import {traceDecision} from "./decision-trace-send"
import {serialSnapshotRefresh} from "./snapshot-roster"
import {chatEventDelay,chatPushSignal} from "./chat-push"
import { readView, mergeTimeline, eventTimeline, turnSummary } from "./read-views"
import {agentName} from "./fallback-settings"
import { BotAvatar } from "./BotAvatar"
import { DispatchedWorkStrip } from "./DispatchedWorkStrip"
import { BotRequestDetail, BotRequestLine, useBotRequests } from "./BotRequestCard"
import { botOf, placeBotRequests, readsBotRequests } from "./bot-request"
import { useDispatchedWorkShown } from "./dispatched-work-preference"
import { AgentBadge } from "./AgentMark"
import "./chat-presentation.css"
import { LeadStarter } from "./LeadStarter"
import { showLeadStarter } from "./lead-starter"
import { LeadSetup, useLeadSetup } from "./LeadSetup"
import {BotSidePanel,type BotInfoTab} from "./BotSidePanel"
import {DevServers,DevServerTitle,serverTitle} from "./DevServers"
import {useDevServerObservation} from "./useDevServerObservation"
import { BotSettings, sameEditableSettings } from "./BotSettings"
import { toast } from "../overlays/toast"
import { BotTemplate } from "./BotTemplate"
import { TeamApprovals } from "./Groups"
import { Routines } from "./Routines"
import { MemoryPanel, projectMemoryPageSize, type MemoryNoteDraft } from "./MemoryPanel"
import {OutputLibrary} from "./OutputLibrary"
import type {ArtifactComposerAccess,ArtifactSendResult} from "./ArtifactWorkspace"
import { ChatRows } from "./ChatRows"
import { HandoffCard } from "./HandoffCard"
import { handoffWords } from "./handoff-card-words"
import { useHandoffCard } from "./useHandoffCard"
import {uploadInputs,MAX_INPUT_BYTES,type DraftInput} from "./inputs"
import { Composer } from "./Composer"
import {clearSendAttachments,loadSendAttachments,saveSendAttachments,sendAttachmentKey,type SendAttachments} from "./send-attachments"
import type { MarkupPicture } from "../session/ImageMarkup.js"
import { BotCallBar, useBotCall, useBotCallStart } from "./BotCall"
import { BotVoicePanel, useBotVoice } from "./BotVoicePanel"
import type { BotVoiceObservation } from "./bot-voice-tools"
import { botCallSendID, isBotCallSend, handoffInputRefused, type CallInput } from "./bot-call"
import { PermissionCard, permissionState, samePermission } from "./PermissionCard"
import { DecisionCard, decisionsUnderCapsules } from "./DecisionCard"
import { decisionFocus, decisionFollowUp, type DecisionFollowUp } from "./decision-focus"
import { Trace,traceLabel,visibleTrace,workingCategory,type TraceLoadState } from "./Trace"
import { activityOf, chatMood, liveActivity, rosterMood } from "./working"
import {FallbackProgress} from "./FallbackProgress"
import { WorkingTransition } from "./WorkingTransition"
import { providerSetupFailure, readOnlyReason } from "./source-errors"
import { TurnInterruption, TurnReviewNotice } from "./TurnInterruption"
import { RuleList } from "./RuleList"
import { BotManage } from "./BotManage"
import { seenThrough } from "./read-position"
import { ProjectMark } from "./ProjectMark"
import { BotMentionText, botMentionCandidates } from "./BotMentionField"
import { decodeBotMentions, encodeBotMentions } from "./bot-mentions"
import { canMarkConversationRead, markConversationRead } from "./conversation-read"
import type { Words } from "./words"
import { ChatSkeleton, useShowAfter } from "./PaLoading"
import { UnsentActions, retryAllowed, retryOnce, unsentDiscardKind, type RetryFailure, type RetryOutcome } from "./UnsentRetry"
import { memoryOpener } from "./loading"
import { BackgroundCapsule, BackgroundHandoff, BackgroundDuration, BackgroundList, BackgroundStop, backgroundTaskLabel, liveBackgroundTask, type BackgroundStopOutcome, type BackgroundTask } from "./BackgroundTasks"
import { BotBrowser, BotBrowserChip, botBrowserWord } from "./BotBrowser"
import { botBrowserSupported, browserToolTrace, toolTraceCategory } from "./bot-browser-entry"

export {EventRefreshMs,PushedEventFallbackMs} from "./chat-push"

function transientChatRead(error:unknown) {
  const code=errorCode(error)
  return code==="pa_offline"||code==="pa_cursor_expired"||code==="pa_stale_generation"
}

// These facts belong to the page's journal rows. A full reload restores a row
// for receipt lookup or manual retry, without borrowing the old page's grant.
const sendReplay = new WeakMap<HeldOutcome,{source:SourceClient;origin:ChatObservation}>()
const sendFailure = new WeakMap<HeldOutcome,RetryFailure>()

async function transmitChatSend(
  source:SourceClient, current:()=>readonly SourceClient[], intent:W.ProjectAssistantSendRequest,
  prepare:(signal:AbortSignal)=>Promise<void>, rebind?: (signal:AbortSignal)=>Promise<boolean>,
):Promise<{attempted:boolean;receipt:W.ProjectAssistantReceipt|null;error:unknown}> {
  const signal=new AbortController().signal
  let attempted=false, receipt:W.ProjectAssistantReceipt|null=null
  const post=()=>authorized(source,current,async s=>{
    attempted=true
    const answer=await source.send(intent,s)
    if(!sendAcknowledgement(answer,intent))throw failure("pa_outcome_unknown")
    receipt=answer.receipt
  },signal)
  try{
    await prepare(signal)
    try{await post()}
    catch(error){
      const refusal=(error as Partial<W.ProjectAssistantErrorResponse>|null)?.error
      if(receipt||refusal?.code!=="pa_stale_generation"||refusal.field!=="fence"||!await rebind?.(signal).catch(()=>false))throw error
      await post()
    }
    return {attempted,receipt,error:undefined}
  }catch(error){return {attempted,receipt,error}}
}
/** Re-read a stale permission view without replaying the answer. */
export async function withPermissionRecovery<T>(run:()=>Promise<T>,current:()=>boolean,recover:()=>void):Promise<T> {
  try{return await run()}
  catch(error){
    const code=errorCode(error)
    if(current()&&(code==="pa_cursor_expired"||code==="pa_stale_generation"))recover()
    throw error
  }
}

function retryOnReentry(row:HeldOutcome,source:SourceClient,observation:ChatObservation,recovering:string) {
  const replay=sendReplay.get(row)
  return replay?.source===source&&replay.origin!==observation&&row.intent.command.idempotency_key!==recovering
}

function settledRetry(held:HeldOutcome,outcome:RetryOutcome):HeldOutcome {
  if(outcome.kind==="admitted")return {...held,receipt:outcome.receipt}
  const rejected=outcome.kind==="rejected"
  const refusal=rejected?{code:"pa_declined",at:Date.now()}:outcome.failure
  const row={...held,...(rejected||outcome.editable&&!retryAllowed(refusal)?{needsEdit:true}:{}),...(rejected?{superseded:true}:{})}
  sendFailure.set(row,refusal)
  return row
}

type PermissionViewCard={summary:W.ProjectAssistantReadPermissionSummary;offer?:W.ProjectAssistantPermissionOffer}

const TURN_REVIEW_PREFIX = "review-interrupted-"

function currentPermission(value:Pick<W.ProjectAssistantReadPermissionSummary,"turn_id"|"fence">,head:W.ProjectAssistantReadViewHead|null) {
  // Only a validated decision may have an empty turn ID. It belongs to the chat.
  return !!head&&sameFence(value.fence,head.current_fence)&&(value.turn_id===""||value.turn_id===head.conversation.active_turn_id)
}

export function pendingPermissionView(head:W.ProjectAssistantReadViewHead|null,cards:readonly PermissionViewCard[],now:number) {
  if(!head||head.pending_permission_count===0)return {count:0,nextDeadline:Infinity}
  // The collection can be paged. Keep counting requests we have yet to read.
  const loadedPending=cards.filter(({summary})=>summary.state==="pending").length
  let count=Math.max(0,head.pending_permission_count-loadedPending),nextDeadline=Infinity
  for(const {summary,offer} of cards){
    if(!currentPermission(summary,head)||permissionState(summary,now)!=="pending")continue
    const value=offer&&samePermission(summary,offer)?offer:summary
    if(permissionState(value,now)!=="pending")continue
    count++
    nextDeadline=Math.min(nextDeadline,permissionDeadline(value),permissionDeadline(summary))
  }
  return {count:Math.min(count,head.pending_permission_count),nextDeadline}
}

export interface ChatProps {
  summary: W.ProjectAssistantBotSummary; source:SourceClient; current:()=>readonly SourceClient[];
  accountKey:string;readerID?:string;projectName?:string;projectIcon?:W.Icon;machineName?:string;view:ViewState;w:Words;language:string;onBack:()=>void;
  openedAt?:number;showBack?:boolean;backLabel?:string;voiceShortcut?:number;thread?:BackgroundTask;editProfileRequest?:number;roster?:readonly W.ProjectAssistantBot[];projectNameOf?:(bot:W.ProjectAssistantBot)=>string|undefined;projectIconOf?:(bot:W.ProjectAssistantBot)=>W.Icon|undefined;pushed?:boolean;
  onBotChanged:(bot:W.ProjectAssistantBot)=>void;onOpenBot?:(key:W.ProjectAssistantBotKey)=>void;onOpenWork?:(work:W.ProjectAssistantWorkLink, view?:"session")=>void;onDeleted?:()=>void;onRead?:()=>void;onNewSession?:()=>void;newSessionCount?:number
  /** The decision a push notification opened this chat at, and the call that consumes it. */
  focusDecision?:string;onDecisionFocused?:()=>void
}
const chatInitialPageRows = 20

export function Chat({summary,source,current,accountKey,readerID,view,w,language,onBack,onBotChanged,onOpenBot,onOpenWork,onDeleted,onRead,onNewSession,newSessionCount,projectName,projectIcon,machineName,openedAt,showBack=false,backLabel,thread,editProfileRequest,roster,projectNameOf,projectIconOf,voiceShortcut,focusDecision,onDecisionFocused,pushed=false}:ChatProps) {
  const teamCards=source.canTeamCards?.()===true
  const deferTraceCount=source.canDeferTraceCount?.()===true
  const key=summary.bot.key,identity=botIdentity(key),draftKey=draftIdentity(accountKey,key)
  const grant=useRef(source.authorizationRevision).current
  const scope=useRef({key:{...key},project:{...summary.bot.project},conversation_id:summary.bot.conversation_id}).current
  const [recoveryRequired,setRecoveryRequired]=useState(()=>view.recoveryRequired.has(draftKey)),recovery=useRef(recoveryRequired)
  const [infoTab,setInfoTab]=useState<BotInfoTab>("details"),[infoOpen,setInfoOpen]=useState(false),[settingsRevision,setSettingsRevision]=useState(0)
  const [selectedTask,setSelectedTask]=useState("")
  const taskOpener=useRef<HTMLButtonElement|null>(null)
  const [backgroundStopping,setBackgroundStopping]=useState("")
  const [backgroundOutcomes,setBackgroundOutcomes]=useState<Record<string,BackgroundStopOutcome>>({})
  const backgroundStopControl=useRef<AbortController|null>(null)
  const [backgroundError,setBackgroundError]=useState("")
  const setSettings=(open:boolean)=>{if(open)setInfoTab("settings");setInfoOpen(open)}
  const openInfo=(tab:BotInfoTab)=>{setInfoTab(tab);setInfoOpen(true);if(tab==="details")servers.refresh();if(tab==="memory"&&!memoryShown.current)void memoryPanel.current?.open()}
  const profileName=useRef<HTMLInputElement>(null),focusedProfile=useRef(0)
  useLayoutEffect(()=>{
    if(editProfileRequest){setInfoTab("settings");setInfoOpen(true)}
  },[editProfileRequest])
  const identityButton=useRef<HTMLButtonElement>(null)
  const focusedThread=useRef("")
  const [templateOpener,setTemplateOpener]=useState<HTMLButtonElement|null>(null)
  const [browserOpener,setBrowserOpener]=useState<HTMLButtonElement|null>(null)
  const [checkingUpdates,setCheckingUpdates]=useState(false)
  const [exporting,setExporting]=useState(false)
  const [exportError,setExportError]=useState("")
  const browserFocus=useRef<HTMLButtonElement|null>(null)
  useLayoutEffect(()=>{
    if(browserOpener){browserFocus.current=browserOpener;return}
    const opener=browserFocus.current
    if(!opener)return
    browserFocus.current=null
    if(opener.getClientRects().length&&!opener.disabled)opener.focus({preventScroll:true})
    else {
      const selectedTab=identityButton.current?.closest(".pa-chat-layout")?.querySelector<HTMLElement>('.pa-info-panel:not([hidden]) [aria-selected="true"]')
      const fallback=selectedTab??identityButton.current
      fallback?.focus({preventScroll:true})
    }
  },[browserOpener])
  const [data,setData]=useState<W.ProjectAssistantGetResponse|null>(null)
  const [timeline,setTimeline]=useState<W.ProjectAssistantReadViewItem[]>([]),timelineRef=useRef(timeline)
  const opening = useRef<{ at: number; trace?: string }>({at:openedAt ?? performance.now()})
  const readyRead = useRef<BotReadObservation | undefined>(undefined)
  const finishCommittedRead = useRef<(() => void) | undefined>(undefined)
  useLayoutEffect(()=>{
    const trace=readyRead.current
    if (!data || !trace) return
    readyRead.current=undefined
    finishCommittedRead.current?.()
    trace.mark("committed")
    let frame=0
    const finish=(paintOpportunity: boolean)=>{
      cancelAnimationFrame(frame)
      document.removeEventListener("visibilitychange",visibility)
      window.removeEventListener("blur", committed)
      window.removeEventListener("pagehide", committed)
      finishCommittedRead.current=undefined
      if(paintOpportunity)trace.mark("paint_opportunity")
      trace.finish(paintOpportunity?"paint_opportunity":"committed")
    }
    const committed=()=>finish(false)
    const visibility=()=>{if(document.visibilityState==="hidden")committed()}
    finishCommittedRead.current=committed
    if(document.visibilityState!=="visible"||!document.hasFocus()){committed();return}
    document.addEventListener("visibilitychange",visibility)
    window.addEventListener("blur", committed)
    window.addEventListener("pagehide", committed)
    // A rendering opportunity between callbacks, using the browser's clock.
    frame=requestAnimationFrame(()=>{frame=requestAnimationFrame(()=>finish(true))})
  },[data,timeline])
  useLayoutEffect(()=>()=>{
    finishCommittedRead.current?.()
    readyRead.current?.finish("abandoned")
  },[])
  const prepareReadDisplay=useCallback((trace?:BotReadObservation)=>{
    if(!trace)return
    readyRead.current?.finish("abandoned")
    trace.mark("ready")
    readyRead.current=trace
  },[])
  // The entry-time question is consumed when the current permission cards arrive.
  const [focusTarget,setFocusTarget]=useState(focusDecision??""),[focusedDecision,setFocusedDecision]=useState(""),[decisionHandled,setDecisionHandled]=useState(false)
  const [rosterQuestion,setRosterQuestion]=useState(()=>!thread&&summary.activity.startsWith("Asks: ")?summary.activity.slice("Asks: ".length):"")
  const [permissionsRead,setPermissionsRead]=useState(false)
  // Each decision answered from this page and the turn its reply started.
  const [decisionTurns,setDecisionTurns]=useState<Record<string,string>>({}),decisionProgress=useRef<Record<string,DecisionFollowUp|undefined>>({})
  useEffect(()=>{if(focusDecision)setFocusTarget(focusDecision)},[focusDecision])
  // A question this chat already shows is not also pushed while it is on
  // screen. Only the console served by this machine reports it.
  useEffect(()=>hostedConsole()?undefined:watchBotViewing(key.bot_id,scope.conversation_id,browserViewingPage(),localViewingReport),[key.bot_id,scope.conversation_id])
  const [permissions,setPermissions]=useState<W.ProjectAssistantReadPermissionSummary[]>([]),[turns,setTurns]=useState<W.ProjectAssistantReadTurnSummary[]>([]),[activity,setActivity]=useState<Record<string,string>>({})
  const [permissionTick,setPermissionTick]=useState(0)
  const permissionRows=useRef(permissions)
  const collapsedPermissions=useRef(new Set<string>())
  const [observedBrowserTraces,setObservedBrowserTraces]=useState<Record<string,W.ProjectAssistantTrace>>({})
  const [traceSummaries,setTraceSummaries]=useState<W.ProjectAssistantReadTraceSummary[]>([])
  const traceRows=useRef(traceSummaries)
  const [traceLoad,setTraceLoad]=useState<TraceLoadState>("idle")
  const traceRequested=useRef(false)
  const [details,setDetails]=useState<Record<string,W.ProjectAssistantPermissionOffer|W.ProjectAssistantTrace>>({})
  const detailRows=useRef(details)
  const requestedTraceReferences=useRef(new Map<string,string>())
  const backgroundTraceReferences=useRef(new Map<string,string>())
  const traceDetailTail=useRef(Promise.resolve())
  const traceDetailReads=useRef(new Map<string,Promise<boolean|undefined>>())
  const permissionDetailTail=useRef(Promise.resolve())
  const [permissionMore,setPermissionMore]=useState(""),[traceMore,setTraceMore]=useState("")
  const [head,setHead]=useState<W.ProjectAssistantReadViewHead|null>(null),headRef=useRef(head)
  // The journal identifies the previous send independently of the entry draft.
  const heldAtMount=useRef(view.heldOutcomes.get(draftKey)).current,heldSendAtMount=heldAtMount?.intent.command.operation==="send"?heldAtMount.intent as W.ProjectAssistantSendRequest:null
  // A send this page still has a request open for, from a Chat that was left: shown as sending until its own answer lands.
  const awaiting=useRef(heldSendAtMount&&view.heldOutcomes.transporting(draftKey,heldSendAtMount.command.idempotency_key)?heldSendAtMount:null),inFlightAtMount=awaiting.current
  // Capture the entry draft before any asynchronous source read or composer edit.
  const entryDraft=useRef({text:view.draft(draftKey),quote:view.quotes.get(draftKey)??""}).current
  const entryChecked=useRef(false),entryUnknown=useRef(false)
  const legacyAtMount=useRef(view.unverifiedDrafts.active(draftKey,scope)).current
  const acceptedAtMount=admittedReceipt(heldAtMount?.receipt)?heldSendAtMount:null
  const observation=useRef(new ChatObservation(heldAtMount?.receipt)).current
  const uncertain=()=>{const held=view.heldOutcomes.get(draftKey);return !!held&&!(held.intent.command.operation==="send"&&admittedReceipt(held.receipt))}
  const [text,setText]=useState(()=>legacyAtMount?.text??entryDraft.text),[quote,setQuote]=useState(legacyAtMount?.quote??entryDraft.quote)
  const [reconnecting,setReconnecting]=useState(false),retryDelay=useRef(BACKOFF.firstMs)
  const [error,setError]=useState(""),[errorField,setErrorField]=useState<string>(),[receipt,setReceipt]=useState<W.ProjectAssistantReceipt|null>(()=>view.heldOutcomes.get(draftKey)?.receipt??null),[busy,setBusy]=useState(!!inFlightAtMount),[sending,setSending]=useState<string|null>((inFlightAtMount??acceptedAtMount)?.text??null)
  const [localSend,setLocalSend]=useState("")
  const [unknown,setUnknown]=useState(()=>(uncertain()&&!inFlightAtMount)||view.heldOutcomes.problem(draftKey)==="unreadable"),[memory,setMemory]=useState<W.ProjectAssistantMemoryViewResponse|null>(null),[memoryLoading,setMemoryLoading]=useState(false),[earlierLoading,setEarlierLoading]=useState(false)
  const [more,setMore]=useState(""),[following,setFollowing]=useState(true),[draftDurable,setDraftDurable]=useState(true)
  const [legacyRemoveFailed,setLegacyRemoveFailed]=useState(false)
  const [journalRevision,setJournalRevision]=useState(0)
  const recoveryKey=JSON.stringify([draftKey,scope.conversation_id,"recovering"])
  const pausedKey=JSON.stringify([draftKey,scope.conversation_id,"paused"])
  const [recoveringID,setRecoveringID]=useState(()=>view.draft(recoveryKey))
  const [paused,setPaused]=useState(()=>!!view.draft(pausedKey))
  const [pendingAttachments,setPendingAttachments]=useState<Record<string,SendAttachments|null>>({})
  const [retrying,setRetrying]=useState("")
  const [discardFailed,setDiscardFailed]=useState("")
  const [receiptCheckRevision,requestReceiptCheck]=useState(0)
  const retryInFlight=useRef(false)
  const assetKey=(id:string,attempt?:string)=>sendAttachmentKey(draftKey,scope.conversation_id,id,attempt)
  const attachmentsFor=(held:HeldOutcome)=>assetKey(held.intent.command.idempotency_key,held.intent.command.client_attempt_id)
  const clearPendingAttachments=(held:HeldOutcome)=>{
    const saved=attachmentsFor(held)
    if(valid())setPendingAttachments(old=>{if(!(saved in old))return old;const next={...old};delete next[saved];return next})
    void clearSendAttachments(saved)
  }
  const repaintJournal=()=>setJournalRevision(n=>n+1)
  const legacyBlocked=()=>!!view.unverifiedDrafts.active(draftKey,scope)||!!view.unverifiedDrafts.problem(draftKey)
    || (entryUnknown.current&&!view.unverifiedDrafts.continuing(draftKey,scope))
  const scroll=useRef<HTMLDivElement>(null),alive=useRef(true),currentData=useRef(data),lastSeq=useRef(0),epoch=useRef("")
  const stopping=useRef(false),callViewFrontier=useRef(0)
  const [stopStatus,setStopStatus]=useState<{turn:string;state:"sending"|"acknowledged"|"unknown"}|null>(null)
  const [queueAction,setQueueAction]=useState<{id:string;operation:"send_now"|"stop"}|null>(null)
  const queueInFlight=useRef(false)
  const [queueEditor,setQueueEditor]=useState<{turnID:string;editID:string;text:string}|null>(null)
  const queueEditorInput=useRef<HTMLTextAreaElement>(null)
  const [queueEditBusy,setQueueEditBusy]=useState(false)
  const [queueEditError,setQueueEditError]=useState("")
  const [memoryNoteDraft,setMemoryNoteDraft]=useState<MemoryNoteDraft|null>(null)
  const memorySave=useRef<W.ProjectAssistantMemoryWriteRequest|null>(null),memoryForget=useRef<W.ProjectAssistantForgetRequest|null>(null),memoryShown=useRef(false)
  const refreshRun=useRef<(manual:boolean)=>Promise<void>>(async()=>{}),refreshQueue=useRef<(manual?:boolean)=>Promise<void>>(async()=>{})
  const refresh=useCallback((manual=false)=>refreshQueue.current(manual),[])
  const pushedRef=useRef(pushed);pushedRef.current=pushed
  const checkForUpdates=async()=>{
    setCheckingUpdates(true)
    try{await refresh(true)}finally{setCheckingUpdates(false)}
  }
  const timer=useRef<ReturnType<typeof setTimeout>|null>(null),eventControl=useRef<AbortController|null>(null),activeMutations=useRef(new Set<AbortController>())
  const reads=useRef(new Set<AbortController>()),collectionBusy=useRef(new Map<string,AbortController>()),historyCursors=useRef(new Set<string>())
  const collectionCursors=useRef({permissions:new Set<string>(),traces:new Set<string>()})
  const collectionsToRetry=useRef(new Set<"permissions"|"traces">())
  const lastRead=useRef(0),reading=useRef(false),restored=useRef(false),followingRef=useRef(true),sendBaselineSeq=useRef(0)
  const messages=timeline.flatMap(row=>row.message?[row.message]:[])
  currentData.current=data;timelineRef.current=timeline;headRef.current=head;permissionRows.current=permissions;traceRows.current=traceSummaries;detailRows.current=details
  const fence=useCallback((d:W.ProjectAssistantGetResponse):W.ProjectAssistantFence=>({key:d.bot.key,project:d.bot.project,conversation_id:d.conversation.id,binding_id:d.conversation.binding_id,generation:d.conversation.generation}),[])
  const valid=useCallback(()=>alive.current&&current().some(s=>s===source&&s.machineID===key.machine_id&&s.authorizationRevision===grant),[source,current,grant,key.machine_id])
  const enabled=!recoveryRequired&&!!data&&canWrite(data.source)
  useLayoutEffect(()=>{
    const field=profileName.current
    if(!editProfileRequest||focusedProfile.current===editProfileRequest||!infoOpen||infoTab!=="settings"||!enabled||!field)return
    focusedProfile.current=editProfileRequest
    field.scrollIntoView({block:"center"})
    field.focus({preventScroll:true})
  },[editProfileRequest,infoOpen,infoTab,enabled])
  const leadSetupBot=data?.bot
  const leadSetupEligible=enabled&&!thread&&!!leadSetupBot?.lead&&key.bot_id==="default-lead"&&!leadSetupBot.login_profile
    &&(leadSetupBot.agent==="claude"||leadSetupBot.agent==="codex")&&data?.conversation.last_message_seq===0
  const leadSetup=useLeadSetup(source,leadSetupEligible,leadSetupBot?.agent)
  const handoff=useHandoffCard(source,current,head?.current_fence,enabled&&!thread,
    JSON.stringify([head?.current_fence,head?.memory_revision,head?.conversation.active_turn_id,head?.conversation.active_turn_id?"":head?.bot_version]))
  const [editingHandoff,setEditingHandoff]=useState(false)
  useEffect(()=>{if(!handoff.available)setEditingHandoff(false)},[handoff.available])
  const fail=useCallback((e:unknown)=>{if(valid()){setErrorField((e as Partial<W.ProjectAssistantErrorResponse>|null)?.error?.field);setError(errorCode(e))}},[valid])
  const changeText=(value:string)=>{view.stage(draftKey,value);setText(value)}
  const flush=()=>setDraftDurable(view.flush(draftKey))
  const archiveLegacyDraft=useCallback(()=>{
    const next=view.archiveUnverifiedDraft(draftKey,scope)
    if(next&&!view.heldOutcomes.has(draftKey)){setText(next.text);setQuote(next.quote)}
  },[view,draftKey,scope])
  const lock=useCallback(()=>{
    view.recoveryRequired.add(draftKey);recovery.current=true;setRecoveryRequired(true)
    activeMutations.current.forEach(control=>control.abort());setTurns([]);setPermissions([]);permissionRows.current=[];requestedTraceReferences.current.clear();backgroundTraceReferences.current.clear();setObservedBrowserTraces({});setTraceSummaries([]);traceRows.current=[];setDetails({});detailRows.current={};setPermissionMore("");setTraceMore("")
    for(const control of reads.current)control.abort()
    if(timer.current){clearTimeout(timer.current);timer.current=null}
  },[view,draftKey])
  const scheduleRead=useCallback((delay:number,full=false)=>{
    if(timer.current!==null||!valid()||document.visibilityState==="hidden"&&!observation.active)return
    timer.current=setTimeout(()=>{timer.current=null;void refresh(full)},delay)
  },[valid,observation,refresh])
  const readFailed=useCallback((error:unknown)=>{
    if(!valid())return
    if(errorCode(error)!=="pa_capacity_full")lock()
    const retry=transientChatRead(error)
    setReconnecting(retry)
    if(!retry){fail(error);return}
    setError("")
    const delay=retryDelay.current
    retryDelay.current=Math.min(delay*2,BACKOFF.maxMs)
    scheduleRead(delay,true)
  },[valid,lock,fail,scheduleRead])
  const request=useCallback((mode:W.ProjectAssistantReadViewMode,cursor="",page_size=100):W.ProjectAssistantReadViewRequest=>({
    key:scope.key,conversation_id:scope.conversation_id,mode,source_epoch:mode==="snapshot"?"":epoch.current,cursor,after_seq:mode==="events"?lastSeq.current:0,page_size,
    ...(teamCards?{team_cards:true}:{}),...(deferTraceCount&&mode!=="traces"?{defer_trace_count:true}:{})
  }),[scope,teamCards,deferTraceCount])
  const read=useCallback(async(q:W.ProjectAssistantReadViewRequest,signal:AbortSignal,trace?:BotReadObservation)=>{
    const answer=await authorized(source,current,s=>typeof source.view==="function"?source.view(q,s,trace):Promise.reject(failure("pa_unsupported")),signal)
    const page=readView(answer,q,scope)
    trace?.mark("validated")
    return page
  },[source,current,scope])
  const adoptHead=useCallback((page:W.ProjectAssistantReadViewResponse)=>{
    const before=headRef.current
    if(before&&(page.head.event_frontier<before.event_frontier||page.head.bot_version<before.bot_version))return
    const sameTurn=before&&sameFence(page.head.current_fence,before.current_fence)&&page.head.conversation.active_turn_id===before.conversation.active_turn_id
    const nextHead=page.mode==="snapshot"||page.mode==="events"?page.head:{...page.head,background_tasks:before?.background_tasks,
      context_usage:sameTurn?before.context_usage:undefined,active_model:sameTurn?before.active_model:undefined}
    headRef.current=nextHead;setHead(nextHead)
    const d=currentData.current
    if(d){const next={...d,bot:page.snapshot?.bot??d.bot,conversation:page.head.conversation,source:page.source};currentData.current=next;setData(next)}
  },[])
  const recoverSnapshot=useCallback(async(signal:AbortSignal)=>{
    const trace=!currentData.current ? source.beginReadObservation?.("snapshot",opening.current.at,opening.current.trace) : undefined
    if(trace)opening.current.trace??=trace.id
    let page: W.ProjectAssistantReadViewResponse
    try {
      page=await read(request("snapshot","",chatInitialPageRows),signal,trace)
    } catch(error) {
      trace?.finish(signal.aborted?"abandoned":"refused",errorCode(error))
      throw error
    }
    if(!valid()||signal.aborted){trace?.finish("abandoned");return}
    if(!entryChecked.current){
      entryChecked.current=true
      const hasJournal=view.heldOutcomes.aside(draftKey).some(held=>sameConversation(held.intent.fence,scope))
      entryUnknown.current=!!entryDraft.text&&page.head.unknown_turn_count>0&&!heldAtMount&&!hasJournal&&view.heldOutcomes.problem(draftKey)!=="unreadable"&&!view.unverifiedDrafts.continuing(draftKey,scope)
      if(entryUnknown.current)view.unverifiedDrafts.capture(draftKey,scope,entryDraft.text,entryDraft.quote)
      if(!heldAtMount&&canWrite(page.source)&&view.heldOutcomes.problem(draftKey)!=="unreadable"&&
        view.unverifiedDrafts.active(draftKey,scope))archiveLegacyDraft()
    }
    const snapshot=page.snapshot!,fresh:W.ProjectAssistantGetResponse={contract_version:page.contract_version,bot:snapshot.bot,conversation:page.head.conversation,source:page.source}
    activeMutations.current.forEach(control=>control.abort())
    for(const control of reads.current)control.abort()
    reads.current.clear();collectionBusy.current.clear()
    collectionCursors.current={permissions:new Set(),traces:new Set()};historyCursors.current=new Set()
    setTraceLoad(traceRequested.current?"loading":"idle")
    setPermissions([]);permissionRows.current=[];requestedTraceReferences.current.clear();backgroundTraceReferences.current.clear();setObservedBrowserTraces({});setTraceSummaries([]);traceRows.current=[];setDetails({});detailRows.current={};setPermissionMore("");setTraceMore("")
    setTurns([...snapshot.pending_turns,...snapshot.last_turn?[snapshot.last_turn]:[]])
    const queued=snapshot.queued_messages.map(message=>({id:message.id,kind:"message" as const,anchor_message_seq:message.seq,observed_event_seq:page.head.event_frontier,message}))
    const rows=mergeTimeline([page.items,queued]);observation.apply(page)
    prepareReadDisplay(trace);timelineRef.current=rows;setTimeline(rows)
    epoch.current=page.source_epoch;lastSeq.current=page.head.event_frontier;callViewFrontier.current=page.head.event_frontier
    headRef.current=page.head;setHead(page.head);currentData.current=fresh;setData(fresh)
    setMore(page.next_cursor);setError("")
    // No receipts in a bounded snapshot means an unknown local intent remains unknown.
    view.recoveryRequired.delete(draftKey);recovery.current=false;setRecoveryRequired(false)
    return page
  },[read,request,valid,view,draftKey,scope,entryDraft,heldAtMount,archiveLegacyDraft,source,prepareReadDisplay])
  const collection=useCallback(async(mode:"permissions"|"traces",cursor="")=>{
    const before=headRef.current,capturedEpoch=epoch.current
    if(!before||recovery.current||!valid()||collectionBusy.current.has(mode))return
    const control=new AbortController();reads.current.add(control);collectionBusy.current.set(mode,control)
    try{
      const page=await read(request(mode,cursor),control.signal)
      if(!valid()||control.signal.aborted||epoch.current!==capturedEpoch||!headRef.current||!sameFence(before.current_fence,headRef.current.current_fence)||recovery.current)return
      // Let catch propagate this typed boundary before lock aborts the read.
      // Aborting here would misclassify our own transition as external cancel.
      if(!sameFence(page.head.current_fence,before.current_fence))throw failure("pa_stale_generation")
      if(!cursor)collectionCursors.current[mode]=new Set()
      const cursors=collectionCursors.current[mode]
      if(page.has_more&&cursors.has(page.next_cursor))throw failure("pa_invalid")
      if(cursor)cursors.add(cursor)
      adoptHead(page)
      if(mode==="permissions"){
        const added=page.items.map(row=>row.permission_summary!)
        const base=cursor?permissionRows.current:[],ids=new Set(base.map(row=>row.id));if(added.some(row=>ids.has(row.id)))throw failure("pa_invalid");const rows=[...base,...added];permissionRows.current=rows;setPermissions(rows);setPermissionMore(page.next_cursor);setPermissionsRead(true)
      }else{
        const added=page.items.map(row=>row.trace_summary!)
        const base=cursor?traceRows.current:[],ids=new Set(base.map(row=>row.id));if(added.some(row=>ids.has(row.id)))throw failure("pa_invalid");const rows=[...base,...added];traceRows.current=rows;setTraceSummaries(rows);setTraceMore(page.next_cursor)
      }
      return true
    }catch(e){if(!control.signal.aborted&&valid())throw e}
    finally{reads.current.delete(control);if(collectionBusy.current.get(mode)===control)collectionBusy.current.delete(mode)}
  },[read,request,valid,adoptHead])
  const loadTraceSummaries=useCallback(async()=>{
    if(collectionBusy.current.has("traces")||!valid()||recovery.current)return
    traceRequested.current=true
    setTraceLoad("loading")
    try {
      if(await collection("traces"))setTraceLoad("ready")
    }catch{
      setTraceLoad("failed")
    }
  },[collection,valid])
  refreshRun.current=async(manual)=>{
    if(!valid())return
    if(source.current?.()===false&&call.active)call.end("disconnected")
    if(document.visibilityState==="hidden"&&!manual&&!observation.active||recovery.current&&!manual)return
    if(timer.current){clearTimeout(timer.current);timer.current=null}
    // Consume a final catch-up demand once. An older snapshot must not turn
    // it into an immediate loop; the existing cadence still owns later reads.
    if(manual)observation.finalSnapshot=false
    const control=new AbortController(),rebuilding=recovery.current||!currentData.current
    let snapshotValidated=false,nextReadDelay=chatEventDelay(pushedRef.current,observation.active)
    eventControl.current=control
    const settleCollections=async(modes:("permissions"|"traces")[])=>{
      // A projection failing at an older fence is retried on the next poll.
      // It cannot take a validated chat snapshot or a write receipt offline.
      await Promise.all(modes.map(async mode=>{
        try{
          const loaded=await collection(mode)
          if(!control.signal.aborted&&valid()){
            collectionsToRetry.current.delete(mode)
            if(loaded&&mode==="traces"&&deferTraceCount)setTraceLoad("ready")
          }
        }catch{
          if(!control.signal.aborted&&valid())collectionsToRetry.current.add(mode)
        }
      }))
    }
    const readSnapshot=async()=>{
      await recoverSnapshot(control.signal)
      if(!valid()||control.signal.aborted)return
      snapshotValidated=true
      if(deferTraceCount){
        if(traceRequested.current)void loadTraceSummaries()
        await settleCollections(["permissions"])
      }else await settleCollections(["permissions","traces"])
    }
    const readRecovered=()=>{
      if(!valid()||control.signal.aborted)return
      retryDelay.current=BACKOFF.firstMs
      setReconnecting(false)
      if(recovery.current||!currentData.current||!canWrite(currentData.current.source))return
      if(observation.finalSnapshot&&(document.visibilityState!=="hidden"||observation.active))void refresh(true)
      else scheduleRead(nextReadDelay)
    }
    try {
      if(recovery.current||!currentData.current||manual||!!error)await readSnapshot()
      else {
        const before=headRef.current!,capturedEpoch=epoch.current
        const page=await read(request("events","",256),control.signal)
        if(!valid()||control.signal.aborted||capturedEpoch!==epoch.current||recovery.current)return
        if(!sameFence(before.current_fence,page.head.current_fence)) {
          lock()
          await readSnapshot()
        }else{
          adoptHead(page);observation.apply(page)
          const rows=eventTimeline(timelineRef.current,page.items);timelineRef.current=rows;setTimeline(rows)
          for(const row of page.items){const event=row.event!
            if(event.turn&&(event.turn.state==="queued"||sameFence(event.turn.fence,page.head.current_fence)))
              setTurns(old=>placeTurn(old,turnSummary(event.turn!)))
            if(!sameFence(event.fence,page.head.current_fence))continue
            if(event.message?.kind==="assistant"&&event.message.turn_id){const id=event.message.turn_id;setActivity(old=>old[id]?{...old,[id]:""}:old)}
            if(event.trace){if(browserToolTrace(event.trace)){const trace=event.trace;setObservedBrowserTraces(old=>({...old,[trace.turn_id]:trace}))}const now=visibleTrace(event.trace)?activityOf(event.trace):"",id=event.trace.turn_id;if(now)setActivity(old=>({...old,[id]:now}))}
            if(event.permission&&event.permission.state!=="pending"&&detailRows.current[event.permission.id]){
              const next={...detailRows.current};delete next[event.permission.id];detailRows.current=next;setDetails(next)
            }
            if(event.kind==="feature_disabled"||event.source&&!canWrite(event.source))activeMutations.current.forEach(control=>control.abort())
          }
          // Another bounded page is already available. The idle cadence
          // resumes once observation reaches the end of the backlog.
          if(page.has_more)nextReadDelay=0
          lastSeq.current=page.next_seq;callViewFrontier.current=page.has_more?page.next_seq:page.head.event_frontier;setError("")
          const modes=new Set(collectionsToRetry.current)
          if(before.pending_permission_count!==page.head.pending_permission_count||page.items.some(row=>row.event?.permission))modes.add("permissions")
          const traceChanged=page.items.some(row=>row.event?.trace)
          if(deferTraceCount?traceRequested.current&&traceChanged:before.trace_category_count!==page.head.trace_category_count||traceChanged)modes.add("traces")
          await settleCollections([...modes])
        }
      }
      readRecovered()
    }catch(error){
      if(control.signal.aborted||!valid())return
      const code=errorCode(error)
      if((code==="pa_cursor_expired"||code==="pa_stale_generation"||code==="pa_invalid")&&(!rebuilding||snapshotValidated)){
        lock()
        try{await readSnapshot();readRecovered()}
        catch(snapshotError){if(!control.signal.aborted)readFailed(snapshotError)}
      }else readFailed(error)
    }
  }
  useEffect(()=>{
    alive.current=true;const control=new AbortController()
    refreshQueue.current=serialSnapshotRefresh(manual=>refreshRun.current(manual),control.signal)
    const pending=view.heldOutcomes.get(draftKey)
    if(pending?.receipt)observation.accept(pending.receipt)
    if(pending?.intent.command.operation==="send"&&!admittedReceipt(pending.receipt)&&!isBotCallSend(pending.intent.command.idempotency_key)
      &&!view.heldOutcomes.transporting(draftKey,pending.intent.command.idempotency_key)){
      const intent=pending.intent as W.ProjectAssistantSendRequest
      view.heldOutcomes.setAside(draftKey)
      if(!view.draft(recoveryKey)&&view.draft(draftKey)===intent.text){
        view.stage(draftKey,"");view.flush(draftKey);setText("")
        if(view.quotes.get(draftKey)===intent.quote_message_id){view.quotes.delete(draftKey);setQuote("")}
      }
      setUnknown(false);repaintJournal()
    }
    // Initial entry uses only the new consistent bounded snapshot.
    // Keep the recovery latch until the bounded snapshot validates.
    void refresh(true)
    // pagehide may run while visibilityState is still visible. Leaving must
    // pause IO rather than start a refresh that the unloading page cannot read.
    const pause=()=>{observation.stop();eventControl.current?.abort();if(timer.current){clearTimeout(timer.current);timer.current=null}for(const read of reads.current)read.abort();view.flush(draftKey)}
    const visibility=()=>{if(document.visibilityState==="hidden"){if(!observation.active)pause()}else {requestReceiptCheck(n=>n+1);void refresh(true)}}
    const show=(event:PageTransitionEvent)=>{if(event.persisted)visibility()}
    document.addEventListener("visibilitychange",visibility);window.addEventListener("pagehide",pause);window.addEventListener("pageshow",show);window.addEventListener("online",visibility)
    return()=>{alive.current=false;observation.stop();control.abort();eventControl.current?.abort();activeMutations.current.forEach(control=>control.abort());for(const read of reads.current)read.abort();if(timer.current)clearTimeout(timer.current);view.flush(draftKey);document.removeEventListener("visibilitychange",visibility);window.removeEventListener("pagehide",pause);window.removeEventListener("pageshow",show);window.removeEventListener("online",visibility)}
  },[identity,source,current,refresh,valid,fail,draftKey,view,recoverSnapshot,collection,lock])
  // A changed roster row for this Bot runs the existing single-flight cursor read.
  const pushSignal=chatPushSignal(summary)
  const lastPushSignal=useRef(pushSignal),lastPushed=useRef(pushed)
  useEffect(()=>{
    // Losing the stream replaces the slow fallback timer with a catch-up read,
    // after which the ordinary cadence resumes.
    const lost=lastPushed.current&&!pushed
    lastPushed.current=pushed
    if(lastPushSignal.current===pushSignal&&!lost)return
    lastPushSignal.current=pushSignal
    if(!pushed&&!lost||!currentData.current||recovery.current||document.visibilityState==="hidden")return
    if(timer.current){clearTimeout(timer.current);timer.current=null}
    void refresh(false)
  },[pushSignal,pushed,refresh])
  // A send made by a Chat that was left settles in the host journal.
  const refreshRef=useRef(refresh);refreshRef.current=refresh
  useEffect(()=>{
    const landed=(id:string,answer:W.ProjectAssistantReceipt|null)=>{
      const was=awaiting.current
      if(!alive.current||!was||was.command.idempotency_key!==id)return
      awaiting.current=null
      const held=view.heldOutcomes.get(draftKey)
      setText(view.draft(draftKey));setQuote(view.quotes.get(draftKey)??"")
      setReceipt(held?.receipt??answer);setUnknown(uncertain())
      setBusy(false);setSending(admittedReceipt(answer)?was.text:null);if(answer)observation.accept(answer);repaintJournal()
      if(valid())void refreshRef.current(true)
    }
    const stop=view.heldOutcomes.subscribe(draftKey,n=>{if(alive.current)repaintJournal();landed(n.idempotency_key,n.receipt)})
    // It may have landed between this Chat's first render and this subscription.
    const was=awaiting.current
    const settled=view.heldOutcomes.find(draftKey,was?.command.idempotency_key??"")
    if(was&&!view.heldOutcomes.transporting(draftKey,was.command.idempotency_key))landed(was.command.idempotency_key,settled?.receipt??null)
    return stop
  },[view,draftKey,valid])

  useLayoutEffect(()=>{
    const el=scroll.current;if(!el||!data)return
    if(!restored.current){const saved=view.position.get(draftKey);el.scrollTop=saved?.following===false?saved.top:el.scrollHeight;followingRef.current=saved?.following??true;setFollowing(followingRef.current);restored.current=true}
    else if(followingRef.current)el.scrollTop=el.scrollHeight
  },[messages,timeline,turns,queueEditor,data,draftKey,view])
  // The end of the conversation also grows without a new message: background work starting or
  // ending, the dispatched-work list opening. A reader at the newest end stays there; one who
  // scrolled up keeps their place, because content added below the viewport does not move it.
  useEffect(()=>{
    const el=scroll.current,content=el?.firstElementChild
    if(!el||!content||typeof ResizeObserver==="undefined")return
    const observer=new ResizeObserver(()=>{if(restored.current&&followingRef.current)el.scrollTop=el.scrollHeight})
    observer.observe(content)
    return()=>observer.disconnect()
  },[!!data])
  const markVisible=()=>{
    if(thread)return
    const el=scroll.current,d=currentData.current
    if(!el||!d||!readerID||!valid()||recovery.current||reading.current||!canMarkConversationRead(d.source)||el.closest("[inert]")||document.visibilityState==="hidden"||!document.hasFocus())return
    const rect=el.getBoundingClientRect()
    const rows=[...el.querySelectorAll<HTMLElement>("[data-message-seq]")].map(row=>{const r=row.getBoundingClientRect();return{top:r.top,bottom:r.bottom,seq:Number(row.dataset.messageSeq)}})
    const saved=summary.read_cursor,seq=seenThrough(rows,rect.top,rect.bottom)
    const readSeq=saved.conversation_id===scope.conversation_id?saved.message_seq:0
    if(seq<=Math.max(lastRead.current,readSeq))return
    const control=new AbortController()
    let advanced=false
    reading.current=true;reads.current.add(control)
    void markConversationRead(source,current,scope,readerID,control.signal,seq).then(cursor=>{
      if(!valid()||control.signal.aborted)return
      lastRead.current=cursor.message_seq;onRead?.()
      advanced=true
    }).catch(error=>{if(!control.signal.aborted)fail(error)}).finally(()=>{
      reading.current=false;reads.current.delete(control)
      // A newer message can enter the viewport while the preceding cursor is in flight.
      if(advanced)markVisible()
    })
  }
  useEffect(()=>{markVisible()},[messages,data])
  useEffect(()=>{identityButton.current?.focus({preventScroll:true})},[identity])
  const onScroll=()=>{const el=scroll.current;if(!el)return;const end=el.scrollHeight-el.scrollTop-el.clientHeight<=1;followingRef.current=end;setFollowing(end);view.position.set(draftKey,{top:el.scrollTop,following:end});markVisible()}
  const mutate=async<T,>(operation:(d:W.ProjectAssistantGetResponse,s:AbortSignal,head:W.ProjectAssistantReadViewResponse)=>Promise<T>,{reportReadFailure=true,lifetime}:{reportReadFailure?:boolean;lifetime?:AbortSignal}={}):Promise<T>=>{
    const captured=currentData.current,capturedEpoch=epoch.current
    if(recovery.current||!captured||!canWrite(captured.source)||!valid())throw failure("pa_read_only")
    const control=lifetime?null:new AbortController()
    if(control)activeMutations.current.add(control)
    const currentView=()=>!recovery.current&&epoch.current===capturedEpoch&&(!!lifetime||valid())
    try{return await authorized(source,current,async s=>{
      let page:W.ProjectAssistantReadViewResponse
      try{page=await read(request("snapshot"),s)}catch(e){if(reportReadFailure&&!s.aborted)readFailed(e);throw e}
      if(s.aborted||!currentView())throw failure("pa_read_only")
      if(!canWrite(page.source))throw failure("pa_read_only")
      const stale=page.source_epoch!==capturedEpoch?"pa_cursor_expired":!sameFence(fence(captured),page.head.current_fence)?"pa_stale_generation":null
      if(stale){const error=failure(stale);if(reportReadFailure)readFailed(error);throw error}
      if(valid())adoptHead(page)
      const fresh:W.ProjectAssistantGetResponse={contract_version:page.contract_version,bot:page.snapshot!.bot,conversation:page.head.conversation,source:page.source}
      // Preflight cannot replace the visible window or advance replay past unseen facts.
      if(s.aborted||!currentView()||!headRef.current||!sameFence(page.head.current_fence,headRef.current.current_fence))throw failure("pa_read_only")
      return operation(fresh,s,page)
    },lifetime??control!.signal)}finally{if(control)activeMutations.current.delete(control)}
  }
  const [images,setImages]=useState<MarkupPicture[]>([])
  const editBot=async(settings:W.ProjectAssistantEditSettings,original?:W.ProjectAssistantBot)=>{
    const result=await mutate(({bot:fresh},signal)=>{
      // A save that names its base is rebased by the source, field by field.
      if(original&&!settings.base&&fresh.version!==original.version){
        if(!sameEditableSettings(original,fresh))throw failure("pa_stale_version")
        settings={...settings,expected_version:fresh.version}
      }
      return source.edit({command:command(key,"edit"),settings},signal)
    })
    if(!valid())throw failure("pa_outcome_unknown")
    setData(old=>old?{...old,bot:result.bot}:old)
    onBotChanged(result.bot)
  }
  const saveSettings=async(settings:W.ProjectAssistantEditSettings,original:W.ProjectAssistantBot)=>{
    try{await editBot(settings,original)}catch(e){
      // The editor shows an unknown outcome inline with its own check action; a toast would contradict a later check.
      if(valid()&&errorCode(e)!=="pa_outcome_unknown")toast(w.settingsSaveFailed,true)
      throw e
    }
    setSettingsRevision(n=>n+1)
    setSettings(false)
    toast(w.settingsSaved)
  }
  // Reconciliation reads the Bot through the same authorized, epoch- and fence-checked preflight as a write.
  const checkSettings=async()=>{
    const fresh=await mutate(async({bot},_,page)=>{
      // A snapshot answered after a newer head was observed describes an older source; it neither confirms nor reverts.
      const seen=headRef.current
      if(!seen||page.head.event_frontier<seen.event_frontier||page.head.bot_version<seen.bot_version||bot.version<seen.bot_version)throw failure("pa_stale_version")
      return bot
    })
    if(!valid())throw failure("pa_read_only")
    setData(old=>old?{...old,bot:fresh}:old)
    onBotChanged(fresh)
    return fresh
  }
  const saveModel=(original:W.ProjectAssistantBot,model:string,overwrite=false)=>editBot(modelEdit(original,model,source.canSettingsBase?.()===true,overwrite),original)
  const marker="clawdline.bot-inputs."+draftKey
  const [files,setFiles]=useState<DraftInput[]>([]),[filesLost,setFilesLost]=useState(()=>{try{return localStorage.getItem(marker)==="selected"}catch{return true}})
  const [uploading,setUploading]=useState(""),[fileError,setFileError]=useState("")
  const uploadControl=useRef<AbortController|null>(null)
  const rememberFiles=(selected:boolean)=>{try{if(selected)localStorage.setItem(marker,"selected");else localStorage.removeItem(marker)}catch{setDraftDurable(false)}}
  const canInput=!thread&&!!source.canInputs?.()&&!!source.upload
  const recovering=recoveringID?view.heldOutcomes.find(draftKey,recoveringID):undefined
  const missingPictures=Math.max(0,(recovering?.pictures??0)-images.length)
  const missingFiles=Math.max(0,(recovering?.files?.length??0)-files.length)
  const composerState=useRef({text,quote,images,files})
  composerState.current={text,quote,images,files}
  const [skipMissing,setSkipMissing]=useState(false)
  const [recoveryNotice,setRecoveryNotice]=useState("")
  const setRecovery=(id:string)=>{
    view.stage(recoveryKey,id);view.flush(recoveryKey);setRecoveringID(id);setSkipMissing(false)
  }
  const pausedAssets=useRef<SendAttachments|null>(null)
  const pauseComposer=()=>{
    const {quote,images,files}=composerState.current
    const text=view.draft(draftKey),recoveringID=view.draft(recoveryKey)
    const present=!!(text||quote||images.length||files.length)
    view.stage(pausedKey,present?JSON.stringify({text,quote,recoveringID,skipMissing,pictureCount:images.length,fileCount:files.length}):"")
    view.flush(pausedKey);setPaused(present)
    pausedAssets.current={images,files}
    if(images.length||files.length)void saveSendAttachments(assetKey(pausedKey),{images,files}).catch(()=>setDraftDurable(false))
    else void clearSendAttachments(assetKey(pausedKey))
  }
  const resumeComposer=async()=>{
    const serialized=view.draft(pausedKey)
    if(!serialized)return
    let previous:{text:string;quote:string;recoveringID?:string;skipMissing?:boolean;pictureCount?:number;fileCount?:number}
    try{previous=JSON.parse(serialized)}catch{return}
    const assets=pausedAssets.current??await loadSendAttachments(assetKey(pausedKey)).catch(()=>null)
    if(!valid()||view.draft(pausedKey)!==serialized)return
    pauseComposer()
    changeText(previous.text);setQuote(previous.quote)
    if(previous.quote)view.quotes.set(draftKey,previous.quote);else view.quotes.delete(draftKey)
    setImages(assets?.images??[]);setFiles(assets?.files??[]);setFilesLost((previous.pictureCount??0)>(assets?.images.length??0)||(previous.fileCount??0)>(assets?.files.length??0));rememberFiles(!!assets?.files.length)
    const previousID=previous.recoveringID
    setRecovery(previousID&&view.heldOutcomes.find(draftKey,previousID)?previousID:"")
    setSkipMissing(!!previous.skipMissing);flush()
  }
  const restoreSend=(held:HeldOutcome)=>{
    const intent=held.intent as W.ProjectAssistantSendRequest,id=intent.command.idempotency_key
    const assets=pendingAttachments[attachmentsFor(held)]
    if(busy||recoveringID||paused||!valid()||!sameConversation(intent.fence,scope))return
    sendReplay.delete(held)
    pauseComposer()
    changeText(intent.text);setQuote(intent.quote_message_id)
    if(intent.quote_message_id)view.quotes.set(draftKey,intent.quote_message_id);else view.quotes.delete(draftKey)
    setImages(assets?.images??intent.images?.map(url=>({id:crypto.randomUUID(),url}))??[]);setFiles(assets?.files??[])
    setFilesLost(false);rememberFiles(false);setFileError("");setRecoveryNotice("")
    setRecovery(id);flush()
    scroll.current?.parentElement?.querySelector<HTMLTextAreaElement>(".pa-composer textarea")?.focus()
  }
  const mountedRecovery=useRef(recovering)
  useEffect(()=>{
    const held=mountedRecovery.current
    if(!held)return
    const intent=held.intent as W.ProjectAssistantSendRequest
    if(!sameConversation(intent.fence,scope))return
    const edited=entryDraft.text
    void loadSendAttachments(attachmentsFor(held)).then(assets=>{
      if(!valid()||view.draft(recoveryKey)!==intent.command.idempotency_key||view.draft(draftKey)!==edited)return
      if(intent.quote_message_id)view.quotes.set(draftKey,intent.quote_message_id)
      setQuote(intent.quote_message_id);setImages(old=>old.length?old:assets?.images??[]);setFiles(old=>old.length?old:assets?.files??[])
      setFilesLost(false)
    }).catch(()=>{})
  },[draftKey,valid])
  const addFiles=(selected:readonly File[])=>{
    if(!canInput||busy||unknown||legacyBlocked())return false
    if(selected.some(f=>f.size>MAX_INPUT_BYTES)){setFileError(w.inputLimits);return false}
    if(selected.some(f=>!f.name||f.name==="."||f.name===".."||/[\/\\\p{Cc}]/u.test(f.name)||new TextEncoder().encode(f.name).length>128)){setFileError(w.invalidFileName);return false}
    const next=[...files,...selected.map(file=>({file,id:crypto.randomUUID()}))];setFiles(next);setFileError("");setFilesLost(false);rememberFiles(true);return true
  }
  const canAttach=!thread&&!!source.pictures?.()
  const [readingImages,setReadingImages]=useState(0),imageReads=useRef(0)
  const attachmentsEnabled=(canAttach||canInput)&&enabled&&!unknown&&!busy&&!legacyBlocked()
  const attach=(selected:readonly File[])=>{
    if(!attachmentsEnabled||!valid())return
    const pictures:File[]=[],regular:File[]=[]
    for(const file of selected){
      const image=pictureFile(file)
      if(image) pictures.push(image)
      else regular.push(file)
    }
    if(regular.some(file=>!!file.webkitRelativePath)){setFileError(w.foldersUnsupported);return}
    if((pictures.length&&!canAttach)||(regular.length&&!canInput)){setFileError(w.filesUnavailable);return}
    if(regular.length&&!addFiles(regular))return
    if(!pictures.length)return
    setFileError("");setReadingImages(++imageReads.current)
    // Large photos are downscaled here, before they become part of a send.
    void Promise.all(pictures.map(pictureDataURL)).then(urls=>{
      if(!valid())return
      const added=urls.map(url=>({id:crypto.randomUUID(),url}))
      setImages(old=>[...old,...added])
    })
      .catch(fail)
      .finally(()=>{const pending=--imageReads.current;if(valid())setReadingImages(pending)})
  }
  // Why a file drag cannot land here, in the person's words; "" when it can.
  const dropRefusal=(transfer:DataTransfer)=>{
    if(thread)return w.dropBlockedThread
    if(!canAttach&&!canInput)return w.dropBlockedUnavailable
    if(reconnecting||error==="pa_offline"||data&&data.source.state==="offline")return w.dropBlockedOffline
    if(!enabled||unknown||legacyBlocked())return w.dropBlockedReadOnly
    if(busy)return w.dropBlockedBusy
    // Browsers expose each dragged file's type before the drop; an empty type is decided by name on drop.
    if(!canInput&&Array.from(transfer.items??[]).some(item=>item.kind==="file"&&!!item.type&&!item.type.startsWith("image/")))return w.dropBlockedImagesOnly
    return ""
  }
  const [dropTarget,setDropTarget]=useState<{refusal:string}|null>(null),dragDepth=useRef(0)
  const showDropTarget=(transfer:DataTransfer)=>{const refusal=dropRefusal(transfer);setDropTarget(old=>old?.refusal===refusal?old:{refusal});return refusal}
  const endFileDrag=useCallback(()=>{dragDepth.current=0;setDropTarget(null)},[])
  useEffect(()=>{
    if(!dropTarget)return
    // A drag dropped elsewhere on the page or cancelled with Esc may not send the chat its last dragleave.
    const key=(e:KeyboardEvent)=>{if(e.key==="Escape")endFileDrag()}
    window.addEventListener("drop",endFileDrag,true);window.addEventListener("dragend",endFileDrag,true);window.addEventListener("keydown",key,true)
    return ()=>{window.removeEventListener("drop",endFileDrag,true);window.removeEventListener("dragend",endFileDrag,true);window.removeEventListener("keydown",key,true)}
  },[dropTarget,endFileDrag])
  const dragHandlers={
    onDragEnter:(e:DragEvent<HTMLDivElement>)=>{if(!fileDrag(e.dataTransfer))return;e.preventDefault();dragDepth.current++;showDropTarget(e.dataTransfer)},
    onDragOver:(e:DragEvent<HTMLDivElement>)=>{if(!fileDrag(e.dataTransfer))return;e.preventDefault();e.dataTransfer.dropEffect=showDropTarget(e.dataTransfer)?"none":"copy"},
    // Moving between children sends enter before leave, so the count never reaches zero mid-chat.
    onDragLeave:(e:DragEvent<HTMLDivElement>)=>{if(!fileDrag(e.dataTransfer)||dragDepth.current===0)return;if(--dragDepth.current===0)setDropTarget(null)},
    onDrop:(e:DragEvent<HTMLDivElement>)=>{endFileDrag();dropFiles(e)},
  }
  const dropFiles=(event:DragEvent<HTMLDivElement>)=>{
    const transfer=event.dataTransfer
    if(!fileDrag(transfer))return
    event.preventDefault()
    if(thread||!attachmentsEnabled)return
    const items=Array.from(transfer.items??[])
    if(items.some(item=>item.webkitGetAsEntry?.()?.isDirectory)){setFileError(w.foldersUnsupported);return}
    const selected=Array.from(transfer.files)
    attach(selected.length?selected:items.flatMap(item=>{const file=item.getAsFile();return file?[file]:[]}))
  }
  useEffect(()=>{
    const held=view.heldOutcomes.get(draftKey)
    if(!held||held.intent.command.operation!=="send"||!admittedMessage(held.intent as W.ProjectAssistantSendRequest,held.receipt,messages))return
    view.heldOutcomes.settle(draftKey,held.intent.command.idempotency_key,null)
    clearPendingAttachments(held)
    setSending(null);setUnknown(false);repaintJournal()
  },[timeline,receipt,view,draftKey])
  // The host resolves quotes within the conversation and checks memory access
  // when it admits the message, even while this view is catching up.
  const textOnlyDraft=images.length===0&&files.length===0&&handoff.card?.state!=="draft"
  // An agent that cannot steer refuses Send now with pa_unsupported/steering;
  // the message stays queued and the composer still sends.
  const sendAuthorityLost=["pa_revoked","pa_unpaired","pa_scope_denied","pa_unsupported","pa_disabled"].includes(error)&&!(error==="pa_unsupported"&&errorField==="steering")
  const canTryText=textOnlyDraft&&!thread&&!unknown&&!legacyBlocked()&&!sendAuthorityLost
  const sendInFlight=useRef(false)
  const send=async(additional?:{message?:W.ProjectAssistantMessage;text:string}):Promise<ArtifactSendResult>=>{
    const refused={state:"refused"} as const
    const artifact=additional?.message
    const review=!!additional&&!artifact
    const plain=additional
      ?!thread&&!sendAuthorityLost&&handoff.card?.state!=="draft"&&!!additional.text.trim()
      :canTryText&&!!text.trim()
    if(!valid()||leadSetup.blocked||call.active||busy||sendInFlight.current||imageReads.current>0||unknown||legacyBlocked()||(!additional&&(filesLost||(!skipMissing&&(missingPictures>0||missingFiles>0))))||editingHandoff||handoff.mutating()
      ||(!additional&&!text.trim()&&images.length===0&&files.length===0)
      ||handoff.error||(!plain&&!currentData.current))return refused
    const earlier=view.heldOutcomes.get(draftKey)
    if(earlier){
      if(earlier.intent.command.operation!=="send"||!admittedReceipt(earlier.receipt))return refused
      view.heldOutcomes.setAside(draftKey)
    }
    const handoffRef=handoff.card?.state==="draft"?handoff.card.ref:undefined
    if(additional&&(!additional.text.trim()||handoffRef))return refused
    if(artifact&&(!artifact.output?.artifact_id||!sameConversation(artifact.fence,scope)))return refused
    const submitted=additional?.text??text,pictures=additional?[]:images,selected=additional?[]:files
    const captured=headRef.current?.current_fence??(currentData.current?fence(currentData.current):{...scope,binding_id:"",generation:0})
    const recovered=additional?undefined:recovering
    const originalID=recovered?.intent.command.idempotency_key
    const idempotencyKey=review?TURN_REVIEW_PREFIX+crypto.randomUUID():recovered&&!recovered.superseded?originalID:undefined
    const sendCommand=command(key,"send",idempotencyKey)
    if(recovered&&!recovered.superseded&&source.canRecoverSend?.())sendCommand.client_attempt_id=crypto.randomUUID()
    const intent:W.ProjectAssistantSendRequest={command:sendCommand,fence:captured,text:submitted,quote_message_id:additional?artifact?.id??"":quote,...(handoffRef?{handoff:handoffRef}:{}),...(pictures.length?{images:pictures.map(picture=>picture.url)}:{})}
    const id=intent.command.idempotency_key
    sendBaselineSeq.current=messages.reduce((max,m)=>m.seq>max?m.seq:max,0)
    sendInFlight.current=true;setBusy(true);setSending(submitted);setLocalSend(id);setReceipt(null)
    followingRef.current=true;setFollowing(true)
    const hold=(receipt:W.ProjectAssistantReceipt|null):HeldOutcome=>({intent,receipt,...(pictures.length?{pictures:pictures.length}:{}),...(selected.length?{files:selected.map(f=>f.file.name)}:{})})
    // This send's own answer changes only its own journal entry: never a newer
    // held send, and a send set aside stays aside. Then any Chat showing this
    // Bot, including one opened after this one was left, is told once.
    const land=(outcome:HeldOutcome|null)=>{
      const replacement=outcome??(originalID===id?recovered:null)??null
      if(view.heldOutcomes.settle(draftKey,id,replacement)==="active"&&replacement&&!admittedReceipt(replacement.receipt))view.heldOutcomes.setAside(draftKey)
      if(!outcome&&(!replacement||attachmentsFor(replacement)!==attachmentsFor(hold(null))))clearPendingAttachments(hold(null))
      if(outcome&&recovered&&attachmentsFor(recovered)!==attachmentsFor(outcome)){
        if(originalID!==id)view.heldOutcomes.settleAside(draftKey,originalID!)
        clearPendingAttachments(recovered)
      }
    }
    const clearRecovery=()=>{
      if(!recovered)return
      view.stage(recoveryKey,"");view.flush(recoveryKey)
      if(valid())setRecovery("")
    }
    const restore=()=>{
      if(additional)return
      const currentDraft=view.draft(draftKey)
      if(currentDraft!==submitted)view.stage(draftKey,currentDraft?`${submitted}

${currentDraft}`:submitted)
      if(quote&&!view.quotes.get(draftKey))view.quotes.set(draftKey,quote)
      if(selected.length)rememberFiles(true)
      if(!valid())return
      setText(view.draft(draftKey));setQuote(view.quotes.get(draftKey)??"");setImages(old=>old.length?old:pictures);setFiles(old=>old.length?old:selected)
    }
    // Journal at submission, before the attachment preflight can yield to navigation.
    view.heldOutcomes.activate(draftKey,hold(null));view.heldOutcomes.beginTransport(draftKey,id)
    if(!additional){changeText("");setImages([]);setQuote("");view.quotes.delete(draftKey);rememberFiles(false);view.flush(draftKey)}
    const asset=attachmentsFor(hold(null)),assets={images:pictures,files:selected}
    const saved=pictures.length||selected.length?saveSendAttachments(asset,assets).catch(()=>{if(valid())setDraftDurable(false)}):undefined
    const prepare=async(lifetime:AbortSignal)=>{
      const prepared=plain&&!review?undefined:mutate(async(d,s,page)=>{
        if(review&&(page.head.unknown_turn_count===0||page.snapshot!.pending_turns.some(turn=>turn.state==="running"||turn.state==="waiting_permission")||
          page.snapshot!.queued_messages.some(message=>message.notice?.key==="restart_continuation")))throw failure("pa_stale_version")
        intent.fence=fence(d)
        if(selected.length){
          const upload=new AbortController();uploadControl.current=upload;const cancel=()=>upload.abort();s.addEventListener("abort",cancel,{once:true})
          try{if(valid())setUploading("0%");intent.upload_ids=await uploadInputs(source,intent.fence,selected,upload.signal,(n,total)=>{if(valid()&&!upload.signal.aborted)setUploading((total?Math.floor(n*100/total):100)+"%")},d.bot.instructions.revision)}
          finally{s.removeEventListener("abort",cancel);uploadControl.current=null;if(valid())setUploading("")}
        }
        view.heldOutcomes.set(draftKey,hold(null))
      },{reportReadFailure:!review,lifetime})
      try{await prepared}finally{await saved}
    }
    const rebind=plain&&!review?async(s:AbortSignal)=>{
      const fresh=await read(request("snapshot"),s)
      if(!sameConversation(fresh.head.current_fence,scope)||sameFence(intent.fence,fresh.head.current_fence))return false
      intent.fence=fresh.head.current_fence;view.heldOutcomes.set(draftKey,hold(null))
      return true
    }:undefined
    const {receipt:answer,error:sendError,attempted}=await transmitChatSend(source,current,intent,prepare,rebind)
    const accepted=admittedReceipt(answer),unresolved=answer?.state==="unknown"||!answer&&attempted&&uncertainAfterSend(sendError)
    const code=answer?.state==="failed"?"pa_declined":errorCode(sendError)
    const handoffRefused=(sendError as Partial<W.ProjectAssistantErrorResponse>|null)?.error?.field?.startsWith("handoff_")
    const refusedSend=!accepted&&!unresolved&&!additional&&!recovered&&!handoffRefused
      &&(attempted&&!provablyNotSent(sendError)||!valid())
    const row=accepted||unresolved||refusedSend?hold(answer?.state==="failed"?null:answer):null
    if(row&&unresolved&&!recovered&&!additional)sendReplay.set(row,{source,origin:observation})
    if(row&&refusedSend){
      const refusal:RetryFailure={code,at:Date.now(),initial:true,...(sendBlockedByInterruptedTurn(sendError)?{interrupted:true}:{})}
      if(!retryAllowed(refusal)&&!refusal.interrupted)row.needsEdit=true
      sendFailure.set(row,refusal)
    }
    land(row)
    if(accepted||unresolved&&!recovered)clearRecovery()
    if(!row||unresolved&&recovered)restore()
    view.flush(draftKey)
    view.heldOutcomes.endTransport(draftKey,id,answer);sendInFlight.current=false
    if(valid()){
      if(answer)observation.accept(answer)
      setReceipt(answer);setUnknown(uncertain());setBusy(false);repaintJournal()
      if(!accepted)setSending(null)
      if(handoffRefused)handoff.conflict(code)
      if(!row&&!review&&!handoffRefused)fail(sendError);else setError("")
      if(row)void refresh(!accepted)
    }
    return accepted?{state:"accepted",turn:answer!.resource_id}:{state:unresolved?"unknown":"refused",code}
  }

  const pendingRows=view.heldOutcomes.aside(draftKey).filter(row=>
    !isBotCallSend(row.intent.command.idempotency_key)&&sameConversation(row.intent.fence,scope))
  const pendingKey=JSON.stringify(pendingRows.map(attachmentsFor))
  useEffect(()=>{
    let active=true
    for(const row of pendingRows){
      const storageKey=attachmentsFor(row)
      if(pendingAttachments[storageKey]!==undefined)continue
      void loadSendAttachments(storageKey).catch(()=>undefined).then(value=>{
        if(active)setPendingAttachments(old=>({...old,[storageKey]:value??null}))
      })
    }
    return()=>{active=false}
  },[pendingKey,draftKey])
  useEffect(()=>{
    for(const row of pendingRows){
      if(!admittedReceipt(row.receipt)||!messages.some(m=>m.kind==="user"&&m.turn_id===row.receipt?.resource_id&&sameConversation(m.fence,scope)))continue
      const id=row.intent.command.idempotency_key
      view.heldOutcomes.settleAside(draftKey,id);clearPendingAttachments(row);repaintJournal()
    }
  },[timeline,journalRevision,draftKey])
  useEffect(()=>{
    if(!enabled||busy||retrying||document.visibilityState==="hidden")return
    const control=new AbortController()
    reads.current.add(control)
    void (async()=>{
      let received=false
      try{
        for(const row of pendingRows){
          if(control.signal.aborted)break
          if(row.superseded||admittedReceipt(row.receipt)||sendFailure.get(row)?.initial)continue
          const intent=row.intent as W.ProjectAssistantSendRequest,id=intent.command.idempotency_key
          if(view.heldOutcomes.transporting(draftKey,id))continue
          const answer=source.sendReceipt&&source.canRecoverSend?.()!==false
            ?await authorized(source,current,s=>source.sendReceipt!(intent,s),control.signal).catch(()=>null):null
          if(control.signal.aborted||!valid()||view.heldOutcomes.find(draftKey,id)!==row)continue
          const actual=answer?.receipt?.command?.client_attempt_id
          if(!sendAcknowledgement(answer,{...intent,command:{...intent.command,client_attempt_id:actual}})||!admittedReceipt(answer.receipt)){
            if(!retryOnReentry(row,source,observation,view.draft(recoveryKey)))continue
            void retrySend(row)
            break
          }
          const different=actual!==intent.command.client_attempt_id
          const editing=view.draft(recoveryKey)===id
          if(different&&intent.command.client_attempt_id&&!editing){
            view.heldOutcomes.settle(draftKey,id,{...row,needsEdit:true,superseded:true})
          }else{
            view.heldOutcomes.settle(draftKey,id,{...row,receipt:answer.receipt})
            if(editing){
              const composer=composerState.current
              const sentPictures=intent.images??pendingAttachments[attachmentsFor(row)]?.images.map(picture=>picture.url)??[]
              const samePictures=sentPictures.length===(row.pictures??0)&&composer.images.length===sentPictures.length
                &&composer.images.every((picture,index)=>picture.url===sentPictures[index])
              const sameFiles=composer.files.length===(intent.upload_ids?.length??0)
                &&composer.files.every((file,index)=>file.id===intent.upload_ids?.[index])
              if(!different&&view.draft(draftKey)===intent.text&&composer.quote===intent.quote_message_id&&samePictures&&sameFiles){
                changeText("");setQuote("");view.quotes.delete(draftKey);setImages([]);setFiles([]);rememberFiles(false);flush()
              }else setRecoveryNotice(w.originalAlreadyReceived)
              setRecovery("")
            }
          }
          received=true
        }
        if(received){repaintJournal();void refresh(true)}
      }finally{reads.current.delete(control)}
    })()
    return()=>{control.abort();reads.current.delete(control)}
  },[enabled,busy,retrying,head?.event_frontier,pendingKey,journalRevision,receiptCheckRevision,draftKey,source,current,valid])
  const retrySend=async(held:HeldOutcome)=>{
    const intent=held.intent as W.ProjectAssistantSendRequest,id=intent.command.idempotency_key
    if(!enabled||busy||retryInFlight.current||!valid()||view.heldOutcomes.transporting(draftKey,id)||view.heldOutcomes.find(draftKey,id)!==held)return
    const pictures=intent.images??pendingAttachments[attachmentsFor(held)]?.images.map(picture=>picture.url)??[]
    if(pictures.length!==(held.pictures??0))return
    retryInFlight.current=true;setRetrying(id);view.heldOutcomes.beginTransport(draftKey,id);sendReplay.delete(held)
    const q={...intent,...(pictures.length?{images:pictures}:{})}
    const outcome=await retryOnce(()=>authorized(source,current,s=>source.send(q,s),new AbortController().signal),
      answer=>!sendAcknowledgement(answer,q)?null:admittedReceipt(answer.receipt)?answer.receipt:answer.receipt.state==="failed"?"rejected":null)
    const accepted=outcome.kind==="admitted"?outcome.receipt:null
    const replacement=settledRetry(held,outcome)
    view.heldOutcomes.settle(draftKey,id,replacement)
    retryInFlight.current=false;view.heldOutcomes.endTransport(draftKey,id,accepted)
    if(valid()){
      if(accepted)observation.accept(accepted)
      setRetrying("");void refresh(true)
    }
  }
  /** Removes one provisional send from this device's journal. */
  const discardSend=(held:HeldOutcome)=>{
    const id=held.intent.command.idempotency_key
    if(retrying===id||view.heldOutcomes.transporting(draftKey,id)||view.heldOutcomes.find(draftKey,id)!==held)return
    const active=view.heldOutcomes.get(draftKey)===held
    if(!view.heldOutcomes.discard(draftKey,id)){setDiscardFailed(id);repaintJournal();return}
    if(active){setSending(null);setUnknown(false)}
    if(admittedReceipt(held.receipt))observation.stop(held.receipt!.resource_id)
    setDiscardFailed("");clearPendingAttachments(held)
    repaintJournal()
  }
  const leaveUnreadable=()=>{
    if(view.heldOutcomes.setAside(draftKey)){setUnknown(false);repaintJournal()}
  }

  const stop=async(expectedTurn?:string,signal?:AbortSignal)=>{
    const active=currentData.current?.conversation.active_turn_id
    if(!active||expectedTurn&&expectedTurn!==active||signal?.aborted||stopping.current||stopStatus?.turn===active||busy||!valid()||recovery.current)return "stop_unavailable"
    stopping.current=true;setBusy(true);setStopStatus({turn:active,state:"sending"})
    const action=command(key,"stop")
    let attempted=false
    try{
      const result=await mutate((d,s)=>{
        if(signal?.aborted||d.conversation.active_turn_id!==active)throw failure("pa_stale_generation")
        attempted=true
        return source.stop({command:action,fence:fence(d),turn_id:active},s)
      })
      if(valid()){
        setReceipt(result.receipt)
        if(result.receipt.state==="failed"){setStopStatus(null);fail(new Error("Stop request failed"));return "stop_refused"}
        setStopStatus({turn:active,state:result.receipt.state==="unknown"?"unknown":"acknowledged"})
        void refresh()
      }
      return result.receipt.state==="unknown"?"stop_outcome_unknown":result.receipt.state
    }catch(e){
      const uncertain=attempted&&memoryOutcomeUnknown(e)
      if(valid()&&uncertain)setStopStatus({turn:active,state:"unknown"})
      else if(valid()){setStopStatus(null);fail(e)}
      return uncertain?"stop_outcome_unknown":"stop_refused"
    }finally{stopping.current=false;if(valid())setBusy(false)}
  }

  const releaseRiskID=`pa-release-risk-${key.bot_id}`
  // Releases a Bot whose earlier Agent cleanup is unconfirmed; the earlier
  // turn keeps its unknown outcome and nothing is sent again.
  const releaseUnconfirmed=async()=>{
    if(busy||!valid()||recovery.current)return
    setBusy(true)
    try{
      await mutate((d,s)=>source.stop({command:command(key,"stop"),fence:fence(d),turn_id:"",release_unconfirmed:true},s))
      if(valid()){setError("");setErrorField(undefined);void refresh()}
    }catch(e){if(valid())fail(e)}
    finally{if(valid())setBusy(false)}
  }

  const editQueued=async(action:"begin"|"save"|"cancel",turnID:string,editID:string,text:string)=>{
    if(!source.editQueue||queueInFlight.current||busy||!valid()||recovery.current)return
    queueInFlight.current=true
    setQueueEditBusy(true)
    setQueueEditError("")
    const editCommand=command(key,"queue_edit")
    try{
      let original=text
      const result=await mutate((d,s,page)=>{
        const turn=page.snapshot!.pending_turns.find(row=>row.id===turnID&&row.state==="queued")
        const message=page.snapshot!.queued_messages.find(row=>row.id===turn?.message_id)
        if(!turn||!message||(turn.queue_edit_id??"")!==editID)throw failure("pa_stale_version")
        original=message.text
        return source.editQueue!({command:editCommand,fence:fence(d),turn_id:turnID,action,edit_id:editID,text:action==="save"?text:""},s)
      })
      if(!valid())return
      const receipt=result.receipt
      if(result.contract_version!=="project-assistants.v1"||receipt?.state!=="delivered"||!receipt.resource_id||
        receipt.command?.idempotency_key!==editCommand.idempotency_key||receipt.command.operation!=="queue_edit"||
        botIdentity(receipt.command.key)!==botIdentity(key)||action!=="begin"&&receipt.resource_id!==editID)throw failure("pa_outcome_unknown")
      setQueueEditor(action==="begin"?{turnID,editID:receipt.resource_id,text:original}:null)
    }catch(e){if(valid())setQueueEditError(errorCode(e))}
    finally{
      queueInFlight.current=false
      if(valid()){setQueueEditBusy(false);void refresh(true)}
    }
  }

  const [noSteering,setNoSteering]=useState<readonly string[]>([])
  const actOnQueued=async(id:string,turnID:string,operation:"send_now"|"stop")=>{
    const request=operation==="send_now"?source.sendNow:source.unqueue
    if(!request||queueInFlight.current||busy||!valid()||recovery.current)return
    queueInFlight.current=true
    setQueueAction({id,operation})
    setBusy(true)
    try{
      const result=await mutate((d,s)=>request.call(source,{command:command(key,operation),fence:fence(d),turn_id:turnID},s))
      if(valid()){setReceipt(result.receipt);await refresh()}
    }catch(e){
      if(operation==="send_now"&&steeringRefused(e)){if(valid()){setNoSteering(old=>old.includes(bot.agent)?old:[...old,bot.agent]);void refresh(true)}}
      else fail(e)
    }
    finally{
      queueInFlight.current=false
      if(valid()){setQueueAction(null);setBusy(false)}
    }
  }

  const loadRules=useCallback((signal:AbortSignal)=>source.rules!(key,signal).then(r=>r.rules),[source,key])

  const detail=async(kind:"permission"|"trace",id:string,reference:string,quiet=false)=>{
    const captured=headRef.current,capturedEpoch=epoch.current
    if(!captured||!valid()||recovery.current||collectionBusy.current.has(reference))return
    const permission=kind==="permission"?permissionRows.current.find(p=>p.id===id&&p.detail_cursor===reference):undefined
    if(kind==="permission"&&!permission)return
    const control=new AbortController();reads.current.add(control);collectionBusy.current.set(reference,control)
    try{
      const page=await read(request("detail",reference,1),control.signal)
      if(!valid()||control.signal.aborted||recovery.current||capturedEpoch!==epoch.current||!headRef.current||!sameFence(captured.current_fence,headRef.current.current_fence))return
      if(!sameFence(page.head.current_fence,captured.current_fence))throw failure("pa_stale_generation")
      const row=page.items[0]
      if(!row||row.kind!==kind||kind==="permission"&&row.permission?.id!==id)throw failure("pa_invalid")
      if(permission){
        const current=permissionRows.current.find(p=>p.id===id)
        if(!current||!samePermission(permission,current))return
        if(!row.permission||!samePermission(current,row.permission))throw failure("pa_stale_generation")
      }
      if(kind==="trace"){
        if(!row.trace)throw failure("pa_invalid")
        if(row.trace.id&&row.id!==id)throw failure("pa_invalid")
        if(traceRows.current.find(t=>t.id===id)?.detail_cursor!==reference)return
        if(!traceRows.current.some(t=>t.id===id&&t.turn_id===row.trace?.turn_id&&t.category===row.trace?.category)){
          if(quiet)return
          throw failure("pa_stale_generation")
        }
      }
      adoptHead(page);const next={...detailRows.current,[id]:row.permission??row.trace!};detailRows.current=next;setDetails(next)
      if(row.trace){requestedTraceReferences.current.set(id,reference);if(browserToolTrace(row.trace)){const trace=row.trace;setObservedBrowserTraces(old=>({...old,[trace.turn_id]:trace}))}}
      return true
    }catch(e){
      if(!control.signal.aborted&&valid()){
        if(quiet&&kind==="permission")throw e
        if(!quiet)readFailed(e)
      }
    }
    finally{reads.current.delete(control);if(collectionBusy.current.get(reference)===control)collectionBusy.current.delete(reference)}
    return false
  }
  const readTraceDetail=(row:W.ProjectAssistantReadTraceSummary)=>{
    if(requestedTraceReferences.current.get(row.id)===row.detail_cursor&&detailRows.current[row.id])return Promise.resolve(true)
    const pending=traceDetailReads.current.get(row.detail_cursor)
    if(pending)return pending
    const read=detail("trace",row.id,row.detail_cursor,true).finally(()=>traceDetailReads.current.delete(row.detail_cursor))
    traceDetailReads.current.set(row.detail_cursor,read)
    return read
  }
  const readPermissionDetails=(id:string,reference:string)=>{
    const read=permissionDetailTail.current.then(async()=>await detail("permission",id,reference,true)===true)
    permissionDetailTail.current=read.then(()=>{},()=>{})
    return read
  }
  // Summaries contain no tool name. Read tool details serially within the existing
  // bounded collection; leave thoughts and reply text behind the explicit Details action.
  useEffect(()=>{
    if(recoveryRequired||!botBrowserSupported(source))return
    let cancelled=false
    traceDetailTail.current=traceDetailTail.current.then(async()=>{
      for(const row of traceSummaries){
        if(cancelled||recovery.current)return
        const value=detailRows.current[row.id]
        if(!toolTraceCategory(row.category)||backgroundTraceReferences.current.get(row.id)===row.detail_cursor||requestedTraceReferences.current.get(row.id)===row.detail_cursor||value&&"text" in value&&browserToolTrace(value))continue
        // A failed background read is retried only when the source supplies a
        // new reference, or through the person's explicit Details action.
        backgroundTraceReferences.current.set(row.id,row.detail_cursor)
        await readTraceDetail(row)
      }
    })
    return()=>{cancelled=true}
  },[traceSummaries,details,recoveryRequired,source])
  const answer=async(offer:W.ProjectAssistantPermissionOffer,option:W.ProjectAssistantPermissionOption|null,text?:string,attempt?:string)=>{
    let attempted=false,sentAt=0,idempotency=""
    const trace=(phase:"request"|"settled"|"failed",code="")=>{if(attempt)traceDecision({attempt,phase,bot_id:key.bot_id,offer_id:offer.id,option:option?.id,idempotency_key:idempotency,code,ms:sentAt?performance.now()-sentAt:0})}
    const displayed=()=>detailRows.current[offer.id]===offer||teamCards&&timelineRef.current.some(row=>row.permission===offer)
    try{
      const result=await withPermissionRecovery(()=>mutate(async(d,s)=>{
        if(!displayed())throw failure("pa_read_only")
        const summary=permissionRows.current.find(p=>p.id===offer.id),capturedEpoch=epoch.current
        if(!summary||!samePermission(summary,offer)||!currentPermission(summary,headRef.current)||headRef.current?.pending_permission_count===0)throw failure("pa_stale_generation")
        if(permissionState(offer)==="expired"||permissionState(summary)==="expired")throw failure("pa_expired")
        const page=await read(request("detail",summary.detail_cursor,1),s),live=page.items[0]?.permission
        // The second read also has an effect fence; ignored cancellation grants no action.
        if(!valid()||s.aborted||recovery.current||capturedEpoch!==epoch.current||!displayed())throw failure("pa_read_only")
        if(!canWrite(page.source)||!headRef.current||!sameFence(page.head.current_fence,headRef.current.current_fence)||!sameFence(page.head.current_fence,fence(d)))throw failure("pa_stale_generation")
        if(!live||!samePermission(live,offer))throw failure("pa_stale_generation")
        if(permissionState(live)==="expired")throw failure("pa_expired")
        if(live.kind==="decision"&&live.state==="answered")throw {contract_version:"project-assistants.v1",error:{code:"pa_stale_version",field:"decision_answered",retry_after_seconds:0}}
        if(live.state!=="pending"||!currentPermission(live,page.head)||!currentPermission(live,headRef.current))throw failure("pa_stale_generation")
        // Any option the agent offered, Always allow and Never included (Codync; decided 2026-11-14).
        if(option?!live.options.some(o=>o.id===option.id&&o.kind===option.kind):live.kind!=="decision"||!text)throw failure("pa_scope_denied")
        const intent:W.ProjectAssistantPermissionAnswerRequest={command:command(key,"answer"),fence:live.fence,offer_id:live.id,turn_id:live.turn_id,option_id:option?.id??"",...(text?{text}:{})}
        attempted=true;sentAt=performance.now();idempotency=intent.command.idempotency_key;trace("request")
        return source.answer(intent,s)
      },{reportReadFailure:false}),valid,()=>void refresh())
      const receipt=result?.receipt
      if(result?.contract_version!=="project-assistants.v1"||!admittedReceipt(receipt)||!receipt?.resource_id||
        !receipt.command?.key||!sameBot(receipt.command.key,key))throw failure("pa_outcome_unknown")
      trace("settled",`${receipt.state}${valid()&&!recovery.current?"":"-unapplied"}`)
      if(valid()&&!recovery.current){
        if(displayed()){
          const next={...detailRows.current,[offer.id]:{...offer,state:"answered" as const,selected_option_id:option?.id??"",...(offer.decision?{decision:{...offer.decision,answer_text:option?.label??text??""}}:{})}}
          detailRows.current=next;setDetails(next)
        }
        if(offer.kind==="decision"&&receipt.state!=="unknown")setDecisionTurns(old=>({...old,[offer.id]:receipt.resource_id}))
        void refresh()
      }
    }catch(e){
      const code=errorCode(e)
      trace("failed",code||"unknown")
      if(code==="pa_expired"||code==="pa_stale_version")void refresh()
      // Only a request that left and may have taken effect is unknown; a
      // typed refusal from the source keeps its code and field.
      if(attempted&&code!=="pa_outcome_unknown"&&uncertainAfterSend(e))
        throw failure("pa_outcome_unknown")
      throw e
    }
  }

  const earlier=async()=>{
    const captured=headRef.current,capturedEpoch=epoch.current,cursor=more
    if(!data||!cursor||!captured||recovery.current||collectionBusy.current.has("history"))return
    const control=new AbortController();reads.current.add(control);collectionBusy.current.set("history",control)
    const el=scroll.current,oldHeight=el?.scrollHeight??0,oldTop=el?.scrollTop??0
    setEarlierLoading(true)
    const trace=source.beginReadObservation?.("history",performance.now())
    let adopted=false
    try{
      const page=await read(request("history",cursor,source.browserRemote?chatInitialPageRows:undefined),control.signal,trace)
      if(!valid()||control.signal.aborted||epoch.current!==capturedEpoch||recovery.current)return
      if(page.has_more&&historyCursors.current.has(page.next_cursor))throw failure("pa_invalid")
      if(!headRef.current||!sameFence(captured.current_fence,headRef.current.current_fence))return
      if(!sameFence(page.head.current_fence,captured.current_fence))throw failure("pa_stale_generation")
      historyCursors.current.add(cursor)
      const rows=mergeTimeline([timelineRef.current,page.items])
      prepareReadDisplay(trace);adopted=true;timelineRef.current=rows;setTimeline(rows)
      setMore(page.next_cursor)
      requestAnimationFrame(()=>{if(el&&valid()&&epoch.current===capturedEpoch)el.scrollTop=oldTop+el.scrollHeight-oldHeight})
    }catch(e){
      trace?.finish(control.signal.aborted?"abandoned":"refused",errorCode(e))
      if(!control.signal.aborted)readFailed(e)
    }finally{
      if(!adopted)trace?.finish("abandoned")
      reads.current.delete(control);if(collectionBusy.current.get("history")===control)collectionBusy.current.delete("history");if(valid())setEarlierLoading(false)
    }
  }
  const viewHandoffHistory=(bookmark?:string)=>{
    const timeline=scroll.current
    if(!timeline)return
    followingRef.current=false;setFollowing(false)
    const message=(bookmark?timeline.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(bookmark)}"]`):null)
      ??timeline.querySelector<HTMLElement>("[data-message-id]")
    if(message){message.scrollIntoView({block:"center"});message.focus({preventScroll:true})}
    else timeline.focus()
  }
  const prepareHandoff=async()=>{
    await handoff.update("prepare")
    if(!valid())return
    setInfoOpen(false)
    requestAnimationFrame(()=>{
      const card=scroll.current?.querySelector<HTMLElement>(".pa-handoff-card")
      card?.scrollIntoView({block:"start"});card?.focus({preventScroll:true})
    })
  }
  const exportBot=async()=>{
    const control=new AbortController()
    reads.current.add(control);setExporting(true);setExportError("")
    try{
      const value=await authorized(source,current,s=>source.export(key,s),control.signal)
      if(!valid()||control.signal.aborted)return
      const url=URL.createObjectURL(new Blob([JSON.stringify(value,null,2)],{type:"application/json"}))
      const link=document.createElement("a")
      link.href=url;link.download="bot-export.json";link.click();URL.revokeObjectURL(url)
    }catch(error){if(valid()&&!control.signal.aborted)setExportError(errorCode(error))}
    finally{reads.current.delete(control);if(valid())setExporting(false)}
  }
  const projectMemoryPages=source.canProjectMemoryPages?.()===true
  const readMemory=async(signal:AbortSignal,detail?:import("./client").MemoryReadDetail,page?:import("./client").ProjectMemoryPage)=>{
    if(!source.canMemorySettings?.()||!source.memoryView)throw failure("pa_unsupported")
    const value=await authorized(source,current,s=>source.memoryView!(key,scope.conversation_id,s,detail,page),signal)
    if(!valid()||!sameConversation({key:value.view.bot.key,project:value.view.bot.project,conversation_id:value.view.conversation_id},scope))throw failure("pa_scope_denied")
    return value
  }
  const botMemoryPage=projectMemoryPages?{page:0,page_size:projectMemoryPageSize}:undefined
  const loadExport=async()=>{
    const value=await readMemory(new AbortController().signal,undefined,botMemoryPage)
    if(memoryShown.current)setMemory(previous=>previous&&previous.view.memory.revision>value.view.memory.revision?previous:value)
  }
  // The panel opens at once with placeholders; a failed read closes it onto the chat's error line.
  const loadExportRef=useRef(loadExport);loadExportRef.current=loadExport
  useEffect(()=>{if(memoryShown.current)void loadExportRef.current().catch(fail)},[data?.conversation.memory_revision,fail])
  const failRef=useRef(fail);failRef.current=fail
  const memoryPanel=useRef<ReturnType<typeof memoryOpener>|null>(null)
  memoryPanel.current??=memoryOpener(()=>loadExportRef.current(),{
    open:open=>{memoryShown.current=open;if(!open)setMemory(null)},
    loading:setMemoryLoading,
    fail:error=>failRef.current(error)})
  const memoryOutcomeUnknown=(error:unknown)=>["unavailable","pa_outcome_unknown","execution_effect_unknown","pa_revoked"].includes(errorCode(error))||(error as Error)?.name==="AbortError"
  const saveMemory=async(input:string|MemoryNoteDraft)=>{
    const note=typeof input==="string"?null:input,text=typeof input==="string"?input:input.text
    if(!memory||!text.trim())return
    if(note&&!source.canTopicMemory?.())throw failure("pa_unsupported")
    if(memoryForget.current)throw failure("pa_outcome_unknown")
    // Keep the same request after an uncertain reply; a second click cannot
    // turn one note into two entries or move it onto a newer revision.
    let intent=memorySave.current
    if(intent&&(intent.entry.text!==text||(intent.entry.kind??"")!==(note?"note":"")||(note&&(intent.entry.id!==note.entry_id||intent.entry.title!==note.title||intent.entry.summary!==note.summary||intent.expected_revision!==note.expected_revision))))throw failure("pa_outcome_unknown")
    if(!intent){
      const id=note?.entry_id??crypto.randomUUID(),revision=note?.expected_revision??memory.view.memory.revision
      intent={command:command(key,"memory_save"),expected_revision:revision,entry:{id,key,project:memory.view.bot.project,text,revision,sources:[],forgotten:false,...(note?{kind:"note",title:note.title,summary:note.summary}:{})}}
      memorySave.current=intent
    }
    const request=intent
    let attempted=false
    try{await mutate((_,signal)=>{attempted=true;return source.saveMemory(request,signal)})}
    catch(error){if(!attempted||!memoryOutcomeUnknown(error))memorySave.current=null;throw error}
    memorySave.current=null
    await loadExport().catch(fail)
  }
  const forgetMemory=async(sourceIDs:string[])=>{
    if(!memory||!sourceIDs.length)return
    if(memorySave.current)throw failure("pa_outcome_unknown")
    let intent=memoryForget.current
    const ids=[...new Set(sourceIDs)].sort()
    if(intent&&JSON.stringify(intent.source_ids)!==JSON.stringify(ids))throw failure("pa_outcome_unknown")
    if(!intent){intent={command:command(key,"forget"),project:memory.view.bot.project,expected_revision:memory.view.memory.revision,source_ids:ids};memoryForget.current=intent}
    const request=intent
    let attempted=false
    try{await mutate((_,signal)=>{attempted=true;return source.forget(request,signal)})}
    catch(error){if(!attempted||!memoryOutcomeUnknown(error))memoryForget.current=null;throw error}
    memoryForget.current=null
    void handoff.refresh()
    await loadExport().catch(fail)
    void refresh(true)
  }
  // Opening a conversation draws chat-shaped placeholders after 150ms, until its first read answers.
  const chatSkeleton=useShowAfter(!data&&!error)
  const bot=data?.bot??summary.bot, parts=partitionMessages(messages,turns)
  const mentions=botMentionCandidates(bot,roster,b=>b.project.kind==="personal"?w.personalWorkspace:projectNameOf?.(b)||w.unnamedProject,projectIconOf)
  const activeTurn=turns.find(turn=>turn.id===data?.conversation.active_turn_id&&(turn.state==="running"||turn.state==="waiting_permission"))
  const live=!recoveryRequired&&!error&&source.current?.()!==false&&liveActivity(summary.source)&&liveActivity(data?.source??summary.source)
  const permissionCards=permissions.map(summary=>{
    const value=details[summary.id]??timeline.find(row=>row.permission?.id===summary.id)?.permission
    const identity=JSON.stringify([summary.fence,summary.turn_id,summary.id])
    // A detail read before the chat's fence moved (a restart or fallback) is
    // not this summary's; the card reads it again instead of answering it.
    return {summary,identity,offer:value&&"fence" in value&&samePermission(summary,value)?value:undefined}
  })
  const inlinePermissionIds=new Set(timeline.flatMap(row=>row.permission?[row.permission.id]:[]))
  const exchangeDecisions=decisionsUnderCapsules(permissionCards,messages,inlinePermissionIds)
  const exchangeDecisionIds=new Set([...exchangeDecisions.values()].map(card=>card.summary.id))
  const sectionPermissions=permissionCards.filter(({summary})=>!inlinePermissionIds.has(summary.id)&&!exchangeDecisionIds.has(summary.id))
  const focusOutcome=focusTarget?decisionFocus(focusTarget,permissionCards,permissionsRead&&!!data):"wait"
  const rosterTarget=rosterQuestion?permissionCards.find(({summary})=>summary.tool_kind==="decision"&&currentPermission(summary,head)&&permissionState(summary)==="pending"&&summary.title?.split("\n",1)[0].trim()===rosterQuestion)?.summary.id:undefined
  useEffect(()=>{
    if(focusTarget){
      if(focusOutcome==="wait")return
      setRosterQuestion("");setFocusTarget("");onDecisionFocused?.()
      if(focusOutcome==="handled")setDecisionHandled(true)
      else setFocusedDecision(focusTarget)
    }else if(rosterTarget){
      setRosterQuestion("");setFocusedDecision(rosterTarget)
    }
  },[focusTarget,focusOutcome,rosterTarget])
  const focusedUnderExchange=exchangeDecisionIds.has(focusedDecision)
  const focusedOfferLoaded=permissionCards.some(card=>card.summary.id===focusedDecision&&!!card.offer)
  useEffect(()=>{
    if(!focusedDecision)return
    const id=focusedDecision
    // The card renders with data-focused this frame; scroll once it is laid out.
    const frame=requestAnimationFrame(()=>{
      const viewport=scroll.current
      const card=viewport?.querySelector<HTMLElement>(`#pa-decision-${CSS.escape(id)}`)
      if(!viewport||!card){setFocusedDecision("");setDecisionHandled(true);return}
      followingRef.current=false;setFollowing(false)
      const still=window.matchMedia?.("(prefers-reduced-motion: reduce)").matches
      const block=card.offsetHeight>=viewport.clientHeight?"start":"center"
      card.scrollIntoView({block,behavior:still?"auto":"smooth"})
      card.focus({preventScroll:true})
    })
    const clear=window.setTimeout(()=>setFocusedDecision(old=>old===id?"":old),2400)
    return()=>{cancelAnimationFrame(frame);window.clearTimeout(clear)}
  },[focusedDecision,focusedUnderExchange,focusedOfferLoaded])
  const inlineReview=timeline.find(({permission})=>permission&&currentPermission(permission,head)&&permissionState(permission)==="pending")?.permission
  const {count:pendingPermissionCount,nextDeadline}=pendingPermissionView(head,permissionCards,Date.now())
  useEffect(()=>{
    if(!Number.isFinite(nextDeadline))return
    // setTimeout clamps at a signed 32-bit delay in the browser.
    const delay=Math.min(Math.max(0,nextDeadline-Date.now()),2**31-1)
    const timer=setTimeout(()=>setPermissionTick(tick=>tick+1),delay)
    return()=>clearTimeout(timer)
  },[nextDeadline,permissionTick])
  const clearHeadContextUsage=()=>{
    setHead(before=>{
      if(!before?.context_usage)return before
      const next={...before,context_usage:undefined}
      headRef.current=next
      return next
    })
  }
  const {compact,compacting,setCompactRequest,freshSessionNotice,dismissFreshSession}=useContextActionStatus({
    turns,messages,w,newSessionCount,activeTurnID:head?.conversation.active_turn_id,
    onClearContextUsage:clearHeadContextUsage,onRefresh:()=>void refresh(true)
  })
  const compactNow=async()=>{
    if(compacting)return
    setCompactRequest({state:"sending"})
    let turnID=""
    try{
      await mutate(async(d,s,page)=>{
        if(d.conversation.active_turn_id||page.snapshot!.queued_messages.length||page.head.queued_turn_count||page.head.unknown_turn_count)throw failure("pa_capacity_full")
        const intent:W.ProjectAssistantSendRequest={command:command(key,"send",`compact-${crypto.randomUUID()}`),fence:page.head.current_fence,text:"",quote_message_id:"",compact:true}
        const result=await source.send(intent,s)
        if(!sendAcknowledgement(result,intent)||!result.receipt.resource_id)throw failure("pa_outcome_unknown")
        turnID=result.receipt.resource_id
      })
      if(valid())setCompactRequest({state:"running",turnID})
    }catch(e){if(valid())setCompactRequest({state:"failed",code:compactRefusalCode(e)})}
    finally{if(valid())void refresh(true)}
  }
  const callObservation=():BotVoiceObservation|null=>{
    const d=currentData.current,h=headRef.current
    if(!d||!h||!epoch.current||!valid()||recovery.current||error||source.current?.()===false
      ||!canWrite(d.source)||document.hidden||!navigator.onLine||browserOpener||templateOpener
      ||editingHandoff||handoff.mutating()||handoff.error||handoff.card?.state==="draft")return null
    return {
      owner:JSON.stringify([accountKey,readerID,grant,epoch.current]),
      sourceEpoch:epoch.current,
      fence:h.current_fence,activeTurn:h.conversation.active_turn_id,turns,
      messages,
      activity:h.conversation.active_turn_id?(activity[h.conversation.active_turn_id]||summary.activity):"",
      background:(h.background_tasks??[]).filter(liveBackgroundTask),
      permissions:permissionCards.filter(({summary,offer})=>currentPermission(summary,h)&&permissionState(offer??summary)==="pending")
        .map(({summary,offer})=>({id:summary.id,options:offer?.options.map(option=>option.label)})),
      moreHistory:!!more,
      frontier:callViewFrontier.current,caughtUp:handoff.ready&&h.event_frontier<=callViewFrontier.current,
    }
  }
  const submitCall=async(input:CallInput)=>{
    if(!handoff.ready||editingHandoff||handoff.mutating()||handoff.error||handoff.card?.state==="draft")return {state:"sendFailed" as const}
    const intent:W.ProjectAssistantSendRequest={
      command:command(key,"send",input.greeting?`lead-call-${input.callID}`:botCallSendID(input.callID)),
      fence:input.fence,text:input.text,quote_message_id:"",...(input.greeting?{call_started:true}:{}),
    }
    const id=intent.command.idempotency_key
    let attempted=false
    const received:{answer:W.ProjectAssistantReceipt|null}={answer:null}
    try{
      await mutate(async(d,s,page)=>{
        const idleRequired=input.greeting||!input.allowQueue
        const waiting=d.conversation.active_turn_id||page.snapshot!.queued_messages.length||page.head.queued_turn_count
        if(input.signal.aborted||!sameFence(input.fence,page.head.current_fence)||idleRequired&&waiting
          ||page.head.unknown_turn_count||view.heldOutcomes.has(draftKey)||legacyBlocked())throw failure("pa_read_only")
        view.heldOutcomes.set(draftKey,{intent,receipt:null});view.heldOutcomes.beginTransport(draftKey,id)
        attempted=true
        // Once the source might have admitted the turn, hangup only ends the
        // audio lease. The original command still settles its own journal.
        const result=await source.send(intent,s)
        if(!sendAcknowledgement(result,intent))throw failure("pa_outcome_unknown")
        received.answer=result.receipt
      })
    }catch(e){
      if(attempted&&(handoffInputRefused(e)||input.greeting&&errorCode(e)==="pa_read_only")) {
        view.heldOutcomes.settle(draftKey,id,null);view.heldOutcomes.endTransport(draftKey,id,null)
        if(valid()){setReceipt(null);setUnknown(false);repaintJournal();void handoff.refresh()}
        return {state:input.greeting&&errorCode(e)==="pa_read_only"?"skipped" as const:"sendFailed" as const}
      }
      if(!attempted||provablyNotSent(e)) {
        if(attempted){view.heldOutcomes.settle(draftKey,id,null);view.heldOutcomes.endTransport(draftKey,id,null);repaintJournal()}
        return {state:input.greeting?"skipped" as const:"sendFailed" as const}
      }
    }
    const {answer}=received
    const admitted=admittedReceipt(answer)
    const hold=admitted||!answer||answer.state==="unknown"
    view.heldOutcomes.settle(draftKey,id,hold?{intent,receipt:answer}:null)
    view.heldOutcomes.endTransport(draftKey,id,answer)
    if(answer)observation.accept(answer)
    if(valid()){
      repaintJournal();setReceipt(answer);setUnknown(uncertain());void refresh(true)
    }
    if(admitted&&answer)return {state:"admitted" as const,turn:answer.resource_id,eventSeq:answer.event_seq}
    return {state:hold?"sendUnknown" as const:"sendFailed" as const}
  }
  const callView=callObservation()
  const {call,state:callState}=useBotCall({current:callObservation,submit:submitCall})
  const canVoiceCall=!thread&&!leadSetup.blocked&&!!callView?.caughtUp
  const botVoice=useBotVoice({current:callObservation,submit:submitCall,stop,memory:signal=>readMemory(signal,undefined,botMemoryPage)},!thread&&canWrite(data?.source??summary.source))
  const greeting=bot.lead&&(data?.source??summary.source).lead_call_supported===true
  const artifactDraftKey=(id:string)=>JSON.stringify(["artifact",draftKey,id])
  const artifactComposer:ArtifactComposerAccess|undefined=thread?undefined:{
    blocked:sendAuthorityLost||busy||unknown||call.active||legacyBlocked()||handoff.card?.state==="draft"||editingHandoff||handoff.busy||!!handoff.error,
    read:id=>view.draft(artifactDraftKey(id)),
    write:(id,value)=>{const key=artifactDraftKey(id);view.stage(key,value);view.flush(key)},
    send:(message,text)=>send({message,text}),
    progress:turn=>messages.some(message=>message.turn_id===turn&&message.kind==="assistant")?"replied":
      turns.some(item=>item.id===turn&&["failed","cancelled","unknown"].includes(item.state))?"failed":null
  }
  const canCall=!thread&&!leadSetup.blocked&&!!callView?.caughtUp&&!busy&&!unknown&&!legacyBlocked()
    &&!view.heldOutcomes.has(draftKey)&&!parts.queued.length&&!head?.unknown_turn_count
  // The composer's call button shows once the call path is known and stays while a
  // send is in flight (disabled), so it never blinks out and back. While the key
  // check is still reading (configured === null) the trailing button stays send.
  const realtimeCall=botVoice.configured===true?botVoice:undefined
  const callStart=useBotCallStart({call,name:bot.name,language,greeting,shortcut:voiceShortcut,realtime:realtimeCall,
    available:!thread&&!leadSetup.blocked&&botVoice.configured!==null,
    ready:realtimeCall?canVoiceCall:canCall})
  const visibleTurn=activeTurn?.state==="waiting_permission"&&pendingPermissionCount===0?undefined:activeTurn
  const showDispatched=useDispatchedWorkShown()
  // Bot request cards (docs/bot-request-cards.md §9): read for this Bot from the local daemon only.
  const requestReading=useBotRequests({enabled:!thread&&readsBotRequests(source),bot:key.bot_id})
  const [openRequest,setOpenRequest]=useState<W.BotRequestView>()
  const requestCards=requestReading.reading.state==="ready"?requestReading.reading.cards:undefined
  const ownMood=live?(data?chatMood(data.source,visibleTurn,pendingPermissionCount,thread||recoveryRequired?[]:head?.background_tasks):rosterMood(summary)):"idle"
  const mood=ownMood==="idle"&&live&&showDispatched&&rosterMood(summary,true)==="waitingOnWork"?"waitingOnWork":ownMood
  const queuedIDs=new Set(parts.queued.map(message=>message.id)),chatTimeline=timeline.filter(row=>!row.message||!queuedIDs.has(row.message.id))
  const liveTraceTurn=live?visibleTurn?.id:undefined
  const requests=useMemo(()=>placeBotRequests(requestCards??[],chatTimeline.flatMap(row=>row.work?[row.work]:[])),[requestCards,timeline,parts.queued])
  const pendingSend=view.heldOutcomes.get(draftKey)
  const sendingAccepted=pendingSend?.intent.command.operation==="send"&&admittedReceipt(pendingSend.receipt)
  const confirmed=sendingAccepted?admittedRow(pendingSend!.intent as W.ProjectAssistantSendRequest,pendingSend!.receipt,messages):undefined
  // A matching body before the receipt is ambiguous. Keep the sending status
  // visible without drawing two copies of the text or treating it as admitted.
  const hideProvisionalText=isSendingAdmitted(sending,messages,sendBaselineSeq.current)
  const motion=useMessageArrivals({items:messages,ready:!!data&&!recoveryRequired,following,seq:m=>m.seq,id:m=>m.id,
    skip:m=>queuedIDs.has(m.id)||m.id===confirmed?.id||m.kind==="notice"})
  const traceGroups=new Map<string,W.ProjectAssistantReadTraceSummary[]>()
  for(const row of traceSummaries){
    const rows=traceGroups.get(row.turn_id)??[]
    rows.push(row);traceGroups.set(row.turn_id,rows)
  }
  const turnAnchors=new Map<string,string>()
  for(const message of messages){
    if(!queuedIDs.has(message.id)&&(message.kind==="user"||message.kind==="assistant"&&!turnAnchors.has(message.turn_id)))turnAnchors.set(message.turn_id,message.id)
  }
  const liveSteps=liveTraceTurn?traceGroups.get(liveTraceTurn)??[]:[]
  const latestStep=liveSteps.find(row=>workingCategory(row.category))
  const currentActivity=liveTraceTurn?activity[liveTraceTurn]||(latestStep?traceLabel(latestStep.category,w):""):""
  const currentStop=stopStatus&&stopStatus.turn===data?.conversation.active_turn_id?stopStatus.state:undefined
  const stopText=currentStop==="sending"?w.stopSending:currentStop==="acknowledged"?w.stopAcknowledged:currentStop==="unknown"?w.stopUnconfirmed:undefined
  const moreTraceTurn=traceMore?traceSummaries.at(-1)?.turn_id:undefined
  const moreOrphanTraces=!!moreTraceTurn&&moreTraceTurn!==liveTraceTurn&&!turnAnchors.has(moreTraceTurn)
  const loadMoreTraces=()=>void collection("traces",traceMore).catch(readFailed)
  const deferredTrace=deferTraceCount?{state:traceLoad,load:loadTraceSummaries}:undefined
  // Keep the first disclosure attached to the most recent visible completed turn.
  let latestTraceAnchor:string|undefined
  if(deferredTrace)for(let i=messages.length-1;i>=0;i--){
    const message=messages[i]
    if(message.turn_id&&message.turn_id!==liveTraceTurn&&turnAnchors.get(message.turn_id)===message.id){
      latestTraceAnchor=message.id
      break
    }
  }
  const afterMessage=(message:W.ProjectAssistantMessage)=>{
    const rows=traceGroups.get(message.turn_id)
    if(message.turn_id===liveTraceTurn||turnAnchors.get(message.turn_id)!==message.id)return
    const deferred=message.id===latestTraceAnchor?deferredTrace:undefined
    if(!rows?.length&&!deferred&&(deferTraceCount||!head?.trace_category_count))return
    const more=message.turn_id===moreTraceTurn||!!deferred&&!rows?.length&&!!traceMore
    return <div className="pa-turn-trace" data-trace-turn={message.turn_id}>{rows?.length||deferred?
      <Trace summaries={rows??[]} incomplete={more} details={details} more={more} onMore={loadMoreTraces}
        onDetail={readTraceDetail} deferred={deferred} w={w}/>:null}</div>
  }
  const orphanTraces=traceSummaries.filter(row=>row.turn_id!==liveTraceTurn&&!turnAnchors.has(row.turn_id))
  const queueEditorOpen=!!queueEditor&&turns.some(turn=>turn.id===queueEditor.turnID&&turn.queue_edit_id===queueEditor.editID)
  useLayoutEffect(()=>{
    if(!queueEditorOpen)return
    const input=queueEditorInput.current,viewport=window.visualViewport
    input?.focus({preventScroll:true})
    const reveal=()=>input?.closest(".pa-queued")?.scrollIntoView({block:"nearest"})
    let frame=0
    const later=()=>{cancelAnimationFrame(frame);frame=requestAnimationFrame(reveal)}
    reveal();viewport?.addEventListener("resize",later)
    return()=>{cancelAnimationFrame(frame);viewport?.removeEventListener("resize",later)}
  },[queueEditorOpen])
  const queuedCount=head?.queued_turn_count??0
  const unknownCount=head?.unknown_turn_count??0
  const restartMessages=new Map(messages.filter(message=>message.notice?.key==="restart_continuation").map(message=>[message.turn_id,message]))
  const restartTurn=turns.find(turn=>restartMessages.has(turn.id)&&["queued","accepted","running","waiting_permission"].includes(turn.state))
  const restartNotice=restartTurn?restartMessages.get(restartTurn.id):undefined
  const restartInterruption=turns.find(turn=>turn.state==="unknown"&&(turn.failure_field==="restart"||turn.failure_field==="restart_confirmed"))
  const userTurns=new Set(messages.filter(message=>message.kind==="user").map(message=>message.turn_id))
  const restartWaitingCount=turns.filter(turn=>["queued","accepted"].includes(turn.state)&&userTurns.has(turn.id)).length
  const queuePaused=queuedCount>0&&unknownCount>0&&!restartNotice
  const requestTurnReview=async()=>{
    const result=await send({text:w.turnReviewPrompt})
    if(valid()&&result.state==="accepted")toast(w.turnReviewAccepted)
    if(valid()&&result.state==="refused"&&["pa_stale_version","pa_stale_generation"].includes(result.code??""))void refresh(true)
    return result.state
  }
  const showTurnReview=!thread&&unknownCount>0&&!restartNotice&&!reconnecting
  const canRequestTurnReview=enabled&&!unknown&&!busy&&!legacyBlocked()&&!call.active&&!activeTurn
    &&!editingHandoff&&!handoff.busy&&!handoff.error&&handoff.card?.state!=="draft"
  const exchangeReplyTurns=new Set(messages.flatMap(message=>message.exchange&&message.turn_id===message.exchange.reply_turn_id?[message.turn_id]:[]))
  const visibleInterruptions=turns.filter(turn=>!["queued","accepted","running","waiting_permission"].includes(turn.state)
    &&turn.failure_field!=="restart_resumed"&&!(restartNotice&&turn.id===restartInterruption?.id)
    &&!(turn.state==="cancelled"&&(!turn.failure_code&&!turn.started_at||exchangeReplyTurns.has(turn.id))))
  const queueLimited=queuedCount>turns.filter(turn=>turn.state==="queued"||turn.state==="accepted").length
  const waitingTurn=!visibleTurn&&live?turns.find(turn=>(turn.state==="accepted"||turn.state==="queued")&&messages.at(-1)?.id===turn.message_id):undefined
  const browserAvailable=!thread&&botBrowserSupported(source)
  const canReadActions=!!data&&!recoveryRequired&&source.current?.()!==false&&canMarkConversationRead(data.source)
  // Closed details read only when the conversation actually moves; the open panel follows every chat read.
  const servers=useDevServerObservation({source,bot,enabled,active:!thread&&canReadActions&&!browserOpener&&!templateOpener,pulse:infoOpen&&infoTab==="details"?head:head?.event_frontier})
  const serverLabel=serverTitle(servers.state,w)
  const legacyLocked=legacyBlocked()
  const legacyProblem=view.unverifiedDrafts.problem(draftKey)
  const legacyRows=view.unverifiedDrafts.history(draftKey,scope)
  const legacyDrafts=legacyLocked&&!legacyRows.length&&entryDraft.text?[entryDraft]:legacyRows
  const canEditLegacy=enabled&&!busy&&!unknown&&!legacyLocked&&!call.active&&!text&&!quote&&!images.length&&!files.length&&!filesLost
  const editLegacyDraft=(draft:{text:string;quote:string})=>{
    if(!canEditLegacy||!valid())return
    changeText(draft.text)
    if(draft.quote){setQuote(draft.quote);view.quotes.set(draftKey,draft.quote)}
    flush()
    scroll.current?.parentElement?.querySelector<HTMLTextAreaElement>(".pa-composer textarea")?.focus()
  }
  const removeLegacyDraft=()=>{
    if(!valid())return
    setLegacyRemoveFailed(!view.unverifiedDrafts.dismiss(draftKey,scope))
    repaintJournal()
  }
  const taskRows=head?.background_tasks
  const tasks=thread||recoveryRequired?[]:taskRows??[]
  const backgroundRunning=tasks.some(liveBackgroundTask)
  const openTask=(task:BackgroundTask,button:HTMLButtonElement)=>{
    taskOpener.current=button
    if(task.worker)setSelectedTask(task.id)
    openInfo("details")
  }
  const renderExchangeWork=(exchangeID:string,sender:W.ProjectAssistantBot|undefined,name:string)=>{
    const task=tasks.find(item=>item.id===exchangeID)
    return task&&<BackgroundHandoff task={task} sender={sender} senderName={name} self={bot} open={openTask} stop={task=>void stopTask(task)} stopping={backgroundStopping} outcomes={backgroundOutcomes} writable={enabled&&!!data&&canWrite(data.source)} w={w}/>
  }

  const selected=infoOpen?tasks.find(task=>task.id===selectedTask&&task.worker):undefined
  const closeTask=()=>{
    setSelectedTask("")
    requestAnimationFrame(()=>{const opener=taskOpener.current;if(opener?.getClientRects().length)opener.focus({preventScroll:true});else identityButton.current?.focus({preventScroll:true})})
  }
  useEffect(()=>()=>backgroundStopControl.current?.abort(),[identity,source])
  useEffect(()=>{
    if(!taskRows)return
    setBackgroundOutcomes(old=>{
      const pending=Object.entries(old)
      const live=pending.filter(([id])=>taskRows.some(task=>task.id===id&&liveBackgroundTask(task)))
      return pending.length===live.length?old:Object.fromEntries(live)
    })
  },[taskRows])
  const stopTask=async(task:BackgroundTask)=>{
    const worker=task.worker
    if(!worker||!source.view||!enabled||!valid()||recovery.current||backgroundStopControl.current||backgroundOutcomes[task.id])return
    const control=new AbortController()
    backgroundStopControl.current=control
    setBackgroundStopping(task.id);setBackgroundError("")
    const settle=(outcome:BackgroundStopOutcome)=>setBackgroundOutcomes(old=>({...old,[task.id]:outcome}))
    let attempted=false
    try{
      const query:W.ProjectAssistantReadViewRequest={key:worker,conversation_id:task.id,mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:1}
      const snapshot=readView(await authorized(source,current,signal=>source.view!(query,signal),control.signal),query,{key:worker,conversation_id:task.id,project:bot.project})
      if(!canWrite(snapshot.source))throw failure("pa_read_only")
      const turnID=snapshot.head.conversation.active_turn_id||snapshot.snapshot?.pending_turns.find(turn=>turn.state==="accepted"||turn.state==="queued")?.id
      if(!turnID){if(valid())void refresh();return}
      const action=command(worker,"stop")
      const result=await authorized(source,current,signal=>{
        attempted=true
        return source.stop({command:action,fence:snapshot.head.current_fence,turn_id:turnID},signal)
      },control.signal)
      if(!valid())return
      const {receipt}=result
      if(result.contract_version!=="project-assistants.v1"||receipt?.command?.operation!=="stop"||
        receipt.command.idempotency_key!==action.idempotency_key||botIdentity(receipt.command.key)!==botIdentity(worker)||receipt.resource_id!==turnID){
        settle("unknown")
      }else if(receipt.state==="failed")setBackgroundError(w.failed)
      else settle(receipt.state==="accepted"||receipt.state==="delivered"?"acknowledged":"unknown")
      void refresh()
    }catch(error){
      if(valid()&&!control.signal.aborted){
        if(attempted&&memoryOutcomeUnknown(error))settle("unknown")
        else setBackgroundError(errorCode(error))
        void refresh()
      }
    }finally{backgroundStopControl.current=null;if(valid())setBackgroundStopping("")}
  }
  useLayoutEffect(()=>{
    if(!thread||!data||focusedThread.current===thread.id)return
    focusedThread.current=thread.id
    const composer=scroll.current?.parentElement?.querySelector<HTMLTextAreaElement>(".pa-composer textarea")
    const alreadyInside=scroll.current?.parentElement?.contains(document.activeElement)
    if(alreadyInside)return
    const editable=enabled&&canWrite(data.source)&&window.matchMedia("(pointer:fine)").matches
    if(editable&&composer&&!composer.disabled)composer.focus({preventScroll:true})
    else identityButton.current?.focus({preventScroll:true})
  },[thread,data,enabled])
  const brief=thread?messages.find(message=>message.seq===1):undefined
  const threadSummary:W.ProjectAssistantBotSummary|undefined=selected?.worker?{
    ...summary,bot:{...bot,key:selected.worker,conversation_id:selected.id},latest_message:"",unread_count:0,
  }:undefined
  const heldOutcome=unknown?view.heldOutcomes.get(draftKey):null
  const heldSend=heldOutcome?.intent.command.operation==="send"?heldOutcome:null
  const reviewPending=[heldSend,...pendingRows].some(row=>row?.intent.command.idempotency_key.startsWith(TURN_REVIEW_PREFIX)&&!admittedReceipt(row.receipt))

  const permissionMachine=machineName||source.machineName||key.machine_id
  const permissionContext=[permissionMachine,projectName||bot.project.project_id].join(" · ")
  const followUpOf=(id:string)=>{const next=decisionFollowUp(decisionTurns[id]??"",turns,decisionProgress.current[id]);decisionProgress.current[id]=next;return next}
  const renderDecision=({summary:row,offer,identity}:(typeof permissionCards)[number])=><DecisionCard key={identity} summary={row} offer={offer} followUp={followUpOf(row.id)} highlight={focusedDecision===row.id} enabled={enabled&&currentPermission(row,head)} onDetail={()=>readPermissionDetails(row.id,row.detail_cursor)} onAnswer={answer} botID={key.bot_id} askerName={bot.name} requesterName={offer?.decision?.source_bot_id?roster?.find(b=>b.key.bot_id===offer.decision!.source_bot_id)?.name:undefined} w={w}/>
  const renderExchangeDecision=(exchangeID:string)=>{const card=exchangeDecisions.get(exchangeID);return card?renderDecision(card):null}
  const renderTeamPermission=(card:W.ProjectAssistantPermissionOffer)=>{
    const detail=details[card.id]
    const offer=detail&&"fence" in detail&&samePermission(detail,card)&&detail.state!=="pending"?detail:card
    return <PermissionCard offer={offer} enabled={enabled&&currentPermission(offer,head)&&permissionRows.current.some(row=>row.id===offer.id)} onAnswer={answer} context={permissionContext} machine={permissionMachine} w={w}/>
  }
  // While a turn runs, a new message waits behind it: it is sent, and then queued, at the end of the conversation.
  const sendingBubble=sending!==null&&!confirmed&&<article key={localSend||pendingSend?.intent.command.idempotency_key} data-local-entrance={!!localSend||undefined} className="pa-message pa-mine pa-sending" aria-live="polite">
    {!hideProvisionalText&&<p>{sending}</p>}
    {sendingAccepted&&pendingSend
      ? <UnsentActions accepted discard={unsentDiscardKind(pendingSend,{accepted:true,transporting:view.heldOutcomes.transporting(draftKey,pendingSend.intent.command.idempotency_key)})}
          discardFailed={discardFailed===pendingSend.intent.command.idempotency_key} onDiscard={()=>discardSend(pendingSend)} w={w} language={language}/>
      : <small><span className="pa-send-spinner" aria-hidden="true"/>{w.sending}</small>}
  </article>
  return <section className={`pa-chat-layout pa-chat-surface${infoOpen?" pa-info-open":""}`} aria-label={thread?.title??bot.name}><div className="pa-chat" inert={!!browserOpener}
    {...dragHandlers} data-file-drag={dropTarget?(dropTarget.refusal?"refused":"ready"):undefined}>
    {dropTarget&&<div className="pa-drop-target" data-refused={dropTarget.refusal?true:undefined} role="status" aria-live="polite">
      <div className="pa-drop-target-label">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{dropTarget.refusal?<><circle cx="12" cy="12" r="8.5"/><path d="M6 6l12 12"/></>:<><path d="M12 15V4"/><path d="M7.5 8.5L12 4l4.5 4.5"/><path d="M5 14v4.5A1.5 1.5 0 0 0 6.5 20h11a1.5 1.5 0 0 0 1.5-1.5V14"/></>}</svg>
        <strong>{dropTarget.refusal?w.dropBlocked:canInput?w.dropAttach:w.dropAttachImages}</strong>
        <span>{dropTarget.refusal||w.dropAttachHint}</span>
      </div>
    </div>}
    {thread?<header className="pa-chat-header pa-thread-header">
      <button ref={identityButton} type="button" className="pa-circle" onClick={onBack} aria-label={w.backgroundBack}>‹</button>
      <div className="pa-thread-heading"><h2>{thread.title}</h2><small><span>{backgroundTaskLabel(thread,w)}</span>
        <BackgroundDuration task={thread}/>{projectName&&<span title={projectName}>{projectName}</span>}
      </small></div>
      {data?.conversation.active_turn_id&&<BackgroundStop w={w} key={data.conversation.active_turn_id} disabled={!enabled||!canWrite(data.source)||!!currentStop||busy} action={()=>void stop()}/>}
    </header>:<header className="pa-chat-header">
      <button className="pa-circle pa-chat-back" data-show-back={showBack||undefined} onClick={()=>{flush();onBack()}} aria-label={backLabel??w.back} title={backLabel??w.back}>‹</button>
      <h2 className="pa-chat-heading">
        <button ref={identityButton} type="button" className="pa-info-toggle pa-chat-identity" aria-label={`${bot.name} · ${w.botInformation}${serverLabel?" · "+serverLabel:""}`} aria-expanded={infoOpen} onClick={()=>infoOpen?setInfoOpen(false):openInfo("details")}>
          <span className="pa-avatar-status"><BotAvatar appearance={bot.appearance} size={28} mood={mood}/><AgentBadge agent={bot.agent} size={9}/></span>
          <span className="pa-chat-identity-text"><span className="pa-chat-server-heading"><strong>{bot.name}</strong><DevServerTitle observation={servers} w={w}/></span><small className="pa-row-project"><ProjectMark icon={projectIcon} size={12}/><span>{[projectName||w.unnamedProject,agentName(bot.agent),machineName].filter(Boolean).join(" · ")}</span></small></span>
          <svg className="pa-chat-identity-chevron" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M6 4l4 4-4 4"/></svg>
        </button>
      </h2>
      <BotBrowserChip source={source} current={current} bot={bot} hidden={recoveryRequired} paused={!!browserOpener} onOpen={setBrowserOpener}/>
    </header>}
    {(reconnecting||error)&&<div className="pa-chat-error" role={reconnecting?"status":"alert"} data-reconnecting={reconnecting||undefined}>
      <span>{reconnecting?w.chatReconnecting:errorField==="inspect_interrupted_turn"?w.sendInterrupted:providerSetupFailure(errorField,w)??(error==="pa_capacity_full"&&errorField==="owned_cleanup"?w.sendCleanupPending:error==="cli_not_installed"?w.setup_cli_not_installed:error==="provider_version_unsupported"?w.setup_version_unsupported:<>{w.unavailable} ({error})</>)}</span>
      <button type="button" className="pa-pill" onClick={()=>void checkForUpdates()} disabled={checkingUpdates} aria-busy={checkingUpdates||undefined}>{w.refresh}</button>
      {!reconnecting&&error==="pa_capacity_full"&&errorField==="owned_cleanup"&&<button type="button" className="pa-pill" title={w.releaseUnconfirmedRisk} aria-describedby={releaseRiskID} onClick={()=>void releaseUnconfirmed()} disabled={busy||!enabled} aria-busy={busy||undefined}>{w.releaseUnconfirmed}</button>}
      {!reconnecting&&error==="pa_capacity_full"&&errorField==="owned_cleanup"&&<small id={releaseRiskID} className="pa-chat-error-risk">{w.releaseUnconfirmedRisk}</small>}
    </div>}
    {decisionHandled&&<div className="pa-decision-handled" role="status"><span>{w.decisionHandled}</span>
      <button type="button" className="pa-pill" onClick={()=>setDecisionHandled(false)}>{w.decisionHandledDismiss}</button></div>}
    {!thread&&<BotVoicePanel bot={bot} language={language} call={botVoice}/>}
    {!thread&&<BotCallBar call={call} state={callState} name={bot.name} language={language} realtime={realtimeCall}/>}
    <div className="pa-chat-scroll" ref={scroll} onScroll={onScroll} onFocus={markVisible} tabIndex={0} aria-label={w.compose+" "+bot.name}>
      <div className="pa-chat-content">{brief&&<details className="pa-thread-brief"><summary>{w.backgroundBrief}</summary><p>{brief.text}</p></details>}{chatSkeleton&&<ChatSkeleton label={w.chatLoading}/>}{data?.source.access==="history_only"&&<p role="status">{w.history_only}</p>}
      {!leadSetup.blocked&&showLeadStarter({thread:!!thread,lead:!!bot.lead,loaded:!!data,writable:enabled&&!unknown&&!legacyLocked,messages:timeline.length,earlier:!!more,sending:sending!==null,queued:parts.queued.length,draft:text})&&<LeadStarter w={w} onChoose={value=>{changeText(value);scroll.current?.parentElement?.querySelector<HTMLTextAreaElement>(".pa-composer textarea")?.focus()}}/>}
      {!!more&&<button className="pa-pill" disabled={earlierLoading} aria-busy={earlierLoading||undefined} onClick={()=>void earlier()}>{earlierLoading?w.earlierLoading:w.earlier}</button>}<ChatRows renderExchangeWork={renderExchangeWork} requestAbove={work=>{const card=requests.above.get(work.id);return card&&<BotRequestLine card={card} w={w} onOpen={setOpenRequest}/>}} onOpenBot={onOpenBot} renderExchangeDecision={renderExchangeDecision} mentions={mentions} roster={[bot,...(roster??[])]} artifactComposer={artifactComposer} arrivals={motion.arrivals} onArrivalEnd={motion.finish} afterMessage={afterMessage} renderPermission={renderTeamPermission} dismissActionsKey={infoOpen?infoTab:""} browserTraces={[...Object.values(details).filter((value):value is W.ProjectAssistantTrace=>"text" in value),...Object.values(observedBrowserTraces)]} onOpenBrowser={browserAvailable&&canReadActions?setBrowserOpener:undefined} source={source} botKey={key} attachmentURL={id=>source.attachmentURL?.(key,id)??null} turns={turns} timeline={brief?chatTimeline.filter(item=>item.message?.id!==brief.id):chatTimeline} onOpenWork={onOpenWork} onQuote={thread?undefined:m=>{setQuote(m.id);view.quotes.set(draftKey,m.id);scroll.current?.parentElement?.querySelector<HTMLTextAreaElement>(".pa-composer textarea")?.focus()}} w={w} language={language}/>{!activeTurn&&sendingBubble}
      {pendingRows.map(row=>{
        const intent=row.intent as W.ProjectAssistantSendRequest,id=intent.command.idempotency_key
        if(id===recoveringID||admittedReceipt(row.receipt)&&messages.some(m=>m.turn_id===row.receipt?.resource_id&&m.kind==="user"))return null
        const assets=pendingAttachments[attachmentsFor(row)]
        const pictures=intent.images??assets?.images??[]
        const loaded=!!intent.images||assets!==undefined||(row.pictures??0)===0
        const missing=loaded&&(row.pictures??0)>pictures.length
        const restore=row.needsEdit||missing||(row.files?.length??0)>(intent.upload_ids?.length??0)
        return <article key={id} className="pa-message pa-mine pa-unsent">
          <p>{intent.text||w.heldNoText}</p>
          {!!intent.quote_message_id&&<small>{w.heldQuoted}</small>}
          {!!row.files?.length&&<small>{row.files.join(" · ")}</small>}
          {!!row.pictures&&<small>{w.outgoingPictureCount.replace("{count}",String(row.pictures))}</small>}
          <UnsentActions retrying={view.heldOutcomes.transporting(draftKey,id)} accepted={admittedReceipt(row.receipt)} failure={sendFailure.get(row)} restore={restore}
            canRestore={!recoveringID&&!paused} canRetry={enabled&&loaded} disabled={busy||(!restore&&!!retrying)}
            onRestore={()=>restoreSend(row)} onRetry={()=>void retrySend(row)}
            discard={unsentDiscardKind(row,{accepted:admittedReceipt(row.receipt),transporting:retrying===id||view.heldOutcomes.transporting(draftKey,id)})}
            discardFailed={discardFailed===id} onDiscard={()=>discardSend(row)} w={w} language={language}/>
        </article>
      })}

      {!thread&&!teamCards&&<TeamApprovals sources={[source]} bots={data?.conversation.active_turn_id?[bot]:[]}/>}
      {(pendingPermissionCount>permissionCards.length||sectionPermissions.length>0)&&<section className="pa-permissions"><h3>{w.permission}{pendingPermissionCount>0&&` · ${pendingPermissionCount}`}</h3>{sectionPermissions.map(({summary:row,offer,identity})=>row.tool_kind==="decision"?renderDecision({summary:row,offer,identity}):<PermissionCard key={identity} startExpanded={row.state==="pending"&&row.tool_kind!=="team_create"&&row.tool_kind!=="team_update"&&!collapsedPermissions.current.has(identity)} onExpandedChange={open=>{if(open)collapsedPermissions.current.delete(identity);else collapsedPermissions.current.add(identity)}} summary={row} offer={offer} enabled={enabled&&pendingPermissionCount>0&&currentPermission(row,head)} onDetail={()=>readPermissionDetails(row.id,row.detail_cursor)} onAnswer={answer} context={permissionContext} machine={permissionMachine} w={w}/>)}{permissionMore&&<button className="pa-pill" onClick={()=>void collection("permissions",permissionMore).catch(readFailed)}>{w.more}</button>}</section>}
      {visibleInterruptions.map(turn=>
        <TurnInterruption key={turn.id} turn={turn} w={w}
          onProgress={turn===restartInterruption&&messages.some(message=>message.id===turn.message_id)?()=>viewHandoffHistory(turn.message_id):undefined}/>)}
      <FallbackProgress primary={bot.agent} turn={activeTurn??turns.find(t=>t.attempts?.length)} w={w}/>
      <WorkingTransition turn={visibleTurn} activity={currentActivity} subject={workingSubject(visibleTurn,timeline,w)} stopStatus={stopText} waiting={mood==="needsInput"} live={live} ready={!!data} w={w}
        steps={liveSteps.length||deferredTrace?expanded=><Trace inline expanded={expanded} summaries={liveSteps} details={details} more={!!traceMore&&(liveTraceTurn===moreTraceTurn||!liveSteps.length)} onMore={loadMoreTraces}
          onDetail={readTraceDetail} deferred={deferredTrace} w={w}/>:undefined}/>
      {stopText&&!visibleTurn&&<p className="pa-turn" role="status">{stopText}</p>}
      {queueLimited&&!queuePaused&&<p className="pa-turn" role="status">{w.queueLimited}</p>}
      {waitingTurn&&<p className="pa-turn" data-waiting-turn={waitingTurn.id} role="status">{w[waitingTurn.state]}</p>}
      {(orphanTraces.length>0||deferredTrace&&!latestTraceAnchor&&!liveTraceTurn&&messages.length>0)&&
        <Trace summaries={orphanTraces} details={details} incomplete={moreOrphanTraces} more={moreOrphanTraces} onMore={loadMoreTraces} onDetail={readTraceDetail}
          deferred={!latestTraceAnchor&&!liveTraceTurn?deferredTrace:undefined} w={w}/>}
      {!!activeTurn&&sendingBubble}
      {parts.queued.length>0&&<div className="pa-queue" role="list" aria-label={w.queued}>{parts.queued.map(message=>{
      const queuedTurn=turns.find(turn=>turn.message_id===message.id&&turn.state==="queued")
      const pending=queueAction?.id===message.id?queueAction.operation:null
      const hold=queuedTurn?.queue_edit_id
      const editable=!thread&&message.kind==="user"&&!!queuedTurn&&source.canEditQueue?.()&&!!source.editQueue
      const editor=editable&&queueEditor&&queueEditor.turnID===queuedTurn.id&&queueEditor.editID===hold?queueEditor:null
      return <div className="pa-queued" role="listitem" data-editing={editor?true:undefined} key={message.id} aria-busy={pending?true:undefined}>
        <div className="pa-queued-content">
          <span className="pa-queued-preview"><span className="pa-queued-label">{queuedLabel(message.id,turns,queuePaused,w)} · </span>{message.text?<BotMentionText text={message.text} candidates={mentions}/>:queuedAttachments(message,w.heldPictures)}</span>
          {editor&&<textarea className="pa-queued-editor" ref={queueEditorInput} rows={1} cols={1}
            aria-label={w.editQueued} aria-description={w.queueHeld} value={decodeBotMentions(editor.text).display} disabled={!enabled||queueEditBusy}
            onChange={event=>setQueueEditor({...editor,text:encodeBotMentions(event.target.value,decodeBotMentions(editor.text).picks)})}/>}
        </div>
        <div className="pa-queued-actions">{editor?<>
          <button className="pa-queued-action" type="button" aria-label={w.save} title={w.save}
            disabled={!enabled||busy||queueEditBusy||!editor.text.trim()&&!message.files?.length&&!message.attachments?.length}
            onClick={()=>void editQueued("save",editor.turnID,editor.editID,editor.text)}>
            <svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m3.5 8 3 3 6-6"/></svg>
          </button>
          <button className="pa-queued-action" type="button" aria-label={w.queueKeepOriginal} title={w.queueKeepOriginal}
            disabled={!enabled||busy||queueEditBusy}
            onClick={()=>void editQueued("cancel",editor.turnID,editor.editID,"")}>
            <svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" aria-hidden="true"><path d="m4.5 4.5 7 7m0-7-7 7"/></svg>
          </button>
        </>:<>
          {editable&&<button className="pa-queued-action pa-queued-edit" type="button" aria-label={w.editQueued}
            aria-description={hold?w.queueHeld:undefined} title={hold?`${w.editQueued} · ${w.queueHeld}`:w.editQueued}
            disabled={!enabled||busy||queueEditBusy||queueEditorOpen}
            onClick={()=>hold?setQueueEditor({turnID:queuedTurn.id,editID:hold,text:message.text}):void editQueued("begin",queuedTurn.id,"",message.text)}>
            <svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M10.5 2.5l3 3L6 13H3v-3z"/></svg>
          </button>}
          {queuedTurn&&(source.canReplyInput?.()||!message.files?.length&&!message.attachments?.length)&&source.canSendNow?.()&&!noSteering.includes(bot.agent)&&activeTurn?.state==="running"&&!restartTurn&&
            <button type="button" className="pa-queued-action pa-queued-send" aria-label={w.sendNow} title={w.sendNow}
              disabled={!enabled||busy||queueEditBusy||!!hold} onClick={()=>void actOnQueued(message.id,queuedTurn.id,"send_now")}>
              {pending==="send_now"?<span className="pa-send-spinner" aria-hidden="true"/>:"↳"}
            </button>}
          {queuedTurn&&source.canUnqueue?.()&&
            <button type="button" className="pa-queued-action pa-queued-remove" aria-label={w.removeQueued} title={w.removeQueued}
              disabled={!enabled||busy||queueEditBusy} onClick={()=>void actOnQueued(message.id,queuedTurn.id,"stop")}>
              {pending==="stop"?<span className="pa-send-spinner" aria-hidden="true"/>:"×"}
            </button>}
        </>}</div>
      </div>
    })}</div>}
      {handoff.available&&<HandoffCard card={handoff.card} language={language} enabled={enabled&&!busy&&!unknown} busy={handoff.busy} error={handoff.error}
        onEdit={(ref,text)=>handoff.update("edit",ref,text)} onKeep={ref=>void handoff.update("keep",ref)}
        onHistory={viewHandoffHistory} onRefresh={()=>void handoff.refresh()} onEditing={setEditingHandoff}/>}
      {legacyDrafts.map((draft,i)=><article className="pa-message pa-mine pa-unverified-draft" key={i}>
        <div className="pa-message-bubble">
          {draft.quote&&<blockquote>{messages.find(message=>message.id===draft.quote)?.text??w.quoteMissing}</blockquote>}
          <p>{draft.text}</p>
        </div>
        <div className="pa-unverified-action">
          <small>{w.unverifiedDraft}</small>
          {"continued" in draft&&draft.continued===true&&<>
            <button type="button" disabled={!canEditLegacy} onClick={()=>editLegacyDraft(draft)}>{w.unverifiedEdit}</button>
            <button type="button" onClick={removeLegacyDraft}>{w.unverifiedRemove}</button>
          </>}
        </div>
        {legacyRemoveFailed&&<p role="alert" className="pa-turn">{w.unverifiedRemoveFailed}</p>}
      </article>)}
      {(legacyProblem||legacyLocked&&enabled)&&<div className="pa-unverified-save">
        {legacyProblem&&<p className="pa-turn" role="alert">{w.unverifiedUnsaved}</p>}
        {enabled&&legacyProblem!=="unreadable"&&view.unverifiedDrafts.active(draftKey,scope)&&
          <button type="button" className="pa-pill" disabled={busy} onClick={()=>{archiveLegacyDraft();repaintJournal()}}>{w.unverifiedSave}</button>}
      </div>}
      {/* Background and dispatched work are the latest facts of the conversation: they scroll at its end instead of sitting over the composer. */}
      {!thread&&(data||error)&&<div className="pa-chat-work">{(()=>{const capsule=<BackgroundCapsule tasks={tasks} appearance={bot.appearance} open={openTask} stop={task=>void stopTask(task)} stopping={backgroundStopping} outcomes={backgroundOutcomes} writable={enabled&&!!data&&canWrite(data.source)} w={w}/>
        const loose=requests.loose.length>0&&<div className="pa-request-loose">{requests.loose.map(card=><BotRequestLine key={card.id} card={card} w={w} onOpen={setOpenRequest}/>)}</div>
        return <>{loose}{showDispatched?<DispatchedWorkStrip links={chatTimeline.flatMap(row=>row.work?[row.work]:[])} work={summary.dispatched_work} ownProjectID={summary.bot.project.kind==="personal"?undefined:summary.bot.project.project_id} background={capsule} onOpen={onOpenWork} w={w}/>:capsule}</>})()}{openRequest&&<BotRequestDetail key={openRequest.id} card={openRequest} bots={[bot,...(roster??[])]} links={requests.links.get(openRequest.id)} w={w} language={language} onClose={()=>{setOpenRequest(undefined);requestReading.refresh()}} onChanged={requestReading.refresh} onOpenChat={openRequest.baton.bot===key.bot_id?()=>setOpenRequest(undefined):onOpenBot&&(card=>{const holder=botOf(card.baton.bot,roster??[]);setOpenRequest(undefined);if(holder)onOpenBot(holder.key)})} onOpenWork={onOpenWork}/>}{backgroundError&&<p className="pa-turn" role="status">{w.unavailable} ({backgroundError})</p>}</div>}
      </div>
      {/* The same floating capsule as an ordinary Session's transcript (session/jump.css): it takes no line of its own. */}
      {/* Hidden while a queued message is edited, so it never covers the message being edited. */}
      <div className="tx-jump pa-jump" data-end="bottom" data-shown={following||queueEditorOpen?"off":"on"} data-fresh="off">
        <button type="button" className="tx-jump-go" tabIndex={following||queueEditorOpen?-1:0} aria-hidden={following||queueEditorOpen?true:undefined} title={w.jump} aria-label={backgroundRunning?`${w.jump} · ${w.jumpWorking}`:undefined} onClick={()=>{followingRef.current=true;setFollowing(true);if(scroll.current)scroll.current.scrollTop=scroll.current.scrollHeight}}>
          <svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M8 3v10M3.5 8.5 8 13l4.5-4.5"/></svg>
          <span>{w.jump}</span>
          {/* The stop control now scrolls with the conversation; while reading earlier messages, the way back says work is running. */}
          {backgroundRunning&&<span className="pa-jump-working"><span className="pa-background-mark" data-active="" aria-hidden="true"/>{w.jumpWorking}</span>}
        </button>
      </div>
    </div>
      {inlineReview&&!following&&<button type="button" className="pa-pill pa-team-jump" onClick={()=>{
        const card=Array.from(scroll.current?.querySelectorAll<HTMLElement>("[data-permission-id]")??[]).find(node=>node.dataset.permissionId===inlineReview.id)
        card?.focus({preventScroll:true});card?.scrollIntoView({block:"nearest"})
      }}>{w.teamReviewJump}</button>}
    <div className="pa-chat-footer">
      {!heldSend&&receipt&&(receipt.state==="failed"||receipt.state==="unknown"&&!unknown)&&<p role="status">{w[receipt.state]}</p>}
      {restartNotice&&!restartNotice.notice?.cause&&<p className="pa-turn pa-restart-state" role="status">
        {w.restartRestoring}{restartWaitingCount>0&&` · ${w.restartQueueKept.replace("{count}",String(restartWaitingCount))}`}
      </p>}
      {showTurnReview&&<TurnReviewNotice w={w} restarted={!!restartInterruption&&unknownCount===1} queued={queuedCount}
        enabled={enabled} canRequest={canRequestTurnReview} pending={reviewPending} onReview={requestTurnReview}/>}
      {!heldSend&&unknown&&<p role="status">{w.unknown}</p>}
      {heldSend&&isBotCallSend(heldSend.intent.command.idempotency_key)&&<p role="status">{w.unknown} <button type="button" onClick={leaveUnreadable}>{w.writeNewMessage}</button></p>}
      {view.heldOutcomes.problem(draftKey)==="unreadable"&&<p role="alert">{w.heldUnreadable} <button type="button" onClick={leaveUnreadable}>{w.writeNewMessage}</button></p>}
      {view.heldOutcomes.problem(draftKey)==="unsaved"&&(unknown||pendingRows.length>0)&&<p role="alert">{w.heldUnsaved}</p>}
      {view.heldOutcomes.unreadable(draftKey)&&<p role="status">{w.heldEarlierUnreadable} <button type="button" onClick={()=>{view.heldOutcomes.dismissUnreadable(draftKey);repaintJournal()}}>{w.dismissHeldNote}</button></p>}
      {recoveryNotice&&<p role="status">{recoveryNotice}</p>}
      {recovering&&!skipMissing&&(missingPictures>0||missingFiles>0)&&<p role="status">
        {missingPictures>0&&w.restoreMissingPictures.replace("{count}",String(missingPictures))} {missingFiles>0&&w.restoreMissingFiles.replace("{count}",String(missingFiles))}
        {" "}<button type="button" onClick={()=>setSkipMissing(true)}>{w.skipMissingAttachments}</button>
      </p>}
      {paused&&<button type="button" className="pa-pill" disabled={busy} onClick={()=>void resumeComposer()}>{w.resumePreviousDraft}</button>}
      {!draftDurable&&<p role="status">{w.draftUnavailable}</p>}
      {filesLost&&<p role="alert">{w.filesLost} <button type="button" onClick={()=>{rememberFiles(false);setFilesLost(false)}}>{w.discardFiles}</button></p>}
      {fileError&&<p role="alert">{fileError}</p>}
      {queueEditError&&<p role="alert">{w.queueEditFailed} ({queueEditError})</p>}
      {!thread&&<ContextActionStatusRow compact={compact} freshSessionNotice={freshSessionNotice} w={w}
        onRetryCompact={()=>void compactNow()} onDismissCompact={()=>setCompactRequest(null)} onDismissFreshSession={dismissFreshSession}/>}
      {!thread&&data&&<div className="pa-chat-metadata">
        {live&&head?.context_usage&&<ContextMenu usage={head.context_usage} w={w} language={language} compacting={compacting}
          compactBlock={compactBlockFor(enabled,bot.agent,!!head.conversation.active_turn_id||!!head.queued_turn_count||!!head.unknown_turn_count||turns.some(turn=>turn.state==="queued"||turn.state==="accepted"),!!head.current_fence.binding_id)}
          onCompact={()=>void compactNow()} onNewSession={onNewSession}/>}
        <ChatModel bot={bot} source={source} enabled={enabled} activeTurn={!!head?.conversation.active_turn_id}
          reported={live?head?.active_model?.name:undefined} save={saveModel} w={w}/>
      </div>}
      {leadSetup.show&&leadSetupBot&&<LeadSetup setup={leadSetup} w={w} choose={agent=>editBot({
        expected_version:leadSetupBot.version, name:leadSetupBot.name, appearance:leadSetupBot.appearance, instructions:leadSetupBot.instructions.text, agent,
      },leadSetupBot)}/>}
      <Composer key={draftKey} mentions={mentions} plain={!!thread} call={callStart.canStart?{label:callStart.label,disabled:!callStart.ready,start:callStart.start}:undefined} callActive={call.active||botVoice.active} text={legacyLocked?"":text} onText={changeText} onSend={()=>void send()} onStop={!thread&&data?.conversation.active_turn_id?()=>void stop():undefined} stopDisabled={!enabled||!!currentStop} stopBusy={currentStop==="sending"} disabled={!enabled||unknown||legacyLocked} draftEditable={!thread&&!unknown&&!legacyLocked&&(reconnecting||canTryText)} sendTextDuringRecovery={canTryText} sendBlocked={readingImages>0||leadSetup.blocked||filesLost||(!skipMissing&&(missingPictures>0||missingFiles>0))||editingHandoff||handoff.busy||!!handoff.error} busy={busy} quote={!legacyLocked&&quote?messages.find(m=>m.id===quote)?.text??w.quoteMissing:undefined} onClearQuote={()=>{setQuote("");view.quotes.delete(draftKey)}} w={thread?{...w,compose:w.backgroundCompose}:w} images={images} onAttach={canAttach||canInput?attach:undefined} acceptFiles={canInput} onRemoveImage={id=>setImages(old=>old.filter(picture=>picture.id!==id))}
        onReplaceImage={(id,url)=>setImages(old=>old.map(picture=>picture.id===id?{...picture,url}:picture))} files={files} onRemoveFile={id=>{const next=files.filter(f=>f.id!==id);setFiles(next);rememberFiles(!!next.length)}} uploading={uploading} onCancelUpload={()=>uploadControl.current?.abort()}/>
    </div>
    </div>
    {browserOpener&&<BotBrowser source={source} current={current} bot={bot} writable={enabled} reason={enabled?"":recoveryRequired?w.unavailable:data?.source.state==="online"&&data.source.fresh?botBrowserWord("readOnly"):readOnlyReason(data?.source,w as unknown as Record<string,string>)||botBrowserWord("readOnly")} onClose={()=>setBrowserOpener(null)} onStale={async()=>{const fresh=await authorized(source,current,s=>source.get(key,s),new AbortController().signal);if(valid()){setData(fresh);onBotChanged(fresh.bot)}}}/>}

    {templateOpener&&<BotTemplate opener={templateOpener} source={source} current={current} scope={scope} available={()=>valid()&&!recovery.current&&!!currentData.current&&canRead(currentData.current.source)&&sameConversation({key:currentData.current.bot.key,project:currentData.current.bot.project,conversation_id:currentData.current.conversation.id},scope)} w={w} onClose={()=>setTemplateOpener(null)}/>}
    {!thread&&<BotSidePanel onOverlayBack={closeTask} overlay={selected&&threadSummary?<Chat key={selected.id} thread={selected} summary={threadSummary} roster={roster} projectNameOf={projectNameOf} projectIconOf={projectIconOf} source={source} current={current} accountKey={accountKey} readerID={readerID} view={view} w={w} language={language} projectName={projectName||bot.project.project_id} onBack={closeTask} onBotChanged={()=>{}}/>:undefined} inactive={!!browserOpener||!!templateOpener} opener={identityButton} bot={bot} open={infoOpen} tab={infoTab} onTab={openInfo} onClose={()=>{setInfoOpen(false);setSelectedTask("")}} w={w} detailsActions={
        <><DevServers key={JSON.stringify([accountKey,readerID,source.authorizationRevision,bot.key,bot.project])} source={source} bot={bot} enabled={enabled} active={infoOpen&&infoTab==="details"&&!browserOpener&&!templateOpener} observation={servers} w={w}/><div className="pa-info-actions">
          {handoff.canPrepare&&<button type="button" className="pa-pill" disabled={busy||unknown||handoff.busy} onClick={()=>void prepareHandoff()}>{handoffWords(language).prepare}</button>}
          {browserAvailable&&<button type="button" className="pa-pill" disabled={!canReadActions} onClick={event=>setBrowserOpener(event.currentTarget)}>{botBrowserWord("open")}</button>}
          <button type="button" className="pa-pill" disabled={checkingUpdates} aria-busy={checkingUpdates||undefined} onClick={()=>void checkForUpdates()}>{w.refresh}</button>
        </div></>
      } editor={<BotSettings key={settingsRevision} embedded nameInput={profileName} workspace={{project:projectName||bot.project.project_id,machine:machineName||source.machineName||key.machine_id}} data={<>
          <button type="button" className="pa-settings-link" disabled={!canReadActions} onClick={event=>setTemplateOpener(event.currentTarget)}>{w.templateCreate}</button>
          <button type="button" className="pa-settings-link" disabled={!canReadActions||exporting} aria-busy={exporting||undefined} onClick={()=>void exportBot()}>{w.export}</button>
          {exportError&&<p role="alert">{w.unavailable} ({exportError})</p>}
        </>} teamAvailable={source.canGroups?.()===true} bot={bot} enabled={enabled} manage={source.canManage?.()&&source.setHidden&&source.deleteBot?<BotManage bare w={w} enabled={enabled} hideDisabled={bot.lead} onHide={async()=>{const r=await mutate((_,s)=>source.setHidden!({command:command(key,"visibility"),hidden:true},s));if(valid()){onBotChanged(r.bot);setSettings(false);onBack()}}} onDelete={async()=>{await mutate((_,s)=>source.deleteBot!({command:command(key,"delete")},s));setSettings(false);onDeleted?.()}}/>:undefined} rules={(bot.agent==="claude"||source.canSourceRules?.()===true)&&source.canRules?.()&&source.rules&&source.removeRule?<RuleList bare w={w} enabled={enabled} load={loadRules} remove={async rule=>{await mutate((_,s)=>source.removeRule!({command:command(key,"rule_remove"),rule},s))}}/>:undefined} reason={recoveryRequired?w.unavailable:readOnlyReason(data?.source,w as unknown as Record<string,string>)} w={w} onClose={()=>{setSettingsRevision(n=>n+1);setSettings(false)}} onSave={saveSettings} onCheck={checkSettings}/>} children={{
      details:<><BackgroundList slots={bot.background_threads??3} section="live" tasks={tasks} workspace={projectName||bot.project.project_id} open={openTask} w={w}/><Routines key={JSON.stringify([accountKey,readerID,source.authorizationRevision,key.machine_id,key.bot_id])} source={source} mentions={mentions} bot={bot} enabled={enabled} w={w} storageKey={JSON.stringify([accountKey,readerID,key.machine_id,key.bot_id])}/>
        <BackgroundList slots={bot.background_threads??3} section="recent" tasks={tasks} workspace={projectName||bot.project.project_id} open={openTask} w={w}/>
      </>,
      library:<OutputLibrary embedded artifactComposer={artifactComposer} source={source} bot={bot} messages={messages} w={w} onClose={()=>setInfoOpen(false)}/>,
      memory:<div aria-busy={memoryLoading||undefined}>{!memory&&!memoryLoading?<p role="status">{w.unavailable} <button type="button" onClick={()=>void memoryPanel.current?.open()}>{w.retry}</button></p>:<MemoryPanel embedded value={memory} readDetail={(detail,page)=>readMemory(new AbortController().signal,detail,page??botMemoryPage)} readProjectPage={projectMemoryPages?async(epoch,page,signal)=>{
        const value=await readMemory(signal,{source_epoch:epoch,layer:"project",entry_id:"",revision:""},page)
        const layer=value.view.layers.find(layer=>layer.layer==="project")
        if(!layer)throw failure("pa_invalid")
        return layer
      }:undefined} pendingText={memorySave.current?.entry.kind==="note"?undefined:memorySave.current?.entry.text} pendingNote={memorySave.current?.entry.kind==="note"?{entry_id:memorySave.current.entry.id,title:memorySave.current.entry.title??"",summary:memorySave.current.entry.summary??"",text:memorySave.current.entry.text,expected_revision:memorySave.current.expected_revision}:undefined} noteDraft={memoryNoteDraft} onNoteDraft={setMemoryNoteDraft} onSaveNote={source.canTopicMemory?.()?saveMemory:undefined} pendingSources={memoryForget.current?.source_ids} enabled={enabled} onSave={saveMemory} onForget={forgetMemory} onRefresh={loadExport} onClose={()=>setInfoOpen(false)} w={w}/>}</div>,
      settings:null
    }}/>}
  </section>
}

function fileDrag(transfer:DataTransfer) {
  return Array.from(transfer.types).includes("Files")||transfer.files.length>0
}
function pictureFile(file:File) {
  if(file.type.startsWith("image/"))return file
  if(file.type)return null
  const ext=file.name.split(".").pop()?.toLowerCase()
  const mime=ext==="jpg"||ext==="jpeg"?"image/jpeg":ext==="png"||ext==="gif"||ext==="webp"?`image/${ext}`:null
  return mime?new File([file],file.name,{type:mime,lastModified:file.lastModified}):null
}

function compactBlockFor(enabled:boolean,agent:string,working:boolean,session:boolean):CompactBlock{
  if(!enabled)return "readOnly"
  if(agent!=="claude"&&agent!=="codex")return "agent"
  if(working)return "working"
  return session?null:"noSession"
}
