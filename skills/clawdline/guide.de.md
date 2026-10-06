# Clawdline-Leitfaden

Für eine Assistenten-Session – Claude Code oder Codex – auf einem Rechner, auf dem **Clawdline Next**
läuft. Er beschreibt, was dieser Daemon heute bereitstellt, und nichts darüber hinaus: Jede Route
unten ist im Build registriert, der diesen Leitfaden ausgegeben hat, und ein Test schlägt fehl, wenn
eine fehlt. Gib ihn mit `clawdline guide de` neu aus (die englische Fassung mit `clawdline guide`),
statt einer Kopie zu vertrauen;
`clawdline guide zh-Hant` gibt den Leitfaden in traditionellem Chinesisch (Taiwan) aus (`zh-TW`
bleibt als Alias erhalten). `clawdline guide de` gibt den Kern aus und nennt die übrigen Abschnitte;
gib einen Abschnitt aus
(`clawdline guide de dispatch`), sobald du die Arbeit erreichst, die er behandelt, oder `clawdline guide de all` für
den vollständigen Text. Was auch immer ausgegeben wird, der Kern eingeschlossen, beginnt mit `guide-version: <sha256>`;
führst du denselben Befehl mit `--since <hash>` aus und ist der Text unverändert, gibt er stattdessen nur
die eine Zeile `unchanged <hash>` aus. `clawdline guide de refused <code>` gibt den Abschnitt aus, der einen
Ablehnungscode erklärt, und endet mit Exit-Code 1 ohne Ausgabe auf stdout, wenn kein Abschnitt ihn nennt.

In diesem Leitfaden ist ein **Schritt** ein Checklisteneintrag an einem Item, die **Schreibpfade** einer
Aufgabe sind die Pfade, die sie ändern darf, die **Zuweisung** sagt, wem ein Item gehört, und ein
**Abschnitt** ist ein benanntes Stück dieses Leitfadens.

## 0. Wenn du Clawdline aus der Swift-App kennst, lies zuerst dies

Die Swift-App wurde am 2026-09-19 außer Dienst gestellt: Sie ist gestoppt, startet nicht mehr bei der
Anmeldung, und auf Port 7717 antwortet nichts mehr. Ihr Verzeichnis `~/.config/clawdline` liegt noch auf
der Festplatte und wird weiterhin gelesen – nur gelesen –, und zwar für Verlaufsdaten, die dieser Daemon
nie besessen hat. Dieser Daemon ist keine Kopie jener App, und an fünf Unterschieden stolpern Leute:

1. **Aufgabenverzeichnisse liegen unter `<state dir>/tasks`, nicht unter `/tmp/.clawdline`.**
   `/tmp/.clawdline` gehörte dem Swift-Broker; zwei Broker, die in ein Verzeichnis von Aufgaben-IDs
   schreiben, wären dort kollidiert, wo niemand hinsieht. Codiere keines von beiden fest: Lies
   `task_root` aus dem Bestand (§3) und schreibe `task.json` darunter.
2. **Es gibt keinen Workflow-Umschlag und keine Workflow-Route, die du aufrufen sollst.** Nachrichten
   tragen keine Board-Einordnung mehr, und eine Session öffnet nie von sich aus Board-Karten.
   `POST /v1/orchestrator/sessions/<terminal>/workflow` existiert nur noch, damit ein alter Helfer nicht
   mitten im Turn scheitert: Die Route antwortet `workflow_retired`, zeichnet nichts auf, und neuer Code
   darf sie nicht aufrufen.
3. **Die Mitarbeit am Board läuft über Vorschläge und Entscheidungen** (§10): Eine Session schlägt vor,
   ein Mensch antwortet. Es gibt nichts, was man mit `begin` oder `deliver` anstoßen müsste.
4. **Eine andere Tür.** Port 7727 (oder `CLAWDLINE_NEXT_PORT`), Zustand in `~/.config/clawdline-next`
   (oder `CLAWDLINE_NEXT_DIR`). Lies niemals `~/.config/clawdline`: Das Token dort gehört nicht zu diesem
   Daemon und wird mit `401 unauthorized` abgelehnt.
5. **Was die Swift-App hatte und dieser Daemon nicht:** die Beförderung zu dauerhaften Berichten
   (antwortet `501 durable_report_promotion_unsupported`), die Nachfolge des Koordinators (antwortet
   `501 succession_unavailable`) sowie die Brief-Felder `serialize`
   und `attach_session` (jedes wird namentlich als `bad_task` abgelehnt). `reasoning_effort` wird
   unterstützt: `high` oder `xhigh`, nur bei einer `codex`-Aufgabe.

## 1. Root oder Child

Lautete deine erste Nachricht *„You are a Clawdline CHILD agent for task …“*, bist du ein **Child**. Die
dort genannte `CHILD.md` bestimmt, was du tust: Du dispatchst nicht, du sendest keine Turn-Quittung, du
quittierst mit `clawdline task accept` und schließt mit `clawdline task finish` ab. Lies hier nicht weiter.

Andernfalls bist du ein **Root**: eine gewöhnliche Session, mit der ein Mensch spricht. Der Rest ist für dich.

Lautete sie *„You are an independently owned Clawdline Feature Root …“*, oder wurde dir ein Board-Item
zugewiesen, gib als Nächstes `clawdline guide de feature-root` aus: Er enthält den ganzen gewöhnlichen Weg vom
Lesen des Items bis `done` und nennt für alles Seltenere den Abschnitt, den du ausgeben sollst.

## 2. Den Daemon erreichen

**Nimm die Befehle, nicht selbst gebautes curl, wo es einen Befehl gibt.** Sie lesen die Zugangsdaten
in ihrem eigenen Prozess, sodass diese nie in einer Befehlszeile, in `ps`, in ihrer Ausgabe oder in
deinem Transkript auftauchen. Ein selbst gebautes curl ohne diese Zugangsdaten bekommt
`401 unauthorized` („No valid credential came with this request …“): Es fehlen die Zugangsdaten, nicht
eine Berechtigung. Führe stattdessen den Befehl aus.

| Befehl | Was er tut |
|---|---|
| `clawdline guide [lang]` | Dieser Leitfaden. Kein Daemon nötig |
| `clawdline session report --summary "…"` | Zeichnet deinen abgeschlossenen Turn auf (§7) |
| `clawdline session close [--dry-run] [--terminal id]` | Prüft und schließt eine fertige Session, niemals mit Gewalt (§2a) |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | Dispatcht ein eigenes Child (§4) |
| `clawdline item show\|steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | Liest ein Board-Item, das dir gehört, und bringt es voran (`clawdline guide de feature-root`, §10) |
| `clawdline todo add\|list\|done` | Die eigenen To-dos dieser Session, nur wenn der Mensch darum bittet (§10) |
| `clawdline heavy -- <command…>` | Führt einen Build oder eine Testsuite im einzigen Compile-Slot des Rechners aus (§11) |
| `clawdline send --to <terminal> "…"` | Leitet eine Nachricht in eine andere Session weiter (§8) |
| `clawdline notify --title "…" --body "…"` | Schickt dem Menschen eine Benachrichtigung (§9) |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | Hinterlässt eine umsetzbare Notiz über einer Session (§9a) |
| `clawdline assistants` | Was dem Konto jedes Assistenten noch bleibt |
| `clawdline landings` | Jedes Landing, das auf diesem Rechner noch aussteht; `--work-id <item id>`: jedes Landing, das für ein Board-Item aufgezeichnet wurde |
| `clawdline leases [--json]` | Wer den Compile-Slot und jede Landing-Lease hält und wer dahinter wartet |
| `clawdline sessions [--json]` | Die Sessions, die ein Send, ein Wait oder eine Übergabe nennen kann, mit Zustand und Aufgabe |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | Was eine Session, eine Child-Aufgabe oder ein Board-Item verbraucht hat, nach Kategorie; standardmäßig deine eigene |
| `clawdline cloud pair [--offer <code>]` | Koppelt einen Cloud-Browser mit diesem Rechner |
| `clawdline task show [--json] <task id>` | Eine Child-Aufgabe kompakt: Zustand, Urteil, Zusammenfassung, Titel der offenen Reste, Verifikation, Landing, Checkout (§5) |
| `clawdline task wait <task id>… [--timeout 9m] [--any]` | Wartet, bis die Children fertig sind (alle, oder mit `--any` eines), zeigt jedes so an wie `task show` und schließt seine Meldung. Exit 0: alle erfolgreich, 1: eines fehlgeschlagen, 5: eines abgebrochen und keines fehlgeschlagen, 3: Zeitüberschreitung, 4: eine Aufgabe ließ sich nicht lesen; 4 vor 3 vor 1 vor 5 (§5) |
| `clawdline callback --title "…" [--timeout 30m] [--work-id <item>] -- <command…>` | Führt einen langen Befehl – ein Deployment samt Prüfung, das Warten auf CI – unter dem Daemon aus und kehrt sofort zurück; beende deinen Turn, und sein Exit erzeugt dieselbe typisierte `<clawdline-notice>` wie ein fertiges Child (§5a, `clawdline guide de callback`) |
| `clawdline task cancel <task id> --reason "…"` | Stoppt ein Child, das du versehentlich dispatcht hast: Sein Tab wird geschlossen, seine Schreibpfade und sein Slot werden freigegeben, ein Branch mit Commits bleibt für dich erhalten (§5) |
| `clawdline task ack <task id> <notice id>` | Schließt eine Abschlussmeldung von Hand; selten nötig, da `task show` und `task wait` sie schließen (§5) |
| `clawdline task accept <task dir>` | Ein Child quittiert damit sein Briefing. Roots führen es nie aus |
| `clawdline task finish <task dir>` | Der Abschluss eines Childs. Roots führen es nie aus |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | Startet einen Zeitplan über seinen Cloud-Webhook, auf jedem Rechner, und wartet auf sein Ergebnis; der Exit-Code sagt, wie er endete („Künftige Arbeit planen“). Kein Daemon nötig |

Ohne ausdrückliches Guide-Tag folgt die CLI-Sprache zuerst `--lang <tag>` vor dem Befehl, dann
`CLAWDLINE_LANG`, dem gespeicherten `product_language` und schließlich Englisch. Ein ausdrückliches
`clawdline guide <tag>` hat Vorrang vor dieser Wahl; ein nicht unterstütztes Tag zeigt Englisch.
`clawdline guide -list` nennt die neun ausgelieferten Tags. Diese Einstellung ändert für Menschen
lesbaren CLI-Text, nicht Protokollfelder und nicht die Sprache eines Agents.

Die Orchestrierungsbefehle oben (nicht `webhook fire`) geben bei Erfolg das JSON des Daemons aus; bei einer Ablehnung geben sie
`refused, <status> <code>: <message>` aus, danach jeden skalaren Wert der Ablehnung als `key: value`, einen pro
Zeile, und zuletzt die Abhilfe, und enden mit Exit-Code 1. Cloud-Befehle haben ihre eigene, für Menschen
lesbare Erfolgs- und Fehlerausgabe. `--port` überschreibt den Port.

`clawdline usage` ist das Token-Hauptbuch (`docs/token-ledger.md` im Repository): wofür jedes Token
ausgegeben wurde – `board`, `protocol`, `rules`, `impl`, `delegate`, `harness`, `talk`, `compaction`,
`other`. Ohne Flag liest es deine eigene Session, benannt durch `CLAUDE_CODE_SESSION_ID` oder
`CODEX_THREAD_ID`. Es gibt eine Kopfzeile aus (Aufrufe, höchster Kontext, Kosten), eine Zeile pro Kategorie,
nach Kosten sortiert – Anteil, Tokens, Kosten –, und danach jede Lücke; `--json` gibt die Antwort des Daemons aus.
`rules` ist eine obere Schranke und sagt das auch: Läuft ein Guard in einem Shell-Befehl zusammen mit anderer
Arbeit, zählt der ganze Befehl. Die Routen sind `GET /v1/usage/sessions/<conversation>`,
`GET /v1/usage/tasks/<task id>` und `GET /v1/usage/items/<item id>`, gelesen mit einem gekoppelten Gerät
oder dem Orchestrator-Token. Eine Session, die das Hauptbuch noch nicht gelesen hat oder nicht mehr lesen
kann, antwortet mit `not_yet_read`, `transcript_missing` oder `transcript_unreadable` – niemals mit einer
leeren Summe; eine ID, die niemand kennt, ist 404 `unknown_session`, `unknown_task` oder `unknown_item`.
Ob das Hauptbuch noch liest, steht unter `usage` in `/v1/diagnostics`.

**Auf einen langen Befehl warten.** `clawdline heavy`, `clawdline dispatch` und ein langer Testlauf geben
während des Wartens nichts aus und enden von selbst. Warte auf einen solchen Befehl mit **einem einzigen
langen Warten**, nicht indem du alle paar Sekunden nachsiehst: Jedes Nachsehen ist ein Turn, der deinen
ganzen Kontext neu liest, und eine Token-Auswertung zählte 520 solcher Turns (72,2 Mio. Tokens) über zehn
Items, überwiegend bei eingereihten `heavy`-Läufen.

- **Claude Code:** ein Bash-Aufruf mit langem `timeout` (bis `600000` ms), oder `run_in_background`
  und dann nichts, bis seine Abschlussbenachrichtigung eintrifft. Keine Schleife aus `sleep` und `tail`.
- **Codex (codex-cli 0.157.1, Code-Modus):** Setze `// @exec: {"yield_time_ms": 600000}` in die
  erste Zeile der `functions.exec`-Zelle. Nachdem `exec_command` eine Session-ID zurückgegeben hat, warte
  mit `write_stdin` mit leerem `chars` und `yield_time_ms: 300000`; läuft er noch, wiederhole das in
  derselben Zelle. Gemessen: Die äußere Zelle blieb mit `600000` 330 Sekunden offen, während ein
  leeres `write_stdin` bis zu 300 Sekunden wartete. Gibt die äußere Zelle die Kontrolle ab, hole das
  Ergebnis mit `wait` und einem langen `yield_time_ms` ab.

  Nimm eine einzige Zelle für das Warten auf ein Child oder für einen Build. Ersetze nur den Befehl;
  lass das Abfragen der Session in der Zelle, damit eine normale `exec_command`-Rückkehr nach 30 Sekunden
  den Agent nicht weckt, nur damit er dasselbe Warten erneut absetzt:

  ```js
  // @exec: {"yield_time_ms": 600000}
  let r = await tools.exec_command({cmd: "clawdline task wait --timeout 9m TASK_ID", yield_time_ms: 30000});
  while (r.session_id) {
    r = await tools.write_stdin({session_id: r.session_id, chars: "", yield_time_ms: 300000});
  }
  text(r.output);
  text(`exit ${r.exit_code}`);
  ```

  Ersetze für einen Build den Befehl durch `tools/heavy.sh …` und behalte seinen
  Exit-Code: 75 bedeutet, dass das Warten auf den Compile-Slot oder auf Speicher abgelaufen ist, bevor der Build lief.

`clawdline heavy` wartet höchstens `--max-wait` (Standard 30m) und endet dann mit 75, ohne den Befehl
auszuführen; ein Warten, das länger ist, als dein Werkzeug erlaubt, gehört in einen Hintergrundlauf.

**Curl an eine Orchestrator-Route.** Lies `<state dir>/orchestrator-token` und sende es im Header
`X-Clawdline-Orchestrator`. Halte das Token aus den Befehlsargumenten heraus: Verwende
`DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"`, dann
`-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")`.
Verwende `curl --fail-with-body`; jeder POST mit JSON-Body braucht außerdem
`-H 'Content-Type: application/json'` (sonst `415 unsupported_media_type`).

### Einen Cloud-Browser koppeln

Das Koppeln ändert, wer diesen Rechner lesen darf. Ein gekoppelter Browser darf ihn sofort lesen und,
wenn Cloud-`commands` eingeschaltet sind, auch steuern. Führe einen Kopplungsbefehl nur aus, wenn der
Mensch ausdrücklich darum bittet, genau diesen Browser zu koppeln, oder dir den genauen Kopplungsbefehl
oder das genaue Angebot gibt. Das Koppeln schaltet Befehle nicht ein; das bleibt eine eigene Einstellung.

Es gibt zwei unterstützte Richtungen:

1. **Der Browser zeigt ein Angebot.** Führe genau die Zeile, die er dir gibt, auf dem Rechner aus:

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

   Behalte die einfachen Anführungszeichen. Das Angebot ist ein undurchsichtiges, kurzlebiges,
   einmal verwendbares Geheimnis: Dekodiere, bearbeite und speichere es nicht und wiederhole es nicht in
   der abschließenden Antwort. Ist es abgelaufen oder schon benutzt, hole dir ein frisches Angebot aus dem
   Browser, statt es erneut zu versuchen oder es zu verändern.
2. **Der Rechner stellt die Einladung aus.** Führe `clawdline cloud pair` aus. Er gibt einen einmaligen
   Link `https://app.clawdline.com/#pair=…` aus und wartet. Der Mensch öffnet diesen vollständigen Link in
   dem Browser, den er koppeln will, angemeldet im selben Konto bei Clawdline Cloud. Behandle den Link wie das
   Angebot: Veröffentliche ihn nicht und bewahre ihn nicht auf.

Bei Erfolg erscheinen drei Zeilen: `paired` nennt die Geräte-ID des Browsers, `browser` ist der
Fingerabdruck des Browsers und `machine` der Fingerabdruck des Rechners. Vergleiche den
Browser-Fingerabdruck mit dem, der im Browser angezeigt wird, und den Rechner-Fingerabdruck mit dem, der
für diesen Rechner angezeigt wird. Eine Abweichung ist kein Erfolg: Führe sofort
`clawdline cloud revoke <device-id>` mit der `paired`-ID aus und melde dann die Abweichung.
`clawdline cloud devices` listet die aktuellen Browser und ihren lokalen Vertrauensstatus auf; es ist
auch die schreibgeschützte Prüfung für die Zeit nach dem Koppeln.

Diese Befehle laufen über den laufenden lokalen Daemon. Schlägt einer fehl, melde seine genaue
stderr-Ausgabe. Schalte Cloud nicht ein, melde dich nicht an, aktiviere keine Befehle, rotiere keine
Schlüssel und ersetze das gelieferte Angebot nicht, es sei denn, der Mensch hat gesondert um genau diese
Änderung gebeten.

**Wo die Dinge liegen.**

- Port: `CLAWDLINE_NEXT_PORT`, sonst **7727**. Nur Loopback: `http://127.0.0.1:<port>`.
- Zustandsverzeichnis: `CLAWDLINE_NEXT_DIR`, sonst `$XDG_CONFIG_HOME/clawdline-next`, sonst
  `~/.config/clawdline-next` (`%APPDATA%\clawdline-next` unter Windows).
- `GET /v1/health` braucht keine Zugangsdaten und antwortet `served_by: "clawdline-go"`. Damit
  unterscheidest du „läuft nicht“ von „abgelehnt“.

**Zugangsdaten.** Es gibt drei, und eine Session verwendet die erste:

| Zugangsdaten | Wo | Gesendet als | Öffnet |
|---|---|---|---|
| Orchestrator-Token | `<state dir>/orchestrator-token` | Header `X-Clawdline-Orchestrator` | Alles unter `/v1/orchestrator/`, `/v1/work/`, `/v1/board`, `GET /v1/places`, `POST /v1/artifacts/images` |
| Aufgabengeheimnis | vom Root beim Dispatch gewählt | Header `X-Clawdline-Task-Secret` | Die eigenen Routen eines Childs unter `/v1/orchestrator/tasks/<id>/` sowie `POST /v1/orchestrator/proposals` |
| Geräte-Token | `<state dir>/local-token`, oder das eines gekoppelten Geräts | `Authorization: Bearer` | Die Routen der Konsole (`/v1/sessions/…`). Eine Session braucht es nicht |

Das Orchestrator-Token, als `Bearer` gesendet, wird mit Geräten verglichen und abgelehnt. Ein falsches
oder fehlendes bekommt `401 unauthorized`, und die Antwort sagt das auch: Das Token fehlt oder gehört
nicht zu diesem Daemon, oder das Gerät ist nicht gekoppelt. `clawdline doctor` gibt das Verzeichnis und
den Port aus, die die CLI liest.

**Wenn du curl verwenden musst**, halte das Token aus der Befehlszeile heraus:

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

