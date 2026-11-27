# The website's feature pictures

clawdline.com shows three real console scenes on a phone. They are the console as it is built,
photographed against a stand-in daemon that serves one small invented story, so a reader who has
never seen Clawdline can follow them:

1. An Agent rebuilding a shop's checkout asks whether to ship it now, with two suggested replies.
2. The person queues the next fix for that Session — the Pay button is cut off on iPhone — with a
   screenshot attached.
3. The Board item for the checkout shows the stage it is in and the Session doing it.

The blog's posts use five more, from two other invented stories in the same file:

4. `todo-screenshot` — a Session refactoring a trip planner's storage gets a bug the person reports
   from the phone: the add-to-do dialog with the words typed and a screenshot attached, the problem
   circled in red in the picture itself. A saved to-do shows its picture only as a filename, so
   the dialog is where the circle can be seen; its thumbnail is 44 points wide, which is why the
   invented screenshot is a tight square crop.
5. `session-message` — a message from the Session running the sync tests, as the conversation
   draws a `clawdline.message`: it found a connection problem, opened a Board item and asks this
   Session to take it.
6. `ask-person` — the Session waiting on its terminal's own menu: take that item now, after the
   current work, or not at all.
7. `squad-list` — the Session list with a coordinating Session in the architect role and three
   child Sessions nested under it by their dispatched tasks, each showing its built-in role.
8. `review-notice` — the coordinator's conversation when the code review child finishes: the
   completion notice, then the coordinator's own reply with the review's verdict. The notice
   carries no verdict of its own; the verdict is in the child's result, which the coordinator
   reads and reports.

The story lives in `STORIES` in `web/console/src/session/site-shots.ts`, written separately in
English and Taiwan Traditional Chinese, so neither language's pictures read as a translation. It
used to be the e2e fixtures' own sample content ("Bravo is still working", `/tmp/fixture`, a
"Manager Session"), translated string by string; that content exists to test edge cases, and in a
picture it read as noise. The ordinary e2e fixtures no longer know the website exists.

## Recapturing

```sh
tools/site-shots.sh <website checkout>/marketing/marketing-claude/public/features
```

The script builds the console through `tools/heavy.sh`, then runs
`web/console/src/session/site-shots.e2e.ts` once per language. Each run writes
`<scene>-en.png` or `<scene>-zh-Hant.png` for each of the eight scenes: a 390 × 844 phone at 2×,
so 780 × 1688 pixels. The roles in `squad-list` are the built-in catalog's names, read from
`internal/domain/persona/catalog`, with icons copied from `internal/domain/persona/icons.go`. Without
`SITE_SHOTS` those tests skip.

## The callouts

Each picture marks what to notice with a hairline outline at the real element's box and a small
number at its corner; the rest of the screen is lightly shaded. The picture carries no legend:
the website's gallery (`marketing/marketing-claude/src/components/ScreenshotGallery.astro` in the
website repository) names each number under the picture and in its lightbox, in the words of
`SITE_CALLOUTS`. A change to one is a change to both, in both languages.

## Before publishing

Look at all sixteen pictures. A fold caught mid-animation, a callout around nothing, a name cut short
in the header, or a label in the wrong language is a capture to fix here, not a picture to
publish.

## The GitHub README recording

The two `docs/assets/cloud-sessions-demo-*.gif` files record the real React console with
sample data supplied by a stand-in daemon. They keep the original multi-machine Session list
and detail components. A continuous capture runs throughout opening the waiting Session,
expanding its to-dos, and sending a request to make the New project button larger. The actual
Send button reaches the stand-in daemon and the console displays its sent receipt.

The attached image is a browser-rendered dashboard design from
`docs/assets/dashboard-preview.html`, in English or Taiwan Traditional Chinese via
`?lang=en` or `?lang=zh-Hant`. Its 720 × 240 shape keeps it short inside the console's normal
image tile. The recording never opens the full-screen image viewer.

Capture the console at 390 × 844 CSS pixels, device scale 2, approximately ten frames per
second. Keep capturing during the actions and retain each frame's elapsed time when encoding
the GIF. Neither README embeds the recording any more: both open with
`docs/assets/readme-overview-{en,zh-Hant}.png` (`docs/share-images.md`). Do not replace the console
with drawn screens, title cards or a slide sequence.
