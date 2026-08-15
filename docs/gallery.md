# Mascot gallery

Every pack here installs into `~/.config/clawdline/mascots/` and shows up in the picker
(<kbd>⌘</kbd><kbd>M</kbd>). Two ways in: the ones shipped with the app, and the ones people
post.

## Shipped with the app

| | name | grid | notes |
|---|---|---|---|
| <img src="assets/dance.gif" width="220"> | **clawd** | 16×11 | The default. Single accent colour, so it follows the app tint. |
| <img src="assets/mochi-dance.gif" width="220"> | **mochi** | 18×15 | A four-colour pack with an outline and a `skin` character — the second shape in the format, so it is more than a theory. |

## From the community

Nothing here yet. Yours can be first.

<!--
Adding to this table:

| <img src="assets/your-pack.gif" width="220"> | **name** | 18×15 | one line, and who made it |
-->

---

## Installing a pack somebody posted

A pack is one JSON file with no assets, so installing is a download:

```bash
curl -L -o ~/.config/clawdline/mascots/whatever.json <url>
```

Then <kbd>⌥</kbd><kbd>Space</kbd> → <kbd>⌘</kbd><kbd>M</kbd> and pick it. Arrow keys preview
as you move, so you choose by looking rather than by reading names.

There is deliberately **no one-click install URL scheme.** A single `curl` you can read is
better than a handler that writes files into your config directory on a click from a web page.

Before trusting one, it is worth a look:

```bash
python3 tools/validate-pack.py ~/.config/clawdline/mascots/whatever.json
```

A pack is pure data — a grid of characters, colours and numbers. It cannot execute anything.
The worst a bad one can do is fail to load and say why.

## Submitting one

1. Build it: [docs/mascots.md](mascots.md) has the format and the prompt to hand Claude Code.
2. Check it: `python3 tools/validate-pack.py Resources/mascots/<name>.json`
   (CI runs the same check on every pull request).
3. Record a preview:

   ```bash
   open "clawdline://filmstrip?dir=/tmp/p&script=dance&fps=24&seconds=2"
   ffmpeg -framerate 24 -i /tmp/p/f%04d.png -c:v libx264 -pix_fmt yuv420p -vf scale=900:-2 /tmp/p.mp4
   ffmpeg -i /tmp/p.mp4 -vf "fps=24,scale=560:-2:flags=lanczos,palettegen" /tmp/pal.png
   ffmpeg -i /tmp/p.mp4 -i /tmp/pal.png -filter_complex "fps=24,scale=560:-2:flags=lanczos[x];[x][1:v]paletteuse" -loop 0 docs/assets/<name>.gif
   ```

4. Open a PR adding the JSON, the GIF and a row in the table above.

**If the character is someone else's**, keep the pack in your own config directory and link a
gist from the community table instead. A link is welcome; shipping the artwork in this repo is
not something the project can take on.