- `--fail-with-body`: Ohne das endet eine Ablehnung mit Exit-Code 0 und liest sich wie ein Erfolg.
- **Jeder POST mit Body braucht `-H 'Content-Type: application/json'`**, sonst wird er mit
  `415 unsupported_media_type` abgelehnt. `curl -d` allein sendet einen Formulartyp.
- Bodies sind auf 2 MiB begrenzt, sofern eine Route nicht weniger angibt.
- Eine tmux-Terminal-ID wie `%47` kommt als ein einzelnes, maskiertes Pfadsegment in einen Pfad: `%2547`.

**Ablehnungen gibt es in zwei Formen.** Verzweige nach dem Code, niemals nach dem Satz:

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}` – das Gate und der Broker.
  Zusätze wie `retry_after` stehen innerhalb von `error`.
- `{"error":"<code>","detail":"…"}` – nicht gefundene Routen, falsche Methoden und manche Lesezugriffe.

Eine Route, die diesem Daemon nicht gehört, wird mit `501 not_implemented` abgelehnt, und die Ablehnung
nennt die Route. Das ist die Antwort auf jedem gewöhnlichen Rechner. Weitergeleitet wird nur, wenn
jemand absichtlich mit `CLAWDLINE_NEXT_UPSTREAM_PORT` einen anderen Daemon hinter diesen gesetzt hat, und
dann nennt ein `502
upstream_unreachable` die Adresse, die nicht geantwortet hat. Keines von beiden ist eine Antwort dieses
Daemons. Vor dem 2026-09-19 war die Weiterleitung standardmäßig an und ging an die Swift-App auf 7717;
eine damals geschriebene Notiz sagt deshalb, eine fremde Route erreiche jene App. Das tut sie nicht.

### Die Clawdline-Anzeige eines Projects einrichten

Verwende diesen Abschnitt, wenn der Mensch dich bittet, das Project, in dem du arbeitest, in Clawdline
lesbar zu machen. Das Ergebnis ist nicht „es gibt ein paar Dateien“, sondern: Das Project hat einen
wahrheitsgemäßen Namen und ein Zeichen, lang laufende Arbeit kann Fortschritt melden, und seine
Entwicklungsserver sind sichtbar, ohne dass Clawdline sie startet.

Lies zuerst die Anweisungen dieses Repositorys, die README, die Deploy- und Build-Skripte und die
vorhandene Konfiguration des Prozessmanagers. Behalte die Befehle bei, die das Project bereits verwendet.
Füge keinen zweiten Deploy-Weg und keinen zweiten Prozess-Supervisor nur für Clawdline hinzu, und
starte, stoppe, starte neu oder deploye nichts, es sei denn, der Mensch hat um genau diese betriebliche
Änderung gebeten. Konfiguration und ein tatsächliches Deployment sind verschiedene Arbeiten.

Geh diese vier Prüfungen durch und überspringe eine nur, wenn sie wirklich nicht zutrifft:

1. **Project.** Führe `clawdline project list` aus. Fehlt dieser Checkout, füge seine Repository-Wurzel
   mit `clawdline project add <absolute-root>` hinzu und liste erneut auf. Das zeichnet einen Ort auf, an
   dem eine Session starten darf; es ändert das Repository nicht.
2. **Name und Symbol.** Ist keines konfiguriert, leitet Clawdline ein stabiles Symbol ab. Will der Mensch
   einen bewusst gewählten Namen oder ein Pixelzeichen, erhalte jeden anderen Eintrag in
   `~/.claude/project-icons.json` und bearbeite nur den längsten Pfad, der dieses Project enthält. Das
   Format ist in `docs/project-status.md` des Clawdline-Repositorys beschrieben; die Seite „Projects“ kann
   auch ein vorhandenes, aufgelöstes Symbol kopieren, ohne dass JSON von Hand bearbeitet wird. Eine
   globale Benutzerdatei ist kein Repository-Inhalt: Zeig den genau vorgeschlagenen Eintrag, bevor du
   ihn änderst, wenn die Bitte diese Änderung nicht schon erlaubt hat.
3. **Deployment und lange Arbeit.** Clawdline liest nur Statusquittungen; es führt nie ein Deployment
   aus. Bei einem GitHub-Repository ist die Deployment-Quittung
   `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`, wobei owner und repo aus `origin` stammen.
   Der Erzeuger, der den Lauf ohnehin kennt, schreibt `state` (`running`, `ok`, `fail` oder `none`),
   `label`, `url`, `started_at` und ein gemessenes `typical_seconds`, und zwar atomar. Für einen lokalen
   Build-, Test-, Import- oder Deploy-Befehl verwende `clawdline-progress run --label <label> -- <command>`,
   wenn dieser Helfer existiert, oder implementiere den `run-<path>.json`-Vertrag aus
   `docs/project-status.md`. Erfinde nie eine Dauer; lass sie weg, bis sie gemessen ist. Ein beendeter
   Erzeuger darf keinen dauerhaften Running-Zustand hinterlassen.
4. **Entwicklungsserver.** Lege `.devstack.json` an der nächstgelegenen deploybaren Wurzel an oder
   aktualisiere sie. Der Go-Daemon liest derzeit die deklarierten `processes` und prüft ihren
   Loopback-`port` oder öffnet ihre `url`; er führt die Befehle `status`, `up`, `down`, `restart` oder
   `logs` aus dem Browser **nicht** aus. Bevorzuge die kleinste wahrheitsgemäße Datei der Stufe 0, zum
   Beispiel:

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

   Einen Prozess ohne stabilen Port und ohne stabile URL errät man nicht in die Datei hinein. Sondiere
   die Produktion nicht und starte sie nicht neu, während du eine Entwicklungsdeklaration verifizierst.

Verifiziere jede geänderte Schicht für sich: `clawdline project list` nennt den Checkout; jede JSON-Datei
lässt sich parsen; die eigenen Tests des Repositorys für geänderte Skripte laufen durch;
`GET /v1/devstacks` zeigt deklarierte Server als running, stopped oder unknown, statt sie stillschweigend
wegzulassen; und eine Session im Project zeigt eine frische Fortschritts- bzw. Deployment-Quittung. Ist
ein Lesezugriff nicht verfügbar, fehlerhaft oder veraltet, sag welcher, und lass ihn unbekannt – melde
Abwesenheit nie als Erfolg. Liste zum Schluss auf, was konfiguriert wurde, was bewusst nicht zutraf und
welche dem Menschen gehörende Datei außerhalb von git geändert wurde.

**Vereinheitlichen: ein Satz Regeln und Skills für Claude und Codex** (`/clawdline unify`). Codex liest
`AGENTS.md` und `.agents/skills/<name>/`; Claude liest `CLAUDE.md` (und `AGENTS.md` nur, wenn
`CLAUDE.md` fehlt oder eine Zeile `@AGENTS.md` enthält) und `.claude/skills/<name>/`. Ein Project ist
vereinheitlicht, wenn seine Regeln in `AGENTS.md` stehen, `CLAUDE.md` fehlt oder sie importiert, und jeder
Skill in `.agents/skills/<name>/` liegt, mit `.claude/skills/<name>` als relativem Link dorthin. Wenn der
Mensch darum bittet oder `/clawdline unify` aufruft:

1. Führe `clawdline project unify` im Project der Session aus (der git-Wurzel; füge das Project zuerst mit
   `clawdline project add` hinzu, wenn es nicht aufgelistet ist). Es ändert nichts. Zeig dem Menschen, in
   seiner Sprache, was Claude und Codex jetzt jeweils lesen und danach lesen werden, jede Skill-Zeile, jeden
   Aktionssatz und jeden Konflikt – einschließlich der `CLAUDE.md`-Zeilen, die Codex nicht sieht.
2. Führe `clawdline project unify --apply` erst aus, nachdem eine eigene Nachricht des Menschen in diesem
   Gespräch diesen Plan genehmigt hat. Es sendet die Version, die du gezeigt hast; hat sich die Festplatte
   seitdem geändert, antwortet es `plan_changed` und wendet nichts an – gib den Plan erneut aus und frag erneut.
3. Zeig das Ergebnis von `clawdline project unify --check` (Exit 0: vereinheitlicht, 1: driftet ab,
   3: unbekannt). Nichts wird committet; sag, welche Dateien sich geändert haben, damit der Mensch oder
   eine Session sie committet.

Löse einen Konflikt nie selbst, indem du `AGENTS.md`, `CLAUDE.md` oder einen Skill bearbeitest, ohne dass
eine Nachricht des Menschen das sagt: Ein Skill, der sich zwischen den beiden Verzeichnissen unterscheidet,
ein Link, der woandershin zeigt, oder Regeln, die Codex nicht sieht, sind seine Entscheidung. Die Routen
sind `GET /v1/projects/{place}/unify` (der Plan) und `POST /v1/projects/{place}/unify` mit
`{"version"}` und `Idempotency-Key`; Ablehnungen sind `plan_changed`, `plan_unknown` (ein Teil des
Projects ließ sich nicht lesen, also hat sich nichts geändert) und `name_taken` (ein Name, den unify
anlegen würde, existiert schon; nichts wird überschrieben).

## 2a. Der gewöhnliche Weg eines Feature Roots

Das ist alles, was ein gewöhnlicher Feature Root – eine Session, der ein Board-Item gehört – der Reihe
nach ausführt. Jeder Schritt ist ein Befehl: Die Befehle tragen die Zugangsdaten, und ein selbst
gebautes curl an dieselbe Route wird abgelehnt. Alles Seltenere ist nur ein `clawdline guide <part>`
entfernt; die Verweise stehen am Ende.

**1. Lies das Item.** `clawdline item show <item id>` gibt seine Art, seine Phase, die Akzeptanzkriterien,
die erfassten Gates, die Akzeptanzversion (`acceptance vN`), bei einem Feature den Schalter „Needs
independent review“ des Menschen, seine Schritte und jedes Dokument samt Inhalt aus. Das ist
der Datensatz, nach dem du arbeitest. `clawdline item show <item id> --doc <doc id>` gibt den Inhalt eines
einzelnen Dokuments allein aus, um ihn in eine Datei zu leiten; `clawdline item steps <item id>` ist derselbe
Datensatz ohne die Inhalte. Jeder Schreibvorgang an einem Item gibt `wrote …; item <id> is at version N`,
eine kurze Item-Zusammenfassung und einen Hinweis auf `item show` aus. Lies die vollständige Akzeptanz,
die Schritte und die Dokumente mit `item show`. Schreibvorgänge wirken auf die aktuelle Version des
Items, es sei denn, du übergibst `--expected-version`.

Hat deine ASSIGNMENT.md eine Überschrift **HANDOFF**, übernimmst du ein Item, das eine andere Session
mitten in der Arbeit hinterlassen hat (neu zugewiesen, nachdem die Implementierung begonnen hatte, aber
vor done). Lies zuerst das darin genannte Paket, bevor du planst. Der Daemon hat es aus seinen eigenen
Aufzeichnungen und aus git gebaut, ohne den bisherigen Eigentümer zu fragen: die an das Item gebundenen
Aufgaben und ihre Ergebnisse, noch nicht gelandete Commits, die nicht committeten Änderungen jedes
Worktrees als Patch mit seinem sha256 und dem `git apply`-Befehl, der ihn auf seiner Basis
wiederherstellt, die letzte Nachricht des bisherigen Eigentümers (oder warum sie nicht lesbar war) sowie
die Phase und die offenen Schritte. Die Patches liegen neben der ASSIGNMENT.md und überdauern die
Worktrees. Mach dort weiter; fang nicht von vorn an.

**2. Benenne deine Session**, wenn sie für dieses Item geöffnet wurde:
`clawdline item name <item id> "<task name>"`, einmal, nachdem du Ziel und Umfang gelesen hast. Das benennt die Session um, nicht das Item.

**3. Vor dem Implementieren.**

- Erfasste Planung an und keine Akzeptanzkriterien: Schreib beobachtbare mit
  `clawdline item acceptance <item id> --body-file acceptance.md`.
- „Needs independent review“ angehakt, oder ein Epic: Zuerst kommt der Weg mit geprüftem Plan aus
  `clawdline guide de epic`. Nicht angehakt: kein Plan, kein Review-Child.
- Mehrstufige Arbeit ohne Schritte: `clawdline item step-add <item id> "first" "second" …` (zwei bis
  acht Schritte, die du einzeln verifizieren kannst; eine einzelne Änderung braucht keine).
- Dann `clawdline item phase <item id> implementing`.

**4. Arbeite standardmäßig in dieser Session.** Untersuche, implementiere, verifiziere und lande das
Feature selbst. Dispatche nur, wenn ein konkreter Bedarf eine eigene Session sinnvoll macht: wirklich
unabhängige parallele Arbeit, andere Werkzeuge oder Berechtigungen oder ein vorgeschriebenes unabhängiges
Review. Sag vor dem Dispatch, warum; eine gewöhnliche Untersuchung oder Implementierung allein ist kein Grund.

Wenn ein Dispatch nötig ist, behalte Synthese, Integration und Landing hier:

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id` bindet das Child an das Item, sodass sein Landing als das des Items zählt. Wiederhole es,
  wenn ein Child mehrere Items erledigt: Das erste ist die Linie des Childs, und das Landing zählt für jedes.
- Der Titel ist eine Zeile von höchstens 60 Zeichen, die sagt, was danach anders sein wird. Jeder
  Doppelpunkt (`:` oder `：`) wird abgelehnt, weil er eine Beobachtung mit einer Erklärung verbindet;
  ebenso „the user“ als Subjekt oder ein Titel, der mit einem als Code formatierten Bezeichner beginnt.
  Jeder dieser Fälle antwortet mit `bad_task` und einem `title: …`, das sagt, welcher.
- Das Briefing steht für sich allein. Schreib die Fakten hinein, die du schon verifiziert hast, jeweils
  mit ihrer `file:line` oder dem Befehl, der sie gezeigt hat, damit das Child sie nicht neu entdeckt.
- Das Briefing eines Untersuchungs- oder Explore-Childs nennt außerdem seine Abbruchbedingung – die
  Frage, deren Beantwortung die Aufgabe beendet – und ein Turn-Limit.
- Schreibgeschützte Arbeit ist `--claims ""`. Jedes Flag und jeder Ablehnungscode steht in
  `clawdline guide de dispatch`.

**5. Wenn ein Child fertig ist**, wird eine Zeile `<clawdline-notice>` in dein Eingabefeld getippt. Führe
`clawdline task show <task id>` aus und integriere dann die Lieferung; das Lesen schließt die Meldung, es
gibt also kein separates ACK. **Beende nach einem Dispatch deinen Turn**: Die Meldung weckt dich, und ein
zum Warten offen gehaltener Turn liest bei jeder Abfrage deinen ganzen Kontext neu. Nur wenn es sonst
nichts zu tun gibt und du blockieren musst, führe `clawdline task wait <task id>…` aus (Standard `--timeout 9m`, `--any` für das erste). Integriere ein Worktree-Child, indem du **seinen Branch** in das Ziel **mergst**. **Der Merge zeichnet das
Landing von selbst auf**, innerhalb weniger Minuten: Poste kein Landing von Hand. `clawdline landings`
listet auf, was noch aussteht. Ein mit `--claims ""` dispatchtes Child, das nichts geschrieben hat, wird
vom Broker als `nothing_to_land` aufgezeichnet. Alles andere ist `clawdline task land <task id> <state>`
(`clawdline guide de landing`).

**6. Abschlussbericht**, wenn das Finden der Ursache eine erhebliche Untersuchung erfordert hat (eine
direkte, beobachtete Korrektur braucht keinen). Füge ihn vor `done` hinzu: Sobald das Item done ist, ist
es niemandem mehr zugewiesen, und der Bericht antwortet mit `409 not_item_owner`.

```sh
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Schreib ihn für den Menschen, der das Problem gemeldet hat, in Markdown, ohne private Daten.

**7. Schließ das Item ab.** Schließ jeden Schritt ab, sobald er verifiziert ist, mit
`clawdline item step-done <item id> <step id>`. Committe und pushe direkte Arbeit aus einem
Wegwerf-Worktree; merge den Branch eines Childs, wenn eines eingesetzt wurde. Ist alles gelandet,
bringt ein einziger Befehl das Item auf `done`. Das aufgezeichnete Landing eines Childs liefert Commit,
Ziel und Remote; direkte Arbeit nennt sie selbst:

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --commit <sha> --target main --remote origin \
  --deployment "what went live, where, which version"      # omit landing flags for a landed child
```

Oder geh eine Phase nach der anderen weiter:

```sh
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> deploying --no-landing-reason "why there is no code"    # work with no code
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

`done` nimmt `--deployment` oder `--no-deployment-reason`, je nachdem, was die Deployment-Richtlinie des
Items sagt. Gibt `clawdline item steps <item id>` eine Gate-Zeile aus, bleibt dieses Item auf dem
längeren Weg, auf den diese Zeile zeigt.

**Auf ein Deployment warten.** Halte den Turn nicht offen, während ein Deployment läuft oder sich
verbreitet. Starte das Deployment und seine Prüfung als einen Callback, beende den Turn und schließ das
Item ab, wenn seine Meldung eintrifft:

```sh
clawdline callback --title "The hosted console serves <sha>" --work-id <item id> --timeout 20m -- \
  sh -c './deploy.sh <sha> && tools/wait-hosted-console.sh <sha>'
# … the notice: clawdline task show <callback id>, then
clawdline item finish <item id> … --deployment "what went live, where, which version (callback <callback id>)"
```

**8. Melde den Turn**: `clawdline session report --summary "…"` (§7).

**9. Lass die besitzende Session offen.** Das Abschließen oder Abbrechen eines Board-Items gibt seine
Zuweisung frei; es beendet nicht die Session, der es gehörte. Lass die Session nach `session report`
für Folgearbeit verfügbar. Führe `clawdline session close` nicht bloß deshalb aus, weil das Item `done`
oder `cancelled` erreicht hat. Ein Mensch kann später ausdrücklich darum bitten, die Session zu
schließen. Unabhängig davon schließt der Broker den Tab eines von einem Agent dispatchten Childs, nachdem
dessen Aufgabe geendet hat, nach der Child-Tab-Regel in
`clawdline guide child`.

**Wenn etwas abgelehnt wird.** `version_conflict`: Führe denselben Befehl noch einmal aus; er liest die
Version neu. `steps_incomplete`: Ein Schritt ist noch offen. Jeder andere Code: §12, dann der Abschnitt,
der ihn behandelt.

**Seltenere Arbeit, je ein Abschnitt:** `clawdline guide de board` – Vorschläge, Entscheidungen, To-dos,
Wiedereröffnen eines erledigten Items, Warten auf den Menschen, Gates und jede Phasenablehnung;
`clawdline guide de epic` – Pläne, Plan-Review, die Child-Items eines Epics, Personas;
`clawdline guide de landing` – Landing von Hand, Übergaben (auch die Meilenstein-Übergabe eines lange
laufenden Roots), Root-Zuweisungen; `clawdline guide de running` – hängende Children, offene Reste, Respawn.

## 3. Vor dem Dispatch: Lies, was schon da ist

Die Arbeit einer anderen Session erledigt deine Aufgabe womöglich schon, und vom gemeinsamen Baum aus ist
das unsichtbar: Eine fertige Lieferung auf einem nicht gemergten Branch taucht in keinem `git status`
auf. Lies zuerst.

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- Antwortet mit `generation`, `task_root` und vier Listen: `live`, `unlanded`, `droppable`,
  `unreadable`. Jede Zeile trägt ein `do`, das der Daemon annehmen würde. Mit `claims` sagt jede
  Live-Zeile, womit sie sich überschneidet (`overlaps`).
- **`generation` ist für den Dispatch erforderlich** (§4). Es sind 16 Hex-Zeichen über die versiegelten
  Felder der Zeilen; der Wert ändert sich, wenn eine Zeile beginnt, endet oder ihre Schreibpfade ändert.
- **`task_root` ist der Ort, an den deine `task.json` gehört.** Es ist ein eigenes Feld dieses Daemons;
  der Swift-Broker hatte keines, weil er `/tmp/.clawdline` fest codiert hatte.
- `400 bad_request`, wenn `project` kein absoluter Pfad innerhalb eines Git-Repositorys ist.

Ebenfalls einen Blick wert:

- `GET /v1/orchestrator/inflight?project=…` – jede ausstehende Arbeitslinie im Repository, wer sie hat
  und was sie beansprucht hat.
- `clawdline assistants` – pro Assistent: `availability` (`ok`, `low`, `exhausted`, `unknown`),
  `windows`, `stale`, `resets_at`. Entscheide nach dem Lesen, an wen du dispatchst; nichts lehnt einen
  Dispatch wegen des Kontingents ab.

**Sollte das überhaupt dispatcht werden?** Arbeit, die in unabhängige Stücke zerfällt, geht parallel
schneller. Eine Kette, in der jeder Schritt vom vorigen abhängt, wird durch Aufteilen schlechter, weil
jede Übergabe sie unterbricht. Diagnose, Arbeit, die kleiner ist als ihr eigenes Briefing, und alles,
worauf jemand wartet, bleiben in deiner eigenen Session. Die Hausregeln des Rechners stehen in
`<state dir>/dispatch-policy.md` (und in der `dispatch-policy.local.md` des Menschen); jedes Child bekommt
sie mit seinem Briefing.

## 4. Ein eigenes Child dispatchen

Ein eigenes Child ist eine begrenzte Aufgabe unter dir. **Synthese, Integration und Landing behältst du.**

**Ein einziger Befehl erledigt alle vier Schritte unten**, mit dem Briefing auf stdin oder in einer Datei:

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--persona <id>] [--work-id uuid …] [--task-id uuid] [--label "…"] [--project-dir D] < brief.md     # or --instructions-file brief.md
```

