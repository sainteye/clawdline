# Guide Clawdline

Pour une session d'assistance — Claude Code ou Codex — sur une machine où **Clawdline Next** est exécuté. Ce guide
décrit les fonctionnalités actuelles de ce démon, et rien d'autre : chaque route ci-dessous est enregistrée par la
version qui a généré ce guide, et un test échoue si l'une d'elles ne l'est pas. Réimprimez-le avec
`clawdline guide` plutôt que de vous fier à une copie ; `clawdline guide zh-Hant` affiche le guide
en chinois traditionnel de Taïwan (`zh-TW` reste un alias). `clawdline guide` affiche le noyau et nomme les autres composants. Imprimez une partie
(`clawdline guide fr dispatch`) lorsque vous atteignez la section correspondante, ou `clawdline guide fr all` pour
le texte complet. Quel que soit le contenu imprimé, y compris le noyau, il commence par `guide-version: <sha256>` ;
exécutez la même commande avec `--since <hash>` et, si ce texte reste inchangé, elle imprimera la ligne
`unchanged <hash>` à la place. `clawdline guide fr refused <code>` imprime la partie expliquant un
code de refus et se termine avec le code 1 sans rien afficher sur la sortie standard si aucune partie ne le nomme.

Dans ce guide, une **étape** correspond à une entrée de la liste de contrôle d'un élément, les **écritures** d'une tâche correspondent aux chemins qu'elle
peut modifier, l'**affectation** indique le propriétaire d'un élément et une **partie** correspond à une partie nommée de ce guide.

## 0. Si vous avez appris Clawdline grâce à l'application Swift, lisez ceci en premier.

L'application Swift a été mise hors service le 19/09/2026 : elle est arrêtée, ne se lance plus à l'ouverture de session et rien ne répond sur le port 7717. Son répertoire, `~/.config/clawdline`, est toujours présent sur le disque et est toujours accessible en lecture seule, pour un historique que ce démon n'a jamais conservé. Ce démon n'est pas une copie de cette application, et voici cinq différences qui peuvent poser problème :

1. **Les répertoires de tâches sont `<state dir>/tasks`, et non `/tmp/.clawdline`.** `/tmp/.clawdline` était celui du
broker Swift ; deux brokers écrivant dans un même répertoire d'identifiants de tâches auraient provoqué un conflit invisible.
Ne codez pas en dur : lisez `task_root` dans l'inventaire (§3) et écrivez `task.json`
en dessous.
2. **Il n'y a pas d'enveloppe de workflow ni de route de workflow à appeler.** Les messages ne comportent plus de
classification de tableau, et une session n'ouvre jamais de cartes de tableau automatiquement. `POST /v1/orchestrator/sessions/<terminal>/workflow` existe encore uniquement pour éviter qu'un ancien assistant ne
tombe en panne en cours de tour : il répond à `workflow_retired`, n'enregistre rien et le nouveau code ne doit pas l'appeler.
3. **La participation à Board passe par des propositions et des décisions** (§10) : une session propose, une
personne répond. Il n'y a rien à faire avec `begin` ou `deliver`.
4. **Une autre porte.** Port 7727 (ou `CLAWDLINE_NEXT_PORT`), état dans `~/.config/clawdline-next`
(ou `CLAWDLINE_NEXT_DIR`). Ne jamais lire `~/.config/clawdline` : son jeton n'appartient pas à ce démon et
est refusé avec `401 unauthorized`.
5. **Éléments que l'application Swift possédait et que ce démon ne possède pas :** promotion de rapport durable (répond
`501 durable_report_promotion_unsupported`), succession de coordinateur (répond
`501 succession_unavailable`) et les champs abrégés `serialize`
et `attach_session` (chacun refusé par son nom avec `bad_task`). `reasoning_effort` est pris en charge :
`high` ou `xhigh`, uniquement pour une tâche `codex`.

## 1. Agent racine ou agent enfant

Si votre premier message contenait *"You are a Clawdline CHILD agent for task …"*, vous êtes un **enfant**. Le
`CHILD.md` qu'il désigne vous régit : vous ne répartissez pas les tâches, vous n'envoyez pas d'accusé de réception, vous signez avec
`clawdline task accept` et vous terminez avec `clawdline task finish`. Arrêtez votre lecture ici.

Sinon, vous êtes un **agent racine** : une session ordinaire à laquelle une personne communique. La suite vous concerne.

Si votre premier message contenait *"You are an independently owned Clawdline Feature Root …"*, ou si un élément Board vous a été attribué, affichez ensuite `clawdline guide fr feature-root` : il s’agit du chemin d’accès normal complet, de la lecture de l’élément à `done`, et il indique la partie à afficher pour les cas plus rares.

## 2. Accès au démon

**Utilisez les commandes, et non une version personnalisée de curl, si elle existe.** Elles lisent les informations d'identification au sein de leur
propre processus ; elles n'apparaissent donc jamais dans la ligne de commande, dans `ps`, dans leur sortie ou dans votre
transcription. Une version personnalisée de curl sans ces informations d'identification répond `401 unauthorized` (« Aucune information d'identification valide n'a été fournie avec cette requête… ») : c'est l'information d'identification qui manque, et non une autorisation. Exécutez plutôt la commande.

| Commande | Fonctionnement |
|---|---|
| `clawdline guide [lang]` | Ce guide. Aucun démon requis. |
| `clawdline session report --summary "…"` | Enregistre votre tour terminé (§7). |
| `clawdline session close [--dry-run] [--terminal id]` | Vérifie et clôture un tour Session terminé, jamais par la force (§2a). |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | Envoie un enfant dont vous êtes propriétaire (§4). |
| `clawdline item show\|steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | Lit et fait avancer un objet Board dont vous êtes propriétaire (`clawdline guide fr feature-root`, §10). |
| `clawdline todo add\|list\|done` | cette Session effectue ses propres tâches, uniquement à la demande de la personne (§10) |
| `clawdline heavy -- <command…>` | Exécute une compilation ou une suite de tests dans l'unique emplacement de compilation de la machine (§11) |
| `clawdline send --to <terminal> "…"` | Transmet un message dans une autre session (§8) |
| `clawdline notify --title "…" --body "…"` | Envoie une notification à la personne (§9) |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | Ajoute une note exploitable au-dessus d'une Session (§9a) |
| `clawdline assistants` | Le solde restant sur le compte de chaque assistant |
| `clawdline landings` | Tous les intégrations encore dus sur cette machine ; `--work-id <item id>` : Chaque intégration enregistré pour un élément Board |
| `clawdline leases [--json]` | Qui détient l'emplacement de compilation et chaque bail d'intégration, et qui attend derrière ? |
| `clawdline sessions [--json]` | Les sessions qu'un envoi, une attente ou un transfert peut nommer, avec leur état et leur tâche |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | Ce qu'une session, une tâche enfant ou un élément Board a dépensé, par catégorie ; le vôtre par défaut |
| `clawdline cloud pair [--offer <code>]` | Associe un navigateur Cloud à cette machine |
| `clawdline task show [--json] <task id>` | Une tâche enfant en résumé : état, verdict, résumé, titres restants, vérification, intégration, sortie (§5) |
| `clawdline task wait <task id>… [--timeout 9m] [--any]` | Attend la fin des tâches enfants (toutes, ou `--any` une seule), affiche chacune d'elles comme le fait `task show` et ferme sa notification. Sortie : 0 (toutes réussies), 1 (une tâche a échoué), 5 (une tâche a été annulée et aucune n'a échoué), 3 (délai d'attente dépassé), 4 (tâche illisible) ; 4 sur 3 sur 1 sur 5 (§5) |
| `clawdline callback --title "…" [--timeout 30m] [--work-id <item>] -- <command…>` | Exécute une commande longue (un déploiement et sa vérification, une attente sur l'élément de configuration) sous le démon et retourne immédiatement ; terminez votre tour ; à la fin, le démon insère un `<clawdline-notice>` comme pour un agent enfant terminé (§5a, `clawdline guide fr callback`) |
| `clawdline task cancel <task id> --reason "…"` | Arrête un processus enfant lancé par erreur : son onglet est fermé, ses écritures et son emplacement sont libérés, une branche contenant les commits est conservée (§5) |
| `clawdline task ack <task id> <notice id>` | Ferme manuellement une notification de fin de processus ; rarement nécessaire, car `task show` et `task wait` la ferment (§5) |
| `clawdline task accept <task dir>` | Signature d'un processus enfant pour son briefing. Les processus racines ne l'exécutent jamais |
| `clawdline task finish <task dir>` | Fin de processus enfant. Les processus racines ne l'exécutent jamais |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | Lance une planification via son webhook Cloud, sur n'importe quelle machine, et attend son résultat ; le code de sortie indique comment cela s'est terminé (« Planifier le travail futur »). Aucun démon requis. |

Sans langue explicite pour le guide, la CLI choisit dans cet ordre : `--lang <tag>` avant la commande,
`CLAWDLINE_LANG`, `product_language` enregistré, puis l'anglais. Un `clawdline guide <tag>` explicite
remplace ce choix ; une langue non prise en charge affiche l'anglais. `clawdline guide -list` énumère
les neuf tags fournis. Cette préférence ne change que le texte de la CLI destiné aux personnes ;
elle ne modifie ni les champs du protocole ni la langue de l'Agent.

Les commandes d'orchestration ci-dessus (sauf `webhook fire`) affichent le JSON du démon en cas de succès. En cas de refus, le programme affiche :
`refused, <status> <code>: <message>`, puis chaque valeur scalaire du refus sous la forme `key: value`, une par ligne,
et sa correction en dernier, puis se termine avec le code 1. Les commandes Cloud utilisent leur propre affichage de succès et d'erreur lisible par l'utilisateur.
`--port` remplace le port.

`clawdline usage` est le registre des jetons (`docs/token-ledger.md` dans le dépôt) : il indique l’utilisation de chaque jeton
— `board`, `protocol`, `rules`, `impl`, `delegate`, `harness`, `talk`, `compaction`,
`other`. Sans indicateur, il lit votre propre session, identifiée par `CLAUDE_CODE_SESSION_ID` ou
`CODEX_THREAD_ID`. Il affiche une ligne d'en-tête (appels, contexte de pointe, coût), une ligne par catégorie par
coût — part, jetons, coût — puis chaque intervalle ; `--json` affiche la réponse du démon. `rules` est une
limite supérieure, et l'indique : une garde exécutée dans une commande shell avec d'autres tâches occupe toute cette commande.
Les routes sont `GET /v1/usage/sessions/<conversation>`, `GET /v1/usage/tasks/<task id>` et
`GET /v1/usage/items/<item id>`, lues avec un périphérique apparié ou le jeton de l'orchestrateur. Une session que le
registre n'a pas lue, ou ne peut plus lire, répond `not_yet_read`, `transcript_missing` ou
`transcript_unreadable` — jamais un total vide ; Un identifiant inconnu est 404 `unknown_session`,
`unknown_task` ou `unknown_item`. La lecture du registre est-elle toujours en cours ? `usage` dans
`/v1/diagnostics`.

**Attente d'une commande longue.** `clawdline heavy`, `clawdline dispatch` et un test long n'affichent rien pendant l'attente et se terminent automatiquement. Attendez une commande avec une **longue attente**, et non en
vérifiant toutes les quelques secondes : chaque vérification correspond à un cycle de relecture de l'intégralité du contexte, et un jeton
a comptabilisé 520 cycles de ce type (72,2 millions de jetons) sur dix éléments, principalement sur des exécutions `heavy` en file d'attente.

- **Claude Code :** un appel Bash avec un long `timeout` (jusqu'à `600000` ms), ou `run_in_background`
puis plus rien jusqu'à la réception de la notification de fin. Il ne s'agit pas d'une boucle `sleep` et `tail`.
- **Codex (codex-cli 0.157.1, mode code) :** placer `// @exec: {"yield_time_ms": 600000}` sur la
première ligne de la cellule `functions.exec`. Après que `exec_command` ait renvoyé un ID de session, attendez
`write_stdin` avec des cellules vides `chars` et `yield_time_ms: 300000` ; si l'exécution se poursuit, répétez l'opération dans
cette même cellule. Mesures : la cellule externe est restée ouverte pendant 330 secondes avec `600000`, tandis qu'une cellule vide `write_stdin` a attendu jusqu'à 300 secondes. Si la cellule externe cède, utilisez `wait` avec une longue
`yield_time_ms` pour la récupérer.

Utilisez une cellule pour une attente enfant ou une construction. Remplacez uniquement la commande ; Conservez l'interrogation de session
à l'intérieur de la cellule afin qu'un retour normal de 30 secondes (`exec_command`)
ne réveille pas l'agent pour qu'il effectue à nouveau la même attente :

  ```js
  // @exec: {"yield_time_ms": 600000}
  let r = await tools.exec_command({cmd: "clawdline task wait --timeout 9m TASK_ID", yield_time_ms: 30000});
  while (r.session_id) {
    r = await tools.write_stdin({session_id: r.session_id, chars: "", yield_time_ms: 300000});
  }
  text(r.output);
  text(`exit ${r.exit_code}`);
  ```

Pour une compilation, remplacez la commande par `tools/heavy.sh …` et conservez son
code de sortie : 75 signifie que l'attente de l'emplacement de compilation ou de la mémoire a expiré avant l'exécution de la compilation.

`clawdline heavy` attend au maximum `--max-wait` (30 minutes par défaut) puis se termine avec le code 75 sans exécuter la
commande ; une attente plus longue que celle autorisée par votre outil correspond à une exécution en arrière-plan.

**Utilisez Curl vers une route d'orchestration.** Lisez `<state dir>/orchestrator-token` et envoyez-le dans l'
en-tête `X-Clawdline-Orchestrator`. N'utilisez pas le jeton dans les arguments de commande : utilisez
`DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"`, puis
`-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")`.
Utilisez `curl --fail-with-body` ; chaque requête POST avec un corps JSON nécessite également
`-H 'Content-Type: application/json'` (sinon `415 unsupported_media_type`).

### Associer un navigateur Cloud

L'association modifie les personnes autorisées à accéder à cette machine. Un navigateur associé peut y accéder immédiatement et, lorsque
Cloud `commands` sont activés, peut la piloter. N'exécutez une commande d'association que si la personne demande explicitement
à associer ce navigateur ou vous fournit la commande ou l'offre d'association exacte. L'association n'active pas
les commandes ; Cela reste un paramètre distinct.

Deux actions sont possibles :

1. **Le navigateur affiche une offre.** Saisissez la commande exacte affichée sur votre ordinateur :

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

Conservez les guillemets simples. L'offre est un secret opaque, éphémère et à usage unique : ne la décodez pas,
ne la modifiez pas, ne la stockez pas et ne la répétez pas dans la réponse finale. Si elle expire ou a déjà été utilisée, obtenez une nouvelle
offre depuis le navigateur au lieu de la réessayer ou de la modifier.
2. **La machine génère l'invitation.** Exécutez `clawdline cloud pair`. Elle imprime un lien unique
`https://app.clawdline.com/#pair=…` et attend. La personne ouvre ce lien complet dans le
navigateur avec lequel elle souhaite se connecter, tout en étant connectée au même compte Clawdline Cloud. Traitez le lien
comme l'offre : ne le publiez pas et ne le conservez pas.

En cas de succès, trois lignes s'affichent : `paired` indique l'identifiant du navigateur, `browser` est l'empreinte numérique du navigateur et `machine` est l'empreinte numérique de la machine. Comparez l'empreinte numérique du navigateur avec celle affichée dans le navigateur et l'empreinte numérique de la machine avec celle affichée pour cette machine. Une différence n'est pas considérée comme un succès : exécutez immédiatement `clawdline cloud revoke <device-id>` en utilisant l'identifiant `paired`, puis signalez la différence. `clawdline cloud devices` liste la liste des navigateurs actuels et leur état de confiance local ; il s'agit également de la vérification en lecture seule à utiliser après l'appairage.

Ces commandes s'exécutent via le démon local. En cas d'échec, signalez la sortie d'erreur standard exacte. Ne pas
activer Cloud, se connecter, activer les commandes, modifier les clés ou remplacer l'offre fournie, sauf si la
personne a demandé cette modification séparément.

**Où se trouvent les éléments ?**

- Port : `CLAWDLINE_NEXT_PORT`, sinon **7727**. Bouclage uniquement : `http://127.0.0.1:<port>`.
- Répertoire d'état : `CLAWDLINE_NEXT_DIR`, sinon `$XDG_CONFIG_HOME/clawdline-next`, sinon
`~/.config/clawdline-next` (`%APPDATA%\clawdline-next` sous Windows).
- `GET /v1/health` ne nécessite aucune authentification et répond à `served_by: "clawdline-go"`. Utilisez-le pour distinguer
« non exécuté » de « refusé ».

**Identifiants.** Il en existe trois, et une session utilise le premier :

| Identifiant | Où | Envoyé comme | Ouvre |
|---|---|---|---|
| Jeton d'orchestrateur | `<state dir>/orchestrator-token` | en-tête `X-Clawdline-Orchestrator` | Tout sous `/v1/orchestrator/`, `/v1/work/`, `/v1/board`, `GET /v1/places`, `POST /v1/artifacts/images` |
| Secret de tâche | choisi par la racine lors de la distribution | en-tête `X-Clawdline-Task-Secret` | Routes propres à un enfant sous `/v1/orchestrator/tasks/<id>/` et `POST /v1/orchestrator/proposals` |
| Jeton de périphérique | `<state dir>/local-token`, ou celui d'un périphérique apparié | `Authorization: Bearer` | Routes de la console (`/v1/sessions/…`). Une session n'en a pas besoin |

Le jeton d'orchestrateur envoyé sous `Bearer` est comparé aux périphériques et refusé. Un jeton incorrect ou manquant
est refusé par `401 unauthorized`, indiquant que le jeton est manquant, n'appartient pas à ce démon ou que le
périphérique n'est pas apparié. `clawdline doctor` affiche le répertoire et le port lus par l'interface de ligne de commande.

**Lorsque vous devez utiliser curl**, ne saisissez pas le jeton dans la ligne de commande :

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

