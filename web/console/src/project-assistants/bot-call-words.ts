import { catalogWord, currentCatalogTag, resolveTag } from "../catalog"

const en = {
  start: "Voice call",
  mute: "Mute microphone",
  end: "Hang up",
  interrupt: "Stop playback",
  muted: "Microphone muted",
  opening: "Connecting…",
  listening: "Listening",
  sending: "Sending…",
  waiting: "Waiting for reply",
  permission: "Waiting for approval in chat",
  speaking: "Speaking",
  unsupported: "This browser needs on-device speech recognition for Bot calls.",
  language: "Install the browser’s on-device speech pack for this language to start a call.",
  microphone: "Microphone unavailable. Finish any recording and check microphone access.",
  audio: "Call audio stopped. Check the installed voice and audio device.",
  disconnected: "Call ended after the connection changed.",
  sendFailed: "The Bot could not complete the spoken request.",
  sendUnknown: "The spoken request’s outcome is unknown. Check the chat before sending again.",
}

const zhHant: Record<keyof typeof en, string> = {
  start: "語音通話",
  mute: "麥克風靜音",
  end: "掛斷",
  interrupt: "停止播放",
  muted: "麥克風已靜音",
  opening: "連線中…",
  listening: "正在聆聽",
  sending: "正在傳送…",
  waiting: "等待回覆",
  permission: "等待在聊天中核准",
  speaking: "正在播放回覆",
  unsupported: "Bot 通話需要支援裝置端語音辨識的瀏覽器。",
  language: "請在瀏覽器安裝此語言的裝置端語音套件，再開始通話。",
  microphone: "無法使用麥克風。請先結束錄音，並檢查麥克風權限。",
  audio: "通話音訊已中斷。請檢查已安裝的語音和音訊裝置。",
  disconnected: "連線變更，通話已結束。",
  sendFailed: "Bot 無法完成語音請求。",
  sendUnknown: "語音請求的結果不明。請先查看聊天，再決定是否重送。",
}

export function botCallWords(language: string): Record<keyof typeof en, string> {
  const tag = resolveTag(language)
  const base = tag === "zh-Hant" ? zhHant : en
  if (tag !== currentCatalogTag()) return base
  return Object.fromEntries(Object.entries(base).map(([key, fallback]) => {
    const value = catalogWord("botCall", key)
    return [key, value === `botCall.${key}` ? fallback : value]
  })) as Record<keyof typeof en, string>
}
