# Source provenance and modifications

## Pinned work and licensing

Source: [isaacin42/herdr-layouts](https://github.com/isaacin42/herdr-layouts/tree/73bf616382d6b5c0ae5f73edf3cd122c4f51cda9/orrery), commit `73bf616382d6b5c0ae5f73edf3cd122c4f51cda9`.
The code/art/layout/theme are Apache-2.0; the exact license is [upstream/LICENSE](upstream/LICENSE).
IM Fell English fonts are Copyright (c) 2010, Igino Marini and SIL OFL 1.1;
retain [the complete font license](upstream/fonts/OFL-IMFellEnglish.txt).
Both full licenses and derivative-work notices are also embedded in distributed
HTML, so the two-entry binary package carries its notices. No upstream application,
installer, transcript scanner, exec handler or network service is run.

## Source-to-port map

| Original file / symbol | Browser representation |
| --- | --- |
| `orrery/mods/orrery/main.js` `C`, `SERIF`, `CAPS` | Exact palette, embedded font outlines, CJK system serif fallback |
| `hash`, `chart` (lines 97–112), `ORBIT` | Same UTF-16 FNV/mod10007 hash, equal whole-circle sectors, 0.14/0.72 inset, five radius shares |
| `Dome.render` | Same desktop center `(W/2,H/2+18)`, radius `max(120,min(W/2-40,H/2-70))`, glass radius `1.04R`, 110px sun |
| `planet`, `sectorLabels` | Original planet/halo images, 170px desktop labels, 0.965R sector names and 0.92R brass boundary pins |
| `cartouche`, `almanac`, `legend` | Engraved title rule, brass/walnut almanac and artwork legend |
| `Eyepiece`, `Plaque`, `layouts/orrery.json` | 1.1:1 dome/mount ratio, 18px mount padding, walnut 2px brass frame, 14px radius, plaque and inset pane |
| `themes/orrery.json` | Original ink/navy, parchment foreground, walnut chrome, brass edges and state colors |
| `art/*.svg` | Unchanged SVG bytes embedded as data URLs; gear retained for provenance but not exposed as unavailable Settings |
| `fonts/*.ttf` | Unchanged originals retained; Basic Latin WOFF derivatives embedded |

## Explicit divergences and reasons

1. SDK v1 supplies opaque Session/Project refs, titles, eight work states and
   freshness. Herdr supplies agents/spaces/tabs/panes/machine metadata. Opaque
   Project refs replace spaces; sorted refs provide the stable order because no
   source space order is supplied. A null Project gets an explicit unassigned
   sector. Opaque Session refs replace addresses for hashing. Positions are
   stable only within the current opening.
2. `waiting_you` uses blocked geometry; both reported-completion states use done;
   working uses the two working radii. Ready, holding, waiting-session and unknown
   use the outer idle geometry. All eight retain their original distinct readable
   host state labels. Completion is a report, never an acceptance claim.
3. Upstream `planet()` grows working diameter from 20 to 34 based on
   `tab.working_since`. That input is unavailable: this port uses the honest
   zero-elapsed 20px baseline, with no timer or invented growth. Blocked/done remain
   30px and idle 18px; 44px transparent hit areas improve touch access.
4. Upstream `sectorLabels()` includes machine names, `cartouche()` includes local
   calendar date, and almanac chooses recent `seq`. This port has no machine or
   activity timestamps and does not fabricate them. Its subtitle gives supplied
   Session count; next-to-attend picks the first waiting-you then reported-done
   row in stable state/ref order. Extra Project names appear only in list mode.
5. A native eyepiece contains live terminals, split/zoom/close and Settings. The
   projection cannot supply terminals or those operations. The equivalent mount
   explicitly says it contains Session summaries, with a detail plaque, complete
   register and explicit Open Session. Selection never navigates automatically;
   stale/unknown/disconnected states cannot navigate. Native terminal content in
   the original preview remains visible only in the labeled comparison image.
6. Browser DOM/SVG replaces GPUI. Source 900ms left/top transitions apply only to
   actual state changes and stop on hidden/reduced/still/stale/disconnected data.
   No permanent rotation or history is introduced. Names preserve full original
   Unicode/case instead of upstream `.toLowerCase()`; list titles wrap safely.
7. The original has desktop minimum widths of 520+420 and no mobile layout.
   At 390px, this port stacks dial, mount/register and almanac; the sun is 64px,
   diagram labels give way to the register, and all controls retain 44px targets.
   The original desktop source geometry is unchanged. Crowded points/labels can
   overlap, as in the source; the full list remains authoritative.
8. Three complete TTF files would exceed the existing 262144-byte STORE ZIP
   bound. Basic Latin WOFF subsets preserve retained glyph outlines, original
   metrics/hinting and relevant kerning, with composite closure. Non-Basic-Latin
   characters use system serif. GSUB is omitted to prevent substitutions to
   removed ligatures; separate letters render correctly. This reduces typographic
   coverage, not title data. The original fonts are kept byte-for-byte for review.
9. Package title is Session Orrery rather than The Herdr Orrery. Controls use
   English/Taiwan Traditional Chinese and replace unavailable native controls.
   The dedicated 128px package cover uses original sun/planet images and a simple
   rim rather than the full highly detailed dial to fit the manifest limit.
10. The host's trusted return footer consumes viewport space outside the package;
    it is not hidden/repositioned for the comparison. The comparison captures the
    package iframe at the original 1600×900 content size, with the trusted footer
    still present outside that capture. Separate 1600×900 outer-window captures
    retain that footer visibly. Session counts and live terminal data differ.

## Unchanged source inventory

`main.js`, `art/`, `fonts/` map directly from `orrery/mods/orrery/`;
`layout.json` maps from `orrery/layouts/orrery.json`; `theme.json` from
`orrery/themes/orrery.json`; `LICENSE` from repository root.

| Local upstream path | SHA-256 |
| --- | --- |
| `LICENSE` | `c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4` |
| `art/dial.svg` | `91f680c90cce909e45352b3a66cd334a6046b42528ead1cab59abc9b2d9e8910` |
| `art/gear.svg` | `9eccefcd5a47557d2728de447afc6138a19d5f808fb8e1ab246558e7b1c821f2` |
| `art/halo-blocked.svg` | `9ed944b2199cdd04c6c4ed5a580872a13d0900fe845036731b75949767d90eb5` |
| `art/halo-focus.svg` | `9677f4b1f7c51cfedeb0a3f82af6700f6a0bcd1331d47b44c78fc332cd5c2499` |
| `art/planet-blocked.svg` | `4f2e879005f519bfa52adb2bad5c3f09308d0ce701d89d6e55e23cc8794ce724` |
| `art/planet-done.svg` | `415544b5c962f2bb30fcda109181042e483bb36bc65e34c8f26044898c9faf41` |
| `art/planet-idle.svg` | `b4febd570a6be52b4bb31ab7dbd79a0c08b0d2bdbcf52c6a55dd8ff6d0220ce6` |
| `art/planet-working.svg` | `028f8b7de28c48ff709bc4a766c7a6b5b6fcfb32c492915de78bb4276ad50d57` |
| `art/sun.svg` | `a08ca8a73cbb5eda2c995c7591219611f7e892a56ab22ce5d4dd9833a862373d` |
| `fonts/IMFeENit28P.ttf` | `47cd75dce54b1f2e0831359d22d5e688f519d68ae45706b664fd310fd0e3ccf7` |
| `fonts/IMFeENrm28P.ttf` | `fe9705bbde51af802719246d4608d08d37bde956ab99d9a590da996a5221a24c` |
| `fonts/IMFeENsc28P.ttf` | `102324fb5434bb5da7963533426b0ad44c85bbc9e7755067535c9d11464a176b` |
| `fonts/OFL-IMFellEnglish.txt` | `2a3ca501fc4d5efcad9798531e3e06962b1e20c60e464f6cbd6c17630112c773` |
| `layout.json` | `4f5be39a6be26061bb96a99c2dc018064b1fb9c194365fe70afb898906287da1` |
| `main.js` | `e10910466346ccc298ce327758fc98251c431775f7868dce5a117b609bd6a1f2` |
| `theme.json` | `071861f28f7b1f6246a15f0a51335890ab50938df698d8f4e0188b3a25d9f727` |

## Reproducible embedded font derivation

Run the following Python standard-library program from the repository root.
It emits deterministic WOFF base64 alongside the script; the three `@font-face`
URLs in index.html contain these outputs, in regular/italic/small-caps order.
No external font tool or dependency is required.

```python
"""Dependency-free WOFF subsets: Basic Latin cmap, original outlines and composite closure.
Keeps original glyph IDs, metrics and hinting; other scripts use CSS serif fallback.
"""
from pathlib import Path
import struct, zlib, base64

def subset(path):
 data=path.read_bytes(); u16=lambda d,o:struct.unpack_from('>H',d,o)[0]
 tables={}
 for i in range(u16(data,4)):
  tag,checksum,off,size=struct.unpack_from('>4sIII',data,12+i*16); tables[tag]=data[off:off+size]
 cmap=tables[b'cmap']; mappings={}
 for i in range(u16(cmap,2)):
  platform,encoding,off=struct.unpack_from('>HHI',cmap,4+i*8)
  if u16(cmap,off)!=4: continue
  segs=u16(cmap,off+6)//2; end=off+14; start=end+2*segs+2; delta=start+2*segs; ranges=delta+2*segs
  for c in range(128):
   for j in range(segs):
    if u16(cmap,start+j*2)<=c<=u16(cmap,end+j*2):
     r=u16(cmap,ranges+j*2); d=u16(cmap,delta+j*2)
     g=u16(cmap,ranges+j*2+r+2*(c-u16(cmap,start+j*2))) if r else c
     g=(g+d)&65535 if g else 0
     if g: mappings[c]=g
     break
 fmt=u16(tables[b'head'],50); ng=u16(tables[b'maxp'],4)
 loc=list(struct.unpack('>'+('I' if fmt else 'H')*(ng+1),tables[b'loca']))
 if not fmt: loc=[x*2 for x in loc]
 glyf=tables[b'glyf']; keep=set(mappings.values())|{0}; pending=list(keep)
 while pending:
  g=pending.pop(); chunk=glyf[loc[g]:loc[g+1]]
  if len(chunk)<10 or struct.unpack_from('>h',chunk)[0]>=0: continue
  at=10
  while True:
   flags,child=struct.unpack_from('>HH',chunk,at); at+=4
   if child not in keep: keep.add(child); pending.append(child)
   at+=4 if flags&1 else 2
   at+=2 if flags&8 else 4 if flags&64 else 8 if flags&128 else 0
   if not flags&32: break
 out=bytearray(); offsets=[]
 for g in range(ng):
  offsets.append(len(out))
  if g in keep: out+=glyf[loc[g]:loc[g+1]]
  while len(out)%4: out.append(0)
 offsets.append(len(out)); tables[b'glyf']=bytes(out)
 head=bytearray(tables[b'head']); struct.pack_into('>h',head,50,1); head[8:12]=b'\0'*4; tables[b'head']=bytes(head)
 tables[b'loca']=struct.pack('>'+'I'*len(offsets),*offsets)
 # One unambiguous format-4 Unicode table; no character outside Basic Latin maps.
 chars=sorted(mappings)+[65535]; n=len(chars); power=2**(n.bit_length()-1)
 body=struct.pack('>7H',4,16+8*n,0,2*n,2*power,power.bit_length()-1,2*n-2*power)
 body+=struct.pack('>'+'H'*n,*chars)+b'\0\0'+struct.pack('>'+'H'*n,*chars)
 body+=struct.pack('>'+'H'*n,*[((mappings.get(c,0)-c)&65535) for c in chars])+b'\0\0'*n
 tables[b'cmap']=struct.pack('>HHHHI',0,1,3,1,12)+body
 # Retain exactly the original format-0 kerning pairs between retained glyphs.
 if b'kern' in tables:
  kern=tables[b'kern']; subtables=[]; at=4
  for _ in range(u16(kern,2)):
   version,length,coverage=struct.unpack_from('>HHH',kern,at)
   if coverage>>8==0:
    pairs=[kern[p:p+6] for p in range(at+14,at+length,6) if u16(kern,p) in keep and u16(kern,p+2) in keep]
    n=len(pairs); power=2**(n.bit_length()-1) if n else 0
    subtables.append(struct.pack('>7H',version,14+6*n,coverage,n,6*power,max(0,power.bit_length()-1),6*n-6*power)+b''.join(pairs))
   at+=length
  tables[b'kern']=struct.pack('>HH',0,len(subtables))+b''.join(subtables)
 # Unused glyph names and DSIG signatures cannot describe this derivative.
 tables.pop(b'DSIG',None)
 tables.pop(b'GSUB',None)
 if b'post' in tables: tables[b'post']=b'\0\3\0\0'+tables[b'post'][4:32]
 tags=sorted(tables); offset=44+20*len(tags); entries=[]; payload=bytearray()
 sfnt_size=12+16*len(tags)
 for tag in tags:
  raw=tables[tag]; padded=raw+b'\0'*((-len(raw))%4)
  checksum=sum(struct.unpack('>'+'I'*(len(padded)//4),padded))&0xffffffff
  compressed=zlib.compress(raw,9); encoded=compressed if len(compressed)<len(raw) else raw
  entries.append(struct.pack('>4sIIII',tag,offset+len(payload),len(encoded),len(raw),checksum));payload+=encoded
  payload+=b'\0'*((-len(payload))%4);sfnt_size+=len(padded)
 header=struct.pack('>4sIIHHIHHIIIII',b'wOFF',0x10000,offset+len(payload),len(tags),0,sfnt_size,1,0,0,0,0,0,0)
 return header+b''.join(entries)+payload

if __name__=='__main__':
 for p in Path('plugins/session-orrery/upstream/fonts').glob('*.ttf'):
  result=subset(p); print(p.name,len(result)); (Path(__file__).parent/(p.stem+'.woff.b64')).write_text(base64.b64encode(result).decode())
```