Er erzeugt die ID und das Geheimnis, liest den Bestand für `generation` und `task_root`, schreibt
`task.json`, postet die Aufgabe und liest bei einem `stale_inventory` den Bestand neu und sendet einmal
erneut. Er gibt `dispatched <id> <state> [worktree <path>]` aus, dann eine Zeile pro Warnung – die des
Daemons und jede laufende Aufgabe, deren Schreibpfade sich mit deinen überschneiden. `--json` gibt
stattdessen die Antwort des Daemons aus. Eine Ablehnung ist `refused, <status> <code>: <message>` auf
stderr, danach ihre Zusätze und die Abhilfe, je eine Zeile, und Exit-Code 1; die Tabelle am
Ende dieses Abschnitts sagt, was jeder Code bedeutet. Der Root ist dein Gespräch, aus
`CLAUDE_CODE_SESSION_ID` oder `CODEX_THREAD_ID`, sonst aus `--conversation`; der Assistent des Childs ist
deiner, sofern `--assistant` nichts anderes sagt; das Project ist die git-Wurzel dieses Verzeichnisses,
sofern `--project-dir` nichts anderes sagt. `--claims ""` deklariert ein Child, das nichts schreibt.
Während der Daemon den Worktree und den Tab des Childs öffnet, gibt der Befehl nichts aus; es ist eine
einzige Anfrage, die antwortet, sobald das Child existiert, also warte einmal darauf (§2, „Auf einen
langen Befehl warten“). Das Geheimnis steht nie in argv, in `task.json` oder in der Ausgabe, und das
Token wird so gelesen, wie jeder schlanke Befehl es liest.

Für ein Review, das eventuell wiederholt werden muss, wähle vor dem ersten Aufruf eine UUID in
Kleinbuchstaben und übergib sie bei jedem Versuch als `--task-id`. Der Befehl bewahrt neben `task.json`
eine private Kopie der ursprünglichen Dispatch-Absicht auf, sodass eine identische Wiederholung erneut
senden kann, selbst nachdem der Daemon das Briefing umgeschrieben hat. Der Broker gibt die ursprüngliche
Aufgaben-ID mit `(replayed)` zurück; ein geändertes Briefing wird lokal abgelehnt. Läuft der Befehl in
eine Zeitüberschreitung oder geht seine Ausgabe verloren, prüfe `GET /v1/orchestrator/tasks/<id>`, bevor
du von einem Fehlschlag ausgehst. Eine fehlende Aufgabe kann mit derselben ID und demselben Briefing
wiederholt werden. Eine ausdrückliche Ablehnung hat keine Aufgabe angelegt und kann ebenfalls wiederholt
werden, nachdem ihre Ursache behoben ist.

`--persona <id>` startet das Child als eingebaute Persona (`persona` in `task.json`); eine ID, die
dieser Build nicht hat, wird lokal abgelehnt. Keine Art bekommt standardmäßig eine, auch `plan_review`
nicht: Nenn `code-reviewer` selbst, wenn du es willst. `GET /v1/personas` listet die IDs auf, die dieser
Build mitbringt; was eine Persona ist, steht im Epic-Teil von §10 (`clawdline guide de epic`).

Die Schritte, die er ausführt, für einen Aufrufer ohne das Binary:

**1. Wähle eine ID und ein Geheimnis.**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 characters, lowercase
SECRET=$(openssl rand -hex 32)          # 64 lowercase hex
```

Das Geheimnis gelangt von dir im POST-Body zum Daemon und vom Daemon zum Child in der einen Zeile, die
er dort eintippt. Es steht nicht in `task.json` und nicht in der Antwort auf den Dispatch, und du
brauchst es nicht wieder. (Ein Respawn ist die einzige Antwort, die ein Geheimnis trägt: das neue ihrer Kopie.)

**2. Lies den Bestand** (§3) für `generation` und `task_root`.

**3. Schreibe `<task_root>/<TASK_ID>/task.json`.** Der Daemon liest das Briefing aus dieser Datei, nicht
aus der Anfrage. Bei der Aufnahme validiert er sie, schreibt `task.json` aus dem, was er angenommen hat,
neu und schreibt die `CHILD.md` des Childs aus demselben Datensatz – Titel, Anweisungen, Schreibpfade,
Lieferobjekte, Art und Zeitlimit eingeschlossen –, sodass die Aufgabe, die das Child liest, die
validierte ist; `task.json` liest es nicht.

| Feld | Regel |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | dieselbe ID |
| `assistant` | `claude` oder `codex` |
| `project_dir` | absoluter Pfad zu einem existierenden Verzeichnis |
| `title` | wird auf dem Bildschirm angezeigt: eine Zeile von höchstens 60 Zeichen, die sagt, was danach anders sein wird. Ein Doppelpunkt (`:` oder `：`), „the user“ als Subjekt oder ein als Code formatierter Bezeichner am Anfang wird als `bad_task` (`title: …`) abgelehnt |
| `instructions` | erforderlich, höchstens 16 KiB. Sie müssen für sich allein stehen: Das Child weiß sonst nichts. Gib die Fakten mit, die du schon verifiziert hast, jeweils mit ihrer `file:line` oder ihrem Befehl; ein Untersuchungs-Child bekommt außerdem eine Abbruchbedingung und ein Turn-Limit |
| `claims` | **erforderlich**: höchstens 32 relative Pfade, die das Child schreiben darf. `[]` bedeutet, dass es nichts schreibt, und erzeugt eine Warnung (`claims_missing`) |
| `isolation` | `none` (Standard) oder `worktree` für einen privaten Checkout auf einem eigenen Branch |
| `permission_mode` | `ask`, `edits` oder `full` |
| `timeout_minutes` | 1–240, Standard 30 |
| `kind`, `deliverables`, `model` | optional; `model` ist `[a-z0-9._-]`, höchstens 64 Zeichen |
| `work_id` | optionale UUID des Board-Items, dem dies dient |
| `persona` | optionale ID einer eingebauten Persona (`GET /v1/personas`); standardmäßig keine |
| `auto_compact_window` | optional, nur Claude: die Kontextgröße in Tokens (50000–1000000), bei der das Child kompaktiert, oder `null` für keine. Fehlt das Feld, gilt `claude_auto_compact_window` des Rechners, das aus ist, sofern der Mensch es nicht gesetzt hat. Zum Vergleichen von Läufen, nicht für alltägliche Briefings: Eine Kompaktierung kann Details verlieren |
| `root` | **erforderlich**: `{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "project_dir": "<the same absolute repository path as project_dir>", "label": "…"}`. Ein rollengebundener Root braucht `root.project_dir`, damit der Daemon seinen Project-Bereich prüfen kann. |

**Stimme Arbeitsoberfläche und Startmodus auf jedes Werkzeug ab, das das Child benutzen muss, bevor du es dispatchst.**
Das Briefing nennt die erforderlichen Werkzeuge, und der Root weist nach, dass die gewählte Oberfläche
sie bereitstellt. Ein Codex-CLI-Child bekommt den eingebauten `@Browser` der ChatGPT-Desktop-App nicht
allein durch ein Berechtigungsflag. Leite ein Review von UI, Barrierefreiheit oder responsivem Layout an
eine Oberfläche, die wirklich Browser/Computer Use hat, oder nenne eine für die Abnahme gleichwertige
lokale Browser-Umgebung wie Playwright/Chrome CDP und weise nach, dass sie installiert ist. Kann diese
Oberfläche Zugriff auf eine App, einen Origin oder die GUI anfordern, dispatche mit
`--permission-mode ask`: Codex `full` bedeutet einen nicht interaktiven Shell-Start
(`--ask-for-approval never`), nicht jedes Werkzeug, und Auto-review kann keine Anfrage prüfen, die nie
gestellt wird. Zu Beginn der Aufgabe benutzt das Child jedes erforderliche Werkzeug tatsächlich, statt
nur einen Befehlsnamen zu prüfen. Fehlt eines, meldet es sofort die genaue Lücke, und der Root stellt den
Zugriff wieder her oder dispatcht neu. Es schließt eine werkzeugabhängige Abnahmeprüfung nicht als
unverifiziert ab, nur weil der Root einen unpassenden Worker gewählt hat.

**`root.session_id` ist deine Gesprächs-ID, niemals eine Terminal-ID.** Claude Code exportiert sie als
`CLAUDE_CODE_SESSION_ID`, Codex als `CODEX_THREAD_ID`. Darüber ordnet der Daemon das Child dir zu und
sagt dir, wann es fertig ist. Um zu prüfen, ob sie diesen Tab bezeichnet:
`GET /v1/orchestrator/whoami?conversation_id=<id>` antwortet mit `terminal_id`.

**4. Dispatche**, mit dem Orchestrator-Token:

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

Sende den Body über stdin (`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`),
damit das Geheimnis nicht in argv steht.
Die Antwort ist `{ok, task, warnings?}`. Lies `warnings`: `claims_overlap`, `claims_missing`,
`claims_ignored_for_worktree`, `dirty_worktree_base` und `work_not_placed` (das genannte Item ließ sich
noch nicht aufs Board bringen; der Durchlauf des Boards erledigt das innerhalb eines Takts). Postest du
dieselbe ID erneut, antwortet der Daemon mit der gespeicherten Aufgabe und `replayed: true`; eine
Wiederholung ist also sicher.

Ein Tab, der sich nicht öffnen lässt, antwortet trotzdem mit 200, mit `task.state: "spawn_failed"`.
`POST /v1/orchestrator/tasks/<id>/respawn` (Orchestrator-Token) öffnet eine Kopie mit neuem Geheimnis,
höchstens zweimal pro Original.

Das falsche Child dispatcht – das falsche Briefing, der falsche Umfang oder dieselbe Arbeit doppelt?
Warte nicht, bis es fertig wird oder in die Zeitüberschreitung läuft, während es einen Slot und seine
Schreibpfade hält: `clawdline task cancel <id>
--reason "…"` stoppt es sofort (§5).

**Ablehnungen, denen du begegnen wirst**, in der Reihenfolge, in der sie geprüft werden:

| Status | Code | Was zu tun ist |
|---|---|---|
| 409 | `task_unreadable` | Eine Aufgabe mit dieser ID ist gespeichert, lässt sich aber nicht lesen; sende nicht erneut unter derselben ID |
| 422 | `bad_task` | Die Meldung nennt das Feld. Schließt „No readable task.json under …“ ein – prüfe `task_root` |
| 422 | `claims_required` | Füge `claims` hinzu |
| 422 | `root_session_required`, `root_assistant_required` | Füge `root.session_id` und `root.assistant` hinzu |
| 403 | `session_scope_mismatch` | Prüfe, dass `root.project_dir` vorhanden ist und zum Project und zur Rollen-Momentaufnahme der Root-Session passt. Korrigiere das Briefing oder die CLI; bitte den Menschen nicht, Project-Einstellungen zu ändern. |
| 422 | `detached_route_required` | Du hast `root.poll_only` gesendet; das ist losgelöste Automatisierung (§6) |
| **409** | **`stale_inventory`** | Deine `generation` fehlt oder ist alt. Der ganze aktuelle Bestand steckt in der Fehlermeldung: Lies ihn, entscheide neu und sende erneut mit seiner `generation` |
| 422 | `work_not_found`, `work_other_project`, `work_closed` | Die genannte `work_id` ist kein Item, gehört zu einem anderen Project oder ist geschlossen |
| 422 | `also_work_not_found` | Eine ID in `also_work_ids` ist kein Board-Item; sie wird danach wie `work_id` geprüft |
| 503 | `store_unavailable` | Das Board ließ sich zum Prüfen des genannten Items nicht lesen; nichts wurde gestartet, sende erneut |
| 409 | `graph_*` | Eine Aufnahmeregel des Aufgabengraphen (das Feld `graph`) |
| 409 | `no_child_capability` | Diese Plattform kann kein Child öffnen; `missing` sagt, was fehlt |
| 429 | `squad_launch_capacity` | Zu viele Persona-Starts warten noch auf ihre Session; versuche es später erneut |
| 429 | `rate_limited` | Zu viele Dispatches in zehn Minuten |
| 422 / 409 | `root_unresolved`, `conversation_ambiguous` | Deine Gesprächs-ID passt zu keiner laufenden Session oder zu mehr als einer. Korrigiere sie; wechsle nicht zu losgelöster Arbeit |
| 403 | `session_actor_required` | Ein mit einer Rolle geöffneter Root muss mit der Squad-Berechtigung seiner eigenen Session dispatchen, aus dieser Session heraus |
| 403 | `session_scope_mismatch` | Auch hier: Das Project der Root-Session passt nicht zu ihrer Rollen-Momentaufnahme |
| 503 | `squad_policy_unavailable` | Die Einstellungen der Rollenzuweisung ließen sich nicht lesen; nichts wurde gestartet |
| 409 | `persona_disabled_for_auto_assignment` | Diese Persona ist im Ziel-Project für die automatische Zuweisung abgeschaltet |
| 429 | `over_capacity` | Deine Child-Slots (Standard 5) oder die des Rechners sind voll; `retry_after` |
| 409 | `workspace_busy` | Die Schreibpfade eines anderen Roots überschneiden sich; der Fehler nennt die blockierende Aufgabe |
| 409 | `worktree_unavailable` | Der private Checkout ließ sich nicht anlegen |
| 429 | `terminal_busy` | Jede Spur zum Schreiben ins Terminal ist belegt; `retry_after: 5` |

## 5. Während es läuft und wenn es fertig ist

Das Child quittiert sein Briefing (`clawdline task accept`, das `/accepted` postet oder
`accepted.json` hinterlässt), darf eine Fortschrittsnotiz senden, wenn sich sein Plan ändert
(`/progress`), darf bis zu fünf Benachrichtigungen schicken (`/notify`) und schließt ab, indem es
`result.json` schreibt und `clawdline task finish` ausführt. Du rufst diese Routen nicht auf.

- `clawdline task show <id>` – eine Aufgabe mit ihrem Zustand (`GET /v1/orchestrator/tasks/<id>`).
  `GET /v1/orchestrator/tasks` listet sie auf (`?state=`, `?limit=` bis 500).
- **Wenn es fertig ist, tippt der Daemon eine Zeile `<clawdline-notice>` in dein Eingabefeld.** Ihr `body`
  ist ein kurzer Satz: die Aufgabe, wie sie geendet hat, die Fakten, die nur diese Lieferung betreffen (ein
  Hängenbleiben, freigegebene Schreibpfade, ihr Branch, wie viele offene Reste) und der eine Befehl, den du
  ausführen sollst, `clawdline task show <id>`, der die Meldung schließt, sobald er die Aufgabe ausgegeben
  hat. Ihr JSON (Version 3) trägt `task`, `state` und `notice_id` sowie `outstanding`, `leftovers` und
  `claims_released` nur dann, wenn sie etwas aussagen; das Ergebnis ist, was `task show` ausgibt. Eine
  Zeile, die sich nicht tippen ließ, wird in wachsenden Abständen von 5 bis 300 Sekunden erneut versucht.
  Steht sie auf deinem Bildschirm und du hast die Aufgabe nicht gelesen, wird sie nicht noch einmal
  vollständig getippt: Stattdessen kommt eine kurze Zeile `task_reminder`, die denselben Befehl nennt, in
  wachsenden Abständen von 2 bis 30 Minuten – insgesamt acht Mal, dann gibt sie auf. Sie tippt nie,
  während du ein Menü anzeigst. Ein Menü verbraucht keines dieser acht Male: Die Zeile wartet bis zu
  12 Stunden und wird getippt, sobald das Menü verschwunden ist. Die Route, die `task show` sendet:

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  `clawdline task show <id>` und `clawdline task wait <id>…` senden sie für eine fertige Aufgabe, nachdem
  sie sie ausgegeben haben; `clawdline task ack <id> <notice_id>` sendet sie von Hand und gibt eine Zeile
  aus. Ein zweites ACK antwortet `changed: false`. Nicht bestätigte Meldungen werden unter
  `GET /v1/orchestrator/completions` aufgelistet; `POST /v1/orchestrator/completions/reconcile` macht sie
  wieder scharf. Eine Meldung, die aufgegeben hat, wird noch einmal getippt, sobald deine Session das
  nächste Mal untätig ist.
- **Vielleicht siehst du die Zeile nie, und du erfährst trotzdem davon.** Dein eigenes
  `GET /v1/work/v2/agent/session-todos/<conversation id>`, das du an jeder Turn-Grenze liest,
  listet `unacknowledged_completions` auf – jedes deiner Children, das fertig ist und das du nicht
  bestätigt hast, mit `task_id`, `title`, `state`, `kind`, `result_path`, `notice_id` und `ack_path`,
  gleichgültig, ob seine Meldung noch aussteht oder aufgegeben hat. `clawdline session report` gibt sie
  nach seiner Quittung aus. Für jedes: `clawdline task show <id>`, dann integriere es; das Lesen ist das
  ACK und nimmt es von beiden Listen. `task show` gibt die Zusammenfassung vollständig aus und zählt, was
  es weglässt; `--json` ist die ganze Antwort des Daemons, Symbole und Artefakte eingeschlossen. Lies
  `result.json` selbst nur, wenn das nicht reicht.
- **Eine Lieferung, die offene Reste nennt** – Dinge, die das Child nach eigener Aussage nicht getan hat –,
  ändert von sich aus nichts. `task show` listet ihre Titel auf. Um einen davon dem Menschen vorzulegen:
  `POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}`;
  der Mensch antwortet mit „verfolgen“, „später“ (Backlog) oder „nein“, und bis dahin landet nichts auf seinem Board.
- **Ein Child, das direkt nach seinem Briefing stehen bleibt, wird angestupst und dann gemeldet.** Hat ein
  Child, dessen Briefing getippt wurde, nicht quittiert und zeigt sein Bildschirm Untätigkeit – Prompt
  gezeichnet, Eingabefeld leer, kein Menü, keine Arbeitszeile – für 5 Minuten, tippt der Daemon ihm eine
  Zeile, die seine `CHILD.md` nennt (niemals das Geheimnis). Ist es 5 Minuten später immer noch nicht
  quittiert und untätig, endet die Aufgabe mit `spawn_failed` und einem Urteil, das sagt, dass sie
  hängen geblieben ist, und du bekommst eine Meldung mit `"kind":
  "task_stalled"` statt `task_finished`. Respawne es (`POST /v1/orchestrator/tasks/<id>/respawn`)
  oder dispatche erneut, dann bestätige mit ACK. Einem Child, das arbeitet, ein Menü zeigt oder quittiert
  hat, wird nie etwas getippt.
- **Brich ein versehentlich dispatchtes Child ab** – das falsche Briefing, der falsche Umfang, ein Duplikat:
  `clawdline task cancel <id> --reason "wrong brief"`
  (`POST /v1/orchestrator/tasks/<id>/cancel`, `{"reason":"…"}`). Der Grund ist erforderlich, höchstens 500
  Bytes. Die Aufgabe endet als `cancelled` mit dem Grund als Urteil, ihr Tab wird geschlossen, ihre
  Schreibpfade und ihr Child-Slot werden freigegeben, und du bekommst eine Meldung, dass sie abgebrochen
  wurde und warum. **Commits werden nicht weggeworfen:** Ein Child, das committet hat, behält seinen Branch
  und seinen Checkout, und sein Landing bleibt ausstehend, mit einer Notiz, wie viele Commits darauf
  liegen; `task show` und `clawdline landings` zeigen es. Merge davon, was du willst, oder zeichne es mit
  `clawdline task land <id> abandoned` auf. Nur die Root-Session, die die Aufgabe dispatcht hat, oder der
  Mensch über die Konsole darf sie abbrechen; jeder andere wird mit `403 not_task_root` abgelehnt, und eine
  mit einer Rolle geöffnete Session muss ihre eigene Berechtigung mitsenden (`session_actor_required`; der
  Befehl erledigt das für dich). Eine bereits beendete Aufgabe antwortet mit `409 task_already_terminal`
  und ihrem `state`; denselben Abbruch noch einmal auszuführen, liefert denselben Erfolg mit
  `replayed: true`. `clawdline task wait` endet mit 5, wenn eine Aufgabe, auf die es gewartet hat,
  abgebrochen wurde. Ansonsten endet eine Aufgabe, indem sie fertig wird, fehlschlägt oder in die
  Zeitüberschreitung läuft.
- **Ein fertiges Child ist kein gelandeter Code.** Seine Arbeit liegt im gemeinsamen Baum oder auf seinem
  Branch, bis du sie integrierst.

## 5a. Auf einen langen Befehl warten: ein Callback

Ein Deployment, ein CI-Lauf, ein Release, das sich verbreitet, ein langer Build: alles, dessen Antwort
„später“ lautet. Warte nicht in deinem Turn darauf und frag es nicht aus späteren Turns ab. Übergib es
dem Daemon:

```sh
clawdline callback --title "CI is green on <sha>" --timeout 45m -- gh run watch <run id> --exit-status
```

Er gibt `callback <id> briefed` aus und kehrt zurück. **Beende deinen Turn.** Wenn der Befehl endet,
tippt der Daemon eine `<clawdline-notice>`, deren Body lautet:
`callback <first 8 of id> finished: success (exit 0 after 6m) — run clawdline task show <id>`; `task show`
gibt aus, wie er geendet hat – seinen Exit-Status und die letzten Zeilen seiner Ausgabe – und schließt die
Meldung, genau wie bei einem Child.

- Es ist eine Aufgabe von dir ohne Tab: `clawdline task cancel <id> --reason "…"` stoppt die ganze
  Prozessgruppe des Befehls; nach Ablauf von `--timeout` (1m bis 4h, Standard 30m) wird er gestoppt und als
  `timeout` abgeschlossen. Ein laufender Callback verhindert, dass deine Session geschlossen wird, so wie
  ein laufendes Child.
- Der Befehl läuft wörtlich, ohne Shell; schreib `sh -c '…'`, wenn du eine brauchst. Er läuft in diesem
  Verzeichnis (`--dir` für ein anderes) und bekommt aus deiner Umgebung nur PATH, HOME, Locale, USER, SHELL,
  TMPDIR und TERM: niemals Zugangsdaten. Ein Befehl, der welche braucht, liest sie aus seiner eigenen Datei.
- Seine Ausgabe liegt im Verzeichnis der Aufgabe, `output.log`, und wird nach seinem Ende 7 Tage aufbewahrt.
- Er wird einmal ausgeführt. Startet der Daemon in der Zwischenzeit neu, nimmt er den Befehl wieder auf; ein
  Befehl, der endete, während kein Daemon ihn beobachtete, und keinen Exit-Status hinterließ, wird als
  `failure` mit unbekanntem Ausgang abgeschlossen und **nicht** erneut ausgeführt. Starte ihn selbst neu,
  wenn das sicher ist.
- Ein unsicherer Start – die CLI konnte den Daemon nicht erreichen – wird mit
  `--task-id <the id it printed>` wiederholt: Dieselbe ID wird nie zweimal gestartet.
- Ein Callback belegt keinen Child-Slot. Höchstens 8 laufen pro Session und 16 pro Rechner; einer mehr wird
  mit `429 callback_capacity` und einem `retry_after` abgelehnt. Ein Dispatch kann sich nicht selbst als
  `kind callback` ausgeben (`bad_task`). Windows lehnt mit `501 no_callback_capability` ab.

## 6. Landing und die drei anderen Arten von Arbeit

**Nachdem du den Branch eines Childs gemergt hast, tu nichts weiter**: Der Broker zeichnet `landed` selbst
auf (siehe unten). Ein Child, das keine Schreibpfade deklariert hat (`--claims ""`), von selbst fertig
wurde und weder auf seinem Branch noch in seinem Checkout etwas hinterlassen hat, wird vom Broker als
`nothing_to_land` aufgezeichnet, bevor seine Meldung getippt wird. Ein von Hand aufgezeichnetes Landing ist
für das, was keiner dieser beiden Fälle abdeckt – ein Cherry-Pick, eine `incorporated`-Lieferung,
`nothing_to_land`, `abandoned` –, und es ist ein einziger Befehl, gesendet mit dem Orchestrator-Token:

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

Es ist diese Route, die ein Skript mit dem Header `X-Clawdline-Orchestrator` aufrufen kann:

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- Nur diese Schlüssel, dazu `delivery`, das angenommen und nicht verwendet wird; jeder andere Schlüssel
  wird abgelehnt. `pending` und `abandoned` nehmen das Aufgabengeheimnis oder das Orchestrator-Token an;
  `landed`, `incorporated` und `nothing_to_land` nehmen nur das Orchestrator-Token an.
- `landed` braucht `target` und `commit`; `incorporated` braucht `target`, `commit` und
  `carrier_task`, die andere Aufgabe, deren verifiziertes Landing diese Lieferung mitgebracht hat. Der
  Daemon **prüft beides in Git**; andernfalls `409 unverified_landing` mit einem dieser `reason`-Werte:
  `commit_unresolved`, `target_unresolved`, `not_on_target`, `base_unknown`, `predates_dispatch`,
  `delivery_unknown`, `nothing_delivered`, `not_the_delivery` und für `incorporated`
  `carrier_required`, `carrier_is_delivery`, `carrier_unresolved`, `carrier_not_landed`,
  `carrier_repository_mismatch`, `carrier_target_mismatch`, `carrier_commit_mismatch`,
  `delivery_is_ancestor`.
- `nothing_to_land` wird mit `409 wrote_to_repository` abgelehnt, wenn die Aufgabe doch geschrieben hat.
- Ein abgeschlossenes Landing kann sich nicht mehr ändern: `409 invalid_transition`, oder
  `409 landing_conflict` bei einem anderen Wert.
- **Ein Merge zeichnet sich selbst auf.** Sobald der Branch einer fertigen Aufgabe in ihr Ziel gemergt
  ist, zeichnet der Broker innerhalb weniger Minuten von selbst `landed` auf, über dieselbe Git-Prüfung,
  mit dem Head des Ziels als Commit. Ist kein Ziel aufgezeichnet, nennt er nur dann eines, wenn der Branch
  des primären Checkouts der einzige Branch ist, der die Lieferung enthält. Einen Cherry-Pick, eine
  `incorporated`-Lieferung und `nothing_to_land` musst du weiterhin selbst aufzeichnen.
- **Die Abschlussmeldung nennt den Zustand des Branches beim Ende der Aufgabe**, und jeder Zustand
  verlangt eine Sache, als Befehl. *Auf seinem Branch ist nichts committet*: Ein Landing wird von diesem
  Branch aus nachgewiesen; so, wie der Branch jetzt ist, könnte also nie etwas als gelandet aufgezeichnet werden –
  committe in seinem Checkout auf diesem Branch, solange der Checkout noch auf der Festplatte liegt, oder
  `clawdline task land <id> abandoned`. *Auf seinem Branch committet*: Merge diesen Branch in sein Ziel;
  der Merge zeichnet das Landing auf. *Konnte nicht gelesen werden*: Sieh dir den Branch an und zeichne es
  dann auf. *Hat in den gemeinsamen Checkout geschrieben*: `clawdline task land <id>
  landed` mit dem Commit, der diese Arbeit in ihr Ziel bringt, oder `abandoned`. *Es hat nichts
  geschrieben, und der Broker hat nothing_to_land aufgezeichnet*: Nur das ACK bleibt übrig.

`clawdline landings` (`GET /v1/orchestrator/landings`) ist jedes ausstehende Landing auf dem Rechner, jedes
mit einem `ownership.status`. `unknown` heißt nicht „niemand“: Es bedeutet, dass die Belege nicht gelesen
werden konnten. `503 landings_incomplete` bedeutet, dass einige Zeilen nicht gelesen werden konnten, und
an ihrer Stelle wird keine kürzere Liste angeboten.

`clawdline landings --work-id <item id>` (`GET /v1/orchestrator/landings?work_id=<item id>`) ist stattdessen
jedes für ein Board-Item **aufgezeichnete** Landing: `{"work_id", "landings": [...], "at"}`, jede Zeile mit
ihrer `id` und ihrer `source` – `task` (der landed- oder incorporated-Datensatz eines gebundenen Childs; die
ID ist die Aufgaben-ID), `root` (der Datensatz, den `item phase deploying --commit` geschrieben hat) oder
`phase_event` (eine Kopie, die ein älterer Daemon im Verlauf des Items aufbewahrt hat). Eine gebundene
Aufgabe, deren Datensatz sich nicht lesen lässt, ist eine Zeile mit `state: "unknown"` und wird nie
weggelassen. Das Lesen des Items trägt dieselben Zeilen als `landings`. Ein Item, das nicht existiert, ist
`404 work_not_found`.

**Zwei Roots, die in einen Checkout landen**, nehmen zuerst eine Landing-Lease (§11).

Die drei anderen Arten von Arbeit haben jeweils ihre eigene Route. Welche es ist, ist eine Grenze, kein Detail:

| Art | Route | Was es ist |
|---|---|---|
| **Übergabe** | `POST /v1/orchestrator/handoffs` | Du gibst eine bestehende Arbeitslinie mit ihrem vollständigen Zustand an eine neue Session |
| **Root-Zuweisung** | `POST /v1/orchestrator/root-assignments` | Ein neuer, eigenständig verantwortlicher Root für ein neues Feature |
| **Losgelöste Automatisierung** | `POST /v1/orchestrator/detached-tasks` | Unbeaufsichtigte Arbeit, ohne jemanden, dem sie berichtet |

**Übergabe.** Schreib zuerst `<state dir>/handoffs/<handoff_id>/handoff.md` (die Listen-Route antwortet mit
`package_root`). Die Datei sollte drei Überschriften tragen: **REFERENCES** (alles, was der Empfänger
lesen muss), **VERIFICATION** (Fragen, die er aus diesen Quellen beantwortet, bevor er weitermacht) und
**OPEN THREADS** (wo er weitermacht). Poste dann mit einem geschlossenen Body:

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

Der Empfänger wird angewiesen, die Datei zu lesen, ihre Verweise durchzugehen, ihre Verifikationsfragen
zu beantworten und weiterzumachen. Beim Öffnen erfasst die Übergabe die offenen Board-Items des Absenders
in diesem Project. Sobald der Empfänger eine Gesprächs-ID hat und sein erster Gesprächsdatensatz
beobachtet wurde, gehen die aktiven Zuweisungen und Eigentümer dieser Items in einer einzigen
Transaktion auf den Empfänger über. Eine gescheiterte Übergabe lässt die Eigentümerschaft beim Absender.
Ein Item, das bereits geschlossen ist, an jemand anderen übergegangen ist oder gerade gesondert zugewiesen
wird, bleibt unberührt. Ein Item mit Verifikations-Gate in verifying oder merging kehrt zu implementing
zurück, sodass sein neuer Eigentümer es erneut verifizieren muss. Du bekommst eine Meldung
`handoff_receipt`, wenn die Übergabezeile getippt wird; diese Meldung allein beweist nicht, dass die
Übertragung auf dem Board stattgefunden hat. Ablehnungen: `bad_task` (einschließlich einer fehlenden oder
leeren `handoff.md`), `sender_not_found`, `sender_ambiguous`, `rate_limited`,
`terminal_busy` und `succession_required`, wenn du die Rolle des Rechner-Koordinators hältst – die
Nachfolge ist in diesem Daemon nicht verfügbar (`501`), also kann diese Session nicht übergeben.

**Meilenstein-Übergabe.** Ein lange laufender Root, der einen Meilenstein erreicht hat, übergibt mit
`clawdline handoff --summary summary.md`, damit die Arbeit danach nicht bei jedem Aufruf alles davor neu
liest. Die Zusammenfassung hat genau fünf `## `-Überschriften – Goal, Verified decisions, Blockers,
Evidence (Pfade, Commits, IDs oder `clawdline`-Befehle zum Öffnen, nicht deren Inhalt), Next step –,
höchstens 6 KiB, ohne Zugangsdaten und ohne Gesprächstext; `--check` listet jedes Problem auf, ohne etwas
zu öffnen, und der Daemon lehnt dieselben als `bad_milestone_summary` ab. Der Daemon schreibt
`obligations.md` daneben: deine Board-Items (sie wandern mit, eine noch unbeantwortete Entscheidung des
Menschen eingeschlossen) sowie deine laufenden Children, nicht bestätigten Meldungen und ausstehenden
Landings (sie bleiben deine). Bestätige und lande diese also nach der Übergabe weiter und führe dann
`clawdline session close` aus, sobald es `safe` meldet. Die Übergabe ist deine Entscheidung:
`clawdline usage --compare-handoff` sagt, ob sie sich auf diesem Rechner ausgezahlt hat, und sie wird nie
erzwungen.

