# Kaiju City for Clawdline

A browser port of Jiqize's original isometric city. Version 0.3.0 retains original plates, eight building slots and monsters, damage layers, day/night artwork and depth. It restores dossier/avatar rosters, the original pixel font, monster entry and retreat, new-building rise, rooftop focus arrow, impact effects and background news. See the [source comparison](../../docs/plugins/kaiju-source-alignment.md) for individual behaviors and host differences.

Projects beyond eight use districts. Idle Sessions stay in the roster; original standing points bound each cast, with explicit batches for additional active monsters. Every Session remains reachable by its full Unicode name. Selection and nearby touch controls keep the city interactive on phones. **Open Session** performs navigation; optional details remain available without an automatic bottom sheet.

Building wear requires trusted working-turn timestamps. Missing times keep the building intact; blocked work retains its observed level. Reported completion uses ruins as a metaphor and does not accept work or claim project damage. Provided state times support elapsed-time fields. Native program names and persistent daily statistics are unavailable and are not fabricated. Confirmed callback waits retain their own caption.

The shared builder embeds lossless original artwork, the unmodified Fusion Pixel font, MIT/OFL notices, SDK and browser renderer in a two-file STORE package:

```sh
python3 plugins/build-herdr-views.py
python3 tools/build-view-plugin-catalog.py
```

The package uses the existing 32 MiB archive/expanded profile and unchanged host API v1/read/open permissions. Official installed cards can update it in place. Still/list mode, reduced motion, hidden hosts and disconnection stop movement. Stale or unknown unclassified readings cannot navigate. There are no external resources or native commands. Packages remain browser-local; roster mode lasts for this opening.

See [PROVENANCE.md](PROVENANCE.md) for original assets, source and licenses.
