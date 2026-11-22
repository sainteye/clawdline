export function callControlGuidance(language = '') {
  if (!language || /^zh(?:-|$)/i.test(language)) {
    return '通話控制：根據整段對話判斷使用者是否要結束這通語音電話。自然的道別或接續上一句的簡短確認都可以表達這個意思。意圖清楚時就結束通話並簡短道別，無須再確認。若是在談論掛斷功能、引用別人的道別、替人傳話或要求關閉終端 Session，請繼續處理原本的話題。'
  }
  return 'Call control: Use the whole conversation to decide whether the person wants to end this voice call. A natural goodbye or a short reply to your last question can convey that intent. When it is clear, end the call and say a short goodbye without asking again. When the person is discussing the hangup feature, quoting a farewell, relaying a message, or asking to close a terminal Session, continue with that topic.'
}
