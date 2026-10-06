# Maintaining the compiled guides

`guide.md` is the English source. The Taiwan Traditional Chinese guide is
`guide.zh-TW.md`, selected by the canonical tag `zh-Hant` or the `zh-TW`
alias. The other seven translations are `ja`, `zh-Hans`, `ko`, `es`,
`pt-BR`, `fr` and `de`.

`freshness.json` pins the SHA-256 of both the English and translated bytes
for the preamble and each of the 20 named parts. `skills.Guide`,
`skills.Section` and `skills.Core` print current translated parts. If
either side changes without a reviewed new pin, they print the current
English part instead and list its name in a visible pending-translation
notice. `skills.PendingSections` exposes the same list to callers.
Changing a command or workflow in English therefore cannot leave an older
translated procedure on screen.

Every English content change must update the affected `zh-Hant` part in the
same change. The Go test `TestTraditionalChineseGuideIsCurrent` enforces
this. The other seven languages can be translated in a later batch; the
English fallback applies until then.

After translating and reviewing a part against the current English text,
including commands, flags, paths, refusal codes, negatives and authorization
conditions, pin it with:

```sh
python3 skills/clawdline/pin-freshness.py --pin zh-Hant dispatch
python3 skills/clawdline/pin-freshness.py
```

The second command prints pending parts by language. `--require-current`
checks that all nine guides are current for a full translation release. Do
not pin a part merely to clear a pending notice. Preserve fenced command
examples byte for byte; the guide tests compare them with English.
