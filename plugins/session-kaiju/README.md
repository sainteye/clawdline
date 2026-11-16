# Kaiju City for Clawdline

A browser port of Jiqize's original isometric city. Original plates, eight
building slots and damage layers, all eight monsters and five poses, day/night
assets, depth ordering, dust, sirens and status-dependent motion are retained.
Projects beyond eight appear in further districts rather than disappearing.
Idle and unknown states stay in the register, following the original city display.
Each building uses its original two to four stand points. Additional active
monsters appear in explicit batches; selecting any registered Session brings its
batch into view. Building hits and the Project strip expose the complete Project
roster. All Sessions remain selectable in the
complete searchable register. Selection does not navigate; Open Session does.

Building wear uses optional trusted working-turn timestamps only. Missing times
keep an intact building; blocked work retains its observed level. Reported
completion uses the original ruin metaphor but does not mark work accepted or
claim project damage. Native timers, statistics and terminal commands are not
available through the Session summary projection and are not fabricated.

The shared builder embeds lossless original raster artwork, full MIT notice,
SDK and browser renderer into a two-file STORE package:

```sh
python3 plugins/build-herdr-views.py
```

The package needs the host's registered 32 MiB archive/expanded profile; it
exceeds the previous 256 KiB archive limit. All other runtime permissions and
bounds remain enforced. Stale/unknown data cannot navigate. Still/list mode,
reduced motion, hidden host and disconnected state stop movement. There are no
external resources or native commands. Installation is explicit and browser-local.

See [PROVENANCE.md](PROVENANCE.md).

On narrow screens, selecting a monster or roster entry opens an accessible detail
sheet with the full title, reading status and explicit Open Session action. The
scene fits its viewport independently of roster height. Selection never opens a
Session, sends a message or accepts reported completion by itself.
