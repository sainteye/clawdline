# Provenance

- Original project: [kevinpardo/world-of-codecraft](https://github.com/kevinpardo/world-of-codecraft).
- Pinned commit: `6c5b65b75eafb78dd906cdccfebabdd262ac9f28`.
- Original code and art: MIT; full notice is in `upstream/LICENSE` and embedded in
  the published HTML. The original license explicitly covers its art.
- Cinzel Decorative: SIL OFL 1.1; `upstream/OFL-Cinzel.txt` is also embedded.
  License wording is preserved; trailing whitespace is normalized.
- Source renderer, generated geometry and hero metadata are preserved in
  `upstream/`. They are reference files and are not executed by the package.
- `art-inventory.json` identifies every original asset path and original/encoded
  SHA-256. Conversion uses `cwebp -lossless -exact`, without resizing or palette
  replacement. The small launcher cover is separately resized and quantized from
  the original elf mage; it is not substituted for the scene artwork.

Browser changes translate GPUI rendering to canvas, provide Clawdline's eight
states, use an explicit Session detail/open action, and add a complete text
register. Native terminal/recruit actions and optional activity hooks are omitted.
Six original hero variants are included; hero assignments are decorative and
last only for the current opening. State changes can move heroes along the
original road; historical activity is not invented. Opening does not replay
historical arrivals. No personal data is part of the package or previews.

To regenerate artwork from an authorized local copy of each pinned repository:

```sh
python3 plugins/import-herdr-art.py /path/to/world-of-codecraft /path/to/herdr-kaiju
python3 plugins/build-herdr-views.py
```

The importer reads data and invokes the installed WebP encoder only. It does not
install, execute or fetch upstream code.
