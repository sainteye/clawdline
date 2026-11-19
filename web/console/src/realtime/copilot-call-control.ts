/** A high-confidence local route for ending the current voice call. */
export function asksToEndCall(text: string): boolean {
  const words = text.normalize('NFKC').trim()
  const call = /通[話话]|電[話话]|對[話话]|[对對]话|聊天|\b(?:call|conversation|chat)\b/i
  const closing = /結束|结束|關[閉闭掉]|关[闭掉]|停在|先到這|先到这|斷開|断开|\b(?:end|finish|wrap up|close|disconnect|stop)\b/i
  const hangingUp = /[掛挂][斷断掉了]|[掛挂][電电][話话]|收[線线]|\bhang\s*up\b/i
  const farewell = /(?:拜拜|掰掰|再[見见]|bye(?:[ -]?bye)?|goodbye|talk to you later)[!.。！\s]*$/i
  if (/(?:不要|別|别|先不|不想|不用|不必|還不|还不)[^，,。.!]*[掛挂結结拜掰關关]|\b(?:do not|don['’]t|not yet|never)\b[^,.!]*\b(?:hang|end|close|finish|bye|disconnect)\b/i.test(words)) return false
  if (/(?:告訴|告诉|轉告|转告|跟|[對对]).*(?:Session|他|她).*[說说]|[他她][說说]|[「『“"]|\b(?:tell|message|ask)\b.*\b(?:session|them|him|her)\b|\b(?:said|quoted)\b/i.test(words)) return false

  const goodbye = words.match(farewell)
  if (goodbye && (goodbye.index === 0 || /(?:[，,。.!！；;]\s*|這[樣里]|这[样里]|謝謝|谢谢|\bthanks?\s*)$/i.test(words.slice(0, goodbye.index)))) return true
  if (/怎[麼么]|如何|假如|如果說|修[正復复]|[問问][題题]|按[鈕钮]|\b(?:how|what if|explain|fix|debug|button|issue|error|problem)\b/i.test(words)) return false

  // Only interpret the last clause about ending a call as call control.
  const clauses = words.split(/[\uff0c,\u3002.!\uff01\uff1b;]/).filter(clause => clause.trim())
  const last = clauses.at(-1) ?? ''
  if (/session|工作階段/i.test(last) && !call.test(last)) return false
  return hangingUp.test(last) || call.test(words) && closing.test(last)
}
export class CallEndIntent {
 requested=false
 private confirmation=false
 reset(){this.requested=false;this.confirmation=false}
 assistant(text:string){if(/(?:要|可以|是否|嗎|吗).*(?:掛斷|挂断|結束.*通話|结束.*通话)|\b(?:want|like|shall|should|may|can)\b.*\b(?:hang up|end (?:this|the|our) (?:call|conversation))\b/i.test(text)&&(/[？?嗎吗]/.test(text)||/^(?:would|do|shall|should|may|can)\b/i.test(text.trim())))this.confirmation=true}
 user(text:string,complete=true){
  if(/^(?:等一下|先等等|不要|取消|wait\b|stop\b|cancel\b|no\b|don[’']t\b|do not\b)/i.test(text)||/(?:不要|別|别).*掛|\bdon't hang/i.test(text)){this.reset();return false}
  const yes=/^(?:對|对|好|好的|是|沒錯|没错|yes|yeah|yep|correct|please do|ok(?:ay)?)[！!。.,，\s]*$/i.test(text.trim())
  this.requested=asksToEndCall(text)||this.confirmation&&yes
  if(complete)this.confirmation=false
  return this.requested
 }
}
export function callControlGuidance(language='') {
 return (!language||/^zh(?:-|$)/i.test(language))?'通話控制：使用者直接對你說「結束通話」「掛斷電話」「結束我們現在的通話」「拜拜／掰掰」表示結束這通電話，不是關閉 Session；請呼叫 end_call，不再反覆確認。若你剛明確問要不要掛斷，接著的「對／好／是」就是該次確認。程式會在辨識到明確要求後直接掛斷並播放結束提示音。如果工具先接受了要求，就簡短道別。按使用者的意思判斷，前面的句子只是例子。引用別人的道別、傳話或明確說不要掛斷都不算。':"Call control: direct requests such as 'end this call', 'hang up', 'end my conversation with you', 'bye' or 'goodbye' end this voice call, never a terminal Session. Call end_call without repeated confirmation. If you just explicitly asked whether to hang up, the person's next 'yes', 'correct' or 'okay' confirms that question. The application disconnects with an ending tone when it recognizes a direct request. If the tool accepted it first, say one short goodbye. Interpret the person’s meaning across natural phrasings. Quoted goodbyes, messages for others and explicit requests not to hang up do not count."
}