**Root-Zuweisung.** Der Header `Idempotency-Key` muss gleich `request_id` sein:

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

Jedes Zuweisungsfeld ist 1–8192 Bytes groß, insgesamt 32 KiB. Der Daemon schreibt das Briefing selbst und
öffnet die Session. Sie hat **kein Elternteil, kein Geheimnis, kein Zeitlimit, kein Ergebnis und kein
Landing**: Niemandem wird gesagt, wann sie fertig ist, weil sie niemandem berichtet. Ablehnungen:
`bad_root_assignment`, `idempotency_mismatch`, `request_conflict`, `rate_limited`. Täusche eine
Root-Zuweisung nie mit einem Child, einer losgelösten Aufgabe oder einer Übergabe vor.

**Losgelöste Automatisierung.** Wie ein Dispatch (§4) – `task.json` unter `task_root`, dann
`{"task_id", "secret", "inventory_generation"}` –, aber der Root des Briefings muss
`{"session_id": null, "poll_only": true}` sein, sonst wird er als `detached_task_required` abgelehnt.
Niemand wird benachrichtigt; frag `GET /v1/orchestrator/tasks/<id>` ab und lies `result.json`. Sie ist
nie ein Root und nie Eigentümer eines Features.

### Künftige Arbeit planen

Clawdline Next verwaltet geplante Arbeit selbst. Verwende nicht die außer Dienst gestellte App, nicht
`cron` und keine Kette losgelöster Aufgaben. Lies `GET /v1/orchestrator/schedules`; lies einen einzelnen
vollständig unter `GET /v1/orchestrator/schedules/<id>`. `GET /v1/places` liefert die `place_id`, die ein
Schreibvorgang nennt.

Ein einmaliger Zeitplan (`on`) darf direkt mit dem Orchestrator-Token angelegt werden. Ein wiederkehrender
Zeitplan (`days`) ist eine dauerhafte Anweisung und braucht die ausdrückliche Anweisung des Menschen aus
dieser Session:

1. Lies `GET /v1/orchestrator/sessions/<conversation>/run`. Das ist der jüngste Lauf, der ausgestellt
   wurde, als der Mensch dieser Session seine Nachricht über Clawdline geschickt hat.
2. `POST /v1/orchestrator/schedules` mit einem `Idempotency-Key` und dem gewöhnlichen Zeitplan-Body sowie
   diesem Gespräch und diesem Lauf:

```json
{"title":"Morning sweep","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"Inspect the overnight failures and report actionable findings.",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

Derselbe Nachweis berechtigt zu `PATCH /v1/orchestrator/schedules/<id>` (sende den ganzen Zeitplan-Body)
und `DELETE /v1/orchestrator/schedules/<id>` (sende die beiden Nachweisfelder als JSON-Body), wenn der
Mensch ausdrücklich um genau diese Änderung gebeten hat.
`POST /v1/orchestrator/schedules/<id>/run` führt einen sofort aus. Lies den angelegten oder geänderten
Zeitplan zurück, bevor du Erfolg meldest.

Ohne `via` bleibt das Orchestrator-Token auf einen einmaligen `on`-Zeitplan beschränkt. Ein erfundener,
abgelaufener oder zu einer anderen Session gehörender Lauf wird als `run_unknown`, `run_expired` oder
`run_other_session` abgelehnt; ein fehlerhafter Nachweis ist `invalid_user_authorization`. Hat der Mensch
direkt in ein Terminal getippt, gibt es keinen Lauf: Bitte ihn, die Anweisung über Clawdline zu schicken.
Verwende einen Lauf nie als Pauschalerlaubnis für Arbeit, um die die Nachricht des Menschen nicht gebeten
hat. Dieser Nachweis macht die Weiterleitung prüfbar; er macht das rechnerweite Orchestrator-Token nicht
zu sessionspezifischen Zugangsdaten.

Ein Zeitplan darf keine Uhrzeit haben: Sende `"trigger_only": true` statt `at`, `days` und `on`. Die Uhr
führt ihn nie aus; er läuft nur über `…/run` oder über seinen Webhook, und nur solange er `enabled` ist.
Er ist eine dauerhafte Anweisung wie ein wiederkehrender Zeitplan und braucht denselben Nachweis.

**Eine Aufgabe auf einem anderen Rechner starten und erfahren, wie sie endete.** Es gibt keinen Kanal von
Rechner zu Rechner. Mach die Aufgabe auf dem Zielrechner zu einem Zeitplan, der nur ausgelöst wird
(trigger-only), binde einen Cloud-Webhook daran und lege die URL dort ab, wo der Aufrufer sie lesen kann
(sie ist eine Zugangsberechtigung: eine Datei, die nur du lesen kannst, oder `CLAWDLINE_WEBHOOK_URL`;
niemals ein Befehlszeilenargument). Dann, auf dem aufrufenden Rechner:

```sh
clawdline webhook fire --url-file <path>
```

Er sendet `{"deliver_within_seconds": 60}` (`--deliver-within`), damit ein ausgeschaltetes Ziel ihn nicht
später ausführt; er verfolgt den Status der Zustellung, bis sie endet oder `--timeout` (60m) verstrichen
ist, gibt Änderungen auf stderr und eine abschließende Zeile auf stdout aus. Exit `0`: erfolgreich · `1`:
ohne Erfolg beendet (failure, timed_out, cancelled, spawn_failed) · `2`: hat den Rechner nie erreicht
(expired, canceled, unreachable) · `3`: abgelehnt (dispatch_refused samt Code, die URL nicht verfügbar,
Ratenbegrenzung) · `4`: Warten aufgegeben, mit dem zuletzt gesehenen Zustand. `--no-wait` gibt die
Zustellungs-ID aus und kehrt nach dem `202` zurück. Melde den Exit-Code und die letzte Zeile so, wie sie
sind: `2` bedeutet, dass nichts gelaufen ist, nicht, dass etwas fehlgeschlagen ist.

Eine geplante Aufgabe, die Daten für etwas liest, das auf Verifikation wartet (驗收 in der Seitenleiste,
docs/verifications.md), schreibt ihre Auswertung als Notiz an diesen Datensatz, mit ihrem eigenen
Aufgabengeheimnis – nicht mit dem Orchestrator-Token, das sie gar nicht haben sollte:

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<readout>"}'
```

Sie landet nur an einem Datensatz, dessen `schedule_id` der Zeitplan ist, der diese Aufgabe gestartet hat
(sonst `schedule_mismatch`), ist mit `task:<task id>` signiert und wird für eine Aufgabe, die kein Zeitplan
gestartet hat, als `not_scheduled` abgelehnt. Die Datensatz-ID findest du mit `clawdline verify list`.

## 7. Deinen eigenen abgeschlossenen Turn melden

Wenn dein Turn wirklich abgeschlossen ist – die Arbeit erledigt, verifiziert und, wo das zutrifft,
committet –, mach dies zu deiner letzten Aktion vor der abschließenden Antwort:

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

Das zeichnet ein Häkchen in die Zeile deiner Session: **geliefert, wartet auf Freigabe**. Es ist schwächer
als ein Landing und verlangt kein Review. Es ist sichtbar, solange der Daemon die Session als untätig
liest – arbeitend, wartend oder ein unlesbarer Bildschirm haben Vorrang – und nur, solange dieses Terminal
dasselbe Gespräch enthält.