- `--fail-with-body` : sans ce jeton, un refus renvoie le code 0 et est interprété comme une réussite.
- **Toute requête POST avec un corps nécessite `-H 'Content-Type: application/json'`**, sinon elle est refusée avec
`415 unsupported_media_type`. `curl -d` seul indique le type du formulaire.
- La taille du corps des requêtes est limitée à 2 Mio, sauf indication contraire dans une route.
- Un identifiant de terminal tmux tel que `%47` est inséré dans un chemin échappé en un seul segment : `%2547`.

**Les refus se présentent sous deux formes.** La branche se base sur le code, jamais sur la phrase :

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}` — la porte et le courtier.
Des éléments supplémentaires tels que `retry_after` sont inclus dans `error`.
- `{"error":"<code>","detail":"…"}` — erreurs de routage, méthodes incorrectes et certaines lectures.

Un itinéraire qui n'appartient pas à ce démon est refusé sous la forme `501 not_implemented`, et le refus nomme l'itinéraire.
C'est la réponse sur n'importe quelle machine standard. Le transfert n'est effectué que lorsqu'un utilisateur a délibérément placé un autre démon derrière celui-ci (`CLAWDLINE_NEXT_UPSTREAM_PORT`), puis qu'un autre démon (`502
upstream_unreachable`) spécifie l'adresse qui n'a pas répondu. Ce démon ne répond pas non plus. Avant le 19/09/2026, le transfert était activé par défaut et redirigeait vers l'application Swift sur le port 7717. Par conséquent, une note écrite à cette époque indiquerait qu'une route non gérée atteint cette application ; ce qui est faux.

### Configurer l'affichage Clawdline d'un projet

Utilisez cette partie lorsque l'on vous demande de rendre le projet sur lequel vous travaillez lisible dans
Clawdline. Le résultat n'est pas simplement « certains fichiers existent », mais le projet doit avoir un nom et une marque corrects, les tâches de longue durée peuvent afficher leur progression et ses serveurs de développement sont visibles sans avoir à les démarrer.
Clawdline

Commencez par lire les instructions de ce dépôt, le fichier README, les scripts de déploiement/construction et la configuration existante du gestionnaire de processus.
Conservez les commandes déjà utilisées par le projet. N'ajoutez pas de deuxième chemin de déploiement ni de superviseur de processus uniquement pour Clawdline, et ne démarrez, n'arrêtez, ne redémarrez ni ne déployez rien, sauf si l'on vous a demandé cette modification opérationnelle. La configuration et le déploiement proprement dit sont deux tâches différentes.

Effectuez ces quatre vérifications, en ignorant une vérification uniquement si elle ne s'applique pas :

1. **Projet.** Exécutez `clawdline project list`. Si cette extraction est absente, ajoutez la racine de son dépôt
avec `clawdline project add <absolute-root>` et listez à nouveau. Ceci enregistre un emplacement à partir duquel un
Session peut démarrer ; cela ne modifie pas le dépôt.
2. **Nom et icône.** Clawdline génère une icône stable lorsqu'aucune n'est configurée. Si l'utilisateur souhaite
un nom ou une icône spécifique, conservez toutes les autres entrées dans `~/.claude/project-icons.json`
et modifiez uniquement le chemin le plus long contenant ce projet. Le format est documenté dans le
dépôt Clawdline, dans `docs/project-status.md` ; la page Projets peut également copier une icône résolue existante
sans modification manuelle du JSON. Un fichier utilisateur global ne fait pas partie du contenu du dépôt : affichez
l'entrée proposée exacte avant de la modifier si la requête n'a pas déjà autorisé cette modification.
3. **Déploiement et opérations longues.** Clawdline lit uniquement les accusés de réception ; il n'effectue jamais de déploiement. Pour un
dépôt GitHub, un accusé de réception de déploiement est
`~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`, où le propriétaire et le dépôt proviennent de `origin`. Le producteur qui connaît déjà l'exécution écrit `state` (`running`, `ok`, `fail` ou `none`),
`label`, `url`, `started_at` et une valeur mesurée de `typical_seconds`, de manière atomique. Pour une compilation locale,
une commande de test, d'importation ou de déploiement, utilisez `clawdline-progress run --label <label> -- <command>` si
cette fonction d'assistance existe, ou implémentez le contrat `run-<path>.json` à partir de `docs/project-status.md`.
Ne jamais inventer une durée ; omettez-la jusqu'à ce qu'elle ait été mesurée. Un producteur arrêté ne doit pas laisser d'état d'exécution permanent.
4. **Serveurs de développement.** Ajoutez ou mettez à jour `.devstack.json` à la racine déployable la plus proche. Le démon Go lit actuellement le fichier `processes` déclaré et sonde leur interface de bouclage `port` ou ouvre leur interface
`url` ; il n'exécute **pas** les commandes `status`, `up`, `down`, `restart` ou `logs` depuis le
navigateur. Privilégiez le plus petit fichier Tier 0 valide, par exemple :

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

Un processus sans port ni URL stables ne doit pas être ajouté au fichier. Ne pas sonder ni
redémarrer la production pendant la vérification d'une déclaration de développement.

Vérifiez chaque couche modifiée séparément : `clawdline project list` nomme le répertoire d'extraction ; chaque fichier JSON
est analysé ; les tests internes du dépôt pour les scripts modifiés réussissent ; `GET /v1/devstacks` affiche les serveurs déclarés comme étant en cours d'exécution, arrêtés ou inconnus au lieu de les omettre silencieusement ; et une Session dans le
projet indique un accusé de réception de progression/déploiement récent. Si une lecture est indisponible, malformée ou obsolète, indiquez
laquelle et laissez-la inconnue — ne signalez jamais une absence comme un succès. Terminez en listant ce qui a été
configuré, ce qui n'était pas intentionnellement applicable et tout fichier appartenant à l'utilisateur modifié en dehors de git.

**Unification : un ensemble de règles et de compétences pour Claude et Codex** (`/clawdline unify`). Codex lit
`AGENTS.md` et `.agents/skills/<name>/` ; Claude lit `CLAUDE.md` (et `AGENTS.md` uniquement lorsque
`CLAUDE.md` est absent ou contient une ligne `@AGENTS.md`) et `.claude/skills/<name>/`. Un projet est unifié
lorsque ses règles se trouvent dans `AGENTS.md`, `CLAUDE.md` étant absent ou l'important, et que chaque compétence se trouve dans
`.agents/skills/<name>/` avec `.claude/skills/<name>` un lien relatif vers celui-ci. Lorsque la personne le demande
ou invoque `/clawdline unify` :

1. Exécutez `clawdline project unify` dans le projet Session (le projet racine git ; ajoutez d'abord le projet avec
`clawdline project add` s'il n'est pas listé). Cela ne change rien. Montrez à la personne, dans sa
langue, ce que Claude et Codex lisent actuellement et liront ensuite, chaque ligne de compétence, chaque action
phrase et chaque conflit — y compris les lignes `CLAUDE.md` que Codex ne voit pas.
2. Exécutez `clawdline project unify --apply` uniquement après que la personne, dans cette conversation, a approuvé ce plan. Il envoie la version que vous avez montrée ; si le disque a été modifié depuis, il répond
`plan_changed` et n'applique rien — imprimez à nouveau le plan et posez à nouveau la question.
3. Affichez le résultat de `clawdline project unify --check` (sortie 0 unifiée, 1 en dérive, 3 inconnue).
Aucune action n'est validée ; Indiquez quels fichiers ont été modifiés afin que la personne concernée (Session) puisse les enregistrer.

Ne résolvez jamais un conflit vous-même en modifiant `AGENTS.md`, `CLAUDE.md` ou une compétence sans que la personne concernée ne l'y autorise.
Une compétence différente entre les deux répertoires, un lien pointant ailleurs, ou des règles que Codex ne voit pas relèvent de sa responsabilité. Les itinéraires possibles sont :
`GET /v1/projects/{place}/unify` (le plan) et `POST /v1/projects/{place}/unify` avec
`{"version"}` et `Idempotency-Key`. Les refus sont `plan_changed`, `plan_unknown` (une partie du projet n'a pas pu être lue, donc rien n'a été modifié) et `name_taken` (un nom que l'unification créerait existe déjà ; rien n'est écrasé).

## 2a. Chemin d'exécution habituel d'un Feature Root

Voici l'ensemble des opérations effectuées par un Feature Root ordinaire — une Session possédant un élément Board —, dans l'ordre.
Chaque étape est une commande : les commandes contiennent les informations d'identification, et une requête curl manuelle vers le même itinéraire
est refusée. Tout ce qui est plus rare est accessible via `clawdline guide <part>` ; les pointeurs se trouvent à la fin.

**1. Lire l'élément.** `clawdline item show <item id>` affiche son type, sa phase, ses critères d'acceptation,
les portes capturées, la version d'acceptation (`acceptance vN`), pour un Feature le commutateur « Besoin d'une revue indépendante » de la personne,
ses étapes et chaque document avec son contenu. C'est
l'enregistrement sur lequel vous travaillez. `clawdline item show <item id> --doc <doc id>` affiche le contenu d'un document
uniquement, à rediriger vers un fichier ; `clawdline item steps <item id>` est le même enregistrement sans les contenus. Chaque élément
affiche `wrote …; item <id> is at version N`, un bref résumé de l'élément et un `item show`
indice. Lisez l'intégralité de l'acceptation, les étapes et les documents avec `item show`. Les écritures agissent sur la version actuelle de l'élément, sauf si vous passez
`--expected-version`.

Si votre fichier ASSIGNMENT.md comporte un en-tête **HANDOFF**, vous reprenez un élément laissé en cours de traitement par un autre Session
(réassigné après le début de l'implémentation, avant la fin). Lisez d'abord le dossier mentionné
avant de planifier. Le démon l'a construit à partir de ses propres enregistrements et de Git, sans consulter le
propriétaire précédent : les tâches liées à l'élément et leurs résultats, les commits non encore intégrés,
les modifications non validées de chaque répertoire de travail enregistrées sous forme de patch avec son hachage SHA-256 et la commande `git apply` qui
le restaure sur sa base, le dernier message du propriétaire précédent (ou la raison pour laquelle il n'a pas pu être lu), ainsi que
la phase et les étapes d'ouverture. Les patchs sont stockés à côté de ASSIGNMENT.md et persistent après la fermeture des répertoires de travail.
Continuez à partir de là ; ne recommencez pas.

**2. Nommez votre Session**, s'il a été ouvert pour cet élément :
`clawdline item name <item id> "<task name>"`, une seule fois, après avoir lu l'objectif et le périmètre. Cela renomme la Session, et non l'élément.

**3. Avant l'implémentation.**

- Planification capturée sans critères d'acceptation : définissez des critères observables avec
`clawdline item acceptance <item id> --body-file acceptance.md`.
- Nécessite une revue indépendante (cochée) ou Epic : le chemin du plan revu dans `clawdline guide fr epic`
est prioritaire. Non cochée : aucun plan, aucune revue enfant.
- Travail en plusieurs étapes sans sous-étapes : `clawdline item step-add <item id> "first" "second" …` (deux à
huit étapes vérifiables une à une ; une modification unique ne nécessite aucune sous-étape).
- Puis `clawdline item phase <item id> implementing`.

**4. Travaillez par défaut dans cette Session.** Recherchez, implémentez, vérifiez et finalisez le Feature
vous-même. Déployez-le uniquement lorsqu'un besoin concret justifie la création d'une Session distinct : travail parallèle véritablement indépendant, outils ou permissions différents, ou revue indépendante requise. Justifiez votre demande avant de le déployer ; une recherche ou une implémentation de routine ne constitue pas une raison suffisante.

Lorsqu'un déploiement est nécessaire, effectuez la synthèse, l'intégration et la finalisation ici :

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id` lie l'enfant à l'élément, de sorte que sa finalisation compte comme celle de l'élément. Répétez l'opération lorsqu'un enfant traite plusieurs éléments : le premier élément est la ligne de l'enfant, et la finalisation compte pour chacun d'eux.
- Le titre est une ligne de 60 caractères maximum indiquant la différence. Tout deux-points (`:` ou
`：`) est refusé, car il associe une observation à une explication ; il en va de même pour « l’utilisateur » en tant que
sujet, ou un titre commençant par un identifiant formaté en code. Chaque réponse `bad_task` est accompagnée de
`title: …` indiquant laquelle.
- Le résumé est autonome. Indiquez-y les faits que vous avez déjà vérifiés, chacun avec son
`file:line` ou la commande qui l’a affiché, afin que l’enfant ne les redécouvre pas.
- Le résumé d’une investigation ou d’une exploration indique également sa condition d’arrêt — la question qui,
une fois répondue, met fin à la tâche — et une limite de tours.
- Le travail en lecture seule est `--claims ""`. Chaque indicateur et chaque code de refus se trouve dans
`clawdline guide fr dispatch`.

**5. Si un processus enfant termine**, une ligne `<clawdline-notice>` est saisie dans votre compositeur. Exécutez
`clawdline task show <task id>`, puis intégrez la livraison ; sa lecture ferme la notification, il n'y a donc pas d'accusé de réception séparé. **Après une distribution, terminez votre tour** : la notification vous réveille, et un tour maintenu ouvert
relit l'intégralité de votre contexte à chaque interrogation. Ce n'est que lorsqu'il n'y a plus rien à faire et que vous
devez bloquer que vous exécutez `clawdline task wait <task id>…` (par défaut `--timeout 9m`, `--any` pour le premier). Intégrez un processus enfant de l'arborescence de travail en **fusionnant sa branche** dans la cible. **La fusion enregistre l'**intégration automatiquement** en quelques minutes : ne publiez pas d'intégration manuellement. `clawdline landings` liste
ce qui reste dû. Un enfant envoyé avec le code `--claims ""` qui n'a rien écrit est enregistré
`nothing_to_land` par le courtier. Tout autre élément est `clawdline task land <task id> <state>`
(`clawdline guide fr landing`).

**6. Rapport d'achèvement**, lorsque la recherche de la cause a nécessité une enquête approfondie (une correction directe,
observée, n'en nécessite aucune). Ajoutez-le avant `done` : une fois l'élément terminé, il est désassigné et le
rapport répond à `409 not_item_owner`.

```sh
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Rédigez-le pour la personne qui a signalé le problème, en Markdown, sans données privées.

**7. Terminez l'élément.** Complétez chaque étape une fois vérifiée avec
`clawdline item step-done <item id> <step id>`. Validez et poussez le travail direct depuis un arbre de travail jetable ; fusionnez une branche enfant si elle a été utilisée. Une fois l'élément déposé, une commande le déplace vers
`done`. L'enregistrement du dépôt d'un enfant fournit les informations de validation, de cible et de dépôt distant ; le travail direct les nomme :

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --commit <sha> --target main --remote origin \
  --deployment "what went live, where, which version"      # omit landing flags for a landed child
```

Ou avancez phase par phase :

```sh
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> deploying --no-landing-reason "why there is no code"    # work with no code
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

`done` prend `--deployment` ou `--no-deployment-reason` selon la politique de déploiement de l'élément. Lorsque
`clawdline item steps <item id>` imprime une ligne de porte, cet élément conserve le chemin le plus long indiqué par cette ligne.

**En attente de déploiement.** Ne laissez pas le tour ouvert pendant l'exécution ou la propagation d'un déploiement.
Démarrez le déploiement et sa vérification en une seule fonction de rappel, terminez le tour et terminez l'élément dès réception de sa notification :

```sh
clawdline callback --title "The hosted console serves <sha>" --work-id <item id> --timeout 20m -- \
  sh -c './deploy.sh <sha> && tools/wait-hosted-console.sh <sha>'
# … the notice: clawdline task show <callback id>, then
clawdline item finish <item id> … --deployment "what went live, where, which version (callback <callback id>)"
```

**8. Signalez le tour :** `clawdline session report --summary "…"` (§7).

**9. Laissez l'élément propriétaire Session ouvert.** La finalisation ou l'annulation d'un élément Board libère son affectation ;
elle ne met pas fin à l'élément Session qui en était propriétaire. Après `session report`, laissez l'élément Session disponible pour
un suivi ultérieur. N'exécutez pas `clawdline session close` simplement parce que l'élément a atteint `done` ou
`cancelled`. Une personne peut explicitement demander la fermeture de l'élément Session ultérieurement. Le courtier ferme séparément
l'onglet d'un enfant distribué par un agent une fois la tâche de cet enfant terminée, conformément à la règle relative aux onglets enfants dans
`clawdline guide child`.

**En cas de refus.** `version_conflict` : exécutez à nouveau la même commande ; elle relit la
version. `steps_incomplete` : une étape est toujours ouverte. Tout autre code : §12, puis la partie correspondante
.

**Tâches plus rares, une partie chacune :** `clawdline guide fr board` — propositions, décisions, tâches à faire, réouverture d'un élément
terminé, attente d'une personne, portes et refus de chaque phase ; `clawdline guide fr epic` — Plans,
révision de plan, éléments enfants d'un Epic, personas ; `clawdline guide fr landing` — intégration manuel,
transferts (y compris un long transfert d'étape Root), affectations Root ; `clawdline guide fr running` — éléments enfants bloqués, restes, réapparition.

## 3. Avant de lancer : vérifiez ce qui est déjà présent

Le travail d'une autre session peut déjà être en cours et invisible dans l'arborescence partagée :
une livraison terminée sur une branche non fusionnée n'apparaît pas dans `git status`. Lisez d'abord.

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- Répond aux codes `generation`, `task_root` et à quatre listes : `live`, `unlanded`, `droppable`,
`unreadable`. Chaque ligne contient un code `do` que le démon accepte. Avec `claims`, chaque ligne active
indique sa valeur `overlaps`.
- **`generation` est requis pour l'envoi** (§4). Il s'agit de 16 caractères hexadécimaux superposés aux champs scellés des lignes ;
il se déplace lorsqu'une ligne commence, se termine ou change d'écriture. - **`task_root` est l'emplacement de votre `task.json`.** Il s'agit du champ propre à ce démon ; le broker Swift
n'en possédait aucun car il avait codé en dur `/tmp/.clawdline`.
- `400 bad_request` lorsque `project` n'est pas un chemin absolu dans un dépôt Git.

À lire également :

- `GET /v1/orchestrator/inflight?project=…` — chaque ligne de travail en cours dans le dépôt,
qui en est responsable et ce qu'elle revendique. - `clawdline assistants` — par assistant : `availability` (`ok`, `low`, `exhausted`, `unknown`),
`windows`, `stale`, `resets_at`. Choisissez le destinataire après lecture ; rien ne refuse une
affectation pour quota.

**Faut-il l'affecter ?** Les tâches divisées en éléments indépendants sont plus rapides en
parallèle. Une chaîne où chaque étape dépend de la précédente est moins performante en division, car chaque transfert
la rompt. Le diagnostic, les tâches plus petites que leur propre briefing et tout ce qu'une autre personne attend restent
dans votre session. Les règles de la machine se trouvent dans `<state dir>/dispatch-policy.md` (et
dans `dispatch-policy.local.md`, la personne) ; chaque processus enfant les reçoit dans son briefing.

## 4. Déléguer une tâche à un agent enfant

Un processus enfant est une tâche délimitée qui vous est confiée. **Vous conservez la synthèse, l'intégration et l'intégration.**

**Une seule commande exécute les quatre étapes ci-dessous**, avec le briefing sur l'entrée standard ou dans un fichier :

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--persona <id>] [--work-id uuid …] [--task-id uuid] [--label "…"] [--project-dir D] < brief.md     # or --instructions-file brief.md
```

