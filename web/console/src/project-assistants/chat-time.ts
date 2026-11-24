const separatorGapMs = 30 * 60 * 1000

export function chatTimeBreak(at:string|undefined, previous:string|undefined) {
  if(!at)return false
  const current=new Date(at),last=new Date(previous??"")
  return Number.isFinite(current.getTime())&&(
    !Number.isFinite(last.getTime())||Math.abs(current.getTime()-last.getTime())>=separatorGapMs||
    current.getDate()!==last.getDate()||current.getMonth()!==last.getMonth()||current.getFullYear()!==last.getFullYear()
  )
}

export function chatTimeFormat(language:string) {
  return new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"})
}