- **Nur für einen abgeschlossenen Turn.** Nicht für Teilarbeit, eine Diagnose, eine Blockade oder eine
  Rückfrage an den Menschen. Ein Child sendet es nie (`409 child_session`).
- Der Befehl findet dein Gespräch über `CLAUDE_CODE_SESSION_ID` oder `CODEX_THREAD_ID`
  (sonst `--conversation`), fragt `GET /v1/orchestrator/whoami` nach dem Terminal und postet
  `{"summary"}` an `POST /v1/orchestrator/sessions/<terminal>/complete`.
- Die Zusammenfassung hat 1–500 Zeichen. Jeder Aufruf ist eine neue Quittung; die neueste zählt.
- Die Antwort trägt außerdem `open_todos`: die direkten To-dos dieser Session, die gesendet oder gelesen
  und nicht erledigt sind, die ältesten zuerst, höchstens 20 (`open_todos_truncated`, wenn es mehr sind).
  Der Befehl gibt sie nach der Quittung auf stderr aus, eine ID und einen Text pro Zeile. Hake jedes, das
  du erledigt hast, mit `clawdline todo done <id>` ab. Die Quittung wird in jedem Fall aufgezeichnet, und
  der Exit-Status ändert sich nicht; `open_todos_unknown: true` bedeutet, dass sie nicht gelesen werden
  konnten, nicht, dass keine offen sind.
- Ablehnungen: `conversation_id_malformed` (keine UUID in Kleinbuchstaben), `conversation_not_found`,
  `conversation_ambiguous`, `registry_stale`, `session_not_found`, `session_unbound`,
  `child_session`. Melde die Ablehnung ehrlich; ein Satz im Chat ist keine Quittung.

**Hinterlasse dem Menschen einen Statusbericht.** Hat der Turn Dateien in einem git-Project geändert, gib
dem Menschen eine Seite, auf der er sieht, wo die Arbeit steht, und jede Datei lesen kann, die der Turn
hinzugefügt oder geändert hat. Es ist eine lokale HTML-Datei: Nichts wird hochgeladen, und sie lädt nichts nach.

```sh
clawdline report --repo <project> --status status.md [--notes notes.txt] [--lang zh-TW] [--open] <commit>…
```

- Nenne **die eigenen Commits dieses Turns, die ältesten zuerst**. Jeder wird einzeln gelesen, sodass
  Commits einer anderen Session dazwischen außen vor bleiben; übergib nie einen Bereich.
- `status.md`: ein optionaler `# Title`, eine optionale Zeile darunter, dann eine `## `-Überschrift pro
  Karte – beginne sie mit ✅, 🟡 oder ❌ – und ein kurzer Markdown-Text.
- `--notes`: ein `path: sentence` pro Zeile, angezeigt über der jeweiligen Datei. `--pin` (wiederholbar)
  stellt eine Datei an den Anfang; `CLAUDE.md` und `AGENTS.md` werden angeheftet, wenn der Turn sie
  berührt hat. `--exclude` lässt einen Pfad weg und sagt das. `--at` ist die Revision, deren Inhalt
  angezeigt wird (Standard `HEAD`).
- Er legt den Bericht unter `<state dir>/reports/<date>-<id>/report.html` ab, außerhalb jedes Repositorys,
  und gibt zwei Adressen aus: **zuerst die `file://`-Adresse**, die ein Terminal öffnet, **dann
  `http://127.0.0.1:<port>/reports/<id>`**, die der Daemon dieses Rechners beantwortet. Setze beide in
  deine abschließende Antwort: Die Konsole zeigt eine `file://`-Adresse als Text, den sie nicht öffnen
  kann, und macht die `http://`-Adresse zu einem Link. Diese Adresse öffnet sich nur in einem Browser auf
  diesem Rechner, der in seiner Konsole angemeldet ist; ein Telefon oder ein Cloud-Betrachter wird
  abgelehnt (`report_not_over_cloud`, `report_local_only`).
- `--out` schreibt stattdessen eine andere Datei oder ein anderes Verzeichnis, nur mit der
  `file://`-Adresse. stderr sagt, was weggelassen oder gekürzt wurde. `--open` öffnet die Datei außerdem
  im Browser dieses Rechners.

## 8. Mit einer anderen Session sprechen

**Finde sie.** `GET /v1/orchestrator/sessions` ist das Adressbuch: die `id` jeder Session (ihre
Terminal-ID), `label`, `assistant`, `cwd`, `state`, `work_state` und `taskId` für ein laufendes Child.

**Senden.**

```sh
clawdline send --to <terminal id> "text"        # or text on stdin
```

Das ist `POST /v1/orchestrator/messages` mit `{from_session, to_session, text}` und einem
`Idempotency-Key`. Der Daemon tippt die Nachricht in das Eingabefeld des Empfängers, in einem Umschlag
`<clawdline-message>`, der dich als Quelle nennt.

- `to_session` ist eine **Terminal-ID**: Eine Nachricht folgt dem Tab, den du gemeint hast, nicht einem
  Gespräch. `from_session` ist deine Terminal- oder Gesprächs-ID (der Befehl trägt sie ein).
- Nur Text, höchstens 100.000 Zeichen. Es gibt kein Feld `images`: Ein mitgesendetes wird stillschweigend
  verworfen.
- `ok` bedeutet, dass die Bytes ein Eingabefeld erreicht haben, nicht, dass jemand sie gelesen hat.
- Es kann einige zehn Sekunden dauern: Der Daemon liest vor dem Tippen jede Session auf dem Rechner (etwa
  30 s pro Weiterleitung auf einem Mac, gemessen am 2026-09-19). Der Befehl wartet bis zu zwei Minuten.
  Brich ihn nicht vorzeitig ab – eine mittendrin abgebrochene Weiterleitung kann tippen, ohne
  aufgezeichnet zu werden, und derselbe Schlüssel antwortet dann mit `409 request_in_progress`.
- Der Befehl gibt zuerst seinen `Idempotency-Key` aus. Scheitert der Aufruf unterwegs, führe ihn mit
  `--key <that key>` erneut aus: Derselbe Schlüssel und derselbe Body werden einmal getippt. Derselbe
  Schlüssel mit einem anderen Body ist `409 idempotency_key_reused`.
- Ablehnungen: `source_not_found`, `target_not_found`, `same_session`, `target_busy` (der Empfänger zeigt
  ein Menü; nichts wurde getippt), `terminal_busy`, `delivery_failed`.

**Zeig dem Menschen ein Bild.** Füge keinen lokalen Pfad ein: Auf einem Telefon öffnet er nichts.

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- Nur mit dem Orchestrator-Token. Eine bis sechs lokale Dateien; jede muss eine gewöhnliche Datei sein,
  höchstens 12 MiB groß und höchstens 12.000 px pro Seite. PNG, JPEG und GIF werden direkt gelesen; andere
  Formate laufen unter macOS durch `sips`.
- Die Antwort listet `artifacts` auf, jedes mit einem `marker` wie `<clawdline-image id="…">`. **Setze den
  Marker in deine Antwort**; die Konsole zeigt das Bild dort, wo der Marker steht. Bilder werden 24 Stunden
  aufbewahrt.

## 9. Dem Menschen Bescheid geben

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

`POST /v1/orchestrator/notify`. Nur für etwas, worauf der Mensch wartet: Der Wert einer Push-Nachricht
liegt darin, dass sie selten ist. Mit `--session <terminal>` öffnet ein Tippen darauf diese Session.

- `409 agent_notify_disabled`: Der Mensch hat Agent-Benachrichtigungen ausgeschaltet. Nicht deine Schuld;
  versuche es nicht erneut.
- `409 not_subscribed`: Kein Gerät hat Push-Nachrichten abonniert.
- `429 rate_limited`: 30 pro Stunde für den ganzen Rechner, geteilt mit den Benachrichtigungen jedes Childs.
- `502 push_failed`: Der Push-Dienst hat abgelehnt; `sent` und `failed` stehen im Fehler.

## 9a. Eine Notiz für menschliches Eingreifen hinterlassen

Verwende eine Notiz, wenn ein lange laufender Agent eine konkrete Sache hat, die der Mensch lesen, tun oder entscheiden sollte, und eine gewöhnliche Chat-Nachricht im Verlauf untergehen könnte. Die Notiz bleibt im eingeklappten Aufmerksamkeitsbereich der Ziel-Session, mit einem roten Punkt markiert, bis der Mensch sie auf „erledigt“ verschiebt. Du kannst dann mit unabhängiger Arbeit weitermachen; der Mensch kann an einem natürlichen Haltepunkt zurückkommen. Eine Notiz ist kein Fortschrittsprotokoll, keine private Erinnerung, keine Benachrichtigung und keine Ermächtigung für eine Board-Entscheidung. Vermeide doppelte Notizen für dieselbe Bitte.

**Bevor du den Menschen im Chat um eine Wahl bittest**, lege eine `answer`-Notiz an, die die eigentliche Frage, die für die Entscheidung nötigen Abwägungen und zwei bis vier vollständige vorgeschlagene Antworten enthält. Ein Tippen auf eine Schaltfläche sendet ihre Antwort als Gesprächsnachricht, also mach jeden `draft` für sich allein eindeutig. Nach dem Anlegen genügt ein kurzer Hinweis im Chat. Werte weder das Anlegen der Notiz noch eine als erledigt markierte Notiz als Antwort des Menschen; warte auf die Gesprächsnachricht – eine angetippte Antwort oder eine, die der Mensch getippt hat –, bevor du danach handelst. Schlägt das Anlegen fehl, sag das und stell die Frage direkt. Behalte Notizen Entscheidungen vor, die menschliches Urteil brauchen, nicht Routinewahlen, die der Agent selbst treffen kann.

Lege eine mit einer JSON-Body-Datei an. Ohne `--target` löst die CLI die Terminal-ID dieses laufenden Roots selbst über `whoami` auf. Für eine andere Session verwende deren aktuelle **Terminal-ID** aus dem Adressbuch (`clawdline guide de send`) als `--target`. `--from` ist standardmäßig die Gesprächs-ID dieses laufenden Roots aus der Umgebung. Die CLI liest die Zugangsdaten des Rechners, ohne sie in die Befehlszeile zu setzen, fügt die Quell- und Ziel-IDs ein und gibt die dauerhafte Notiz-ID des Daemons aus. Verwende nach einem unsicheren Ergebnis den ausgegebenen `--key` erneut.

```json
{"kind":"answer","title":"Choose a date","summary":"One release date needs your choice.","action":"Choose a date when you have a moment.","reason":"Only you can choose it.","options":[{"label":"Tuesday","draft":"Tuesday works for me."},{"label":"Wednesday","draft":"Wednesday works for me."}]}
```

```sh
clawdline note create --body-file note.json
# For another Session: clawdline note create --target <terminal-id> --body-file note.json
```

`kind` ist `read`, `answer`, `action` oder `report`; `title`, `summary`, `action` und `reason` sind erforderlich. Eine `answer` kann zwei bis vier Wahlmöglichkeiten anbieten. Jeder `draft` ist die vorgeschlagene Antwort, die auf seiner Schaltfläche steht. Tippt der Mensch darauf, sendet die Konsole sie sofort als Gesprächsnachricht an die Session der Notiz, gefolgt von einer Kontextzeile mit Notiz-ID, Titel und Aktion, damit die empfangende Session weiß, auf welche Bitte der Mensch geantwortet hat; der Kontext wird auf der Schaltfläche nicht angezeigt. Erst nachdem das Senden gelungen ist, wandert die Notiz zu „kürzlich erledigt“. Scheitert das Senden, bleibt die Notiz ausstehend, und das Aufmerksamkeitselement sagt, dass die Antwort nicht gesendet wurde. `detail` kann längeren Text aufnehmen. `document_url` kann auf ein echtes, lesbares Cloud-Dokument verlinken; prüfe die Dokument-Route und die Datei, bevor du es postest. Handle nach der Gesprächsnachricht, die ankommt, nicht nach dem Zustand der Notiz: Eine Notiz, die der Mensch von Hand als erledigt markiert hat, hat nichts gesendet. Ist deine Arbeit tatsächlich durch die Antwort blockiert, zeichne den Zustand „wartet auf den Menschen“ auf und sende die vorhandene Aufmerksamkeitsbenachrichtigung einmal. Eine sichtbare Notiz allein sendet keine Push-Nachricht und weckt keinen Agent.

## 10. Das Board

Das Board hat drei Strukturen – Board-Items, den Backlog und die eigene To-do-Liste jeder Session –, und
**was darauf kommt, entscheidet ein Mensch**. Eine Session legt ein Board-Item nur an, wenn die eigene
Nachricht des Menschen, über Clawdline gesendet, sie dazu auffordert; andernfalls schlägt sie vor. Sie
legt nie aus eigener Initiative eine Karte an. Die einzige Ausnahme ist der Eigentümer eines Epics: Nach
dem geprüften Plan des Epics darf er das Epic in Feature- und Issue-Items zerlegen und sie Sessions
zuweisen (`clawdline guide de epic`).

**TODO / 待辦 / 土度, zusammen mit einem Board-Item gesagt, meint die Schritte dieses Items.** Setze sie mit
`--step` an das Item. Schreibe sie **nicht** zusätzlich mit `clawdline todo add`. `clawdline todo add` ist
nur für eine Liste, die du auf Bitte des Menschen als eigene To-dos dieser Session ohne Board-Item
verfolgen sollst.

**Wenn der Mensch dich anweist, ein Board-Item anzulegen.** Nur wenn seine Nachricht – über Clawdline
gesendet, sodass sie einen Lauf hat – ausdrücklich um eines bittet, lege es selbst an:

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin] [--assign-self]
```

Durchgerechnetes Beispiel. Der Mensch schreibt: *„Make a Board item to clean up the release notes, TODO: draft
them, check the links, publish.“* Das ist ein einziger Befehl und nichts weiter:

```
echo "Clean up the release notes before the next release." | \
  clawdline item add --project <place id> --kind feature --title "Clean up the release notes" \
  --step "Draft the notes" --step "Check the links" --step "Publish"