Il génère l'identifiant et le secret, lit l'inventaire pour `generation` et `task_root`, écrit
`task.json`, publie la tâche, et sur `stale_inventory`, relit l'inventaire et renvoie
une fois. Il affiche `dispatched <id> <state> [worktree <path>]`, puis une ligne par avertissement — celui du
démon, et chaque tâche active dont les écritures chevauchent les vôtres. `--json` affiche la réponse du démon
à la place. Un refus est `refused, <status> <code>: <message>` sur stderr, puis ses informations supplémentaires et sa correction, une ligne chacune
et il sort avec le code 1 ; le tableau à la
fin de cette partie indique la signification de chaque code. La racine est votre conversation, depuis
`CLAUDE_CODE_SESSION_ID` ou `CODEX_THREAD_ID`, sinon `--conversation` ; l'assistant de l'enfant est
le vôtre, sauf indication contraire de `--assistant` ; le projet est le dépôt Git racine de ce répertoire, sauf indication contraire de
`--project-dir`. `--claims ""` déclare un enfant qui n'écrit rien. Pendant que le
démon ouvre l'arborescence de travail et l'onglet de l'enfant, il n'affiche rien ; il s'agit d'une requête qui répond
lorsque l'enfant existe, attendez donc une seule fois (§2, « Attente d'une commande longue »). Le secret
n'est jamais dans argv, dans `task.json` ni dans ce qu'il affiche, et le jeton est lu comme chaque commande fine
le lit.

Pour une révision pouvant nécessiter une nouvelle tentative, choisissez un UUID en minuscules avant le premier appel et transmettez-le
sous la forme `--task-id` à chaque tentative. La commande conserve une copie privée de l'intention de répartition d'origine
à côté de `task.json`, permettant ainsi une nouvelle tentative identique même après la réécriture du
brief par le démon. Le broker renvoie l'identifiant de tâche d'origine avec `(replayed)` ; un brief modifié est refusé localement. Si
la commande expire ou si sa sortie est perdue, vérifiez `GET /v1/orchestrator/tasks/<id>` avant
de conclure à un échec. Une tâche manquante peut être relancée avec le même identifiant et le même brief. Un refus explicite
n'a pas créé de tâche et peut également être relancé après correction de sa cause.

`--persona <id>` lance l'enfant en tant que persona intégré (`persona` dans `task.json`) ; un identifiant manquant pour cette
build est refusé localement. Aucun type n'en reçoit un par défaut, `plan_review` inclus : indiquez vous-même `code-reviewer` si vous en voulez un. `GET /v1/personas` liste les identifiants que cette build possède ; la définition d'un persona se trouve dans la Epic
partie du §10 (`clawdline guide fr epic`).

Étapes suivies, pour un appelant sans le binaire :

