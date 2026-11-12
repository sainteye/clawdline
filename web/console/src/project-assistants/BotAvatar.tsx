import type { ProjectAssistantAppearance } from "@clawdline/contract"
import { avatarPaths, palette } from "./avatar-paths"
import type { Words } from "./words"
export function BotAvatar({appearance, size = 44}: {appearance: ProjectAssistantAppearance; size?: number}) {
  const path = avatarPaths[appearance.shape] ?? avatarPaths.blob
  const color = palette.find(c=>c===appearance.color.toLowerCase()) ?? palette[0]
  return <svg className="pa-avatar" width={size} height={size} viewBox="-5 -5 110 110" aria-hidden="true" focusable="false">
    <path d={path} fill="none" stroke={color} strokeWidth="4.4" strokeLinejoin="round" />
    <path d="M36 40 V55 M63 40 V55" stroke={color} strokeWidth="5.5" strokeLinecap="round" />
  </svg>
}
export function AppearancePicker({value,onChange,w}: {value: ProjectAssistantAppearance; onChange:(value:ProjectAssistantAppearance)=>void; w:Words}) {
  return <fieldset className="pa-appearance"><legend>{w.appearance}</legend>
    <div className="pa-shapes">{Object.keys(avatarPaths).map(shape=><button type="button" key={shape} aria-label={w[`shape_${shape}` as keyof Words]} aria-pressed={value.shape===shape} onClick={()=>onChange({...value,shape:shape as ProjectAssistantAppearance["shape"]})}><BotAvatar appearance={{...value,shape:shape as ProjectAssistantAppearance["shape"]}} size={40}/></button>)}</div>
    <div className="pa-colors">{palette.map(color=><button type="button" key={color} aria-label={color} aria-pressed={value.color===color} style={{color}} onClick={()=>onChange({...value,color})}>●</button>)}</div>
  </fieldset>
}