```

- `item add` liest den jüngsten Lauf dieses Gesprächs (`GET /v1/orchestrator/sessions/<conversation>/run`),
  sofern `--run` keinen nennt, gibt seinen Idempotency-Key aus, bevor es fragt (`--key` wiederholt
  denselben Schreibvorgang), und gibt das angelegte Item mit der ID jedes Schritts aus. Es ist
  `POST /v1/work/v2/agent/items` mit `{"session_id", "via": {"run"}, "project_id", "kind",
  "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`,
  beantwortet mit `201` und `{"item", "assigned", "assignment_state"}`.
- Ein Feature, Issue oder Epic kommt standardmäßig **nicht zugewiesen** an, dort, wo die nicht
  zugewiesenen Items dieser Art des Menschen auf dem Board warten, mit seinen Schritten: den
  `--step`-Zeilen in ihrer Reihenfolge oder – wenn du keine angibst – zwei oder mehr Markdown-Listenzeilen
  der obersten Ebene aus der Beschreibung. Oft bittet dich der Mensch, Arbeit für später festzuhalten; das
  Anlegen des Items macht es nicht zu deinem. `assignment_state` ist `not_requested`.
- Füge `--assign-self` (`"assign": {"mode": "self"}`) **nur hinzu, wenn die Nachricht des Menschen diese
  Session bittet, die Arbeit jetzt zu erledigen** („leg ein Item dafür an und erledige es“). Das Item kommt
  dann im selben Schreibvorgang **dir zugewiesen** an, in `assigned`. In dein Terminal wird nichts getippt;
  du hast es ja angefordert. Arbeite die Schritte der Reihe nach ab, schließ jeden ab, sobald er
  verifiziert ist (`clawdline item steps <item id>`, `clawdline item step-done <item id> <step id>`;
  `clawdline item step-add` fügt einen hinzu, den die Arbeit doch braucht), und bring die Phasen mit
  `clawdline item phase` voran wie bei jedem zugewiesenen Item (unten). Ein so übernommenes Epic folgt dann
  dem Epic-Verfahren (unten), bevor es implementiert werden darf. Bittet dich der Mensch später, ein Item
  zu übernehmen, das du nicht zugewiesen angelegt hast, verwende `clawdline item claim` (unten).
  Ein Refactor ist ausführbare Arbeit, die die innere Struktur ändert, aber nicht das Verhalten nach
  außen: Er wird zugewiesen, nimmt Schritte und folgt den Phasen, dem Gate und dem Review-Schalter eines
  Features. Ein Plan wird nicht zugewiesen angelegt, in Planning, mit oder ohne `--assign-self`, und
  nimmt keine Schritte (`planning_has_no_steps`).
- Der registrierte Clawdfather ist die Ausnahme für ausführbare Project-Arbeit: Ihm gehört nie
  Project-Code, und er bearbeitet ihn nie. Bittet die Nachricht des Menschen ausdrücklich um ein neues
  Item, darf er `clawdline item add --project <place id> --kind feature --title "…" --assign-new` (oder
  `--assign-terminal <id>`) verwenden, um das Item zuerst anzulegen, und es dann an eine Project-Session
  delegieren. `assignment_state` sagt, ob ein Project-Eigentümer aufgezeichnet wurde (`assigned`), ob bei
  einer neuen Session der erste Dialog beantwortet werden muss (`awaiting_user`), ob die Zuweisung
  fehlgeschlagen ist (`failed` mit `assignment_error`) oder ob keine Zuweisung angefordert wurde
  (`not_requested`). In den letzten beiden Fällen kann der Mensch über das Board zuweisen. Eine
  Wiederholung mit `pending` nennt nach einer unterbrochenen Delegation das ursprüngliche Item; sieh dir
  das Board an, bevor du eine weitere Zuweisung versuchst. Ohne diese ausdrückliche Nachricht schlag das
  Item vor und warte auf die Annahme.
- Der Mensch sieht die Karte mit „Created by the Session from your message at HH:MM“ markiert, mit seinen
  Worten als Zitat.
- Ablehnungen, die jeweils nichts schreiben: `run_unknown` (kein Lauf genannt oder keiner ausgestellt),
  `run_expired` (älter als ein Tag), `run_other_session` (eine Nachricht an eine andere Session),
  `session_not_found`, `child_session` (ein Child berichtet über `result.json`), `project_not_found`,
  `project_mismatch` (ein gewöhnliches ausführbares Item muss in dem Project liegen, in dem du arbeitest),
  `coordinator_required` (eine Rechner-Session ohne die aktive Rolle), `machine_delegation_required`
  (eine gewöhnliche Session hat die nur für den Rechner vorgesehene kombinierte Zuweisung an eine andere
  Session angefordert), `invalid_assignment` (ein fehlerhaftes `assign` oder Clawdfather, der `self`
  verlangt), `too_many_steps` (mehr als 128), `run_items_exhausted` (eine Nachricht deckt höchstens fünf
  Items).
- **Kein Lauf** – der Mensch hat direkt ins Terminal getippt, also antwortet `item add` mit `no_run` oder
  `run_unknown`: Weiche auf einen Vorschlag aus (unten) und sag dem Menschen, er möge ihn unter den
  Agent-Vorschlägen des Boards annehmen.

Lege nie aus eigener Initiative ein Board-Item an, und nie mehrere, um spekulative Arbeit zu planen.

**Übernimm ein Board-Item, auf das der Mensch dich hingewiesen hat.** Wenn dich die Nachricht des Menschen
über Clawdline anweist, ein bestimmtes Item zu übernehmen, das schon auf dem Board steht – *„take the
release-notes item“*, *„claim <item id>“* –, übernimm es; das ist ein einziger Befehl:

```
clawdline item claim <item id>
```

- `item claim` liest den jüngsten Lauf dieses Gesprächs, sofern `--run` keinen nennt, liest das Item für
  seine Version, gibt seinen Idempotency-Key aus, bevor es fragt (`--key` wiederholt denselben
  Schreibvorgang), und gibt danach das Item aus. Es ist `POST /v1/work/v2/agent/items/<id>/claim` mit
  `{"expected_version", "session_id", "via": {"run"}}` und weist das Item **dir zu, der Session, an die
  diese Nachricht gesendet wurde** – nichts darin nennt eine andere Session oder ein anderes Terminal.
- Das Item liest sich danach genau so, als hätte der Mensch es dir über das Board zugewiesen: Es gehört
  dir, es wechselt zu `assigned`, und hatte es keine Schritte, werden sie aus der Liste der Beschreibung
  angelegt. In dein Terminal wird nichts getippt. Bearbeite es wie jedes zugewiesene Item (unten).
- Der Mensch sieht die Karte mit „Claimed by the Session from your message at HH:MM“ markiert, mit seinen
  Worten als Zitat.
- Ablehnungen, die jeweils nichts schreiben: `run_unknown`, `run_expired`, `run_other_session`,
  `session_not_found`, `child_session` (wie bei `item add`); `work_not_found`; `project_mismatch`
  (das Item liegt in einem Project, in dem du nicht arbeitest); `item_assigned` (es hat schon eine Session,
  oder gerade wird eine dafür geöffnet – nur der Mensch verschiebt ein Item zwischen Sessions);
  `item_terminal` (done oder cancelled); `planning_not_assignable` (ein Plan bleibt in Planning; ein Epic
  oder ein Refactor kann übernommen werden); `version_conflict` (es hat sich geändert; führe den Befehl
  erneut aus); `run_claims_exhausted` (eine Nachricht deckt höchstens fünf Verwendungen).
- **Kein Lauf** antwortet mit `no_run` oder `run_unknown`: Überlass das Item dem Menschen zur Zuweisung.

**Weise ein Board-Item einer neuen Session zu, um die der Mensch gebeten hat.** Wenn die Nachricht des
Menschen über Clawdline dich bittet, ein bestimmtes nicht zugewiesenes Feature oder Issue an eine neue
Session zu geben – *„open a security Session for <item id>“* –, weise es zu; das ist ein einziger Befehl:

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- Bei einem Item, das kein Child eines Epics ist, liest `item assign` den jüngsten Lauf dieses Gesprächs,
  sofern `--run` keinen nennt, so wie `item claim`. Es ist `POST /v1/work/v2/agent/items/<id>/assign` mit
  `{"expected_version", "session_id", "mode": "new_session", "assistant"?, "model"?, "persona"?,
  "via": {"run"}}` und öffnet die neue Session, die auch die eigene Wahl „New Session“ des Menschen öffnet.
- Auf der Karte steht „Assigned by a Session from your message at HH:MM“, mit den Worten des Menschen als
  Zitat und der Persona, als die die neue Session läuft.
- Ablehnungen, die jeweils nichts schreiben: die von `item claim` (`run_unknown`, `run_expired`,
  `run_other_session`, `session_not_found`, `child_session`, `project_mismatch`, `item_assigned`,
  `item_terminal`, `version_conflict` und `run_claims_exhausted`: `item claim` und `item assign` teilen sich
  die fünf einer Nachricht); `kind_person_assigns` (nur ein Feature oder ein Issue); `new_session_only`
  (um es selbst zu nehmen, übernimm es mit claim); `unknown_persona`; `persona_disabled_for_auto_assignment`
  (die Rolle ist in diesem Project für die automatische Zuweisung abgeschaltet).

Übernimm oder weise nie ein Item aus eigener Initiative zu – nur das, das die Nachricht des Menschen nennt –,
und verwende nie das `POST /v1/work/v2/items/<id>/assign` des Menschen, das eine Session ablehnt
(`session_cannot_create_item`). Der Eigentümer eines Epics weist auch die eigenen Children des Epics mit
`clawdline item assign` zu (`clawdline guide de epic`).

**Benenne eine neue Session, die für ein Board-Item geöffnet wurde.** Nachdem du Ziel und Umfang gelesen
hast, wähle einen kurzen Namen, der deine tatsächliche Aufgabe beschreibt, und führe
`clawdline item name <item id> "<task name>"` aus. Das ändert den Namen deiner Session einmal, ohne den
Titel des Board-Items zu ändern oder einen weiteren Modell-Turn zu starten. Nur die neue Session, der das
Item aktiv gehört, darf das. Denselben Namen erneut zu senden ist sicher; ein anderer Name wird abgelehnt, und
der Mensch kann weiterhin von Hand einen Session-Titel setzen. Ein Board-Item, das einer bestehenden
Session gegeben wird, lässt deren Namen unverändert.

**Schlag ein Board-Item vor.** Die Warteschlange **Agent proposals** des Boards wird von einer einzigen
Route gespeist:

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id` ist die `id` einer Zeile aus `GET /v1/places`.
- Eine Quelle ist erforderlich, und sie muss deine sein: ein Board-Item, das dieser Session gehört, oder
  eines der eigenen To-dos dieser Session (`proposal_source_required`, `proposal_source_invalid`). Ein
  Vorschlag, der aus etwas entstanden ist, worum der Mensch gebeten hat, nennt das To-do, aus dem er
  stammt – der Weg ist also: Der Mensch bittet, du legst es mit `clawdline todo add` an (unten), und du
  schlägst aus der ID dieses To-dos vor.
- Schreib den Vorschlag in einfacher Sprache, die ein Mensch direkt versteht: `title` nennt das Ergebnis,
  das er bemerken kann, `description` sagt, was sich ändert, `reason` sagt, warum es sich jetzt lohnt, und
  `suggested_acceptance` sagt, was er beobachten kann, wenn es erledigt ist. Alle vier sind erforderlich.
  Mach nicht unerklärte Abkürzungen, interne Bezeichner, Codepfade oder Implementierungsjargon zur
  Haupterklärung. Das Board zeigt zuerst Titel, Quelle und Grund; **Explain / 詳細說明** klappt auf, was
  sich ändert und was der Mensch sehen wird, wenn es erledigt ist.
- **Um ein Item mit Schritten vorzuschlagen**, schreib die Liste als zwei oder mehr Markdown-Listenzeilen
  der obersten Ebene in `description`. Nimmt der Mensch das Item an und weist es zu, wird jede Zeile zu
  einem seiner `steps` (siehe unten).
- `201` antwortet mit dem ausstehenden Vorschlag. Der Mensch nimmt ihn in der Warteschlange Agent
  proposals des Boards an, bearbeitet ihn oder lehnt ihn ab; nichts wird zum Board-Item, bevor er das tut.
  Ablehnungen: `invalid_proposal`, `proposal_too_large`, `project_not_found`, `proposals_full`.

**Die ältere Vorschlagsroute.** `POST /v1/orchestrator/proposals` (mit `…/<id>/asked`, nachdem im Gespräch
gefragt wurde) wird weiterhin bedient: Hier reicht ein Child mit seinem Aufgabengeheimnis und `task_id`
einen offenen Rest ein, und hier bekommt der Vorschlag eines Roots für eine Arbeitslinie seine
`instructions`, ob er jetzt fragen oder zurückhalten soll. Ihre Zeilen erscheinen im älteren Bereich „to
confirm“, **nicht** in der Warteschlange Agent proposals des v2-Boards; so bringt man also kein Item auf
dem Board vor den Menschen.

**Bitte den Menschen um eine Entscheidung.**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "work_id": "<the Board item this is about>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

Das Item muss offen sein und dieser Session gehören: Seine Karte ist der Kontext, in dem das Board die
Frage zeigt (`decision_source_required`, `decision_source_not_found`, `decision_source_invalid`,
`decision_source_closed`). Zwei bis vier Optionen; `default` muss eine davon sein und ist das, was
geschieht, wenn niemand antwortet (nach 7 Tagen, sofern `due_in_minutes` nicht 60–10080 angibt). Nur eine
`blocking`-Entscheidung wird per Push gemeldet. Lies die Antwort mit `GET /v1/orchestrator/decisions/<id>`.

**Der Mensch antwortet; eine Session leitet nur weiter, was er gesagt hat.** Vorschläge, Entscheidungen und
Board-Items werden unter `/v1/work/…` beantwortet. Eine Session, die dort schreibt, muss den Lauf nennen,
der die Worte des Menschen getragen hat, `"via": {"run": "<id>"}`, und wird ohne ihn abgelehnt (`403
session_cannot_decide`). Lies den jüngsten Lauf unter
`GET /v1/orchestrator/sessions/<conversation>/run`; ein erfundener, abgelaufener, zu einer anderen
Session gehörender oder vor der Frage liegender Lauf wird namentlich abgelehnt. Ein Mensch, der direkt in
ein Terminal tippt, hat keinen Lauf; bitte ihn also, über Clawdline oder in der Konsole zu antworten.

**Deine Children, die noch abzuholen sind.** `GET /v1/orchestrator/sessions/<conversation id>/todos` –
benannt über die Gesprächs-ID, nicht über das Terminal (sonst `409 session_id_is_terminal`). Der Broker
öffnet und schließt diese Einträge anhand der Fakten der Aufgaben; es gibt nichts zu schreiben.

Lies an jeder Turn-Grenze, bevor du dich für untätig erklärst, außerdem
`GET /v1/work/v2/agent/session-todos/<conversation id>`. Seine `assigned_items` sind Board-Items, die der
Mensch dieser Session gegeben hat, seine `recent_items` sind Items, die diese Session kürzlich abgeschlossen
hat, seine `direct_todos` sind kurze Bitten, und seine `unacknowledged_completions` sind deine Children,
die ohne dein ACK fertig geworden sind (§5). Über diese Abfrage wartet eine Zuweisung, die während deiner
Arbeit erfolgte, ohne den laufenden Turn zu unterbrechen. Schließ den laufenden Turn ab, nimm dann das
zugewiesene Item als deine nächste eigene Arbeit und lies seinen vollständigen Datensatz, die Inhalte der
Dokumente eingeschlossen, mit `clawdline item show <id>`.

**Ein To-do, das der Mensch gesendet hat.** Eine Nachricht, deren letzte Zeile
`(Clawdline to-do <id>. When it is done: clawdline todo done <id>)` lautet, ist eines der `direct_todos`
dieser Session, das der Mensch aus Clawdline gesendet hat; die Worte über dieser Zeile sind die Bitte.
Erledige die Arbeit, und führe, sobald sie nachweislich erledigt ist, `clawdline todo done <id>` mit dieser
ID aus, **bevor** du den Turn meldest – sonst bleibt die Zeile auf der Liste des Menschen offen, obwohl die
Arbeit fertig ist. Eines, das nicht fertig ist, bleibt offen. `clawdline session report` listet auf stderr
jedes To-do auf, das an diese Session gesendet und noch nicht abgehakt wurde (§7).

**Deine eigenen To-dos, wenn der Mensch darum bittet.** Nur wenn der Mensch diese Session ausdrücklich
bittet, ihre Arbeit als Clawdline-To-dos festzuhalten – oder ihr eine Liste mit mehreren Punkten gibt und
sagt, sie dort zu verfolgen –, schreib sie in die eigene Liste dieser Session:

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add` ist `POST /v1/work/v2/agent/session-todos/<conversation id>` mit
`{"todos": [{"text": "…"}, …]}` und einem Idempotency-Key, den es zuerst ausgibt (`--key` wiederholt
denselben Schreibvorgang). Ein Aufruf trägt 1–20 Zeilen von je höchstens 8 KiB, innerhalb des 96 KiB großen
Anfrage-Bodys, und fügt alle oder keine hinzu; eine Liste, die diese Session über 500 offene To-dos bringen
würde, wird als Ganzes abgelehnt (`direct_todos_full`). Es antwortet mit `201` und den Zeilen in der
angegebenen Reihenfolge. Das Gespräch muss eine laufende Session sein, die dieser Daemon kennt
(`conversation_id_malformed`, `session_not_found`); ein Clawdline-Child wird abgelehnt (`child_session`)
und berichtet weiterhin über `result.json`.

Tu das nie aus eigener Initiative und nie, um spekulative Arbeit zu planen. Schließ jede Zeile mit
`clawdline todo done <id>` erst ab, wenn sie nachweislich erledigt ist. Der Mensch sieht diese Zeilen als
von der Session hinzugefügt markiert, und nur der Mensch kann sie senden oder löschen. Sie sind keine
Board-Items und erscheinen nie auf dem Board. To-dos sind eine Liste kleiner Aufgaben in der aktuellen
Session; ein Board-Item ist Arbeit, die der Mensch auf dem Board verfolgt haben will – wenn er darum
bittet, verwende `clawdline item add` (oben), und seine Liste kommt als `--step`-Zeilen des Items hinein,
niemals zusätzlich als To-dos.

Geht aus dem, was der Mensch meint, klar hervor, dass das Item, das du gerade abgeschlossen hast, noch
unfertig ist, korrigiere das Board selbst; lass es nicht unter „Recently Done“ stehen, lege kein Ersatz-Item
an und bitte den Menschen nicht, es wieder zu öffnen. Lies das Item für seine aktuelle Version neu und
verwende dann:

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "The concrete behavior or acceptance claim that remains unfinished"}
```

Verwende das nur, wenn der Bezug auf dein gerade abgeschlossenes Item eindeutig ist. Die Route nimmt nur
`done`-Arbeit an, deren letzte Zuweisung von genau dieser Session freigegeben wurde; sie kann weder einen
Abbruch durch den Menschen rückgängig machen noch den Abschluss einer anderen Session übernehmen. Sie
erhält die früheren Belege, beginnt einen neuen Zyklus in `implementing`, setzt diese Session wieder als
Eigentümer ein und zeichnet den Grund im unveränderlichen Verlauf des Items auf. Der Grund ist höchstens
8 KiB lang. Eine mehrdeutige Folgenachricht ist keine Ermächtigung, ein Board-Item zu ändern.

Braucht der besitzende Agent eine Handlung oder Wahl des Menschen, bittet er um eine Entscheidung und
wartet darauf. Öffne zuerst eine Entscheidung zu diesem Item (`POST /v1/orchestrator/decisions` mit der
`work_id` dieses Items, zwei bis vier Optionen, einem `default` und einer Frist – für eine Handlung etwa
Optionen wie `{"id": "done", "label": "I've done it"}` und `{"id": "cannot", "label": "I can't"}`), und
lass dann das Item über die rechnerauthentifizierte Route darauf zeigen:

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

Die Entscheidung muss existieren (`decision_not_found`), zu dieser Session gehören
(`decision_other_session`), dieses Item betreffen (`decision_other_item`) und noch offen sein
(`decision_not_open`); `decision_id` bei jeder anderen Bedingung ist `decision_requires_waiting_user`. Ein
`waiting_user` ohne Entscheidung wird mit `waiting_user_requires_decision` abgelehnt. Der Mensch antwortet
auf der Board-Karte oder unter „Waiting on you“; ist die Entscheidung beantwortet oder gilt zum
Fristende ihr Standardwert, löscht der Daemon im selben Schreibvorgang das `waiting_user` des Items,
zeichnet die Antwort am Item auf und tippt die ID und die Beschriftung der gewählten Option in diese
Session, sobald sie untätig ist. Um selbst mit dem Warten aufzuhören, setze `condition` über dieselbe Route
auf den leeren String (oder eine andere Bedingung); die Entscheidung ist dann `withdrawn` und verschwindet
aus „Waiting on you“, ebenso, wenn das Item freigegeben, neu zugewiesen, abgebrochen oder abgeschlossen
wird. Die Antwort auf eine zurückgezogene Entscheidung wird mit `decision_withdrawn` abgelehnt.

**Erfasste Planungs- und Verifikations-Gates.** `planning_gate` ist standardmäßig an und `verify_gate` aus;
`clawdline setting get|set planning_gate|verify_gate` nimmt `on/off` oder `true/false` an. Die erste
erfolgreiche Zuweisung in einem Ausführungszyklus friert beide Werte ein. Eine Neuzuweisung und spätere
Änderungen der globalen Einstellung ändern diesen Zyklus nicht. Ein Epic oder Feature mit erfasster
Planung braucht vor dem Implementieren Akzeptanzkriterien. Ein Epic braucht außerdem einen Plan und ein
unabhängiges Review; ein Feature braucht sie nur, wenn der Mensch seinen Schalter „Needs independent
review“ angehakt hat (unten). Ein Issue hat nie ein Planungs-Gate. Ist die Planung aus, entfällt auch die
erzwungene Epic-Planung. Beide an bedeutet Planung und danach unabhängige Verifikation; nur Planung behält
die gewöhnliche Merge-Verifikation; nur Verifikation überspringt die Planung, prüft aber trotzdem genau den
Kandidaten; beide aus folgt dem gewöhnlichen Lebenszyklus. Der Mensch muss die Akzeptanz nicht auf dem
Board ausfüllen. Kommt ein Item mit Gate ohne sie an, schreib nach der Zuweisung und vor dem Übergang mit
Gate beobachtbare Kriterien mit `clawdline item acceptance <item id> --body-file <file>`. Die besitzende
Session darf einen leeren Vertrag einmal füllen. Wenn der Mensch diesem besitzenden Root über Clawdline
ausdrücklich sagt, die Akzeptanz dieses Items zu überarbeiten, schreib den vollständigen Ersatz-Markdown in
eine Datei und führe
`clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>` aus.
Die Item-Version ist die Zeile `item version N`, die `clawdline item show <id>` ausgibt (`item steps`
gibt nur die Akzeptanzversion aus); lies den Nachrichtenlauf aus
`GET /v1/orchestrator/sessions/<conversation>/run`. Der aufbewahrte Nachrichtenauszug muss ausdrücklich
eine Änderung der Akzeptanz verlangen; ein Verbot, eine Diskussion oder eine bloße Frage ist keine
Ermächtigung. Er darf das Item über den Gesprächskontext meinen, wenn diesem Root genau ein offenes Item
gehört; andernfalls muss er das Item über ID oder Titel benennen. Der Lauf muss neuer sein als die
aktuelle Akzeptanzversion. Eine typisierte Ablehnung bedeutet, dass sich nichts geändert hat. Wiederhole
eine unsichere Antwort mit demselben `--key`, `--run`, `--expected-version` und denselben Dateibytes. Der
Mensch kann auch direkt bearbeiten. Eine Änderung vor dem Mergen macht alte PASS-Urteile und Overrides
ungültig; sobald das Mergen beginnt, ist sie gesperrt.

Ist die erfasste Verifikation an, führe `clawdline item phase <id> verifying` aus einem sauberen,
registrierten Worktree auf seinem committeten Kandidaten aus: Die CLI sendet den aktuellen Branch und den
vollständigen HEAD, und der Daemon prüft Project, Zyklusbasis, Baum und Akzeptanz-Digest. Ein losgelöster,
schreibgeschützter Codex-Prüfer verwendet `code-reviewer` für ein Issue, `reality-checker` für ein Epic und
`evidence-collector` für ein Feature mit Referenzbildern oder einem Designdokument (sonst
`reality-checker`). Sein typisiertes Urteil ist `PASS`, `FAIL` oder `NEEDS_WORK`; unverifizierte Aussagen
sagen, warum, und erlauben niemals ein Mergen. Ein fehlendes oder fehlerhaftes Ergebnis ist ein technischer
Fehler, mit einem begrenzten Wiederholungsversuch und danach Eskalation. Die abschließende
End-to-End-Runde eines Epics wartet, bis alle Children in einem Endzustand sind und die betroffenen
Komponenten zu einem lauffähigen Kandidaten integriert sind. Gezielte Tests der Children und
Integrations-Smoke-Checks kommen zuerst; dispatche keine abschließende Browser- oder
Mehrkonten-End-to-End-Arbeit gegen eine Mock-UI, unverbundene Branches oder unvollständige APIs. Weise vor
dem Dispatch nach, dass der gewählte Worker die Ziel-URL tatsächlich mit einem berechtigten Browser oder
einer gleichwertigen lokalen Automatisierung öffnen kann und die Testkonten, Fixtures und
Origin-Berechtigungen hat, die er braucht. Nenne diesen Weg im Briefing; `--permission-mode full` allein ist
kein Browserzugriff. Behebe eine fehlgeschlagene Werkzeug-Vorabprüfung, bevor du es erneut versuchst,
statt einen weiteren Prüfer in dieselbe Blockade zu schicken; eine fehlgeschlagene Vorabprüfung ist kein
End-to-End-Versuch. Plane eine umfassende End-to-End-Runde pro Epic, nicht eine pro Child oder Revision.
Führe nach der Behebung eines Fehlers nur die betroffenen Szenarien erneut aus. Wiederhole die umfassende
Runde nur, wenn sich der Akzeptanzumfang oder die Integrationsgrenze wesentlich ändert, und halte fest,
warum. `verifying → merging` braucht ein gültiges
PASS für genau diesen Kandidaten und diese Kriterien oder einen ausdrücklich begründeten Override; ein
Verifikationssatz allein kann es nicht gewähren. Drei aufeinanderfolgende FAILs eskalieren an den
aktiven Eigentümer des übergeordneten Epics und, wenn dieser nicht verfügbar ist, an den Menschen; ein
technischer Fehler eskaliert gesondert. Nur der bestimmte übergeordnete Eigentümer verwendet
`POST /v1/work/v2/agent/items/<id>/gate-decision`; ein Mensch verwendet
`POST /v1/work/v2/items/<id>/gate-decision`. Verwende als Agent nie die Route des Menschen. Das Board nennt
KI-, Menschen- und technische Overrides getrennt, nie als PASS des Prüfers. Ist die Kapazität für
aufbewahrte Details erreicht, lädt der Mensch zuerst `GET /v1/work/v2/items/<id>/gate-export` herunter,
prüft den Manifest-Digest und bestätigt dann `POST /v1/work/v2/items/<id>/gate-purge` mit diesem Digest und
der Item-Version. Das Bereinigen entfernt nur dafür geeignete, abgeschlossene Details; Aggregate, die
neuesten Fakten und das Audit bleiben erhalten.

**Die Phase voranbringen.** Die besitzende Session bewegt ihr Item selbst durch die Ausführungsphasen;
niemand sonst tut es, und weder eine Turn-Quittung noch eine aufgehobene Bedingung tun es. Die Phase ist
kein Feld von `…/edit` (`phase_not_editable`). Führe jeden Übergang aus, wenn die Arbeit, die er nennt,
tatsächlich geschehen ist:

```
clawdline item phase <item id> implementing                  # when you start
clawdline item phase <item id> verifying                     # the change exists; now check it
clawdline item phase <item id> merging --verification "what was run and what it showed"
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin --landing-project <place id>
clawdline item phase <item id> deploying --no-landing-reason "why there is no code to land"
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

