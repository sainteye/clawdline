#!/usr/bin/env python3
"""Render the English and Traditional Chinese cross-machine feature diagrams."""
from pathlib import Path
from html import escape
import json
root=Path(__file__).resolve().parents[1]
def replace_colors(text, palette):
 for old,new in palette.items(): text=text.replace(old,new)
 return text
for lang in ['en','zh-Hant']:
 zh=lang!='en'
 labels=(['跨機器 Session 協作','讓 Mac 與 Linux 上的 Agent 一起完成工作','Mac · 來源機器','Codex Session','規劃任務 · 保留交付責任','專案與 Session 探索','Clawdline Cloud','端對端加密','只轉送簽章密文','Linux · 配對機器','Claude Code Session','在目標專案執行工作','本機專案 · 執行環境 · 金鑰','1  探索','取得遠端 Project ID、Session 狀態與能力','2  派工與對話','開啟遠端 Session，或傳訊給既有 Session','3  回傳並接續','結果通知回到來源；Agent 讀取後繼續工作','雙向配對與授權 · 兩台機器須在線 · 可撤銷連線','程式碼、執行與內容金鑰留在各自機器上'] if zh else ['Sessions that work across machines','Let Agents on your Mac and Linux work together','Mac · source machine','Codex Session','Plans work · owns the delivery','Project + Session discovery','Clawdline Cloud','End-to-end encrypted','Routes signed ciphertext only','Linux · paired machine','Claude Code Session','Runs work in its own Project','Local Projects · runtime · keys','1  Discover','Find remote Project IDs, Session status and capabilities','2  Dispatch + talk','Start a remote Session or message an existing one','3  Return + continue','Results wake the source; its Agent reads and resumes','Reciprocal pairing + permission · Both machines online · Revocable','Code, execution and content keys stay on each machine'])
 def t(x,y,i,size=20,fill='#e9edf5',weight=400,anchor='start'):
  return f'<text x="{x}" y="{y}" font-size="{size}" fill="{fill}" font-weight="{weight}" text-anchor="{anchor}">{escape(labels[i])}</text>'
 s=['<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="790" viewBox="0 0 1200 790" role="img" aria-labelledby="title desc">',f'<title id="title">{escape(labels[0])}</title>',f'<desc id="desc">{escape(". ".join(labels[2:]))}</desc>', '<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto-start-reverse"><path d="M0 0L8 4L0 8" fill="none" stroke="#91c9ff" stroke-width="1.4"/></marker></defs>', '<rect width="1200" height="790" rx="11" fill="#10151e"/>','<g font-family="system-ui, -apple-system, sans-serif">',t(48,64,0,34,weight=700),t(48,104,1,21,'#abb9cc')]
 for x,side in [(48,False),(812,True)]:
  s += [f'<rect x="{x}" y="156" width="340" height="257" rx="11" fill="#1b2534" stroke="#40516a"/>',t(x+62,196,9 if side else 2,23,weight=600),f'<rect x="{x+20}" y="220" width="300" height="112" rx="10" fill="#101a29" stroke="#596f8f"/>',t(x+76,257,10 if side else 3,23,weight=600),t(x+72,292,11 if side else 4,16,'#b2c2d9'),t(x+24,371,12 if side else 5,17,'#b2c2d9')]
 s += ['<rect x="440" y="197" width="320" height="171" rx="11" fill="#172f3c" stroke="#70b7c7"/>', t(625,243,6,25,weight=650,anchor='middle'),t(600,286,7,21,'#98e3cc',anchor='middle'),t(600,323,8,17,'#b2c2d9',anchor='middle')]
 for a,b in [(394,432),(768,806)]:
  s += [f'<path d="M{a} 269H{b}" stroke="#91c9ff" stroke-width="2.5" marker-start="url(#arrow)" marker-end="url(#arrow)"/>']
 s += ['<path d="M64 450H1136" stroke="#33465f"/>']
 for y,i in [(495,13),(570,15),(645,17)]:
  s += [t(60,y,i,23,'#98e3cc',600),t(320,y,i+1,21)]
 s += [t(60,718,19,19,'#bac8dc'),t(60,753,20,18,'#91a2ba'),'</g></svg>']
 palette={'#10151e':'#16161a','#e9edf5':'#e8e6e3','#abb9cc':'#9a978f','#1b2534':'#1c1c22','#40516a':'#24242b','#101a29':'#131317','#596f8f':'#24242b','#b2c2d9':'#9a978f','#172f3c':'#1c1c22','#70b7c7':'#24242b','#98e3cc':'#9da4ed','#91c9ff':'#9da4ed','#33465f':'#24242b','#bac8dc':'#9a978f','#91a2ba':'#9a978f'}
 s=[replace_colors(item,palette) for item in s]
 icons=json.loads((root/'docs/assets/cross-machine-icons.json').read_text())
 extra=[]
 for row,line in enumerate(['.######.','.#o##o#.','########','.######.']):
  for col,ch in enumerate(line):
   if ch in '#o': extra.append(f'<rect x="{461+col*5}" y="{225+row*5}" width="5" height="5" fill="{"#d97757" if ch=="#" else "#141416"}"/>')
 for x,machine,assistant in [(48,'apple','codex'),(812,'linux','claude')]:
  for name,ix,iy,size in [(machine,x+24,173,26),(assistant,x+38,237,26)]:
   icon=icons[name]
   extra.append(f'<svg x="{ix}" y="{iy}" width="{size}" height="{size}" viewBox="0 0 24 24" aria-hidden="true"><path fill="{icon["colour"]}" fill-rule="evenodd" d="{icon["path"]}"/></svg>')
  for row,line in enumerate(['#.#.#','#####','#.#.#','##.##']):
   for col,ch in enumerate(line):
    if ch=='#': extra.append(f'<rect x="{x+38+col*4}" y="{280+row*4}" width="4" height="4" fill="#9da4ed"/>')
 s.insert(-1,''.join(extra))
 s.insert(1,'<!-- Assistant logos: Lobe Icons (MIT). Platform logos: Simple Icons (CC0). See cross-machine-sessions.md. -->')
 content='\n'.join(s)+'\n'
 name=f'cross-machine-sessions-{lang}.svg'
 (root/'docs/assets'/name).write_text(content)
