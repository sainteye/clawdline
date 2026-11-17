/** Adapted from Codync 9f67eb09 desktop/thread/mentions.ts (Apache-2.0).
 * Copyright 2026 Po Kai Lee and the Codync contributors.
 * Routing remains source-owned; these helpers only edit the draft. */
export function groupMentionQuery(draft:string,caret:number,names:readonly string[]):string|null {
  if(caret!==draft.length)return null
  const at=draft.lastIndexOf("@")
  if(at<0||(at>0&&!/\s/u.test(draft[at-1])))return null
  const query=draft.slice(at+1).toLowerCase()
  if(/^\s|\n/u.test(query)||/^(all|everyone)\s*$/u.test(query)||names.some(name=>query===name.toLowerCase()+" "))return null
  return query
}

export function groupMentionOptions<T extends {name:string}>(draft:string,caret:number,members:readonly T[]):T[] {
  const query=groupMentionQuery(draft,caret,members.map(member=>member.name))
  return query===null?[]:members.filter(member=>member.name.toLowerCase().includes(query))
}

export function withGroupMention(draft:string,name:string):string {
  return draft.slice(0,draft.lastIndexOf("@"))+"@"+name+" "
}
