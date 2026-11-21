# Lantern Deep for Clawdline

An original hand-drawn underground scene. Every Project is a chamber with its own
painted backdrop, floating slabs on three levels and ground set pieces. Every Session
is one of twelve small wanderers, each with sixteen frames of frame-by-frame animation
(run, slash, jump, fall, wall cling, landing, idle, puzzled, sitting). A small wisp
trailing each wanderer carries the state colour, and its behaviour shows the state:

| State | What the wanderer does |
|---|---|
| `working` | Fights one of six enemies in its own lane, on the floor or a slab: slashes, back-dashes, leaps over a lunge and strikes downward |
| `holding` | Drills against a training post: combinations, dashes and a leaping strike |
| `ready` | Patrols the floor or a floating slab, with an occasional practice swing |
| `waiting_session` | Climbs a shaft by kicking between two walls, rests on the ledge, then drops |
| `waiting_you` | Stands puzzled under a warm beacon with a floating question mark |
| `milestone_complete`, `work_complete` | Sits by a small fire with the blade planted |
| `unknown`, or any stale reading | Stands still and grey, and cannot be opened |

A struck enemy recoils in its own hurt frame with a flash, impact lines and a brief
hit-stop, and breaks apart over three frames when defeated. Enemies, strikes and climbing are decoration. They do not measure progress, count
anything or imply that work was accepted. Stale and disconnected readings never animate.

Drag, scroll or use the arrow keys to wander. Selecting a wanderer shows its full
title, Project, state and freshness with one explicit **Open Session** button. The
**needs you** button steps through every Session waiting for the person. The readable
list holds every Session with its complete Unicode title. Embedded beside the Session
list, the view drops its title, list and close control and keeps the scene, the
needs-you button and the selection card. Reduced motion draws one still frame.

## Build

`scene.js` and `src.html` are the sources; `art/` holds the backdrops, scenery sprites
and the character (`fNN-*`) and enemy (`gN-*`) frames.

```sh
python3 plugins/session-lantern-deep/build.py
python3 tools/pack-view-plugin.py plugins/session-lantern-deep web/console/public/view-plugins/session-lantern-deep.zip
python3 tools/build-view-plugin-catalog.py
```

The package stays in the two-file STORE profile and uses only `sessions.read` and
`sessions.open` from the v1 SDK.
