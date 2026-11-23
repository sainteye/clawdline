import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import {sameBot,sameConversation} from "./view-state.ts"

export function admittedReceipt(receipt:W.ProjectAssistantReceipt|null|undefined) {
  return !!receipt&&["accepted","executing","delivered","observed","read"].includes(receipt.state)
}

/** A malformed write answer cannot prove refusal or release the original intent. */
export function sendAcknowledgement(value:unknown,intent:W.ProjectAssistantSendRequest):value is W.ProjectAssistantReceiptResponse {
  const v=value as W.ProjectAssistantReceiptResponse|undefined,r=v?.receipt,c=r?.command
  return v?.contract_version==="project-assistants.v1"&&!!r&&!!c?.key&&sameBot(c.key,intent.command.key)&&c.operation==="send"&&c.idempotency_key===intent.command.idempotency_key&&c.client_attempt_id===intent.command.client_attempt_id&&typeof c.content_hash==="string"&&typeof r.id==="string"&&!!r.id&&Number.isSafeInteger(r.event_seq)&&r.event_seq>=0&&typeof r.resource_id==="string"&&["accepted","executing","delivered","observed","read","failed","unknown"].includes(r.state)&&(!admittedReceipt(r)||!!r.resource_id)
}

/** Admission is not observation: match the source's turn and message, not text alone. */
export function admittedMessage(intent:W.ProjectAssistantSendRequest,receipt:W.ProjectAssistantReceipt|null|undefined,messages:readonly W.ProjectAssistantMessage[]) {
  return !!admittedRow(intent,receipt,messages)
}

export function admittedRow(intent:W.ProjectAssistantSendRequest,receipt:W.ProjectAssistantReceipt|null|undefined,messages:readonly W.ProjectAssistantMessage[]) {
  if(!admittedReceipt(receipt)||receipt!.command.operation!=="send"||!sameBot(receipt!.command.key,intent.command.key)||receipt!.command.idempotency_key!==intent.command.idempotency_key)return
  return messages.find(message=>message.kind==="user"&&message.turn_id===receipt!.resource_id&&message.text===intent.text&&sameConversation(message.fence,intent.fence))
}

/** Only a send this Chat owns may keep reading while its browser is hidden. */
export class ChatObservation {
  private turn=""
  private admittedAt=0
  finalSnapshot=false
  constructor(receipt?:W.ProjectAssistantReceipt|null){if(receipt)this.accept(receipt)}
  accept(receipt:W.ProjectAssistantReceipt) {if(receipt.command.operation==="send"&&admittedReceipt(receipt)){this.turn=receipt.resource_id;this.admittedAt=receipt.event_seq;this.finalSnapshot=false}}
  get active(){return !!this.turn}
  stop(){this.turn="";this.finalSnapshot=false}
  apply(page:W.ProjectAssistantReadViewResponse) {
    if(!this.turn)return
    if(page.snapshot){
      // A read that started before admission cannot finish observation of
      // this turn just because its older snapshot has no pending turn.
      if(page.head.event_frontier<this.admittedAt)return
      // Require the owned turn's terminal fact, not absence in an early
      // admission snapshot. This final snapshot also recovers late rows.
      const terminal=page.snapshot.last_turn
      if(terminal?.id===this.turn&&["succeeded","failed","cancelled","unknown"].includes(terminal.state))this.stop()
      return
    }
    if(page.items.some(row=>row.event?.turn?.id===this.turn&&["succeeded","failed","cancelled","unknown"].includes(row.event.turn.state)))this.finalSnapshot=true
  }
}