**1. Choisissez un identifiant et un secret.**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 characters, lowercase
SECRET=$(openssl rand -hex 32)          # 64 lowercase hex
```

Le secret est transmis de vous au démon dans le corps de la requête POST, puis du démon à l'enfant dans la
ligne qu'il y saisit. Il ne se trouve ni dans `task.json`, ni dans la réponse du dispatch, et vous n'en aurez plus besoin.
(Une réapparition est la seule réponse qui contient un secret : le nouveau secret de sa copie.)

**2. Lisez l'inventaire** (§3) pour `generation` et `task_root`.

**3. Écrivez `<task_root>/<TASK_ID>/task.json`.** Le démon lit le brief à partir de ce fichier, et non de la
requête. Lors de l'admission, il valide l'entrée, réécrit `task.json` à partir de ce qu'il a admis, et écrit
le `CHILD.md` de l'enfant à partir du même enregistrement — titre, instructions, écritures, livrables, type et délai d'expiration
inclus — de sorte que la tâche que l'enfant lit est celle qui a été validée, et il ne lit pas `task.json`.

| Champ | Règle |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | même identifiant |
| `assistant` | `claude` ou `codex` |
| `project_dir` | chemin absolu vers un répertoire existant |
| `title` | affiché à l'écran : une ligne de 60 caractères maximum indiquant la différence. Un deux-points (`:` ou `：`), « l'utilisateur » comme sujet, ou un identifiant au format de code d'ouverture est refusé car `bad_task` (`title: …`) |
| `instructions` | requis, maximum 16 Kio. Ils doivent être autonomes : l'enfant ne connaît rien d'autre. Transportez les faits que vous avez déjà vérifiés, chacun avec son `file:line` ou sa commande ; un enfant d'investigation reçoit également une condition d'arrêt et une limite de virage |
| `claims` | **requis** : au maximum 32 chemins relatifs que l'enfant peut écrire. `[]` signifie qu'il n'écrit rien et qu'un avertissement est émis à propos de (`claims_missing`) |
| `isolation` | `none` (par défaut) ou `worktree` pour un copie de travail privée sur sa propre branche |
| `permission_mode` | `ask`, `edits` ou `full` |
| `timeout_minutes` | 1 à 240, 30 par défaut |
| `kind`, `deliverables`, `model` | facultatif ; `model` est `[a-z0-9._-]`, maximum 64 caractères |
| `work_id` | UUID optionnel de l'élément du tableau géré |
| `persona` | Identifiant de personnalité intégré optionnel (`GET /v1/personas`) ; aucun par défaut |
| `auto_compact_window` | Optionnel, Claude uniquement : taille du contexte en jetons (50 000 à 1 000 000) à partir de laquelle l'enfant est compacté, ou `null` pour aucune. Absent suit la valeur `claude_auto_compact_window` de la machine, qui est désactivée sauf si l'utilisateur l'a définie. Pour la comparaison d'exécutions, pas pour les briefs quotidiens : une compaction peut entraîner une perte de détails |
| `root` | **obligatoire** : `{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "project_dir": "<the same absolute repository path as project_dir>", "label": "…"}`. Un processus racine avec un rôle spécifique nécessite `root.project_dir` pour que le démon puisse vérifier son périmètre de projet. |

**Assurez-vous que la surface de travail et le mode de lancement correspondent à chaque outil que le processus enfant doit utiliser avant de le déployer.**
Le brief nomme les outils requis et le processus racine prouve que la surface choisie les fournit.
Un processus enfant CLI (Codex) n'obtient pas automatiquement l'interface intégrée (`@Browser`) de l'application de bureau ChatGPT par simple autorisation.
Pour une revue d'interface utilisateur, d'accessibilité ou de mise en page adaptative, dirigez le travail vers une surface disposant réellement de l'autorisation « Utilisation du navigateur/ordinateur », ou indiquez un environnement de navigateur local équivalent, tel que Playwright/Chrome CDP, et prouvez son installation. Lorsqu'une surface peut demander un accès à une application, à l'origine ou à une interface graphique,
l'envoi d'une requête avec `--permission-mode ask` : Codex `full` signifie un lancement de shell non interactif
(`--ask-for-approval never`), pas tous les outils, et la vérification automatique ne peut pas examiner une requête qui n'est jamais
créée. Au démarrage de la tâche, le processus enfant teste chaque outil requis, et ne se contente pas de vérifier un nom de commande. Si
l'un d'eux est indisponible, il signale immédiatement l'indisponibilité exacte et le processus racine rétablit l'accès ou
redistribue la requête. Il ne termine pas une vérification d'acceptation dépendante d'un outil comme non vérifiée parce que le processus racine
a choisi un processus incompatible.

**`root.session_id` est votre identifiant de conversation, jamais un identifiant de terminal.** Claude Code l'exporte comme
`CLAUDE_CODE_SESSION_ID` ; Codex comme `CODEX_THREAD_ID`. C'est ainsi que le démon regroupe le processus enfant sous vous
et vous informe de la fin de son exécution. Pour vérifier, nommez cet onglet :
`GET /v1/orchestrator/whoami?conversation_id=<id>` répond à `terminal_id`.

**4. Distribution**, avec le jeton d'orchestration :

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

Envoyez le corps via l'entrée standard (`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`)
afin que le secret ne soit pas inclus dans argv.
La réponse est `{ok, task, warnings?}`. Lecture de `warnings` : `claims_overlap`, `claims_missing`,
`claims_ignored_for_worktree`, `dirty_worktree_base` et `work_not_placed` (l'élément nommé n'a pas encore pu être déplacé sur le plateau ; le balayage du plateau le fera en une seule itération). Envoyer à nouveau le même identifiant répond à la tâche stockée avec `replayed: true`, une nouvelle tentative est donc possible.

Un onglet qui ne s'ouvre pas répond tout de même 200, avec `task.state: "spawn_failed"`.
`POST /v1/orchestrator/tasks/<id>/respawn` (jeton d'orchestrateur) ouvre une copie avec un nouveau secret, au maximum deux fois par original.

Vous avez envoyé la mauvaise tâche enfant (mauvais brief, mauvaise portée ou même tâche deux fois) ? N’attendez pas
la fin de son exécution ou l’expiration du délai pendant qu’elle occupe un emplacement et que ses écritures sont en cours : `clawdline task cancel <id>
--reason "…"` l’arrête immédiatement (§5).

**Refus rencontrés**, dans l’ordre de vérification :

| Statut | Code | Action |
|---|---|---|
| 409 | `task_unreadable` | Une tâche avec cet identifiant est enregistrée mais ne peut pas être lue ; ne pas la renvoyer sous le même identifiant. |
| 422 | `bad_task` | Le message indique le champ. Inclut « Aucun fichier task.json lisible sous… » — vérifier `task_root` |
| 422 | `claims_required` | Ajouter `claims` |
| 422 | `root_session_required`, `root_assistant_required` | Ajouter `root.session_id` et `root.assistant` |
| 403 | `session_scope_mismatch` | Vérifier que `root.project_dir` est présent et correspond à l'instantané du projet et du rôle racine Session. Corriger le brief ou l'interface de ligne de commande ; ne pas demander à la personne de modifier les paramètres du projet. |
| 422 | `detached_route_required` | Vous avez envoyé `root.poll_only` ; il s'agit d'une automatisation détachée (§6) |
| **409** | **`stale_inventory`** | Votre `generation` est manquant ou obsolète. L'inventaire actuel complet est inclus dans l'erreur : veuillez le consulter, prendre une nouvelle décision et renvoyer avec son `generation` |
| 422 | `work_not_found`, `work_other_project`, `work_closed` | Le `work_id` que vous avez indiqué ne correspond à aucun élément, appartient à un autre projet ou est fermé |
| 422 | `also_work_not_found` | L'identifiant `also_work_ids` ne correspond pas à l'élément Board ; il est vérifié comme `work_id` ensuite |
| 503 | `store_unavailable` | Impossible de lire le tableau pour vérifier l'élément nommé ; aucune opération n'a été lancée, veuillez le renvoyer |
| 409 | `graph_*` | Règle d'admission du graphe de tâches (champ `graph`) |
| 409 | `no_child_capability` | Cette plateforme ne peut pas ouvrir d'enfant ; `missing` indique ce qui se passe |
| 429 | `squad_launch_capacity` | Trop de lancements de personas sont encore en attente de leur Session ; Réessayez plus tard |
| 429 | `rate_limited` | Trop d'envois en dix minutes |
| 422 / 409 | `root_unresolved`, `conversation_ambiguous` | Votre identifiant de conversation ne correspond à aucune session active, ou à plusieurs. Veuillez corriger cela ; ne passez pas en mode détaché |
| 403 | `session_actor_required` | Une session racine ouverte avec un rôle doit envoyer des envois avec sa propre capacité d'escouade Session, à partir de celle-ci Session |
| 403 | `session_scope_mismatch` | Également ici : le projet de la session racine Session ne correspond pas à l'instantané de son rôle |
| 503 | `squad_policy_unavailable` | Impossible de lire les paramètres d'attribution des rôles ; aucune opération n'a été lancée. |
| 409 | `persona_disabled_for_auto_assignment` | Ce profil est désactivé pour l'attribution automatique dans le projet cible. |
| 429 | `over_capacity` | Vos emplacements enfants (5 par défaut) ou ceux de la machine sont saturés. `retry_after` |
| 409 | `workspace_busy` | Chevauchement d'écritures d'un autre utilisateur racine ; l'erreur indique la tâche bloquante. |
| 409 | `worktree_unavailable` | Impossible d'effectuer l'extraction privée. |
| 429 | `terminal_busy` | Toutes les voies d'écriture du terminal sont occupées. `retry_after: 5` |

## 5. Pendant son exécution et à son terme

L'enfant se connecte pour son briefing (`clawdline task accept`, qui affiche `/accepted` ou quitte
`accepted.json`), peut envoyer une note d'avancement en cas de changement de plan (`/progress`), peut envoyer jusqu'à cinq
notifications (`/notify`), et termine en écrivant `result.json` et en exécutant `clawdline task finish`.
Vous n'appelez pas ces itinéraires.

- `clawdline task show <id>` — une tâche, avec son état (`GET /v1/orchestrator/tasks/<id>`).
`GET /v1/orchestrator/tasks` les liste (`?state=`, `?limit=` jusqu'à 500).
- **Une fois terminée, le démon saisit une ligne `<clawdline-notice>` dans votre compositeur.** Cette ligne `body`
est une courte phrase : la tâche, son déroulement, les informations spécifiques à cette livraison (un blocage,
écritures libérées, sa branche, le nombre de données restantes) et la commande à exécuter,
`clawdline task show <id>`, qui ferme la notification après avoir affiché la tâche. Son JSON
(version 3) contient `task`, `state` et `notice_id`, ainsi que `outstanding`, `leftovers` et
`claims_released` uniquement lorsqu'ils ont une instruction ; le résultat est ce que `task show` affiche. Une ligne
qui n'a pas pu être saisie est réessayée avec un intervalle de 5 à 300 secondes. Une fois affichée à l'écran et si vous
n'avez pas lu la tâche, elle n'est pas saisie à nouveau en entier : une courte ligne `task_reminder` nommant la
même commande est saisie, avec un intervalle de 2 à 30 minutes — huit saisies au total, puis le programme abandonne. Il ne saisit jamais de texte pendant l'affichage d'un menu. Un menu n'utilise pas ces huit lignes : la ligne attend jusqu'à
12 heures et est saisie une fois le menu terminé. La route `task show` envoie :

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

`clawdline task show <id>` et `clawdline task wait <id>…` l'envoient pour une tâche terminée après l'avoir imprimée ;
`clawdline task ack <id> <notice_id>` l'envoie manuellement et imprime une ligne. Un deuxième accusé de réception répond à
`changed: false`. Les notifications non acquittées sont listées à
`GET /v1/orchestrator/completions` ; `POST /v1/orchestrator/completions/reconcile` les réarme.
Une notification d'abandon est saisie une nouvelle fois lors de la prochaine période d'inactivité de votre session.
- **Vous ne verrez peut-être jamais la ligne, et pourtant vous en prendrez connaissance.** Votre propre
`GET /v1/work/v2/agent/session-todos/<conversation id>`, que vous lisez à chaque limite de tour,
liste `unacknowledged_completions` — chacun de vos enfants qui a terminé et que vous n'avez pas
accusé réception, avec `task_id`, `title`, `state`, `kind`, `result_path`, `notice_id` et `ack_path`, si
son avis est toujours en attente ou abandonné. `clawdline session report` les imprime après sa réception.
Pour chacun : `clawdline task show <id>`, puis intégrez-le ; La lecture correspond à l'ACK et le retire des deux listes.
`task show` affiche le résumé complet et compte ce qu'il omet ; `--json` est la réponse complète du démon,
symboles et artefacts inclus. Lisez `result.json` uniquement si cela ne suffit pas.
- **Une livraison qui nomme les éléments restants** — choses que l'enfant dit ne pas avoir faites — ne change rien par elle-même.
`task show` liste leurs titres. Pour en attribuer un à la personne,
`POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}` ;
elle répond « suivre », « plus tard (en attente) » ou « non », et rien n'atteint son tableau tant qu'elle n'a pas répondu. - **Un processus enfant qui s'arrête juste après que son briefing a été déclenché, puis signalé.** Si un processus enfant dont le
briefing a été saisi ne l'a pas signé et que son écran affiche « inactif » (invite affichée, compositeur
vide, aucun menu, aucune ligne active) pendant 5 minutes, le démon saisit une ligne indiquant son
`CHILD.md` (jamais le code secret). Toujours non signé et inactif 5 minutes plus tard, la tâche se termine
`spawn_failed` avec un verdict indiquant qu'elle a été bloquée, et vous recevez une notification `"kind":
  "task_stalled"` au lieu de `task_finished`. Relancez-le (`POST /v1/orchestrator/tasks/<id>/respawn`)
ou réexécutez-le, puis accusez réception. Un processus enfant actif, affichant un menu ou signé n'est jamais
traité.
- **Annulez un agent enfant lancé par erreur** — brief incorrect, portée incorrecte, doublon :
`clawdline task cancel <id> --reason "wrong brief"`
(`POST /v1/orchestrator/tasks/<id>/cancel`, `{"reason":"…"}`). La raison est requise (500 octets maximum).
La tâche se termine (`cancelled`) avec la raison comme verdict, son onglet est fermé, ses écritures et son emplacement enfant sont libérés, et vous recevez une notification indiquant l'annulation et sa raison. **Les commits ne sont pas supprimés :** une tâche enfant ayant effectué un commit conserve sa branche et son checkout, et son landing reste en attente (`task show` et `clawdline landings`).
Fusionnez les éléments souhaités ou enregistrez-les avec (`clawdline task land <id> abandoned`). Seule la Session racine qui a lancé la tâche, ou la personne depuis la console, peut l'annuler ; toute autre demande est
refusée avec `403 not_task_root`, et une Session ouverte avec un rôle doit envoyer sa propre capacité
(`session_actor_required` ; la commande le fait pour vous). Une tâche déjà terminée répond
`409 task_already_terminal` avec son `state` ; exécuter à nouveau la même annulation répond de la même manière en cas de succès
avec `replayed: true`. `clawdline task wait` se termine avec le code 5 lorsqu'une tâche qu'elle attendait a été annulée.
Sinon, une tâche se termine en aboutissant, en échouant ou en expirant.
- **Un enfant terminé n'est pas du code intégré.** Son travail reste dans l'arbre partagé ou sur sa branche jusqu'à ce que
vous l'intégriez.

## 5a. Attente d'une commande longue : un rappel

Un déploiement, une exécution CI, une propagation de version, une compilation longue : toute opération dont la réponse est « plus tard ». Ne l'attendez pas à votre tour et ne l'interrogez pas lors des tours suivants. Transmettez-la au démon :

```sh
clawdline callback --title "CI is green on <sha>" --timeout 45m -- gh run watch <run id> --exit-status
```

Il affiche `callback <id> briefed` et se termine. **Terminez votre tour.** Lorsque la commande se termine, le démon
saisit `<clawdline-notice>` dont le corps contient :
`callback <first 8 of id> finished: success (exit 0 after 6m) — run clawdline task show <id>` ; `task show`
affiche son mode de sortie (son code de retour et les dernières lignes de sa sortie) et ferme la notification,
exactement comme pour un processus enfant.

- Il s'agit d'une tâche sans onglet : `clawdline task cancel <id> --reason "…"` arrête l'ensemble du groupe de processus de la commande.
Au-delà de `--timeout` (1 min à 4 h, 30 min par défaut), le processus est arrêté et terminé (`timeout`).
Un rappel en cours d'exécution empêche la fermeture de votre Session, comme le fait un agent enfant actif.
- La commande s'exécute conformément à ses instructions, sans shell ; utilisez `sh -c '…'` si vous avez besoin d'un shell. Elle s'exécute dans ce répertoire (`--dir` pour un autre) avec uniquement les variables d'environnement PATH, HOME, locale, USER, SHELL, TMPDIR et TERM : jamais d'identifiants. Une commande qui en nécessite lira les informations depuis son propre fichier.
- Sa sortie se trouve dans le répertoire de la tâche, `output.log`, et est conservé pendant 7 jours après sa fin.
- Elle est exécutée une seule fois. Si le démon redémarre entre-temps, il reprend la commande ; une commande qui
s'est terminée alors qu'aucun démon ne la surveillait et n'a renvoyé aucun code de sortie est considérée comme ayant un résultat inconnu (`failure`)
et n'est **pas** réexécutée. Relancez-la manuellement si cela ne présente aucun risque.
- Un démarrage incertain (l'interface de ligne de commande n'a pas pu joindre le démon) est relancé avec
`--task-id <the id it printed>` : le même identifiant n'est jamais lancé deux fois.
- Un rappel n'occupe pas d'emplacement enfant. Au maximum 8 exécutions par Session et 16 par machine ; une exécution supplémentaire est
refusée avec `429 callback_capacity` et un `retry_after`. Un dispatch ne peut pas se déclarer de type
`kind callback` (`bad_task`). Windows refuse `501 no_callback_capability`.

## 6. Intégration et les trois autres types de travaux

**Après la fusion de la branche d'un processus enfant, aucune autre action n'est requise :** le broker enregistre `landed` (voir ci-dessous).
Un processus enfant qui n'a déclaré aucune écriture (`--claims ""`), s'est terminé de lui-même et n'a rien laissé sur sa branche
ni dans son répertoire d'extraction est enregistré `nothing_to_land` par le broker avant même que sa notification ne soit générée. Une intégration
enregistrée manuellement correspond à ce qu'aucun des deux ne couvre : une sélection directe, une livraison `incorporated`,
`nothing_to_land`, `abandoned` — et constitue une commande unique, envoyée avec le jeton d'orchestration :

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

C'est cette route qu'un script peut appeler avec l'en-tête `X-Clawdline-Orchestrator` :

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- Seules ces clés, ainsi que `delivery`, sont acceptées mais non utilisées ; toute autre clé est refusée.
`pending` et `abandoned` acceptent le secret de la tâche ou le jeton de l'orchestrateur ; `landed`,
`incorporated` et `nothing_to_land` acceptent uniquement le jeton de l'orchestrateur.
- `landed` nécessite `target` et `commit`. `incorporated` a besoin de `target`, `commit` et
`carrier_task`, l'autre tâche dont l'intégration vérifié a transporté cette livraison. Le démon
**vérifie dans Git** ; Sinon, `409 unverified_landing` avec l'un des `reason` suivants :
`commit_unresolved`, `target_unresolved`, `not_on_target`, `base_unknown`, `predates_dispatch`,
`delivery_unknown`, `nothing_delivered`, `not_the_delivery`, et pour `incorporated` :
`carrier_required`, `carrier_is_delivery`, `carrier_unresolved`, `carrier_not_landed`,
`carrier_repository_mismatch`, `carrier_target_mismatch`, `carrier_commit_mismatch`,
`delivery_is_ancestor`.
- `nothing_to_land` est refusé avec `409 wrote_to_repository` lorsque la tâche a bien écrit.
- Un intégration validé ne peut pas être modifié : `409 invalid_transition` ou `409 landing_conflict` pour une
valeur différente.
- **Une fusion s'enregistre elle-même.** Une fois la branche d'une tâche terminée fusionnée dans sa cible, le broker
enregistre automatiquement `landed` en quelques minutes, via la même vérification Git, avec la tête de la cible comme commit. Sans cible enregistrée, il n'en nomme une que lorsque la branche de la copie principale
est la seule branche contenant la livraison. Une sélection de modifications, une livraison `incorporated` et
`nothing_to_land` restent à enregistrer.
- **La notification d'achèvement indique l'état de la branche à la fin de la tâche**, et chacune demande
une action, sous forme de commande. *Aucune modification n'est validée sur sa branche* : une intégration est prouvée à partir de cette
branche, de sorte qu'en l'état, rien ne pourrait être enregistré comme intégré — validez dans son extraction, sur cette
branche, tant que l'extraction est encore sur le disque, ou `clawdline task land <id> abandoned`. *Validée sur
sa branche* : fusionnez cette branche dans sa cible ; la fusion enregistre l'intégration. *Impossible à lire* : examinez la branche, puis enregistrez-la. *Écriture de l'extraction partagée* : `clawdline task land <id>
  landed` avec la validation qui transfère ce travail sur sa cible, ou `abandoned`. *Aucune écriture n'a été effectuée,
et le broker a enregistré nothing_to_land* : seul l'accusé de réception (ACK) reste.

`clawdline landings` (`GET /v1/orchestrator/landings`) correspond à chaque intégration en attente sur la machine,
avec un `ownership.status`. `unknown` ne signifie pas « personne » : cela signifie que les données n'ont pas pu être lues.
`503 landings_incomplete` signifie que certaines lignes n'ont pas pu être lues et qu'aucune liste plus courte n'est proposée à leur place.

`clawdline landings --work-id <item id>` (`GET /v1/orchestrator/landings?work_id=<item id>`) correspond à
chaque intégration **enregistré** pour un élément Board : `{"work_id", "landings": [...], "at"}`, chaque
ligne avec ses `id` et `source` — `task` (enregistrement d'intégration ou d'incorporation d'un enfant lié ; l'identifiant est
l'identifiant de la tâche), `root` (l'enregistrement écrit par `item phase deploying --commit`) ou `phase_event` (une copie conservée par
un ancien démon dans l'historique de l'élément). Une tâche liée dont l'enregistrement est illisible est une ligne avec
`state: "unknown"`, jamais omise. L'élément lu contient les mêmes lignes que `landings`. Un élément qui
n'existe pas est `404 work_not_found`.

**Deux racines aboutissant à une même caisse** prennent d'abord un bail d'intégration (§11).

Les trois autres types de travail ont chacun leur propre itinéraire. Lequel est une limite, et non un détail :

| Type | Itinéraire | Description |
|---|---|---|
| **Transfert** | `POST /v1/orchestrator/handoffs` | Vous transférez une ligne de travail existante, avec son état complet, à une nouvelle session |
| **Attribution Root** | `POST /v1/orchestrator/root-assignments` | Nouvelle affectation indépendante Root pour une nouvelle fonctionnalité |
| **Automatisation détachée** | `POST /v1/orchestrator/detached-tasks` | Travail sans surveillance, sans responsable |

**Transfert.** Commencez par écrire `<state dir>/handoffs/<handoff_id>/handoff.md` (la liste des réponses de la route
`package_root`). Ce message doit comporter trois en-têtes : **RÉFÉRENCES** (tout ce que le destinataire doit
lire), **VÉRIFICATION** (questions auxquelles il répond à partir de ces sources avant de continuer) et **SUJETS OUVERTS** (où reprendre). Ensuite, envoyez le message avec un corps fermé :

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

Le destinataire est invité à lire le fichier, à parcourir ses références, à répondre à ses questions de vérification et à
continuer. À l'ouverture, le transfert capture les éléments ouverts Board de l'expéditeur dans ce projet. Une fois que le
destinataire dispose d'un identifiant de conversation et que son premier enregistrement de conversation est observé, les affectations actives et les propriétaires de ces éléments sont transférés au destinataire en une seule transaction. En cas d'échec du transfert, la propriété reste
à l'expéditeur. Un élément déjà clôturé, transféré à un autre utilisateur ou en cours d'attribution séparée est
laissé tel quel. Un élément soumis à vérification, en cours de vérification ou de fusion, retourne à l'état d'implémentation ; son nouveau
propriétaire doit donc le vérifier à nouveau. Vous recevez une notification `handoff_receipt` lors de la saisie de la ligne de transfert ;
cette notification seule ne prouve pas que le transfert Board a eu lieu. Refus : `bad_task` (un `handoff.md` manquant ou
vide est inclus), `sender_not_found`, `sender_ambiguous`, `rate_limited`,
`terminal_busy` et `succession_required` si vous détenez le rôle de coordinateur de machine — la succession n’est
pas disponible dans ce démon (`501`), la session ne peut donc pas être transférée.

**Transfert de jalon.** Un processus de longue durée (Root) ayant atteint un jalon passe le relais avec
`clawdline handoff --summary summary.md`, afin que le travail suivant ne relise pas tout ce qui l'a précédé
à chaque appel. Le résumé comporte exactement cinq en-têtes (`## `) : Objectif, Décisions vérifiées, Blocages,
Preuves (chemins, commits, identifiants ou commandes `clawdline` à ouvrir, et non leur contenu), Prochaine étape —
maximum 6 Kio, sans identifiants ni texte de conversation ; `--check` liste tous les problèmes sans
ouvrir quoi que ce soit, et le démon refuse les mêmes problèmes que `bad_milestone_summary`. Le démon
écrit `obligations.md` à côté : vos éléments Board (ils se déplacent, décision à laquelle la personne n'a pas répondu incluse) et vos tâches en cours, notifications non acquittées et intégrations dus (ils restent les vôtres). Après le transfert, continuez à les accuser réception et à les atterrir, puis `clawdline session close`
une fois qu'il lit `safe`. Le transfert est à votre discrétion : `clawdline usage --compare-handoff` indique s'il a payé sur cette machine, et il n'est jamais forcé.

**Attribution Root.** L'en-tête `Idempotency-Key` doit être égal à `request_id` :

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

Chaque champ d'attribution fait entre 1 et 8 192 octets, soit 32 Kio au total. Le démon écrit lui-même le brief et ouvre
la session. Il n'a **ni parent, ni secret, ni délai d'expiration, ni résultat, ni destination** : personne n'est informé de sa
fin, car il ne répond à personne. Refus : `bad_root_assignment`, `idempotency_mismatch`,
`request_conflict`, `rate_limited`. Ne jamais simuler un refus avec un enfant, une tâche détachée ou un transfert.

**Automatisation détachée.** Comme une répartition (§4) — `task.json` sous `task_root`, puis
`{"task_id", "secret", "inventory_generation"}` — mais la racine du brief doit être
`{"session_id": null, "poll_only": true}`, sinon le refus est qualifié de `detached_task_required`. Personne n'est
notifié ; interroger `GET /v1/orchestrator/tasks/<id>` et lire `result.json`. Il ne s'agit jamais d'un Root ni d'un
propriétaire de fonctionnalité.

### Planifier les tâches futures

Clawdline Next gère lui-même les tâches planifiées. N'utilisez pas l'application retirée, `cron`, ni une chaîne de
tâches détachées. Consultez `GET /v1/orchestrator/schedules` ; consultez-en une en entier à
`GET /v1/orchestrator/schedules/<id>`. `GET /v1/places` fournit à `place_id` un nom d'écriture.

Une planification ponctuelle (`on`) peut être effectuée directement avec le jeton de l'orchestrateur. Une planification répétitive
(`days`) est une instruction permanente et nécessite l'instruction explicite de la personne concernée lors de cette session :

1. Lisez `GET /v1/orchestrator/sessions/<conversation>/run`. Il s'agit de la dernière exécution lancée lorsque
la personne a envoyé son message à cette session via Clawdline.
2. `POST /v1/orchestrator/schedules` avec `Idempotency-Key` et le corps de la planification habituelle, ainsi que
cette conversation et cette exécution :

```json
{"title":"Morning sweep","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"Inspect the overnight failures and report actionable findings.",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

La même preuve autorise `PATCH /v1/orchestrator/schedules/<id>` (envoi du corps complet de la planification)
et `DELETE /v1/orchestrator/schedules/<id>` (envoi des deux champs de preuve sous forme de corps JSON) lorsque la
personne a explicitement demandé cette modification.
`POST /v1/orchestrator/schedules/<id>/run` en exécute une maintenant. Relisez la planification créée ou modifiée
avant de signaler la réussite.

Sans `via`, le jeton d'orchestrateur reste limité à une seule planification `on`. Une exécution inventée,
expirée ou d'une autre session est refusée comme `run_unknown`, `run_expired` ou `run_other_session` ;
La preuve mal formée est `invalid_user_authorization`. Si la personne a saisi directement dans un terminal,
il n'y a pas d'exécution : demandez-lui d'envoyer l'instruction via Clawdline. Ne réutilisez jamais une exécution comme autorisation générale
pour une tâche que le message de la personne n'a pas demandée. Cette preuve rend le relais auditable ; elle
ne transforme pas le jeton d'orchestration global en une information d'identification spécifique à la session.

Une planification peut ne pas avoir d'heure : envoyez `"trigger_only": true` au lieu de `at`, `days` et `on`.
L'horloge ne l'exécute jamais ; elle ne s'exécute que par `…/run` ou par son webhook, et seulement pendant `enabled`. Il s'agit d'une
instruction permanente, comme une instruction répétitive, et elle nécessite la même preuve.

**Lancer une tâche sur une autre machine et observer son déroulement.** Il n'existe pas de canal de communication entre machines.
Configurez la tâche comme une planification déclenchée uniquement sur la machine cible, associez-lui un webhook Cloud et stockez
l'URL où l'appelant peut la lire (il s'agit d'une information d'identification : un fichier lisible uniquement par vous, ou
`CLAWDLINE_WEBHOOK_URL` ; jamais un argument de ligne de commande). Ensuite, sur la machine appelante :

```sh
clawdline webhook fire --url-file <path>
```

Elle envoie `{"deliver_within_seconds": 60}` (`--deliver-within`), de sorte qu'une cible hors tension ne l'exécute pas ultérieurement. Il suit l'état de la livraison jusqu'à sa fin ou jusqu'à ce que `--timeout` (60 m) s'écoule, en affichant
les modifications sur stderr et une dernière ligne sur stdout. Sortie `0` réussie · `1` terminée sans succès
(failure, timed_out, cancelled, spawn_failed) · `2` n'a jamais atteint la machine (expired, canceled, unreachable) · `3` refusée (dispatch_refused et son code, URL indisponible, limitation de débit) · `4`
a cessé d'attendre, avec le dernier état vu. `--no-wait` affiche l'identifiant de la livraison et retourne après le
`202`. Signalez le code de sortie et la ligne finale tels quels : `2` signifie qu'aucune opération n'a été exécutée, et non qu'elle a échoué.

Une tâche planifiée qui lit des données pour un élément en attente de vérification (la section « Lecture » ​​de la barre latérale,
docs/verifications.md) écrit sa lecture sous forme de note sur cet enregistrement avec son propre secret de tâche — et non
le jeton de l'orchestrateur, qu'elle ne devrait pas contenir :

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<readout>"}'
```

Elle s'arrête uniquement sur un enregistrement dont `schedule_id` correspond à la planification qui a lancé cette tâche
(`schedule_mismatch` sinon), est signé `task:<task id>` et est refusé comme `not_scheduled` pour une
tâche non planifiée. Trouvez l'identifiant de l'enregistrement avec `clawdline verify list`.

## 7. Signalez la fin de votre tour

Lorsque votre tour est réellement terminé (travail effectué, vérifié et validé le cas échéant),
effectuez cette dernière action avant la réponse finale :

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

Une seule coche apparaît sur la ligne de votre session : **livré, en attente d'approbation**. Ce statut est moins contraignant qu'une
réception et ne nécessite aucune vérification. Il s'affiche tant que le démon considère la session comme inactive (les statuts « en cours »,
en attente ou un écran illisible sont prioritaires) et uniquement tant que ce terminal maintient la même
conversation.

