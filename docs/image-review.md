# Optional space-saving image review

Settings → Sessions and access → Integrations offers **Space-saving image review**.
It is a machine-wide opt-in and is off by default. There is no Project override.
The setting changes only instructions prepared for subsequent new Sessions and
children. Existing Sessions and resumes keep their original context. Children
sample this setting at their own launch while retaining their parent's workflow.

The guidance prefers thumbnails for layout, broken images and colors, and labeled
multi-image overviews when practical. Small text, alignment, edges and suspicious
areas warrant original-size crops. An explicit request to examine an image
carefully warrants reading the original directly. Full-size viewing remains
available. Long tool output goes to files with a summary and path in the
conversation. This neither changes provider permissions nor edits stored
conversations. Thumbnails can hide details, and guidance alone cannot guarantee
smaller transcripts or agent compliance.

The optional snapshot field is omitted when false so historical snapshot bytes
and digests remain valid. Tests pin a pre-feature snapshot and child briefing,
compare disabled instruction bytes across all three workflows, exercise new
root/child toggles and unchanged resumes, and check the Console in English and
Taiwan Traditional Chinese at desktop and phone widths. Older daemons that omit
the setting show an unavailable switch rather than an assumed writable value.
See the [instruction inventory](transparency.md) for the injection boundaries.

## Small local sample, 2026-11-07

Two fresh Codex CLI Sessions authenticated through ChatGPT reviewed the same
four synthetic Console screenshots: two desktop images at 1280×800 and two phone
images at 390×844, showing the setting off and on. They used the same task and
inherited model configuration. The experimental difference was the exact optional
launch instruction text; the measurement runner supplied it through the same
Codex developer-instruction mechanism. This was not a production daemon launch.
Each Session had its own input directory. The prompt asked it to identify the
changed setting, inspect overflow, broken images and colors, and flag uncertain
small text. No tools were installed and no stored conversation was rewritten.

| Measurement | Guidance off | Guidance on |
|---|---:|---:|
| Independent tasks | 1 | 1 |
| Successful image deliveries to the model | 5 | 6 |
| Image-view attempts, including failures | 5 | 7 |
| Completed native transcript, bytes | 674,619 | 681,422 |
| Serialized image-block bytes | 467,288 | 483,056 |

The enabled sample was **6,803 bytes larger (about 1.0%)**, not smaller. It tried
to build a labeled overview with Pillow, which was unavailable, then fell back
to the four originals and two detail crops using the existing system tools.
One attempted view of the absent overview failed. The control also encountered
missing Pillow when preparing detail crops, then used a system crop tool and
viewed one crop. Both identified the same setting change, the phone navigation
clipping and unreadable clipped text at the desktop's bottom edge. This does not
prove equal review quality on other tasks.

The counts come from successful image blocks in each completed transcript,
checked against its actual tool calls; failed views are counted separately.
File sizes are UTF-8 file byte counts after task completion. Session metadata,
text and tool output also contribute to transcript size, so total file size is
not an image-only metric. The sample has one task per condition, no repeated
trials, and no successful overview; it establishes no causal or general savings
claim. It demonstrates why the opt-in is guidance with a visible trade-off.
The private delivery evidence retains the common prompt, source-image hashes,
CLI events, transcript identities, metrics and on/off Console screenshots.
