# Lantern Deep for Clawdline

An original hand-drawn underground scene. Every Project is a chamber with its own
painted backdrop, floating slabs on three levels and ground set pieces. Every Session
is one of twelve small wanderers, each with fifty frames of frame-by-frame animation:
an eight-frame run, four- and six-frame attacks, upward and downward strikes, dash, jump,
fall, wall cling, landing, idle, look, wave, yawn, puzzled, think, shrug, guard, victory,
sitting, warming hands, drinking, eating, polishing the weapon, gazing up, stretching,
dozing and sleeping. A resting wanderer's weapon stands planted behind it. They carry different weapons:
short blades, a longsword, a spear, a war hammer, twin daggers, an axe, a greatsword, a
scythe and a bow. The archer keeps its distance and its arrow strikes when it arrives. A small wisp
trailing each wanderer carries the state colour, and its behaviour shows the state:

| State | What the wanderer does |
|---|---|
| `working` | Fights one of six enemies in its own lane, on the floor or a slab: slashes, back-dashes, leaps over a lunge and strikes downward |
| `holding` | Drills against a training post: combinations and dashes |
| `ready` | Patrols the floor or a floating slab, with an occasional practice swing; it never hops in place |
| `waiting_session` | Climbs a shaft by kicking between two walls, rests on the ledge, then drops |
| `waiting_you` | Stands puzzled under a warm beacon with a floating question mark |
| `milestone_complete` | Rests by the campfire: sits, warms its hands, eats, drinks, polishes its weapon, gazes up, stretches, dozes |
| `work_complete` | Sleeps on a bedroll by the campfire |
| `unknown` | Stands still and grey |
| any stale reading | Keeps its place, turns grey, stops acting and lands if it was in the air; it cannot be opened |

A struck enemy recoils in its own hurt frame with a flash, impact lines and a brief
hit-stop, and breaks apart over three frames when defeated. A fight keeps two enemies in its lane, the second stepping in when the first falls.
Wild creatures also drift through the air, hang from the ceiling and cross the floor;
they belong to no Session. A wanderer leaves the ground only to strike an enemy, to
climb its shaft or to reach its slab. A fight draws the nearest one in as its next enemy before
conjuring a new one, and a patroller swats a floor creature that walks past. Each wanderer also carries its Session's load, when the machine states it:

- a **life bar** above its head is the context window still free, draining green to amber
  to red as the conversation fills (the host's `contextUsed`, renewed at most every 15 s);
- a **spirit beast** trails behind it for each shell or daemon callback (`shells`);
- a **spirit flame** circles it for each running provider subagent (`agents`).

Beyond three beasts or four flames a small `+N` counts the rest. Missing values draw
nothing rather than an empty bar. The selection card and readable list state the same
numbers in words.

Enemies, strikes and climbing are decoration. They do not measure progress, count
anything or imply that work was accepted. Stale and disconnected readings never animate.

Drag, scroll or use the arrow keys to wander. Selecting a wanderer shows its full
title, Project, state and freshness with one explicit **Open Session** button. The
**needs you** button steps through every Session waiting for the person. The readable
list holds every Session with its complete Unicode title. Embedded beside the Session
list, the view drops its title, list and close control and keeps the scene, the
needs-you button and the selection card. Reduced motion draws one still frame.

## Build

Fireflies wander and blink through every chamber, and each camp has a framed fire with
rising sparks; a camp on the floor also has a tent, a cooking pot and firewood.

`scene.js` and `src.html` are the sources; `art/` holds the backdrops, scenery sprites
and the character (`fNN-*`) and enemy (`gN-*`) frames.

```sh
python3 plugins/session-lantern-deep/build.py
python3 tools/pack-view-plugin.py plugins/session-lantern-deep web/console/public/view-plugins/session-lantern-deep.zip
python3 tools/build-view-plugin-catalog.py
```

The package stays in the two-file STORE profile and uses only `sessions.read` and
`sessions.open` from the v1 SDK.