- **Uniquement pour un tour de parole terminé.** Ne convient pas pour un travail partiel, un diagnostic, un blocage ou une question posée à la personne.
Un enfant ne l'envoie jamais (`409 child_session`).
- La commande trouve votre conversation depuis `CLAUDE_CODE_SESSION_ID` ou `CODEX_THREAD_ID`
(`--conversation` sinon), demande le terminal à `GET /v1/orchestrator/whoami` et envoie
`{"summary"}` à `POST /v1/orchestrator/sessions/<terminal>/complete`.
- Le résumé contient entre 1 et 500 caractères. Chaque appel constitue un nouvel accusé de réception ; le plus récent est pris en compte. La réponse contient également `open_todos` : il s'agit des tâches directes (Session) envoyées ou lues et qui sont
non terminées, de la plus ancienne à la plus récente, au maximum 20 (`open_todos_truncated` s'il y en a davantage). La commande
les affiche sur la sortie d'erreur standard après l'accusé de réception, un identifiant et un texte par ligne. Cochez celles que vous
avez terminées avec `clawdline todo done <id>`. L'accusé de réception est enregistré dans tous les cas et le code de sortie
ne change pas ; `open_todos_unknown: true` signifie qu'elles n'ont pas pu être lues, et non qu'aucune n'est ouverte. - Refus : `conversation_id_malformed` (UUID non minuscule), `conversation_not_found`,
`conversation_ambiguous`, `registry_stale`, `session_not_found`, `session_unbound`,
`child_session`. Signalez le refus honnêtement ; une simple phrase dans le chat ne constitue pas un accusé de réception.

**Laissez un rapport d'état à la personne.** Lorsqu'un utilisateur a modifié des fichiers dans un projet Git,
fournissez-lui une page où il peut voir l'état d'avancement du travail et consulter tous les fichiers ajoutés ou
modifiés. Il s'agit d'un fichier HTML local : aucun fichier n'est téléchargé et rien ne se charge.

```sh
clawdline report --repo <project> --status status.md [--notes notes.txt] [--lang zh-TW] [--open] <commit>…
```

- Nommez **les commits de ce tour, du plus ancien au plus récent**. Chaque commit est lu individuellement, donc les commits d'un autre Session
intercalés ne sont pas pris en compte ; ne dépassez jamais une plage.
- `status.md` : un `# Title` optionnel, une ligne optionnelle en dessous, puis un titre `## ` par carte —
ouvrez-la avec ✅, 🟡 ou ❌ — et un court corps Markdown.
- `--notes` : un `path: sentence` par ligne, affiché au-dessus du fichier correspondant. `--pin` (répétable) place un fichier
en premier ; `CLAUDE.md` et `AGENTS.md` sont épinglés lorsque le tour les a touchés. `--exclude` laisse un chemin d'accès et l'indique. `--at` est la révision dont le contenu est affiché (par défaut `HEAD`).
- Il conserve le rapport dans `<state dir>/reports/<date>-<id>/report.html`, en dehors de chaque dépôt,
et affiche deux adresses : **d'abord l'adresse `file://`**, qu'un terminal ouvre, **ensuite
`http://127.0.0.1:<port>/reports/<id>`**, à laquelle le démon de cette machine répond. Intégrez les deux dans votre
réponse finale : la console affiche une adresse `file://` sous forme de texte qu'elle ne peut pas ouvrir, et transforme l'adresse
`http://` en lien. Cette adresse s'ouvre uniquement dans un navigateur sur cette machine connectée à
sa console ; l'accès à un téléphone ou à une visionneuse Cloud est refusé (`report_not_over_cloud`,
`report_local_only`).
- `--out` écrit un autre fichier ou répertoire à la place, avec uniquement l'adresse `file://`. stderr indique
les éléments omis ou tronqués. `--open` ouvre également le fichier dans le navigateur de cette machine.

## 8. Communiquer avec une autre session

**Recherchez-le.** `GET /v1/orchestrator/sessions` est le carnet d'adresses : il contient les identifiants de terminal de chaque session : `id` (son
identifiant de terminal), `label`, `assistant`, `cwd`, `state`, `work_state` et `taskId`.

**Envoyer.**

```sh
clawdline send --to <terminal id> "text"        # or text on stdin
```

Il s'agit de `POST /v1/orchestrator/messages` avec `{from_session, to_session, text}` et un
`Idempotency-Key`. Le démon l'insère dans le compositeur du destinataire à l'intérieur d'une
`<clawdline-message>` enveloppe qui vous désigne comme source.

- `to_session` est un **identifiant de terminal** : un message suit l'onglet que vous vouliez sélectionner, et non une conversation.
`from_session` est votre identifiant de terminal ou de conversation (la commande le renseigne).
- Texte uniquement, 100 000 caractères maximum. Le champ `images` n'existe pas : tout message envoyé est ignoré.
- `ok` indique que les octets ont été reçus par un compositeur, et non qu'ils ont été lus.
- Cela peut prendre plusieurs dizaines de secondes : le démon lit toutes les sessions de la machine avant d'écrire (environ
30 s par relais sur un Mac, mesure effectuée le 19/09/2026). La commande attend jusqu'à deux minutes. Ne coupez pas
— un relais interrompu en cours de route peut saisir du texte sans que celui-ci soit enregistré, et la même touche répond ensuite
`409 request_in_progress`.
- La commande affiche d'abord son `Idempotency-Key`. Si l'appel échoue en cours de route, relancez-le avec
`--key <that key>` : la même touche et le même texte sont saisis une fois. La même touche avec un texte différent est
`409 idempotency_key_reused`.
- Refus : `source_not_found`, `target_not_found`, `same_session`, `target_busy` (le destinataire
affiche un menu ; rien n'a été saisi), `terminal_busy`, `delivery_failed`.

**Montrez une image à la personne.** Ne collez pas de chemin local : sur un téléphone, cela n'ouvrira rien.

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- Jeton Orchestrator uniquement. Un à six fichiers locaux ; chacun doit être un fichier simple, d'une taille maximale de 12 Mio et d'une résolution maximale de 12 000 px de côté. Les formats PNG, JPEG et GIF sont lus directement ; les autres formats passent par `sips` sur macOS.
- La réponse liste `artifacts`, chacun avec un `marker`, par exemple `<clawdline-image id="…">`. **Placez le
marqueur dans votre réponse** ; la console affiche l'image à l'emplacement du marqueur. Les images sont conservées pendant 24 heures.

## 9. Informez la personne

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

`POST /v1/orchestrator/notify`. N'envoyez une notification que lorsque la personne attend une action de votre part : une notification push est utile parce qu'elle reste rare. `--session <terminal>` ouvre cette session lorsque la personne appuie sur la notification.

- `409 agent_notify_disabled` : la personne a désactivé les notifications des agents. Ne réessayez pas.
- `409 not_subscribed` : aucun appareil n'est abonné aux notifications push.
- `429 rate_limited` : limite de 30 notifications par heure pour toute la machine, partagée avec celles des agents enfants.
- `502 push_failed` : le service push a refusé l'envoi ; `sent` et `failed` figurent dans l'erreur.

## 9a. Laisser une note d'intervention humaine

Utilisez une note lorsqu'un Agent engagé dans un travail de longue durée a une action précise à faire lire, effectuer ou décider par la personne, et qu'un message ordinaire pourrait se perdre dans la conversation. La note reste dans la zone d'attention repliée de la Session cible, signalée par un point rouge jusqu'à ce que la personne la marque comme traitée. Vous pouvez poursuivre le travail indépendant ; elle y reviendra à un moment opportun. Une note n'est ni un journal d'avancement, ni un rappel privé, ni une notification, ni une autorisation de décision Board. Évitez les doublons pour une même demande.

**Avant de demander à la personne de choisir dans le chat**, créez une note `answer` qui contient la question exacte, les compromis nécessaires pour décider et deux à quatre réponses suggérées complètes. Appuyer sur un bouton envoie sa réponse comme message dans la conversation : chaque `draft` doit donc être compréhensible sans contexte supplémentaire. Après la création, un bref renvoi dans le chat suffit. Ni la création de la note ni son passage à l'état traité ne constituent une réponse. Attendez le message envoyé par la personne, depuis un bouton ou saisi librement, avant d'agir. Si la création échoue, signalez-le et posez directement la question. Réservez les notes aux décisions qui exigent son jugement, et non aux choix ordinaires que l'Agent peut faire lui-même.

Créez la note à partir d'un fichier contenant son corps JSON. Sans `--target`, la CLI trouve l'identifiant de terminal de cette Root active au moyen de `whoami`. Pour une autre Session, prenez son **identifiant de terminal** actif dans le carnet d'adresses (`clawdline guide fr send`) et passez-le avec `--target`. Par défaut, `--from` utilise l'identifiant de conversation de cette Root active, fourni par l'environnement. La CLI lit les identifiants d'accès de la machine sans les placer dans la ligne de commande, ajoute les identifiants source et cible et affiche l'identifiant durable de la note. En cas de résultat incertain, réutilisez le `--key` affiché.

```json
{"kind":"answer","title":"Choose a date","summary":"One release date needs your choice.","action":"Choose a date when you have a moment.","reason":"Only you can choose it.","options":[{"label":"Tuesday","draft":"Tuesday works for me."},{"label":"Wednesday","draft":"Wednesday works for me."}]}
```

```sh
clawdline note create --body-file note.json
# For another Session: clawdline note create --target <terminal-id> --body-file note.json
```

`kind` vaut `read`, `answer`, `action` ou `report` ; `title`, `summary`, `action` et `reason` sont obligatoires. Une note `answer` peut proposer de deux à quatre choix. Chaque `draft` est la réponse suggérée affichée sur son bouton. Lorsque la personne appuie dessus, la console l'envoie immédiatement comme message à la Session de la note, puis ajoute une ligne de contexte avec l'identifiant, le titre et l'action de la note, afin que la Session destinataire sache à quelle demande elle répond ; cette ligne ne figure pas sur le bouton. La note ne passe dans les éléments récemment traités qu'après l'envoi réussi. En cas d'échec, elle reste en attente et le contrôle d'attention indique que la réponse n'a pas été envoyée. `detail` peut contenir un texte plus long. `document_url` peut pointer vers un vrai document Cloud lisible ; vérifiez la route et le fichier avant de publier le lien. Agissez sur le message reçu, pas sur l'état de la note : la marquer manuellement comme traitée n'envoie rien. Si la réponse bloque réellement votre travail, enregistrez l'état d'attente de la personne et envoyez une seule fois la notification d'attention prévue. Une note visible, à elle seule, n'envoie aucune notification push et ne réveille aucun Agent.

## 10. Le tableau

Le Board comporte trois structures : ses éléments, le Backlog et la liste personnelle de tâches de chaque session. **La personne décide de ce qui y entre.** Une session ne crée directement un élément Board que si le message de la personne, envoyé par Clawdline, le demande ; sinon, elle propose l'élément. Elle n'ajoute jamais une carte de sa propre initiative. Seule exception : le propriétaire d'un Epic peut, après révision du plan de cet Epic, décomposer l'Epic en éléments Feature et Issue, puis les affecter à des Sessions (`clawdline guide fr epic`).

**TODO / 待辦 / 土度 mentionné avec un élément Board désigne les étapes de cet élément.** Ajoutez-les à l'élément avec `--step`. Ne les ajoutez **pas** aussi avec `clawdline todo add`. `clawdline todo add` sert uniquement à la liste de tâches que la personne demande de suivre pour cette Session, sans élément Board.

**Lorsque la personne vous demande de créer un élément Board.** Uniquement si son message — envoyé via
Clawdline, donc exécuté — en fait explicitement la demande, créez-le vous-même :

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin] [--assign-self]
```

Exemple fonctionnel. La personne écrit : « Créer un élément Board pour nettoyer les notes de version, À FAIRE : les rédiger, vérifier les liens, publier. »* Ceci est une seule commande, et rien d’autre.

```
echo "Clean up the release notes before the next release." | \
  clawdline item add --project <place id> --kind feature --title "Clean up the release notes" \
  --step "Draft the notes" --step "Check the links" --step "Publish"
