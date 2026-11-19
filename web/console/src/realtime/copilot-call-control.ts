/** Call-local intent only. It never closes a terminal Session or authorizes work. */
export function asksToEndCall(text:string):boolean {
 const t=text.trim(),call=/(?:通話|通话|電話|电话|對話|对话|聊天|\bcall\b|\bconversation\b)/i
 if(/(?:不要|別|别|先不|不想|不用|不必|還不|还不).*(?:掛|挂|結束|结束|拜|掰|關|关)|\b(?:do not|don't|not yet|never|don't want to)\b.*\b(?:hang|end|close|bye)|\bhow\b.*\bhang\b/i.test(t))return false
 if(/(?:告訴|告诉|跟|對|对).*(?:Session|他|她|他們|他们).*(?:說|说)|\b(?:tell|message|ask)\b.*\b(?:session|them|him|her)\b/i.test(t))return false
 if(/session|工作階段/i.test(t)&&!/(?:這通|这通|跟你|和你|我們|我们|\b(?:this|our) call\b)/i.test(t))return false
 if(/修正|修復|修复|問題|问题|按鈕|按钮|錯誤|错误|\b(?:fix|debug|button|issue|error|problem)\b/i.test(t))return false
 if(/怎麼|怎么|如何|\bhow\b|\bexplain\b/i.test(t))return false
 if(/^(?:(?:好|好的|那|先這樣|先这样|謝謝|谢谢|再見|再见|ok(?:ay)?|thanks?(?: you)?|that's all)[，,。.!\s]*)*(?:拜拜|掰掰|再見|再见|bye(?:[ -]?bye)?|goodbye)[！!。.\s]*$/i.test(t))return true
 // A spoken sign-off can follow a whole sentence, e.g. "就這樣囉，掰掰".
 if(/(?:^|[，,。.!！；;\s])(?:那(?:就)?|就|先)?[這这][樣样](?:了|囉|啰|啦|吧)?[，,。.!！\s]+(?:拜拜|掰掰|再見|再见)[！!。.\s]*$|(?:^|[,.!;\s])(?:that['’]s (?:all|it)|talk to you later)[,.!\s]+(?:bye(?:[ -]?bye)?|goodbye)[!.\s]*$/i.test(t)&&!/(?:[「『“"]|他說|她說|\b(?:said|quoted)\b)/i.test(t))return true
 if(/掛斷|挂断|掛掉|挂掉|掛了|挂了|掛電話|挂电话|收線|收线|\bhang\s*up\b/i.test(t))return true
 return call.test(t)&&/(?:結束|结束|關閉|关闭|關掉|关掉|\bend\b|\bclose\b)/i.test(t)
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
 return (!language||/^zh(?:-|$)/i.test(language))?'通話控制：使用者直接對你說「結束通話」「掛斷電話」「結束我們現在的通話」「拜拜／掰掰」表示結束這通電話，不是關閉 Session；請呼叫 end_call，不再反覆確認。若你剛明確問要不要掛斷，接著的「對／好／是」就是該次確認。程式會在辨識到明確要求後直接掛斷並播放結束提示音。如果工具先接受了要求，就簡短道別。引用別人的道別、傳話或明確說不要掛斷都不算。':"Call control: direct requests such as 'end this call', 'hang up', 'end my conversation with you', 'bye' or 'goodbye' end this voice call, never a terminal Session. Call end_call without repeated confirmation. If you just explicitly asked whether to hang up, the person's next 'yes', 'correct' or 'okay' confirms that question. The application disconnects with an ending tone when it recognizes a direct request. If the tool accepted it first, say one short goodbye. Quoted goodbyes, messages for others and explicit requests not to hang up do not count."
}