Der Befehl liest die Version des Items, gibt seinen Idempotency-Key aus (`--key` wiederholt denselben
Schreibvorgang) und gibt das Item aus. Es ist

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "no_landing_reason"?: "…", "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Ein Schritt nach dem anderen: `assigned → implementing → verifying → merging → deploying → done`.
  Von `verifying` darfst du zurück zu `implementing`; von `merging` zurück zu `implementing` oder
  `verifying`. Ein Item, dessen erfasstes Verifikations-Gate aus ist, darf auf seine Landing-Belege hin
  (unten) auch direkt von `implementing` zu `deploying` gehen, wobei `verification` optional ist; einem
  Item mit Gate wird dieser Schritt mit `verification_gate_on` verweigert, und es geht den ganzen Weg.
  Nichts anderes überspringt eine Phase, und `done` wird nur von `deploying` aus erreicht.
- `merging` braucht `verification`. `deploying` braucht ein Landing: ein Broker-Child dieses Items, das
  gelandet ist, oder `landing` mit einem Commit, den der Daemon sowohl im lokalen `target`-Branch des
  Projects als auch in `refs/remotes/<remote>/<target>` findet – pushe zuerst. Ist die Arbeit in einem
  anderen Repository gelandet (ein Backend-Item, dessen Änderung ein Frontend-Commit war), nennt
  `landing.project` (`--landing-project`) die ID dieses Projects aus `GET /v1/places`, und der Commit wird
  stattdessen dort gesucht – ein verschachteltes Repository im Project-Verzeichnis (`cloud/`) ist hier ein
  eigenes Project. Der Nachweis wird einmal als **Root-Landing-Datensatz** des Brokers geschrieben:
  Dasselbe Item, Repository, derselbe Commit und dasselbe Ziel, erneut aufgezeichnet, sind derselbe
  Datensatz. Der Verlauf des Items nennt ihn als `landing_id`, und
  `clawdline landings --work-id <item id>` listet ihn auf. Vor `deploying` fragt der Daemon git sofort, ob
  der Branch eines gebundenen Childs gemergt wurde, sodass ein eben erst gemachter Merge zählt, ohne auf
  den nächsten Blick des Brokers zu warten. Arbeit ohne Code nimmt `no_landing_reason`
  (`--no-landing-reason`) statt eines Landings; das wird neben einem `landing` abgelehnt
  (`invalid_landing_evidence`), ebenso, solange ein gebundenes Child sein Landing noch schuldet
  (`landing_owed`, mit Nennung der Aufgabe), und neben einem Child, das gelandet ist (`landing_recorded`).
  `done` braucht `deployment` oder `no_deployment_reason`; die `deployment_policy` des Items entscheidet,
  welches (`required` nimmt nur `deployment`, `not_required` nur `no_deployment_reason`, `agent_decides`
  beides). Zuvor muss jeder Schritt abgeschlossen sein.
- `done` gibt deine Zuweisung frei und verschiebt das Item in die Zeile „zuletzt erledigt“ der Session.
  Füge vorher einen Abschlussbericht (unten) hinzu, wenn einer geschuldet ist.
- Ablehnungen: `invalid_transition` (keine nächste Phase, oder ihre Belege fehlen), `steps_incomplete`,
  `not_item_owner`, `item_unassigned`, `item_terminal` (ein Mensch öffnet es wieder), `evidence_unknown`,
  `direct_landing_not_applicable`, `invalid_landing_evidence`, `landing_project_not_found`,
  `landing_commit_unresolved`,
  `landing_target_unresolved`, `landing_not_on_target`, `landing_remote_unresolved`,
  `landing_not_published`, `landing_owed`, `landing_recorded`, `landings_full` (das Item hält 64
  Root-Landings) und `version_conflict`: neu lesen und erneut senden. Eine Ablehnung mangels Landing endet
  mit dem, was der Broker gefunden hat, als er eben nachsah (ein noch nicht gemergter Branch, ein
  Repository, das er nicht lesen konnte).

**Mit einem einzigen Befehl abschließen.** Nachdem die Arbeit gelandet ist, führt
`clawdline item finish <item id>` das Item in einer einzigen Transaktion von `implementing`, `verifying`,
`merging` oder `deploying` bis `done`:

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?}, "no_landing_reason"?: "…",
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Jeder Schritt ist der, den `item phase` geht, durch dieselben Gates, und schreibt sein eigenes
  `item.phase_changed`; wird irgendein Schritt abgelehnt, wird das Ganze abgelehnt und nichts geschrieben.
- Das Landing wird gelesen, nicht eingetippt: Der Commit ist bei einem Item mit Gate der vom Gate
  autorisierte Kandidat, sonst der gelandete Commit der gebundenen Children; das Ziel ist der Branch, den
  ihre Landings nennen; das Remote ist das, dem dieser Branch folgt. Jedes Feld, das du angibst, hat
  Vorrang, und das Ergebnis wird gegen git nachgewiesen, so wie `item phase deploying` es nachweist. Ein
  erfasstes Verifikations-Gate braucht weiterhin sein PASS: Aus `implementing` wird ein Item mit Gate mit
  `verification_candidate_required` abgelehnt; geh also mit `item phase` aus dem Kandidaten-Worktree in
  `verifying`, warte auf das PASS und schließ dann ab.
- Ein gebundenes Child, dessen Branch eben erst gemergt wurde, wird vom Abschluss selbst als gelandet
  aufgezeichnet: Der Daemon fragt git, bevor er die Landings liest, sodass `landing_required` direkt nach
  einem Merge bedeutet, dass der Branch auf keinem Ziel liegt, und die Ablehnung sagt, was der Broker
  gefunden hat.
- Arbeit ohne Code: `clawdline item finish <item id> --no-landing-reason "…" --no-deployment-reason "…"`,
  nach denselben Regeln wie bei `item phase`.
- Ein Item, das schon `done` ist, wird so beantwortet, wie es steht, und nichts wird geschrieben; dasselbe
  Landing, zweimal gesehen, bewegt also nichts.
- Als Ablehnungen kommen hinzu: `verification_required`, `landing_required`, `deployment_required` (die
  Angabe, die diesem Schritt fehlte), `landing_target_unknown`, `landing_remote_unknown`,
  `landing_remote_unreadable`, `landing_ambiguous` (nenne es mit dem Flag), `landing_owed`,
  `landing_recorded` und `finish_not_started` (noch vor `implementing`).

Ein zugewiesenes Item kann `steps` enthalten. Eine erfolgreiche Zuweisung kann sie aus zwei oder mehr
Markdown-Listenzeilen der obersten Ebene in der Beschreibung anlegen, und ein Item, das du mit
`clawdline item add` angelegt hast, trägt seine `--step`-Zeilen. Jeder Schritt ist ein Checklisteneintrag an
diesem Item, kein weiteres Board-Item.
Schließ einen verifizierten Schritt mit `clawdline item step-done <item id> <step id>` ab; das sendet
`POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete` mit `{"session_id"}`. Auf den
Agent-Routen ist `expected_version` optional: Weggelassen, wirkt der Schreibvorgang auf die aktuelle
Version; angegeben (`--expected-version`), wird es verglichen, und eine veraltete antwortet mit `version_conflict`. Ein Übergang zu `done` wird mit `steps_incomplete` abgelehnt, solange irgendein Schritt
offen ist; Clawdline hakt nie einen ab, bloß weil die übergeordnete Phase vorangekommen ist.

**Dein eigenes Item in Schritte zerlegen.** Hat ein Item, das dir gehört, keine Schritte, und ist die
Arbeit mehrstufig – mehrere Änderungen, die getrennt verifiziert werden, oder mehr als ein Teil des
Systems –, zerlege es selbst in seine geordneten Schritte, bevor du implementierst: zwei bis acht konkrete
Schritte, jeder für sich verifizierbar. Eine einzelne, unkomplizierte Änderung braucht **keine** Schritte;
blähe keine Liste auf, nur um eine zu haben. Stellt sich die Arbeit als größer heraus als gedacht, füge
den Schritt dann hinzu.

```
clawdline item step-add <item id> "Wire the route" "Cover it with a test" "Say it in the guide"
```

Titel sind Argumente oder einer pro nicht leerer stdin-Zeile. Der Befehl liest das Item vor jedem Titel
neu, gibt jeden Idempotency-Key vor seinem Schreibvorgang aus und gibt eine kurze Quittung mit einem
Hinweis auf `item show` aus. Es ist
eine Anfrage pro Titel, nur für den Eigentümer,

```
POST /v1/work/v2/agent/items/<id>/steps     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

mit `"position"` eins hinter der des letzten vorhandenen Schritts, da Schritte nach Position geordnet sind.
Schließ dann jeden mit `clawdline item step-done` ab, sobald er verifiziert ist. Das ist nicht die „eigene
Initiative“, die für Board-Items und To-dos verboten ist: Das Item gehört schon dir, und seine Schritte
zeigen dem Menschen die Arbeitsstufen, die dir übertragen wurden.

Hat die Lösung eines Issues oder Vorfalls eine erhebliche Untersuchung erfordert, um die eigentliche
Ursache zu finden oder die echte Korrektur von plausiblen Alternativen zu unterscheiden, füge einen für
Menschen lesbaren Abschlussbericht hinzu, bevor du das Item auf `done` bringst. Eine unkomplizierte,
direkt beobachtete Korrektur braucht keinen. Schreib in eine Datei, was passiert ist, die eigentliche
Ursache, was sich geändert hat, wie es verifiziert wurde und welche Grenze noch bleibt, dann:

```
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Der Befehl sendet `POST /v1/work/v2/agent/items/<id>/documents` für dich (der Epic-Teil von §10,
`clawdline guide de epic`, listet seine Felder auf). Ein selbst gebautes curl an diese Route ohne die
Zugangsdaten, die der Befehl liest, bekommt `401 unauthorized`. Der Inhalt ist Markdown, höchstens 64 KiB. Schreib für den Menschen, der das Problem gemeldet hat, nicht als rohes
Debug-Protokoll, und halte private Daten heraus. Der aktive Eigentümer muss ihn hinzufügen, bevor das Item
einen Endzustand erreicht; lies nach einem Versionskonflikt neu. Ein Abschlussbericht ist eine
zugeordnete Erzählung und ersetzt nie Belege für Verifikation, Landing oder Deployment. Ist er vorhanden,
bleibt er am geschlossenen Board-Item und öffnet sich direkt aus der Zeile „Recently Done“ der Session.

`/v1/board` sind die alten Karten der Swift-App, nur lesbar. Landing ist ein Fakt des Brokers: Ein Item
wird nie von Hand als gelandet markiert (`422 landing_is_broker_fact`).

### Epic und Feature: Folge vor der Implementierung dem Review-Schalter des Menschen

Ein Epic, dessen Zyklus die Planung als eingeschaltet erfasst hat, braucht einen Plan und ein
unabhängiges Review. Ein Feature trägt den Schalter **Needs independent review** des Menschen
(`review_required` am Item; `clawdline item steps
<id>` gibt ihn aus). Nur der Mensch setzt ihn, auf dem Board; du kannst es nicht, und du beurteilst das
Risiko des Features nicht selbst. Der Daemon liest den Schalter, wenn du den Eintritt in `implementing`
beantragst.

- **Nicht angehakt** (Standard): Schreib die kurzen Akzeptanzkriterien des Features, implementiere und
  führe gezielte Tests aus. Schreib keinen Plan zur Prüfung, dispatche kein `plan_review`-Child und
  zeichne keine Risikobewertung auf.
- **Angehakt**, und für jedes Epic mit eingeschalteter Planung: Nimm den Weg mit geprüftem Plan.

Meinst du, ein nicht angehaktes Feature verdiene ein Review, sag das dem Menschen und lass ihn den Haken
setzen; es gibt keinen Weg, auf dem ein Agent den Daemon um eines bitten kann.

1. Plane sorgfältig und schreib den Plan an das Item:
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. Dispatche ein schreibgeschütztes Child, dessen Briefing lautet, diesen Plan kritisch zu prüfen – was
   fehlt, falsch oder riskant ist:
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. Warte, bis das Child fertig ist. Ein erfolgreiches, mit `--work-id` dispatchtes Review-Child zeichnet
   seine Review-Quittung von selbst als `plan_review`-Dokument am Item auf; prüfe `.documents` in
   `GET /v1/work/v2/items/<id>`. Nur wenn es dort nicht steht – zum Beispiel, weil das Child ohne
   `--work-id` dispatcht wurde –, zeichne es von Hand auf:
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
   Diesen Befehl für dieselbe Aufgabe erneut auszuführen, schadet nicht: Er antwortet mit dem schon
   vorhandenen Dokument. Eine kurze Zusammenfassung für den Menschen, was der Plan daraufhin geändert hat,
   ist ein eigenes `other`-Dokument, kein zweites Review.
   Ändert sich der Plan eines Features nach dem Review, schreib nach dem überarbeiteten Plan ein
   `other`-Dokument mit dem Titel `Review boundary assessment` und dem JSON
   `{"new_risk_boundary":false,"reason":"..."}` – aber nur, wenn die Änderung innerhalb der Risikogrenze
   des früheren Reviews bleibt. Eine neue oder unsichere Grenze bekommt ein gezieltes neues Review.
   Die bestehende Obergrenze von zwei Reviews für ein Epic gilt weiterhin.
4. Zerlege die Arbeit mit `clawdline item step-add <item id> …` in Schritte.
5. Erst dann `clawdline item phase <item id> implementing`.

Plane die Verifikation als Abfolge. Jedes Implementierungs-Child prüft seinen eigenen Code mit gezielten
Tests; der Eigentümer des Epics integriert die betroffenen Komponenten und führt den kleinsten sinnvollen
komponentenübergreifenden Smoke-Check aus. Erst wenn der integrierte Kandidat funktioniert, sollte der
Eigentümer echte End-to-End-Verifikation und das zutreffende unabhängige UX-/Produkt-Review dispatchen.
Prüfe vor dem Dispatch den Browserweg, die Ziel-URL, die Testkonten, Fixtures und Berechtigungen des
Prüfers. Verwende keine wiederholten schreibgeschützten Prüfaufgaben, um einen fehlenden Browser zu
entdecken oder zu umgehen: Behebe zuerst den Zugriff oder wähle eine gleichwertige lokale
Browser-Umgebung. Eine fehlgeschlagene Vorabprüfung ist kein End-to-End-Versuch. Plane eine umfassende
End-to-End-Runde pro Epic für den stabilen Kandidaten, nicht eine pro Child oder Revision; führe nach einer
gezielten Korrektur nur die betroffenen Pfade erneut aus. Wiederhole die ganze Runde nur nach einer
wesentlichen Änderung von Akzeptanz oder Integration, und halte diesen Grund fest.

`clawdline item doc` liest das Item für seine Version und die letzte Dokumentposition, gibt seinen
Idempotency-Key aus (`--key` wiederholt denselben Schreibvorgang) und gibt das Item aus. Der Inhalt kommt
aus `--body-file` oder stdin. Es ist `POST /v1/work/v2/agent/items/<id>/documents` mit
`{"expected_version", "session_id", "role", "title", "body", "reference", "position"}`; die Rollen
sind `spec`, `design`, `test`, `deploy`, `completion_report`, `other`, `plan` und `plan_review`.

Um ein Dokument zu überarbeiten, schreib es erneut mit derselben `--role` und demselben `--title`: Der
Daemon ersetzt Inhalt, Referenz und Position, behält die ID, erhöht die Version um eins und zeichnet
`document.revised` auf. Die CLI meldet `added … at v1` oder `revised … to vN`, und `clawdline item show`
gibt die `vN` jedes Dokuments aus. Denselben Text erneut zu senden ändert nichts und antwortet mit dem
Dokument, wie es ist; eine Wiederholung ist also sicher. Der alte Text wird nicht aufbewahrt; verwende
einen anderen Titel, um beide zu behalten.

- `plan`, `plan_review` und die Review-Grenze werden nie an Ort und Stelle überarbeitet: Jeder
  Schreibvorgang fügt ein neues Dokument hinzu, weil das Planungs-Gate sie der Reihe nach liest und ein
  Review den Plan nennt, den es gelesen hat.
- Ein Item hält höchstens 32 Dokumente, und ein `completion_report` zählt nicht dazu: Er passt immer, auch
  bei einem vollen Item. Ein Item hält einen `completion_report`; einen weiteren zu schreiben, unter
  welchem Titel auch immer, überarbeitet ihn und übernimmt den neuen Titel.
- Ein 33. Dokument wird mit `documents_full` abgelehnt, und nichts wird geschrieben; die Meldung nennt
  stattdessen den `clawdline item doc`-Befehl, der ein vorhandenes Dokument überarbeitet.

- `plan` und `plan_review` gehören zu einem Epic oder Feature (`document_role_not_applicable` für andere Arten).
- Die `reference` eines `plan_review` ist die Aufgaben-ID des Clawdline-Childs, das den Plan geprüft hat.
  Der Daemon nimmt sie nur an, wenn diese Aufgabe existiert (`plan_review_task_unknown`), von der
  besitzenden Session des Items dispatcht wurde (`plan_review_task_not_owned`), auf der Linie dieses Items
  liegt, falls sie eine nennt (`plan_review_task_other_item`), die Art `plan_review` hat
  (`plan_review_task_wrong_kind`), mit `success` beendet wurde (`plan_review_task_unfinished`) und nicht
  früher als der neueste Plan dispatcht wurde (`plan_review_task_stale`). Ein Review ohne vorausgehenden
  Plan wird mit `epic_plan_required` abgelehnt. Das automatische Dokument eines Review-Childs durchläuft
  dieselben Prüfungen, und ein wiederholter Schreibvorgang für dieselbe Aufgabe ist idempotent.
- `clawdline item phase <item id> implementing` wird bei einem Epic mit eingeschalteter Planung ohne
  seinen geprüften Plan abgelehnt (`epic_plan_required` oder `epic_plan_review_required`). Ein Feature mit
  eingeschalteter Planung, bei dem der Mensch „Needs independent review“ angehakt hat, wird auf dieselbe
  Weise abgelehnt (`feature_plan_required` oder
  `feature_plan_review_required`); ein nicht angehaktes braucht nur seine Akzeptanzkriterien. Ein
  überarbeiteter Plan bei einem angehakten Feature braucht außerdem einen Beleg für die unveränderte
  Grenze oder ein weiteres Review. Ein Epic mit ausgeschalteter Planung darf direkt in implementing eintreten.