```

- `item add` lit la dernière exécution de cette conversation (`GET /v1/orchestrator/sessions/<conversation>/run`)
sauf si `--run` en nomme un, affiche sa clé d'idempotence avant de poser la question (`--key` réessaie la même
écriture), et affiche l'élément créé avec l'identifiant de chaque étape. Il est
`POST /v1/work/v2/agent/items` avec `{"session_id", "via": {"run"}, "project_id", "kind",
  "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`,
a répondu à `201` avec `{"item", "assigned", "assignment_state"}`.
Un élément Feature, Issue ou Epic arrive **non attribué** par défaut. Les éléments non attribués de la personne sont alors en attente de l'élément Board, dont les étapes sont les suivantes : les lignes `--step` dans l'ordre, ou — si vous n'en indiquez aucune —
deux lignes ou plus de liste Markdown de premier niveau contenant la description. La personne vous demande souvent de rédiger
du travail à l'avance ; la création de l'élément ne vous en confère pas la propriété. `assignment_state` est
`not_requested`. Ajoutez `--assign-self` (`"assign": {"mode": "self"}`) **uniquement lorsque le message de la personne le demande
Session pour effectuer le travail immédiatement** (« créez une tâche et exécutez-la »). La tâche vous est alors
**attribuée**, dans `assigned`, dans le même message. Rien n'est saisi dans votre terminal ; vous
l'avez demandée. Suivez les étapes dans l'ordre, terminez chacune d'elles lorsqu'elle est vérifiée
(`clawdline item steps <item id>`, `clawdline item step-done <item id> <step id>` ; `clawdline item step-add` ajoute
une tâche nécessaire), et passez aux phases suivantes avec `clawdline item phase` comme pour toute
tâche attribuée (ci-dessous). Un élément Epic pris en charge de cette manière suit ensuite la procédure Epic (ci-dessous) avant d'être
implémenté. Si la personne vous demande de reprendre ultérieurement un élément que vous avez créé sans l'assigner, utilisez
`clawdline item claim` (ci-dessous).
Une refactorisation est un travail exécutable qui modifie la structure interne mais pas le comportement externe : elle est
assignée, comporte des étapes et suit les phases, les validations et le mécanisme de révision d'un élément Feature. Un plan est créé
sans être assigné, dans la section Planification, avec ou sans `--assign-self`, et ne comporte aucune étape (`planning_has_no_steps`).
- Le Clawdfather enregistré est l'exception pour les travaux de projet exécutables : il ne possède ni ne modifie jamais
le code du projet. Lorsque le message d'une personne demande explicitement un nouvel élément, il peut utiliser
`clawdline item add --project <place id> --kind feature --title "…" --assign-new` (ou
`--assign-terminal <id>`) pour créer l'élément, puis le déléguer à un projet Session.
`assignment_state` indique si un propriétaire de projet a été enregistré (`assigned`), si un nouvel élément Session nécessite une
première réponse à un dialogue (`awaiting_user`), si l'attribution a échoué (`failed` avec `assignment_error`), ou
si aucune attribution n'a été demandée (`not_requested`). Dans les deux derniers cas, la personne peut attribuer l'élément
à partir de Board. Une réponse avec `pending` mentionne l'élément d'origine après une délégation interrompue ;
vérifiez le Board avant de tenter une autre attribution. Sans ce message explicite, proposez l'élément et attendez l'acceptation.
- La personne voit la carte marquée « Créé par la Session à partir de votre message à HH:MM », avec ses
mots entre guillemets.
- Refus, chacun ne contenant aucune information : `run_unknown` (aucune exécution nommée ou non émise), `run_expired`
(plus d'un jour), `run_other_session` (un message à un autre Session), `session_not_found`,
`child_session` (un processus enfant communique via `result.json`), `project_not_found`,
`project_mismatch` (un élément exécutable ordinaire doit se trouver dans le projet sur lequel vous travaillez),
`coordinator_required` (une machine Session sans le rôle actif), `machine_delegation_required`
(un élément exécutable ordinaire Session a demandé l'affectation combinée machine uniquement à un autre Session),
`invalid_assignment` (un `assign` malformé, ou Clawdfather demandant `self`), `too_many_steps`
(plus de 128), `run_items_exhausted` (un message renvoie au maximum cinq éléments).
- **Pas d'exécution** — la personne a tapé directement dans le terminal, donc `item add` répond à `no_run` ou
`run_unknown` : se rabattre sur une proposition (ci-dessous) et demander à la personne de l'accepter dans les propositions de l'agent Board.

Ne créez jamais un élément Board de votre propre initiative, et n'en utilisez jamais plusieurs pour planifier un travail spéculatif.

**Réclamez l'élément Board que la personne vous a indiqué.** Lorsque le message de la personne via Clawdline vous demande
de prendre un élément spécifique déjà présent sur Board — *"prendre l'élément des notes de version"*, *"réclamez
<identifiant de l'élément>"* — réclamez-le ; il s'agit d'une seule commande :

```
clawdline item claim <item id>
```

`item claim` lit la dernière exécution de cette conversation, sauf si `--run` en désigne une, lit l'élément pour en déterminer
la version, affiche sa clé d'idempotence avant de poser une question (`--key` réessaie l'écriture), et
affiche l'élément ensuite. Il s'agit de `POST /v1/work/v2/agent/items/<id>/claim` avec
`{"expected_version", "session_id", "via": {"run"}}`, et il vous attribue l'élément, à vous,
Session, le destinataire du message. Rien dans cet élément ne désigne un autre Session ou terminal. - L'objet s'affiche alors exactement comme si la personne vous l'avait attribué depuis Board : il vous appartient,
il est déplacé vers `assigned`, ses étapes sont initialisées à partir de la liste de la description si elle n'en contenait aucune. Rien
n'est saisi dans votre terminal. Traitez-le comme n'importe quel objet attribué (voir ci-dessous).
- La personne voit la carte marquée « Réclamé par Session suite à votre message à HH:MM », avec ses
mots entre guillemets.
- Refus, chacun ne s'écrivant rien : `run_unknown`, `run_expired`, `run_other_session`,
`session_not_found`, `child_session` (comme pour `item add`) ; `work_not_found` ; `project_mismatch`
(L'élément se trouve dans un projet auquel vous n'appartenez pas) ; `item_assigned` (Il possède déjà une Session, ou un
est en cours d'ouverture pour cet élément — seule la personne déplace un élément entre les sessions) ; `item_terminal` (Terminé ou
Annulé) ; `planning_not_assignable` (Un plan reste en planification ; un Epic ou une refactorisation peut être pris en charge) ;
`version_conflict` (Il a été modifié ; exécutez à nouveau la commande) ; `run_claims_exhausted` (Un message est valable pour au maximum cinq utilisations). - **Aucune réponse** `no_run` ou `run_unknown` : laissez l'élément à la personne qui l'attribuera.

**Attribuez un élément Board à un nouvel élément Session demandé par la personne.** Lorsque le message de la personne via
Clawdline vous demande de remettre un élément Feature ou d'émettre un nouvel élément Session non attribué — *"ouvrez un
élément de sécurité Session pour <identifiant de l'élément>"* — attribuez-le ; Voici une commande :

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- Sur un élément qui n'est pas un enfant de Epic, `item assign` lit la dernière exécution de cette conversation, sauf si
`--run` en nomme un, comme le fait `item claim`. Il s'agit de `POST /v1/work/v2/agent/items/<id>/assign` avec
`{"expected_version", "session_id", "mode": "new_session", "assistant"?, "model"?, "persona"?,
  "via": {"run"}}`, et ouvre le nouveau Session correspondant au choix « Nouveau Session » de la personne.
- La carte indique « Attribué par une Session de votre message à HH:MM », avec ses mots cités et
le personnage sous lequel le nouveau Session s'exécute.
- Refus, chacun ne contenant rien : ceux de `item claim` (`run_unknown`, `run_expired`,
`run_other_session`, `session_not_found`, `child_session`, `project_mismatch`, `item_assigned`,
`item_terminal`, `version_conflict` et `run_claims_exhausted` : `item claim` et `item assign` partagent un
message sur cinq) ; `kind_person_assigns` (uniquement un Feature ou un problème) ; `new_session_only` (pour le prendre en charge,
réclamez-le) ; `unknown_persona` ; `persona_disabled_for_auto_assignment` (le rôle est désactivé pour
l'attribution automatique dans ce projet).

Ne jamais réclamer ni attribuer un élément de votre propre initiative — uniquement celui mentionné dans le message de la personne — et
ne jamais utiliser le `POST /v1/work/v2/items/<id>/assign` de la personne, qui refuse une Session
(`session_cannot_create_item`). Le propriétaire d'un Epic attribue également aux propres enfants du Epic le
`clawdline item assign` (`clawdline guide fr epic`).

**Créez un nouvel élément Session associé à un élément Board.** Après avoir pris connaissance de son objectif et de son champ d'application, choisissez un nom court décrivant votre tâche et exécutez `clawdline item name <item id> "<task name>"`.
Cette action modifie le nom de votre élément Session une seule fois, sans modifier le titre de l'élément Board ni démarrer un nouveau cycle de modélisation.
Seul le propriétaire actif de l'élément Session peut effectuer cette opération. L'envoi du même nom est sans risque ; un nom différent est refusé, et la personne peut toujours définir manuellement un titre Session. Un élément Board attribué à un élément Session existant ne modifie pas le nom de cette Session.

**Proposer un élément Board.** La file d'attente des **propositions d'agent** de l'élément Board est alimentée par une seule route :

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id` est le `id` d'une ligne de `GET /v1/places`.
- Une source est requise et doit vous appartenir : un élément Board appartenant à cet Session, ou l'une des tâches de cet utilisateur
Session (`proposal_source_required`, `proposal_source_invalid`). Une proposition
issue d'une demande cite la tâche d'origine ; le chemin est donc le suivant :
la personne fait sa demande, vous la `clawdline todo add` (voir ci-dessous), et vous faites votre proposition à partir de l'identifiant de cette tâche. - Rédigez la proposition dans un langage clair et compréhensible : `title` nomme le résultat
qu'ils peuvent constater, `description` indique ce qui change, `reason` explique pourquoi il est important d'agir maintenant, et
`suggested_acceptance` décrit ce qu'ils pourront observer une fois terminé. Ces quatre éléments sont obligatoires. N'utilisez pas
d'acronymes non expliqués, d'identifiants internes, de chemins de code ou de jargon technique comme explication principale.
Board présente d'abord le titre, la source et la raison ; **Expliquer / 詳細說明** détaille les
changements et ce que la personne verra une fois terminé.
- **Pour proposer un élément avec des étapes**, rédigez la liste sous forme de deux listes Markdown de premier niveau ou plus
lignes dans `description`. Lorsque la personne accepte et attribue l'élément, chaque ligne devient l'une de ses
`steps` (voir ci-dessous).
- `201` répond à la proposition en attente. La personne l'accepte, la modifie ou la refuse dans la file d'attente des propositions de l'agent Board
rien ne devient un élément Board tant qu'elle ne l'a pas fait. Refus : `invalid_proposal`,
`proposal_too_large`, `project_not_found`, `proposals_full`.

**Ancienne voie de proposition.** `POST /v1/orchestrator/proposals` (avec `…/<id>/asked` après la demande
dans la conversation) est toujours utilisée : c’est là qu’un processus enfant dépose un élément restant avec son secret de tâche et
`task_id`, et où la proposition de ligne de travail d’un processus racine obtient son option « demander maintenant ou attendre » `instructions`. Ses
lignes apparaissent dans l’ancienne zone « à confirmer », **et non** dans la file d’attente des propositions d’agent v2 Board, donc ce n’est pas ainsi que
l’élément est présenté à la personne sur Board.

**Demandez une décision à la personne.**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "work_id": "<the Board item this is about>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

L'objet doit être ouvert et appartenir à cette Session : sa carte constitue le contexte dans lequel le Board affiche
la question (`decision_source_required`, `decision_source_not_found`, `decision_source_invalid`,
`decision_source_closed`). Deux à quatre options ; `default` doit en faire partie et correspond à ce qui se produit
lorsqu'aucune réponse n'est reçue (après 7 jours, sauf si `due_in_minutes` indique 60–10080). Seule une décision `blocking`
est imposée. Lisez la réponse avec `GET /v1/orchestrator/decisions/<id>`.

**La personne répond ; une session ne fait que relayer ses propos.** Les propositions, décisions et points du conseil d'administration
sont traités sous `/v1/work/…`. Une session écrivant à cet emplacement doit indiquer l'exécution contenant les propos de la personne (`"via": {"run": "<id>"}`) ; sans cela, la réponse est refusée (`403
session_cannot_decide`). Consultez l'exécution la plus récente à l'adresse
`GET /v1/orchestrator/sessions/<conversation>/run` ; une exécution inventée, expirée, provenant d'une autre session ou
antérieure à une question est refusée par son nom. Une personne saisissant directement dans un terminal n'a pas d'exécution ; demandez-lui donc de répondre via Clawdline ou dans la console.

**Vos enfants restent à récupérer.** `GET /v1/orchestrator/sessions/<conversation id>/todos` — nommé par
l'identifiant de conversation, et non par le terminal (`409 session_id_is_terminal` sinon). Le courtier ouvre et ferme
ces éléments à partir des informations de la tâche ; il n'y a rien à écrire.

À chaque limite de tour, avant de vous déclarer inactif, lisez également
`GET /v1/work/v2/agent/session-todos/<conversation id>`. Ses `assigned_items` sont des éléments Board que
la personne a donnés à cette Session, ses `recent_items` sont des éléments que cette Session a récemment terminés, et ses
`direct_todos` sont des demandes rapides, et ses `unacknowledged_completions` sont vos enfants qui
ont terminé sans votre ACK (§5). Cette récupération est la façon dont une tâche effectuée pendant que vous travailliez attend
sans interrompre le tour en cours. Terminez le tour en cours, puis prenez l'élément assigné comme votre
prochain travail possédé et lisez son enregistrement complet, corps de document inclus, avec
`clawdline item show <id>`.

**Une tâche envoyée par la personne.** Un message dont la dernière ligne est
`(Clawdline to-do <id>. When it is done: clawdline todo done <id>)` fait partie des `direct_todos` de cette Session envoyés par la personne depuis Clawdline ; les mots au-dessus de cette ligne constituent la demande.
Effectuez la tâche, et une fois qu'elle est terminée et vérifiée, exécutez `clawdline todo done <id>` avec cet identifiant **avant** de
signaler votre tour — sinon, la ligne reste ouverte dans la liste de la personne, même si la tâche est terminée.
Une tâche non terminée reste ouverte. `clawdline session report` liste sur la sortie d'erreur standard toutes les tâches
envoyées à cette Session et qui ne sont toujours pas cochées (§7).

**Vos propres tâches, à la demande.** Uniquement lorsque la personne demande explicitement à cette Session d'enregistrer son travail comme tâches Clawdline — ou lui remet une liste de plusieurs éléments et lui demande de les suivre
à cet endroit — les écrit dans la liste propre de cette Session :

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add` est `POST /v1/work/v2/agent/session-todos/<conversation id>` avec
`{"todos": [{"text": "…"}, …]}` et une clé d'idempotence qu'il imprime en premier (`--key` réessaie la même
écriture). Un appel transporte 1 à 20 lignes d'au maximum 8 Kio chacune, dans le corps de la requête de 96 Kio, et les ajoute
toutes ou aucune ; Une liste qui porterait le nombre de tâches ouvertes à plus de 500 (Session) est refusée en totalité
(`direct_todos_full`). Le système répond (`201`) avec les lignes, dans l'ordre indiqué. La conversation doit être
une tâche active (Session) connue de ce démon (`conversation_id_malformed`, `session_not_found`) ; une tâche enfant (Clawdline)
est refusée (`child_session`) et continue de signaler jusqu'à (`result.json`).
(

)
Ne faites jamais cela de votre propre initiative et ne planifiez jamais de travail spéculatif. Complétez chaque ligne avec (`clawdline todo done <id>`) uniquement une fois qu'elle est vérifiée. La personne voit ces lignes marquées comme
ajoutées par Session, et elle seule peut les envoyer ou les supprimer. Ce ne sont pas des éléments Board et
elles n'apparaissent jamais dans Board. Les tâches sont une liste de corvées dans la Session actuel ; un élément Board est un travail
que la personne souhaite suivre dans le Board — lorsqu'elle le demande, utilisez `clawdline item add` (ci-dessus),
et sa liste est ajoutée comme lignes `--step` de l'élément, jamais comme tâches supplémentaires.

Si la personne indique clairement que l'élément que vous venez de terminer est toujours inachevé, corrigez
vous-même le code Board ; ne le laissez pas dans « Récemment terminé », ne créez pas d'élément de remplacement et ne demandez pas à la personne
de le rouvrir. Relisez l'élément pour connaître sa version actuelle, puis utilisez :

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "The concrete behavior or acceptance claim that remains unfinished"}
```

Utilisez cette fonction uniquement lorsque la référence à l'élément que vous venez de terminer est claire. Cette route n'accepte que les travaux
`done` dont la tâche finale a été validée par ce même Session ; elle ne peut pas annuler l'annulation d'une personne ni prendre en charge la réalisation d'un autre Session. Elle conserve la preuve précédente, démarre un nouveau
cycle dans `implementing`, rétablit cette Session comme propriétaire et enregistre la raison dans l'historique immuable de l'élément.
La raison ne dépasse pas 8 Kio. Un suivi ambigu ne vous autorise pas à modifier un élément Board.

Lorsque l'agent propriétaire a besoin que la personne agisse ou choisisse, il lui demande une décision et attend sa réponse. Commencez par ouvrir une décision concernant cet élément (`POST /v1/orchestrator/decisions` avec les
`work_id`, deux à quatre options, un `default` et une échéance — pour une action, des options telles que
`{"id": "done", "label": "I've done it"}` et `{"id": "cannot", "label": "I can't"}`), puis pointez
l'élément vers celui-ci sur le chemin authentifié par la machine :

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

La décision doit exister (`decision_not_found`), être celle de cette Session (`decision_other_session`), concerner
cet article (`decision_other_item`) et être encore ouverte (`decision_not_open`) ; `decision_id`
toute autre condition est `decision_requires_waiting_user`. Un `waiting_user` sans décision est
refusé avec `waiting_user_requires_decision`. La personne répond sur la carte Board ou dans
« En attente » ; Lorsque la décision est prise ou que sa valeur par défaut est atteinte à l'échéance, le démon
efface le champ `waiting_user` de l'élément lors de la même écriture, enregistre la réponse dans l'élément et saisit l'identifiant et l'étiquette de l'option choisie dans le champ Session lorsqu'il est inactif. Pour mettre fin à l'attente, définissez
`condition` sur une chaîne vide (ou une autre condition) sur le même itinéraire ; la décision est alors
`withdrawn` et affiche « En attente », de même lorsque l'élément est libéré, réaffecté,
annulé ou terminé. Répondre à une décision retirée est refusé avec `decision_withdrawn`.

**Portes de planification et de vérification capturées.** `planning_gate` est activé par défaut et `verify_gate` est désactivé ;
`clawdline setting get|set planning_gate|verify_gate` accepte `on/off` ou `true/false`. La première
affectation réussie d'un cycle d'exécution fige les deux valeurs. Les réaffectations et les modifications ultérieures des paramètres globaux
ne modifient pas ce cycle. Un Epic ou Feature avec une planification capturée nécessite
des critères d'acceptation avant sa mise en œuvre. Un Epic nécessite également un plan et une revue indépendante ; un Feature
n'en a besoin que si la personne a coché l'option « Revue indépendante requise » (ci-dessous). Un problème n'a jamais de porte de planification. La désactivation de la planification contourne la planification forcée Epic.
L'activation des deux options implique une planification suivie d'une vérification indépendante ; la planification seule conserve la fusion ordinaire
la vérification seule ignore la planification mais vérifie tout de même le candidat exact ; l'activation des deux options utilise
le cycle de vie ordinaire. La personne n'a pas besoin de renseigner l'acceptation sur Board. Si un élément soumis à une porte arrive
sans cela, écrivez des critères observables avec `clawdline item acceptance <item id> --body-file <file>`
après l'affectation et avant la transition soumise à une porte. Le propriétaire Session peut renseigner un contrat vide
une seule fois. Lorsque la personne demande explicitement à ce propriétaire Root via Clawdline de réviser l'acceptation de cet élément,
écrivez le Markdown de remplacement complet dans un fichier et exécutez
`clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>`.
La version de l'élément est la ligne `item version N`. `clawdline item show <id>` l'imprime (`item steps`
n'imprime que la version acceptée) ; lire l'exécution du message à partir de
`GET /v1/orchestrator/sessions/<conversation>/run`. L'extrait de message conservé doit explicitement
demander une modification d'acceptation ; une interdiction, une discussion ou une simple question ne constitue pas une autorisation.
Il peut faire référence à l'élément par le contexte de la conversation si ce Root
possède exactement un élément ouvert ; sinon, il doit identifier l'élément par son ID ou son titre. L'exécution doit être
plus récente que la version acceptée actuelle. Un refus saisi au clavier signifie qu'aucune modification n'a été apportée. Réessayez une réponse incertaine avec
les mêmes `--key`, `--run`, `--expected-version` et les mêmes octets de fichier. La personne peut également modifier directement. Le modifier avant la fusion invalide les anciennes procédures PASS et les substitutions ; une fois la fusion commencée, il est verrouillé.

Avec la vérification de capture activée, exécutez `clawdline item phase <id> verifying` à partir d'un répertoire de travail propre et enregistré
au niveau de son candidat validé : l'interface de ligne de commande envoie la branche courante et le HEAD complet, et le démon
vérifie le projet, la base de cycle, l'arbre et le condensé d'acceptation. Un vérificateur détaché en lecture seule Codex utilise
`code-reviewer` pour les problèmes, `reality-checker` pour Epic et `evidence-collector` pour un Feature avec
des images de référence ou un document de conception (sinon `reality-checker`). Le verdict est :
`PASS`, `FAIL` ou `NEEDS_WORK` ; les instructions non vérifiées expliquent la raison et n'autorisent jamais la fusion. Un résultat manquant
ou malformé constitue une défaillance technique, avec une seule tentative de nouvelle exécution limitée, puis une escalade. Le cycle final de bout en bout d'un Epic
attend que tous les processus enfants soient terminés et que les composants affectés soient intégrés
en un candidat exécutable. Les tests enfants ciblés et les vérifications d'intégration sont effectués en premier ; ne
lancez pas le travail final de bout en bout pour navigateur ou multi-comptes sur une interface utilisateur factice, des branches déconnectées ou des
API incomplètes. Avant le lancement, prouvez que le processus choisi peut effectivement ouvrir l'URL cible avec un
navigateur autorisé ou une automatisation locale équivalente, et qu'il dispose des comptes de test, des fixtures et des autorisations d'origine
nécessaires. Nommez cette route dans le brief ; `--permission-mode full` seul ne correspond pas à un accès navigateur.
Résolvez un échec de vérification préliminaire avant de réessayer, plutôt que d'envoyer un autre vérificateur sur le même blocage ; un échec de vérification préliminaire ne constitue pas une tentative de bout en bout. Prévoyez un cycle complet de bout en bout par Epic, et non un par enfant ou révision. Après la correction d'un défaut, relancez uniquement les scénarios concernés.
Répétez le cycle complet uniquement lorsque le périmètre d'acceptation ou les limites d'intégration changent de manière significative, et consignez la raison. `verifying → merging` nécessite une validation active pour le candidat/critère exact ou une dérogation explicitement justifiée ; une phrase de vérification seule ne peut pas l'accorder. Trois échecs consécutifs sont remontés au propriétaire du parent actif Epic, puis à la personne si ce propriétaire est indisponible ; une défaillance technique est remontée séparément. Seul le propriétaire parent désigné utilise `POST /v1/work/v2/agent/items/<id>/gate-decision` ; une personne utilise `POST /v1/work/v2/items/<id>/gate-decision`. N'utilisez jamais la voie de la personne en tant qu'agent. Board nomme séparément l'IA, la personne et les substitutions techniques, jamais comme vérificateur PASS. En cas de capacité de détails conservés, la personne télécharge d'abord `GET /v1/work/v2/items/<id>/gate-export`, vérifie le condensé du manifeste, puis confirme `POST /v1/work/v2/items/<id>/gate-purge` avec ce condensé et la version de l'élément. La purge supprime uniquement les détails clôturés éligibles ; les agrégats, les faits les plus récents et l'audit restent.

**Progression de la phase.** Le propriétaire Session fait progresser son élément à travers les phases d'exécution lui-même ; personne d'autre ne le fait, et un reçu de tour ou une condition résolue ne le fait pas. La phase n'est pas un champ de
`…/edit` (`phase_not_editable`). Exécutez chaque transition lorsque l'opération qu'elle désigne a effectivement eu lieu :

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

La commande lit la version de l'élément, affiche sa clé d'idempotence (`--key` tente à nouveau l'écriture)
et affiche l'élément.

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "no_landing_reason"?: "…", "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Étape par étape : `assigned → implementing → verifying → merging → deploying → done`.
Depuis `verifying`, vous pouvez revenir à `implementing` ; depuis `merging`, à `implementing` ou
à `verifying`. Un élément dont la porte de vérification est désactivée peut également passer directement de `implementing`
à `deploying` sur son point d'arrivée (ci-dessous), `verification` étant optionnel ; un élément bloqué est
refusé à cette étape avec `verification_gate_on` et parcourt toute la ligne. Rien d'autre ne saute une
phase, et `done` n'est accessible que depuis `deploying`.
- `merging` nécessite `verification`. `deploying` nécessite un intégration : un enfant broker de cet élément qui
a atterri, ou `landing` nommant un commit que le démon trouve à la fois sur la branche locale du projet `target`
et `refs/remotes/<remote>/<target>` — pousser d'abord. Lorsque le travail a été transféré vers un autre dépôt,
(un élément backend dont la modification était un commit frontend), `landing.project` (`--landing-project`)
identifie l'ID de ce projet à partir de `GET /v1/places`, et le commit est recherché à cet endroit.
Un dépôt imbriqué dans le répertoire du projet (`cloud/`) est ici considéré comme un projet distinct. La preuve est
écrite comme l'**enregistrement racine d'intégration** du broker, une seule fois : le même élément, dépôt, commit et
cible enregistrés à nouveau constituent le même enregistrement. L'historique de l'élément le nomme `landing_id`, et
`clawdline landings --work-id <item id>` le répertorie. Avant `deploying`, le démon interroge immédiatement git
pour savoir si la branche d'un enfant lié a été fusionnée, de sorte qu'une fusion effectuée il y a un instant est prise en compte sans
attendre la prochaine vérification du broker. Un travail sans code prend `no_landing_reason`
(`--no-landing-reason`) au lieu d'un landing ; il est refusé à côté d'un `landing`
(`invalid_landing_evidence`), alors qu'un enfant lié doit encore son landing (`landing_owed`, nommant
la tâche), et à côté d'un enfant qui a été landing (`landing_recorded`). `done` a besoin de `deployment` ou
`no_deployment_reason` ; Le code `deployment_policy` détermine lequel (`required` prend uniquement
`deployment`, `not_required` uniquement, `no_deployment_reason`, `agent_decides` les deux). Chaque étape
doit être terminée au préalable.
- `done` libère votre tâche et déplace l'élément vers la ligne des éléments récemment terminés de Session. Ajoutez un
rapport d'achèvement (ci-dessous) avant celui-ci lorsqu'il est dû. - Refus : `invalid_transition` (phase suivante incomplète ou preuves manquantes), `steps_incomplete`,
`not_item_owner`, `item_unassigned`, `item_terminal` (rouverture par une personne), `evidence_unknown`,
`direct_landing_not_applicable`, `invalid_landing_evidence`, `landing_project_not_found`,
`landing_commit_unresolved`,
`landing_target_unresolved`, `landing_not_on_target`, `landing_remote_unresolved`,
`landing_not_published`, `landing_owed`, `landing_recorded`, `landings_full` (l'élément conserve 64
points d'intégration racine), et `version_conflict` : relire et renvoyer. Un refus pour absence de point d'intégration
se termine par ce que le broker a trouvé lors de sa dernière recherche (une branche non encore fusionnée, un dépôt qu'il
n'a pas pu lire).

**Fin en une seule commande.** Une fois le travail atterri, `clawdline item finish <item id>` parcourt
l'élément de `implementing`, `verifying`, `merging` ou `deploying` à `done` en une seule transaction :

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?}, "no_landing_reason"?: "…",
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Chaque étape est celle que `item phase` effectue, en passant par les mêmes portes, et écrit sa propre
`item.phase_changed` ; toute étape refusée refuse l'ensemble et rien n'est écrit.
- L'intégration est lu, non saisi : le commit est le candidat autorisé de la porte sur un élément contrôlé,
sinon le commit atterri des enfants liés ; la cible est la branche dont l'intégration est nommé ; le
dépôt distant est celui que cette branche suit. Tout champ que vous indiquez l'emporte, et le résultat est prouvé par rapport à
git comme `item phase deploying` le prouve. Une porte de vérification capturée nécessite toujours son PASS :
`implementing`, un élément contrôlé est refusé `verification_candidate_required`, donc entrez `verifying`
avec `item phase` depuis l'arborescence de travail candidate, attendez le PASS, puis terminez.
- Un enfant lié dont la branche a été fusionnée il y a un instant est enregistré comme ayant été intégré par la fin elle-même : le
démon interroge git avant de lire les intégrations, donc `landing_required` juste après une fusion signifie que la
branche n'est pas sur une cible, et le refus indique ce que le broker a trouvé.
- Travaillez sans code : `clawdline item finish <item id> --no-landing-reason "…" --no-deployment-reason "…"`,
selon les mêmes règles que sur `item phase`. - Un élément déjà référencé `done` reçoit une réponse telle quelle, sans aucune modification. Ainsi, si le même intégration est vu deux fois,
aucun mouvement n'est effectué.
- Les refus ajoutent `verification_required`, `landing_required`, `deployment_required` (la note indique que
l'étape était manquante), `landing_target_unknown`, `landing_remote_unknown`, `landing_remote_unreadable`,
`landing_ambiguous` (nommer avec l'indicateur), `landing_owed`, `landing_recorded` et
`finish_not_started` (toujours avant `implementing`).

Un élément attribué peut contenir `steps`. Une attribution réussie peut les initialiser à partir de deux ou plusieurs lignes de liste Markdown de niveau supérieur dans la description, et un élément que vous avez créé avec `clawdline item add` contient ses lignes `--step`. Chaque étape est une entrée de liste de contrôle sur cet élément, et non un autre élément Board.
Terminez une étape vérifiée avec `clawdline item step-done <item id> <step id>` ; elle envoie
`POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete` avec `{"session_id"}`. Sur les routes de l'agent, `expected_version` est facultatif : omis, l'écriture agit sur la version actuelle ; Si l'élément nommé (`--expected-version`) est comparé, une valeur obsolète renvoie `version_conflict`. Une transition vers `done` est refusée avec `steps_incomplete` tant qu'une étape reste
ouverte ; Clawdline n'est jamais vérifié simplement parce que la phase parente a progressé.

**Décomposition de votre élément en étapes.** Lorsqu'un élément dont vous êtes responsable ne comporte aucune étape et que le travail est
multi-étapes (plusieurs modifications vérifiées séparément ou plusieurs parties du système),
décomposez-le vous-même en étapes ordonnées avant de l'implémenter : deux à huit étapes concrètes, chacune
que vous pouvez vérifier individuellement. Une modification simple ne nécessite **aucune** étape ; n'ajoutez pas d'étapes à une
liste. Si le travail s'avère plus important que prévu, ajoutez l'étape à ce moment-là.

```
clawdline item step-add <item id> "Wire the route" "Cover it with a test" "Say it in the guide"
```

Les titres sont des arguments, soit un par ligne d'entrée standard non vide. La commande relit l'élément avant chaque
titre, affiche chaque clé d'idempotence avant son écriture et affiche un bref accusé de réception avec une indication `item show`. Il s'agit d'une
requête réservée au propriétaire par titre,

```
POST /v1/work/v2/agent/items/<id>/steps     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

avec `"position"` une étape après la dernière étape existante, car les étapes sont ordonnées par position. Ensuite,
terminez chaque étape avec `clawdline item step-done` une fois vérifiée. Il ne s'agit pas de l'« initiative personnelle »
qui est interdite pour les éléments et les tâches Board : l'élément vous appartient déjà, et ses étapes sont la façon dont vous
montrez à la personne les étapes du travail qui vous ont été confiées.

Lorsqu'une enquête approfondie est nécessaire pour identifier la cause première d'un problème ou d'un incident afin d'en déterminer la cause première,
ou pour distinguer la solution optimale des alternatives plausibles, ajoutez un rapport de résolution lisible par l'utilisateur
avant de faire passer l'élément à `done`. Une correction simple, directement observée, n'en nécessite pas.
Consignez dans un fichier la description de l'incident, sa cause première, les modifications apportées, la méthode de vérification et les éventuelles limites restantes,
puis :

```
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Il envoie `POST /v1/work/v2/agent/items/<id>/documents` pour vous (la partie Epic du §10,
`clawdline guide fr epic`, liste ses champs). Une requête curl manuelle vers cette route sans les informations d'identification
la commande lit les réponses `401 unauthorized`. Le corps est au format Markdown, d'une taille maximale de 64 Kio. Rédigez pour la personne qui a signalé le problème, et non comme un journal de débogage brut,
et n'y incluez aucune donnée privée. Le propriétaire actif doit l'ajouter avant que l'élément ne devienne
terminal ; relisez-le après un conflit de versions. Un rapport d'achèvement est un récit et ne
remplace jamais les preuves de vérification, d'intégration ou de déploiement. Lorsqu'il est présent, il reste sur l'élément fermé
Board et s'ouvre directement depuis la ligne « Récemment terminé » de Session.

`/v1/board` correspond aux anciennes cartes de l'application Swift, en lecture seule. L'intégration est un fait établi : un élément n'est jamais
marqué comme atterri manuellement (`422 landing_is_broker_fact`).

### Epic et Feature : suivez le paramètre de révision de la personne avant la mise en œuvre.

Un Epic dont le cycle a capturé la planification nécessite un plan et une révision indépendante. Un Feature porte le
paramètre **Nécessite une révision indépendante** de la personne (`review_required` sur l'élément ; `clawdline item steps
<id>` l'imprime). Seule la personne qui le configure, sur Board ; vous ne pouvez pas le faire et vous n'évaluez pas vous-même le risque de
Feature. Le démon lit le commutateur lorsque vous demandez à entrer `implementing`.

- **Décoché** (par défaut) : rédiger les critères d'acceptation succincts de Feature, implémenter et exécuter
des tests ciblés. Ne pas rédiger de plan de revue, ne pas lancer de tâche enfant `plan_review` et ne pas
enregistrer d'évaluation des risques.
- **Coché**, et pour chaque tâche Epic en cours de planification : utiliser le chemin du plan revu.

Si vous pensez qu'un élément Feature non vérifié mérite d'être examiné, signalez-le à la personne concernée et laissez-la le faire ;
il n'existe aucun moyen côté agent de demander une vérification au démon.

1. Planifiez soigneusement et inscrivez le plan sur l'élément :
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. Dépêchez un processus enfant en lecture seule dont la mission est d'examiner ce plan de manière critique : qu'est-ce qui manque, est erroné ou risqué ?
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. Attendez que le processus enfant ait terminé. Un processus enfant ayant effectué une vérification réussie avec le code `--work-id` enregistre son
accusé de réception de vérification sur l'élément sous la forme du document `plan_review` ; vérifiez `.documents` dans
`GET /v1/work/v2/items/<id>`. Seulement si l'information est absente (par exemple, si l'enfant a été envoyé en mission)
sans `--work-id`, veuillez l'enregistrer manuellement :
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
Relancer cette commande pour la même tâche est sans conséquence : elle se réfère au document déjà présent.
Un bref résumé des modifications apportées au plan, destiné à la personne concernée, constitue un document distinct
`other`, et non une seconde révision.
Si un plan Feature est modifié après révision, rédigez un document `other` intitulé
`Review boundary assessment` après le plan révisé au format JSON
`{"new_risk_boundary":false,"reason":"..."}` uniquement si la modification reste dans les limites de risque de la
révision précédente. Toute modification ou incertitude quant aux limites de risque justifie une nouvelle révision ciblée.
La limite de deux révisions Epic reste applicable. 4. Décomposez le travail en étapes avec `clawdline item step-add <item id> …`.
5. Ensuite seulement, `clawdline item phase <item id> implementing`.

Planifiez la vérification comme une séquence. Chaque composant enfant vérifie son propre code avec des tests ciblés ;
le responsable Epic intègre les composants concernés et exécute le plus petit test de fumée inter-composants utile.
Ce n'est qu'une fois le candidat intégré fonctionnel que le responsable doit lancer une véritable vérification de bout en bout
et la revue UX/produit indépendante applicable. Vérifiez la route du navigateur du vérificateur, l'URL cible,
les comptes de test, les fixtures et les permissions avant le lancement. N'utilisez pas de tâches de vérification en lecture seule répétées
pour découvrir ou contourner un navigateur manquant : corrigez d'abord l'accès ou choisissez un équivalent local
de navigateur. Un test préliminaire échoué n'est pas une tentative de bout en bout. Planifiez un cycle complet
de bout en bout par Epic pour le candidat stable, et non un par composant enfant ou révision ; après une correction ciblée, réexécutez uniquement les chemins concernés. Répétez le cycle complet uniquement après une acceptation importante ou une modification d'intégration,
et consignez cette raison.

`clawdline item doc` lit l'élément pour connaître sa version et sa dernière position dans le document, affiche sa
clé d'idempotence (`--key` tente à nouveau l'écriture) et affiche l'élément. Le corps provient de
`--body-file` ou de l'entrée standard. Il s'agit de `POST /v1/work/v2/agent/items/<id>/documents` avec
`{"expected_version", "session_id", "role", "title", "body", "reference", "position"}` ; Les rôles
sont `spec`, `design`, `test`, `deploy`, `completion_report`, `other`, `plan` et `plan_review`.

Pour réviser un document, réécrivez-le en utilisant les mêmes `--role` et `--title` : le démon remplace son
corps, sa référence et sa position, conserve son identifiant, incrémente sa version et enregistre
`document.revised`. L'interface de ligne de commande (CLI) indique `added … at v1` ou `revised … to vN`, et `clawdline item show`
imprime le `vN` de chaque document. Renvoyer le même texte ne change rien et répond au document
tel quel, une nouvelle tentative est donc sans risque. L'ancien texte n'est pas conservé ; utilisez un titre différent pour conserver les deux.

- `plan`, `plan_review` et la limite de révision ne sont jamais modifiés sur place : chaque écriture ajoute un nouveau
document, car le processus de planification les lit dans l'ordre et une révision nomme le plan qu'il a lu.
- Un élément contient au maximum 32 documents, et `completion_report` n'en fait pas partie : il tient toujours,
même dans un élément plein. Un élément contient un code `completion_report` ; en écrire un autre, quel que soit son titre,
le modifie et lui attribue le nouveau titre.
- Un 33e document est refusé avec le code `documents_full` et rien n'est écrit ; le message indique la
commande `clawdline item doc` qui modifie un document existant.

- `plan` et `plan_review` appartiennent à un Epic ou Feature (`document_role_not_applicable` pour les autres types).
- Le `reference` d'un `plan_review` correspond à l'identifiant de la tâche enfant Clawdline qui a examiné le plan. Le
démon n'accepte la requête que si la tâche existe (`plan_review_task_unknown`), a été lancée par le
propriétaire de l'élément (Session ou `plan_review_task_not_owned`), se trouve sur la ligne de cet élément si celui-ci en désigne une
(`plan_review_task_other_item`), est de type `plan_review` ou `plan_review_task_wrong_kind`, s'est terminée
avec `success` ou `plan_review_task_unfinished`, et n'a pas été lancée avant le dernier plan
(`plan_review_task_stale`). Une révision sans plan préalable est refusée (`epic_plan_required`).
Le document automatique issu d'une révision enfant passe les mêmes vérifications, et une écriture répétée pour la
même tâche est idempotente. - `clawdline item phase <item id> implementing` sur un Epic en cours de planification est refusé sans son
plan révisé (`epic_plan_required` ou `epic_plan_review_required`). Un Feature en cours de planification, vérifié par la personne concernée,
nécessite une vérification indépendante, est refusé de la même manière (`feature_plan_required` ou
`feature_plan_review_required`) ; un élément non coché nécessite uniquement ses critères d'acceptation.
Un plan révisé sur un Feature vérifié nécessite également une preuve de limites inchangées ou une autre vérification.
Un Epic en phase de mise en œuvre peut entrer directement en phase d'exécution.
- Le portail lit le dernier accusé de réception `plan_review` de sa tâche de vérification. Chaque constatation comporte un
`severity` de `blocking` ou `non_blocking` (`important` et `minor`, de l'ancien modèle,
comptent comme non bloquantes). Le verdict est `safe_to_land` en l'absence de constatation, `proceed_with_findings`
lorsque chaque constatation est `non_blocking`, et `changes_required` lorsqu'au moins une constatation est `blocking`. Une dernière revue
comportant un blocage refuse `item phase implementing` et toute exécution avec
`--work-id` sur l'élément toujours attribué, à l'exception de `--kind plan_review`
(`epic_plan_review_blocking` ou `feature_plan_review_blocking`) ; le refus liste les blocages
et les instructions suivantes : réviser le plan, puis lancer une nouvelle revue. Seules les observations non bloquantes
ou un compte rendu antérieur dont les blocages ne présentent aucune gravité permettent la poursuite de l'élément. Un Epic
fait l'objet de deux revues au maximum : si la seconde bloque toujours, réviser le plan pour répondre à ses blocages,
et le plan révisé est mis en œuvre sans troisième revue ; n'en lancer aucune.

**Décomposez le Epic en sous-éléments et distribuez-les.** Ceci constitue la seule exception à la règle « une session
crée un élément Board uniquement lorsque le message de la personne le lui demande » et à la règle « seule la personne attribue les éléments » : la personne qui vous a attribué le Epic est habilitée à le décomposer. Une fois que le
plan révisé a transformé le Epic en `implementing`, si certaines parties sont mieux gérées par d'autres sessions,
créez des éléments Feature ou des éléments « À émettre » sous celui-ci et attribuez-les.

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | description on stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- Les identifiants de terminal se trouvent dans le carnet d'adresses de session, `GET /v1/orchestrator/sessions` (`clawdline guide
  send`) ; la Session doit fonctionner dans le projet du Epic. Vous pouvez vous attribuer un enfant, et
`--assign-new` ouvre une nouvelle session Session avec une affectation Root qui nomme le Epic. Sans
indicateur `--assign`, l'enfant reste non affecté.
`item child` lit Epic pour sa version, imprime sa clé d'idempotence (`--key` réessaie la même
écriture) et imprime l'enfant. Il s'agit de `POST /v1/work/v2/agent/items/<epic id>/children` avec
`{"expected_version", "session_id", "kind", "title", "description", "steps"?, "deployment_policy"?,
  "assign"?: {"mode": "existing_session", "terminal_id"} | {"mode": "new_session", "assistant"?,
  "model"?, "persona"?}}`, qui a répondu à `201` avec `{"item", "assigned", "assignment_error"?: {"code", "message"}}`.
L'enfant fait partie du projet de Epic, porte `parent_id` (le Epic), et sa carte indique que le propriétaire de Epic,
Session, l'a créé. Ses étapes correspondent à vos lignes `--step`, ou — si vous n'en indiquez aucune — à sa
liste de description une fois l'affectation effectuée.
- L'enfant est d'abord créé, puis affecté. En cas d'échec de l'affectation, l'enfant **reste,
non affecté**, la réponse contient `assignment_error` avec le code de l'affectation
(`session_unavailable`, `project_mismatch`, `assignment_failed`, …), et la commande renvoie le code 1 :
réaffectez-le avec `item assign`, ou laissez-le à la personne.
- `item assign` est `POST /v1/work/v2/agent/items/<child id>/assign` avec `{"expected_version",
  "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}` ; Cela déplace un enfant ouvert de votre Epic
vers un autre Session, la même affectation que celle effectuée par le choix d'une personne.
- Refus, chacun ne contenant rien : `not_epic_owner` (vous n'êtes pas le propriétaire de Epic), `parent_not_epic`
(le parent n'est pas un Epic), `epic_not_planned` (le Epic est encore avant `implementing` : les
enfants sortent d'un plan révisé), `item_terminal` (le Epic est terminé),
`child_kind_not_allowed` (seulement `feature` ou `issue`), `epic_children_full` (un Epic contient au maximum
32 enfants (ouverts ou fermés), `not_epic_child` (`item assign` d'un élément qui n'est pas un enfant de Epic —
la personne l'attribue, sauf si son message vous le demande : `clawdline guide fr board`), `invalid_assignment`, `version_conflict`, `persona_not_applicable` (422 :
un compte utilisateur avec une Session existant) et `unknown_persona` (400 : un identifiant manquant dans le catalogue).
- **Un persona** est un rôle avec lequel un nouveau Session est lancé : du texte est ajouté à son invite système pour qu'il fonctionne comme ce rôle, pendant toute la conversation. Ceci est uniquement pour un nouveau Session.
(`--assign-new`, `--new`, `dispatch`) ; une Session existant conserve celui avec lequel il a été ouvert. Aucun par
par défaut. Un persona ne remplace jamais `CLAUDE.md`/`AGENTS.md`, le brief, `CHILD.md` ou ce
protocole. `GET /v1/personas` les liste ; Les identifiants (`teams` sur chaque liste indiquent toutes les équipes auxquelles appartient un personnage, et un personnage peut appartenir à plusieurs équipes) :
- `architect` — planification d'un Epic ;
- `backend` — un démon, une API ou une fonctionnalité de la boutique ;
- `frontend` — une fonctionnalité de console ou de téléphone ;
- `minimal-change` — un problème : la plus petite correction possible ;
- `code-reviewer` — révision et `plan_review` éléments enfants ;
- `reality-checker` — vérification : preuves avant de conclure au « fonctionnement » ;
- `security` — modifications des autorisations, du jumelage ou Cloud ;
- `technical-writer` — documentation et guides ; - Marketing, pour un blog, un site ou un dépôt de documents : `seo` (pages et métadonnées), `content-writer`
(articles rédigés dans des fichiers), `ai-search` (pages que les moteurs de réponse d’IA peuvent citer), `social-media`,
`instagram`, `email` (newsletters), `growth` (expériences mesurées) `pr` (annonces) et `zh-editor` (rédige ou restructure des articles entiers en chinois à partir du lectorat, du sujet et du plan des titres).
- Produit, qualité et opérations : `product-manager`, `sprint-prioritizer`, `feedback-synthesizer`,
`trend-researcher`, `ux-researcher` ; `test-automation`, `accessibility`, `performance`,
`api-tester`, `evidence-collector` (règles RÉUSSI ou ÉCHEC par réclamation à partir de la preuve capturée) ; `sre`, `devops`,
`incident-commander`, `finops` et `secrets`.
- Conception et aspects commerciaux : `ui-designer` (écrans du système de conception du projet), `ux-architect` (flux
et structure de la mise en page), `brand-guardian` (cohérence de la marque), `ui-finish-gate` (vérification visuelle
avant expédition), `image-prompt` (invites de génération d'images), `pricing`, `customer-success`,
`support` (réponses préliminaires), `analytics` (réponses basées sur des données réelles), `devrel` (exemples fonctionnels)
et `privacy` (vérifications des données personnelles ; ne constitue pas un avis juridique). - `zero-review-lead` — est responsable d'une revue Epic qui réexamine une fonctionnalité ou un processus existant à partir de zéro :
planifie les objectifs, les envoie en tant que réviseurs enfants en lecture seule avec un ensemble de faits partagés,
et transforme leurs preuves en une conception cible ; sa compétence est `zero-based-review`.
- **Ajoutez une revue UX/produit indépendante lorsque Epic modifie une expérience utilisateur.** Dans le
plan, indiquez si Epic modifie une interface utilisateur, le parcours utilisateur ou la politique produit.
Si c'est le cas, avant la fusion, envoyez au moins un réviseur enfant spécialiste en lecture seule, en utilisant `ux-architect`
par défaut pour la mise en page, l'interaction et le flux produit de bout en bout :

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

Son résumé nomme le candidat intégré et demande des données pour ordinateur et appareils mobiles compatibles (le plus petit),
des preuves concernant le comportement au clavier et avec un lecteur d'écran, les impasses, l'adéquation au produit, la gravité et une recommandation concrète.
Il doit **indiquer « non vérifié » et expliquer pourquoi** lorsque les preuves sont indisponibles.
Utilisez plutôt `product-manager` lorsque la politique et la portée, et non la mise en page, constituent le risque principal ; ajoutez
`ui-finish-gate` lorsqu'une vérification visuelle avant expédition distincte est essentielle. Résolvez chaque blocage
et enregistrez l'identifiant de la tâche, le verdict et la disposition dans les preuves de vérification ou le rapport d'achèvement de Epic.
S'il n'y a pas d'impact sur l'utilisateur, expliquez pourquoi dans le plan et n'ajoutez pas de cérémonie de revue.
Indiquez la portée de cette revue. En règle générale, n'envoyez chaque spécialiste concerné qu'une seule fois pour
le candidat intégré Epic ; N'envoyez pas systématiquement les rôles UX, marque, sécurité et autres sous forme de liste de contrôle.
Clôturez vous-même les corrections mineures (texte, espacement, tests, etc.) par des vérifications ciblées.
Ne redistribuez que si une modification ultérieure altère sensiblement le parcours utilisateur, la politique produit, la marque,
l'orientation de la marque, le périmètre de sécurité ou tout autre risque hors du périmètre enregistré ; nommez le périmètre modifié et sollicitez uniquement le spécialiste concerné. Cette revue ne remplace jamais la validation du plan ni le contrôle d'exactitude PASS.
- **Vous restez responsable de Epic après la fusion de chaque élément enfant.** Relisez immédiatement l'élément enfant et `clawdline item steps <child id>` ; vérifiez que chaque étape est terminée. Une fusion ne ferme pas l'élément enfant et `merging` n'est pas un état final. Le processus enfant Session doit :
terminer les étapes restantes, enregistrer un accusé de réception pour le commit exact déjà accessible
depuis la cible locale et `origin/main`, puis effectuer la migration `deploying` → `done` avec une preuve de déploiement
ou une raison de non-déploiement correspondant à sa politique de déploiement. Si vous êtes propriétaire du processus enfant, effectuez ces actions
vous-même. Si un autre processus Session en est propriétaire, contactez ce propriétaire ou utilisez la procédure de réaffectation autorisée pour les processus enfants ;
n'usurpez pas l'identité de son propriétaire (`not_item_owner`). Accusez réception de la notification de fin de traitement du broker
si elle existe, puis classez les résidus de l'arborescence de travail et supprimez uniquement les éléments identiques à l'élément atterri
ou les éléments temporaires de la tâche. Conservez les octets non atterris, mixtes ou inconnus pour le prochain propriétaire. Ne déclarez pas
le parent Epic terminé tant que tous ses enfants ne sont pas `done` ou `cancelled` : `epic_children_open`
noms du nombre restant. Ne créez aucun élément Board autre que les enfants de Epic.
- **Un enfant terminé ne ferme pas ses Feature, Root et Session indépendants.** Pour chaque Root ouvert
par le Epic avec `--assign-new`, utilisez `clawdline session close --dry-run --terminal <id>` (il répond
`closing as epic_owner` uniquement pour un Root que ce Epic a ouvert) pour lire son `closeability` ; ne déduisez pas la propriété d'une étiquette ou d'une position terminale,
et ne traitez pas `clawdline session report` comme une fermeture. Une fois que l'enfant atteint `done`, demandez à ce que
le propriétaire de Root vérifie ses propres tâches, pages d'accueil, notifications, listes de tâches et son arborescence de travail, puis termine son
Rapport de fermeture. Obtenez une attestation uniquement via une route prise en charge par le démon actuel ;
la route de fermeture Swift (désormais indisponible) ne l'est pas. Si cette route ou une fermeture protégée est
indisponible, enregistrez le bloqueur de produit et le prochain propriétaire, et conservez la Session. Ce n'est que lorsque
l'identité et le travail sont vérifiés et
`closeability.state=safe` peut `clawdline session close --terminal <id>` y mettre fin ; il relit d'abord l'
inventaire, puis une seconde exécution répond `session_not_found` une fois qu'il a disparu. Ne contournez pas la protection avec `clawdline close <terminal id>`. Assurez le suivi de
`blocked` auprès de son déménageur nommé. Pour `unknown` (y compris `terminal_unreadable`), conservez
Session et enregistrez les preuves manquantes ainsi que le prochain propriétaire ; ne forcez pas la fermeture, n'archivez pas et ne déclarez pas
qu'il a été effacé. Avant de déclarer la coordination de Epic terminée, énumérez le résultat de fermeture ou
le bloqueur nommé de chaque Root. Board et `done` ne remplacent pas cet inventaire.

## 11. Coordination

**Le coordinateur de la machine (« Clawdfather »).** Il fonctionne depuis un espace de travail machine appartenant au démon, en dehors des Projets, pour rendre compte des Sessions et gérer les opérations machine prises en charge. Il ne modifie jamais le code source du projet, y compris Clawdline. À la demande explicite d'une personne pour des travaux d'ingénierie, créez d'abord un élément de projet Board avec `clawdline item add --project … --assign-new`, puis déléguez-le à un projet Session. Sans cette demande, proposez un élément à la personne pour qu'elle l'accepte. Le propriétaire du projet désigné gère la distribution, la vérification et l'intégration des éléments enfants. L'espace de travail est une limite organisationnelle, et non un environnement isolé du système de fichiers. Les nouvelles liaisons doivent provenir de cet espace de travail ; les liaisons existantes restent accessibles en lecture. Ouvrez Session à partir de l'action Clawdfather distincte de la console, puis enregistrez son ID de conversation. Voir `docs/clawdfather-role.md` pour la limite du produit. Exécutez `clawdline coordinator bind` dans ce nouveau Session pour l'enregistrer ou rattacher un prédécesseur hors ligne validé. La commande lit son propre identifiant de conversation et refuse de remplacer un détenteur en ligne ou illisible.
`GET /v1/orchestrator/coordinator` inspecte le rôle ;
`/coordinator/bearings` donne un aperçu de la machine (tâches actives, intégrations en attente, attentes ouvertes,
lettres non distribuées, baux détenus et ce qu'est `unknown`). `POST …/coordinator/register` avec
`{"session_id": "<conversation id>"}` prend le rôle ; `POST …/coordinator/rebind` le déplace une fois que
la session liée est hors ligne (`expected_coordinator_id`, `expected_generation`). Succession
Réponses `501 succession_unavailable`.

**Attentes de fichiers.** Une attente signifie « prévenez-moi quand le propriétaire aura terminé avec ces chemins ». Il s'agit d'un enregistrement et d'un
message, et non d'un verrou ou d'une surveillance de fichier.

- `POST /v1/orchestrator/waits` —
`{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}`
(les identifiants de session sont des identifiants de conversation). Le propriétaire est informé une seule fois, dans son compositeur.
- Le propriétaire termine l'attente : `POST /v1/orchestrator/waits/<id>/release` avec `{"owner_session_id",
  "commit"?, "note"?}` ; chaque processus en attente est informé. Rien ne libère une attente programmée.
- Un processus en attente quitte l'attente : `POST …/waits/<id>/cancel` avec `{"waiter_session_id"}`. `409 owner_busy` et `502 request_delivery_failed` indiquent que **l'attente a été enregistrée** mais que le propriétaire
n'a pas encore été informé. `502 release_incomplete` liste les utilisateurs toujours en attente : renvoyez la demande.

**Baux.** Deux ressources : `heavy_compile` (un emplacement de compilation de la machine) et `landing` (une par
extraction).

**Exécute une compilation ou une suite de tests via `clawdline heavy -- <command>`**, et non directement. Il met en file d'attente pour
`heavy_compile`, attend que la machine dispose de mémoire disponible (un quart, au maximum 1 Go, et
pas de blocage de mémoire au-delà de 10 %), exécute la commande à une priorité inférieure — sous Linux, c'est également la première chose que
le noyau arrête en cas de manque de mémoire — renouvelle le bail pendant son exécution et le libère ensuite. Il conserve le
code de sortie de la commande. Il ne refuse jamais de compiler en raison d'un démon manquant ou d'un refus qu'il ne
connaît pas : il exécute la commande malgré tout avec un message sur stderr. Lorsque `--max-wait` (30 min par défaut) passe
avant qu'il n'ait l'emplacement et la mémoire, il abandonne sa place, n'exécute pas la commande et se termine avec le code
**75** — un code avec lequel aucune erreur de commande n'est confondue ; Réexécutez-le plus tard. Pendant l'attente,
il affiche une ligne au début et une à la fin de l'attente, sans rien entre les deux : attendez une fois,
long (§2, « Attente d'une commande longue »). Un `heavy` à l'intérieur d'un `heavy` s'exécute directement. `--min-available 1500M` demande des informations supplémentaires ; `--no-slot` vérifie uniquement la mémoire. Dans un dépôt
qui le contient, `tools/heavy.sh <command>` trouve le binaire pour vous.

- `POST /v1/orchestrator/leases` —
`{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`.
Réponses `granted` ou `queued` avec `position` et `retry_after_seconds`. Une requête en file d'attente est posée
à nouveau avec le même `request_id`.
- `POST …/leases/renew | release | cancel` avec `{"request_id", "resource", "checkout"}`. Le titulaire
renouvelle avec `/renew` (demandant à nouveau `POST /v1/orchestrator/leases` avec son propre `request_id`
renouvelle également).
- Renouvelez dans les 60 secondes, sinon le bail est considéré comme résilié. `409 lease_lost` signifie qu'il l'était.
`429 queue_full` à 32 serveurs.

**Graphiques** (`GET /v1/orchestrator/graphs`) sont des vues en lecture seule calculées à partir des champs des tâches distribuées.
`graph` La fonction **Récupération** (`/v1/orchestrator/reclaim`) récupère les commandes terminées ; une requête POST est un test à blanc,
sauf si le corps de la requête contient la valeur `{"dry_run": false}`.

## 12. Que faire en cas de refus ?

- Suivre la branche `error.code` (ou `error` dans la représentation plate). Le message est destiné aux utilisateurs.
- `retry_after` indique une réponse de capacité insuffisante : patientez pendant ce délai, puis renvoyez la même requête. - `409 stale_write`, `503 orchestrator_store_busy` : le magasin était occupé ; la même requête est à nouveau
sûre.
- `unknown` (où que ce soit : propriété, état, source) signifie que le démon n'a pas pu le lire.
Il n'est pas « absent » et rien ne doit être supprimé ni déclaré inactif à son sujet.
- Si une route attendue renvoie `404 not_found` ou `501`, elle n'est pas disponible dans ce démon. Signalez-le ; n'utilisez pas
les routes de l'application Swift ni les sous-agents natifs du fournisseur à sa place.