- Das Gate liest die Quittung des neuesten `plan_review` aus seiner Review-Aufgabe. Jeder Befund hat eine
  `severity` von `blocking` oder `non_blocking` (`important` und `minor` aus der älteren Vorlage zählen
  als nicht blockierend). Das Urteil ist `safe_to_land` ohne Befunde, `proceed_with_findings`, wenn jeder
  Befund `non_blocking` ist, und `changes_required`, wenn irgendeiner `blocking` ist. Ein neuestes Review
  mit einem blockierenden Befund lehnt `item phase implementing` und jeden Dispatch mit
  `--work-id` auf das noch zugewiesene Item ab, außer `--kind plan_review`
  (`epic_plan_review_blocking` oder `feature_plan_review_blocking`); die Ablehnung listet die
  blockierenden Befunde und die nächsten Befehle auf: Plan überarbeiten, dann ein neues Review
  dispatchen. Nur nicht blockierende Befunde oder eine ältere Quittung, deren Befunde keine Schwere
  tragen, lassen das Item weitergehen. Ein Epic bekommt höchstens zwei Reviews: Blockiert das zweite
  immer noch, überarbeite den Plan so, dass er seine Befunde beantwortet, und der überarbeitete Plan geht
  ohne drittes Review in implementing; dispatche keines.

**Zerlege das Epic in Child-Items und verteile sie.** Das ist die einzige Ausnahme von „eine Session legt
ein Board-Item nur an, wenn die Nachricht des Menschen sie dazu auffordert“ und von „nur der Mensch weist
Items zu“: Der Mensch hat dir das Epic zugewiesen, und das ist die Ermächtigung, es zu zerlegen. Nachdem
der geprüfte Plan das Epic in `implementing` gebracht hat, lege, wenn Teile davon besser von anderen
Sessions erledigt würden, Feature- oder Issue-Items darunter an und weise sie zu:

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | description on stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- Terminal-IDs stehen im Session-Adressbuch, `GET /v1/orchestrator/sessions` (`clawdline guide de
  send`); die Session muss im Project des Epics arbeiten. Du darfst ein Child dir selbst zuweisen, und
  `--assign-new` öffnet eine neue Session mit einer Root-Zuweisung, die das Epic nennt. Ohne ein
  `--assign`-Flag wartet das Child nicht zugewiesen auf den Menschen.
- `item child` liest das Epic für seine Version, gibt seinen Idempotency-Key aus (`--key` wiederholt
  denselben Schreibvorgang) und gibt das Child aus. Es ist `POST /v1/work/v2/agent/items/<epic id>/children` mit
  `{"expected_version", "session_id", "kind", "title", "description", "steps"?, "deployment_policy"?,
  "assign"?: {"mode": "existing_session", "terminal_id"} | {"mode": "new_session", "assistant"?,
  "model"?, "persona"?}}`, beantwortet mit `201` und `{"item", "assigned", "assignment_error"?: {"code", "message"}}`.
  Das Child liegt im Project des Epics, trägt `parent_id` (das Epic), und seine Karte sagt, dass die
  Eigentümer-Session des Epics es angelegt hat. Seine Schritte sind deine `--step`-Zeilen oder – wenn du
  keine angibst – die Liste seiner Beschreibung, sobald es zugewiesen ist.
- Das Child wird zuerst angelegt und danach zugewiesen. Scheitert die Zuweisung, **bleibt das Child
  bestehen, nicht zugewiesen**, die Antwort trägt `assignment_error` mit dem Code der Zuweisung
  (`session_unavailable`, `project_mismatch`, `assignment_failed`, …), und der Befehl endet mit Exit-Code 1:
  Weise es mit `item assign` erneut zu oder überlass es dem Menschen.
- `item assign` ist `POST /v1/work/v2/agent/items/<child id>/assign` mit `{"expected_version",
  "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}`; es verschiebt ein offenes
  Child deines Epics zu einer anderen Session, dieselbe Zuweisung, die auch die Wahl eines Menschen vornimmt.
- Ablehnungen, die jeweils nichts schreiben: `not_epic_owner` (du bist nicht der Eigentümer des Epics),
  `parent_not_epic` (das übergeordnete Item ist kein Epic), `epic_not_planned` (das Epic ist noch vor
  `implementing`: Die Children gehen aus einem geprüften Plan hervor), `item_terminal` (das Epic ist
  abgeschlossen), `child_kind_not_allowed` (nur `feature` oder `issue`), `epic_children_full` (ein Epic hält
  höchstens 32 Children, offen oder geschlossen), `not_epic_child` (`item assign` für ein Item, das kein
  Child eines Epics ist – der Mensch weist es zu, es sei denn, seine Nachricht bittet dich darum:
  `clawdline guide de board`), `invalid_assignment`, `version_conflict`, `persona_not_applicable` (422: eine
  Persona bei einer bestehenden Session) und `unknown_persona` (400: eine ID, die der Katalog nicht hat).
- **Eine Persona** ist eine Rolle, mit der eine neue Session gestartet wird: Text, der ihrem Systemprompt
  hinzugefügt wird und sie für das ganze Gespräch so arbeiten lässt, wie diese Rolle arbeitet. Sie gilt nur
  für eine neue Session (`--assign-new`, `--new`, `dispatch`); eine bestehende Session behält die, mit der
  sie geöffnet wurde. Standardmäßig keine. Eine Persona setzt nie `CLAUDE.md`/`AGENTS.md`, das Briefing,
  `CHILD.md` oder dieses Protokoll außer Kraft. `GET /v1/personas` listet sie auf; die IDs (`teams` an
  jeder listet jedes Team auf, in dem eine Persona ist, und eine kann in mehreren sein):
  - `architect` – ein Epic planen;
  - `backend` – ein Feature für Daemon, API oder Speicher;
  - `frontend` – ein Feature für die Konsole oder das Telefon-Layout;
  - `minimal-change` – ein Issue: die kleinste Korrektur, die hält;
  - `code-reviewer` – Review- und `plan_review`-Children;
  - `reality-checker` – Verifizieren: Belege vor „es funktioniert“;
  - `security` – Arbeit, die Berechtigungen, Kopplung oder Cloud berührt;
  - `technical-writer` – Dokumentation und Leitfäden;
  - Marketing, für ein Blog-, Website- oder Dokumentations-Repository: `seo` (Seiten und Metadaten),
    `content-writer` (in Dateien entworfene Artikel), `ai-search` (Seiten, die KI-Antwortmaschinen
    zitieren können), `social-media`, `instagram`, `email` (Newsletter), `growth` (gemessene Experimente)
    und `pr` (Ankündigungen).
  - Produkt, Qualität und Betrieb: `product-manager`, `sprint-prioritizer`, `feedback-synthesizer`,
    `trend-researcher`, `ux-researcher`; `test-automation`, `accessibility`, `performance`,
    `api-tester`, `evidence-collector` (entscheidet PASS oder FAIL pro Behauptung anhand erfasster
    Nachweise); `sre`, `devops`, `incident-commander`, `finops` und `secrets`.
  - Design und Geschäft: `ui-designer` (Bildschirme im Designsystem des Projects), `ux-architect` (Abläufe
    und Layoutstruktur), `brand-guardian` (Markenkonsistenz), `ui-finish-gate` (die visuelle Prüfung vor
    der Auslieferung), `image-prompt` (Prompts für Bildgenerierung), `pricing`, `customer-success`,
    `support` (entworfene Antworten), `analytics` (Antworten aus echten Daten), `devrel` (Beispiele, die
    laufen) und `privacy` (Prüfungen personenbezogener Daten; keine Rechtsberatung).
  - `zero-review-lead` – verantwortet ein Review-Epic, das ein bestehendes Feature oder einen bestehenden
    Prozess von null an neu untersucht: plant die Rollenperspektiven, dispatcht sie als schreibgeschützte
    Reviewer-Children mit einem gemeinsamen Faktenpaket und macht aus ihren Belegen einen Zielentwurf;
    sein Skill ist `zero-based-review`.
- **Füge ein unabhängiges UX-/Produkt-Review hinzu, wenn das Epic eine Erfahrung ändert, die Menschen
  betrifft.** Ordne im Plan ein, ob das Epic eine Schnittstelle für Menschen, eine Nutzerreise oder eine
  Produktrichtlinie ändert. Tut es das, dispatche vor dem Mergen mindestens ein schreibgeschütztes
  Spezialisten-Child, standardmäßig mit `ux-architect` für Layout, Interaktion und den Produktablauf von
  Anfang bis Ende:

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

  Sein Briefing nennt den integrierten Kandidaten und verlangt Belege für Desktop und das kleinste
  unterstützte Mobilgerät, das Verhalten mit Tastatur und Screenreader, Sackgassen, Produkteignung,
  Schweregrad und eine konkrete Empfehlung. Wo Belege nicht verfügbar sind, muss es **als unverifiziert
  markieren und sagen, warum**. Verwende stattdessen `product-manager`, wenn Richtlinie und Umfang, nicht
  das Layout, das vorherrschende Risiko sind; füge `ui-finish-gate` hinzu, wenn ein gesonderter visueller
  Durchgang vor der Auslieferung wesentlich ist. Löse jeden blockierenden Befund und zeichne Aufgaben-ID,
  Urteil und Erledigung in den Verifikationsbelegen oder im Abschlussbericht des Epics auf. Gibt es keine
  Auswirkung auf Menschen, sag im Plan, warum, und füge kein Review-Zeremoniell hinzu. Halte den Umfang
  fest, den dieses Review abgedeckt hat. Dispatche normalerweise jeden zuständigen Spezialisten nur einmal
  für das integrierte Epic; schick UX, Marke, Sicherheit und andere Rollen nicht routinemäßig als
  Checkliste los. Erledige kleine Korrekturen an Texten, Abständen, Tests oder Befunden innerhalb des
  Umfangs selbst, mit gezielten Prüfungen. Dispatche nur dann erneut, wenn eine spätere Änderung die
  Nutzerreise, die Produktrichtlinie, die Markenrichtung, die Sicherheitsgrenze oder ein anderes Risiko
  außerhalb dieses festgehaltenen Umfangs wesentlich verändert; nenne die geänderte Grenze und fordere nur
  den dafür zuständigen Spezialisten an. Dieses Review ersetzt nie das erfasste Plan-Gate oder das PASS
  des Prüfers für genau den Kandidaten am Verifikations-Gate.
- **Du bleibst für das Epic verantwortlich, nachdem jedes Child gemergt ist.** Lies sofort das Item dieses
  Childs und `clawdline item steps <child id>` neu; prüfe, dass jeder Schritt abgeschlossen ist. Ein Merge
  schließt das Child nicht, und `merging` ist kein Ruhezustand. Die besitzende Session des Childs muss alle
  verbleibenden Schritte abschließen, eine Landing-Quittung für genau den Commit aufzeichnen, der vom
  lokalen Ziel und von `origin/main` aus schon erreichbar ist, und dann `deploying` → `done` voranbringen,
  mit Deployment-Belegen oder einem Grund ohne Deployment, der zu ihrer Deployment-Richtlinie passt. Gehört
  dir das Child, führe diese Schritte selbst aus. Gehört es einer anderen Session, hake bei diesem
  Eigentümer nach oder nimm den autorisierten Weg zur Neuzuweisung des Childs; gib dich nicht als dessen
  Eigentümer aus (`not_item_owner`). Bestätige die Abschlussmeldung des Brokers mit ACK, wo es eine gibt,
  ordne dann die Worktree-Reste ein und entferne nur Material, das nachweislich identisch gelandet ist oder
  nur vorübergehend zur Aufgabe gehörte. Behalte nicht gelandete, gemischte oder unbekannte Bytes für den
  nächsten Eigentümer. Erkläre das übergeordnete Epic erst dann für erledigt, wenn jedes Child `done` oder
  `cancelled` ist: `epic_children_open` nennt, wie viele noch übrig sind. Lege kein anderes Board-Item an
  als die Children des Epics.
- **Ein abgeschlossenes Child schließt nicht seine unabhängige Feature-Root-Session.** Verwende für jeden
  Root, den das Epic mit `--assign-new` geöffnet hat, `clawdline session close --dry-run --terminal <id>`
  (es antwortet `closing as epic_owner` nur für einen Root, den dieses Epic geöffnet hat), um seine
  `closeability` zu lesen; schließ nicht aus einem Label oder einer Terminalposition auf die
  Eigentümerschaft, und behandle `clawdline session report` nicht als Schließung. Nachdem das Child `done`
  erreicht hat, bitte den Eigentümer dieses Roots, seine eigenen Aufgaben, Landings, Meldungen, To-dos und
  seinen Worktree zu prüfen und dann seinen Schließungsbericht abzuschließen. Hol eine Bestätigung nur über
  eine Route ein, die der aktuelle Daemon unterstützt; die Schließungsroute der außer Dienst gestellten
  Swift-App ist keine solche Route. Ist diese Route oder ein abgesichertes Schließen nicht verfügbar,
  zeichne die Produktblockade und den nächsten Eigentümer auf und behalte die Session. Nur wenn Identität
  und Arbeit verifiziert sind und
  `closeability.state=safe` gilt, darf `clawdline session close --terminal <id>` sie beenden; der Befehl
  liest zuerst den Bestand neu, und ein zweiter Lauf antwortet `session_not_found`, sobald sie weg ist.
  Umgehe den Schutz nicht mit `clawdline close <terminal id>`. Hake bei
  `blocked` beim genannten Verantwortlichen nach. Bei `unknown` (einschließlich `terminal_unreadable`)
  behalte die Session und zeichne die fehlenden Belege und den nächsten Eigentümer auf; schließ sie nicht
  zwangsweise, archiviere sie nicht und behaupte nicht, sie sei bereinigt. Bevor du die Koordination des
  Epics für beendet erklärst, zähle für jeden Root das Schließungsergebnis oder die benannte Blockade auf.
  `done` auf dem Board ersetzt diese Bestandsaufnahme nicht.

## 11. Koordination

**Der Rechner-Koordinator („Clawdfather“).** Er arbeitet aus einem vom Daemon verwalteten Rechner-Arbeitsbereich außerhalb der Projects, um über Sessions zu berichten und unterstützte Rechner-Operationen zu verwalten. Er bearbeitet nie Quellcode eines Projects, auch nicht den von Clawdline. Bittet der Mensch ausdrücklich um Entwicklungsarbeit, lege zuerst mit `clawdline item add --project … --assign-new` ein Board-Item im Project an und delegiere dann an eine Project-Session. Ohne diese Bitte schlag ein Item vor, das der Mensch annehmen kann. Der zugewiesene Project-Eigentümer kümmert sich um Child-Dispatch, Verifikation und Landing. Der Arbeitsbereich ist eine organisatorische Grenze, keine Dateisystem-Sandbox. Neue Bindungen müssen aus diesem Arbeitsbereich kommen; bestehende Bindungen bleiben lesbar. Öffne die Session über die eigene Clawdfather-Aktion der Konsole und registriere dann ihre Gesprächs-ID. Die Produktgrenze steht in `docs/clawdfather-role.md`.
Führe `clawdline coordinator bind` in dieser neuen Session aus, um sie zu registrieren oder einen nachweislich offline gegangenen Vorgänger neu zu binden. Der Befehl liest seine eigene Gesprächs-ID und weigert sich, einen Inhaber zu ersetzen, der online oder nicht lesbar ist.
`GET /v1/orchestrator/coordinator` zeigt die Rolle an;
`/coordinator/bearings` ist der Rechner auf einen Blick (aktive Aufgaben, ausstehende Landings, offene
Wartevorgänge, unzustellbare Nachrichten, gehaltene Leases und was `unknown` ist).
`POST …/coordinator/register` mit `{"session_id": "<conversation id>"}` übernimmt die Rolle;
`POST …/coordinator/rebind` verschiebt sie, sobald die gebundene Session offline ist
(`expected_coordinator_id`, `expected_generation`). Die Nachfolge antwortet mit
`501 succession_unavailable`.

**Warten auf Dateien.** Ein Wartevorgang sagt: „Sag mir Bescheid, wenn der Eigentümer mit diesen Pfaden
fertig ist.“ Er ist ein Datensatz und eine Nachricht, keine Sperre und keine Dateiüberwachung.

- `POST /v1/orchestrator/waits` –
  `{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}`
  (Session-IDs sind Gesprächs-IDs). Der Eigentümer wird einmal benachrichtigt, in seinem Eingabefeld.
- Der Eigentümer beendet ihn: `POST /v1/orchestrator/waits/<id>/release` mit `{"owner_session_id",
  "commit"?, "note"?}`; jeder Wartende wird benachrichtigt. Nichts gibt einen Wartevorgang nach Zeit frei.
- Ein Wartender steigt aus: `POST …/waits/<id>/cancel` mit `{"waiter_session_id"}`.
- `409 owner_busy` und `502 request_delivery_failed` bedeuten, **dass der Wartevorgang aufgezeichnet
  wurde**, der Eigentümer aber noch nicht benachrichtigt ist. `502 release_incomplete` listet auf, wer noch
  aussteht: Sende die Freigabe erneut.

**Leases.** Zwei Ressourcen: `heavy_compile` (der einzige Compile-Slot des Rechners) und `landing` (eine
pro Checkout).

**Führe einen Build oder eine Testsuite über `clawdline heavy -- <command>` aus**, nicht direkt. Er reiht
sich für `heavy_compile` ein, wartet, bis der Rechner Speicher frei hat (ein Viertel davon, höchstens
1 GB, und kein Speicherstau über 10 %), führt den Befehl mit niedrigerer Priorität aus – unter Linux auch
als Erstes, was der Kernel beendet, wenn der Speicher ausgeht –, erneuert die Lease, während er läuft, und
gibt sie danach frei. Er behält den Exit-Status des Befehls. Er verweigert den Build nie wegen eines
fehlenden Daemons oder einer Ablehnung, die er nicht kennt: Er führt den Befehl trotzdem aus, mit einem
Satz auf stderr. Verstreicht `--max-wait` (Standard 30m), bevor er den Slot und den Speicher hat, gibt er
seinen Platz auf, führt den Befehl nicht aus und endet mit **75** – einem Code, den man mit keinem
eigenen Fehlschlag eines Befehls verwechselt; führe ihn später erneut aus. Während er wartet, gibt er eine
Zeile aus, wenn das Warten beginnt, und eine, wenn es endet, dazwischen nichts: Warte einmal und lange
darauf (§2, „Auf einen langen Befehl warten“). Ein `heavy` innerhalb eines `heavy` läuft direkt.
`--min-available 1500M` verlangt mehr; `--no-slot` prüft nur den Speicher. In einem Repository, das es hat,
findet `tools/heavy.sh <command>` das Binary für dich.

- `POST /v1/orchestrator/leases` –
  `{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`.
  Antwortet `granted` oder `queued` mit einer `position` und `retry_after_seconds`. Eine eingereihte
  Anfrage fragt mit derselben `request_id` erneut.
- `POST …/leases/renew | release | cancel` mit `{"request_id", "resource", "checkout"}`. Der Inhaber
  erneuert mit `/renew` (erneut `POST /v1/orchestrator/leases` mit der eigenen `request_id` zu fragen,
  erneuert ebenfalls).
- Erneuere innerhalb von 60 Sekunden, sonst gilt die Lease als verloren. `409 lease_lost` bedeutet, dass
  sie verloren ist. `429 queue_full` bei 32 Wartenden.

**Graphen** (`GET /v1/orchestrator/graphs`) sind schreibgeschützte Ansichten, berechnet aus den
`graph`-Feldern dispatchter Aufgaben. **Reclaim** (`/v1/orchestrator/reclaim`) räumt fertige Checkouts
auf; ein POST ist ein Probelauf, es sei denn, der Body sagt `{"dry_run": false}`.

## 12. Was zu tun ist, wenn etwas abgelehnt wird

- Verzweige nach `error.code` (oder `error` in der flachen Form). Die Meldung ist für Menschen.
- `retry_after` bedeutet, dass es eine Kapazitätsantwort ist: Warte so lange und sende dann dieselbe Anfrage.
- `409 stale_write`, `503 orchestrator_store_busy`: Der Speicher war belegt; dieselbe Anfrage erneut zu
  senden, ist sicher.
- `unknown` bei Zuständigkeit, Lebenszeichen oder Herkunft bedeutet, dass
  der Daemon die jeweiligen Daten nicht lesen konnte. Es heißt nicht „nicht vorhanden“, und aufgrund dessen darf nichts
  gelöscht oder für tot erklärt werden.
- Antwortet eine Route, die du erwartet hast, mit `404 not_found` oder `501`, ist sie nicht in diesem
  Daemon. Sag das; weiche an ihrer Stelle nicht auf die Routen der Swift-App oder auf
  anbietereigene Subagents aus.
