# Guía de Clawdline

Para una sesión de asistente —Claude Code o Codex— en una máquina donde corre **Clawdline Next**.
Cubre lo que este daemon sirve hoy y nada más: cada ruta de abajo la registra el mismo build que
imprimió esta guía, y una prueba falla si alguna no está registrada. Vuelve a imprimirla con
`clawdline guide` en lugar de fiarte de una copia; `clawdline guide zh-Hant` imprime la guía en
chino tradicional de Taiwán (`zh-TW` sigue siendo un alias). `clawdline guide` imprime el núcleo y
nombra las demás partes; imprime una parte
(`clawdline guide dispatch`) cuando llegues al trabajo que cubre, o `clawdline guide all` para
el texto completo. Todo lo que imprime, núcleo incluido, empieza por `guide-version: <sha256>`;
ejecuta el mismo comando con `--since <hash>` y, si ese texto no ha cambiado, imprime en su lugar
la única línea `unchanged <hash>`. `clawdline guide refused <code>` imprime la parte que explica
un código de rechazo, y sale con 1 sin nada en stdout cuando ninguna parte lo nombra.

En esta guía, un **paso** (step) es una entrada de la lista de verificación de un ítem, las
**escrituras** (writes) de una tarea son las rutas que puede cambiar, la **asignación**
(assignment) es quién es dueño de un ítem, y una **parte** es un fragmento con nombre de esta guía.

## 0. Si aprendiste Clawdline con la app Swift, lee esto primero

La app Swift se retiró el 2026-09-19: está detenida, ya no arranca al iniciar sesión y nada
responde en el puerto 7717. Su directorio, `~/.config/clawdline`, sigue en el disco y todavía se
lee —solo se lee— para el historial que este daemon nunca tuvo. Este daemon no es una copia de esa
app, y hay cinco diferencias donde la gente tropieza:

1. **Los directorios de tareas son `<state dir>/tasks`, no `/tmp/.clawdline`.** `/tmp/.clawdline`
   era del broker de Swift; dos brokers escribiendo en un mismo directorio de ids de tarea habrían
   chocado donde nadie mira. No fijes ninguno de los dos en el código: lee `task_root` del
   inventario (§3) y escribe `task.json` debajo de él.
2. **No hay envoltorio de workflow ni ruta de workflow a la que llamar.** Los mensajes ya no llevan
   una clasificación del tablero, y una sesión nunca abre tarjetas del tablero por su cuenta.
   `POST /v1/orchestrator/sessions/<terminal>/workflow` todavía existe solo para que un helper
   antiguo no falle a mitad de turno: responde `workflow_retired`, no registra nada, y el código
   nuevo no debe llamarla.
3. **La participación en el Board pasa por propuestas y decisiones** (§10): una sesión propone y
   una persona responde. No hay nada que hacer `begin` ni `deliver`.
4. **Otra puerta.** Puerto 7727 (o `CLAWDLINE_NEXT_PORT`), estado en `~/.config/clawdline-next`
   (o `CLAWDLINE_NEXT_DIR`). Nunca leas `~/.config/clawdline`: su token no es de este daemon y se
   rechaza con `401 unauthorized`.
5. **Lo que tenía la app Swift y este daemon no tiene:** la promoción de informes duraderos
   (responde `501 durable_report_promotion_unsupported`), la sucesión del coordinador (responde
   `501 succession_unavailable`), y los campos del brief `serialize`
   y `attach_session` (cada uno se rechaza por su nombre como `bad_task`). `reasoning_effort` sí
   se admite: `high` o `xhigh`, solo en una tarea `codex`.

## 1. Root o child

Si tu primer mensaje decía *"You are a Clawdline CHILD agent for task …"*, eres un **child**. El
`CHILD.md` que nombra es el que te rige: no despachas tareas, no envías un recibo de turno, firmas
con `clawdline task accept` y terminas con `clawdline task finish`. Deja de leer aquí.

Si no, eres un **root**: una sesión normal con la que habla una persona. El resto es para ti.

Si decía *"You are an independently owned Clawdline Feature Root …"*, o se te asignó un ítem del
Board, imprime a continuación `clawdline guide feature-root`: es todo el camino habitual, desde leer
el ítem hasta `done`, y nombra la parte que debes imprimir para cualquier caso menos frecuente.

## 2. Llegar al daemon

**Usa los comandos, no curl armado a mano, cuando exista uno.** Leen la credencial dentro de su
propio proceso, así que nunca aparece en una línea de comandos, en `ps`, en su salida ni en tu
transcripción. Un curl armado a mano sin esa credencial recibe `401 unauthorized` ("No valid
credential came with this request …"): lo que falta es la credencial, no un permiso. Ejecuta el
comando en su lugar.

| Comando | Qué hace |
|---|---|
| `clawdline guide [lang]` | Esta guía. No necesita el daemon |
| `clawdline session report --summary "…"` | Registra tu turno terminado (§7) |
| `clawdline session close [--dry-run] [--terminal id]` | Audita y cierra una Session terminada, nunca a la fuerza (§2a) |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | Despacha un child propio (§4) |
| `clawdline item show\|steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | Lee y hace avanzar un ítem del Board que es tuyo (`clawdline guide feature-root`, §10) |
| `clawdline todo add\|list\|done` | Las tareas pendientes propias de esta Session, solo cuando la persona lo pide (§10) |
| `clawdline heavy -- <command…>` | Ejecuta un build o una batería de pruebas en el único slot de compilación de la máquina (§11) |
| `clawdline send --to <terminal> "…"` | Retransmite un mensaje a otra sesión (§8) |
| `clawdline notify --title "…" --body "…"` | Envía una notificación push a la persona (§9) |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | Deja una nota accionable encima de una Session (§9a) |
| `clawdline assistants` | Lo que le queda a la cuenta de cada asistente |
| `clawdline landings` | Cada landing aún pendiente en esta máquina; `--work-id <item id>`: cada landing registrado para un ítem del Board |
| `clawdline leases [--json]` | Quién tiene el slot de compilación y cada lease de landing, y quién espera detrás |
| `clawdline sessions [--json]` | Las Sessions que un send, una espera o un traspaso pueden nombrar, con su estado y su tarea |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | Lo que gastó una sesión, una tarea child o un ítem del Board, por categoría; por defecto, lo tuyo |
| `clawdline cloud pair [--offer <code>]` | Empareja un navegador de Cloud con esta máquina |
| `clawdline task show [--json] <task id>` | Una tarea child en forma compacta: estado, veredicto, resumen, títulos de pendientes (leftovers), verificación, landing, checkout (§5) |
| `clawdline task wait <task id>… [--timeout 9m] [--any]` | Espera a que terminen los children (todos, o uno con `--any`), muestra cada uno como lo hace `task show` y cierra su aviso. Sale con 0 si todos tuvieron éxito, 1 si uno falló, 5 si uno se canceló y ninguno falló, 3 si se agotó el tiempo, 4 si no se pudo leer una tarea; 4 prevalece sobre 3, 3 sobre 1 y 1 sobre 5 (§5) |
| `clawdline callback --title "…" [--timeout 30m] [--work-id <item>] -- <command…>` | Ejecuta un comando largo —un despliegue y su comprobación, una espera de CI— bajo el daemon y vuelve enseguida; termina tu turno, y su salida tipa el mismo `<clawdline-notice>` que el de un child terminado (§5a, `clawdline guide callback`) |
| `clawdline task cancel <task id> --reason "…"` | Detiene un child que despachaste por error: se cierra su pestaña, se liberan sus escrituras y su slot, y una rama con commits se conserva para ti (§5) |
| `clawdline task ack <task id> <notice id>` | Cierra a mano un aviso de finalización; rara vez hace falta, porque `task show` y `task wait` lo cierran (§5) |
| `clawdline task accept <task dir>` | Un child firmando el recibo de su briefing. Los roots nunca lo ejecutan |
| `clawdline task finish <task dir>` | La finalización de un child. Los roots nunca lo ejecutan |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | Arranca una programación a través de su webhook de Cloud, en cualquier máquina, y espera su resultado; el código de salida dice cómo terminó ("Programar trabajo futuro"). No necesita el daemon |

Sin una etiqueta explícita de la guía, el idioma de la CLI sigue `--lang <tag>` antes del comando,
luego `CLAWDLINE_LANG`, el `product_language` guardado y, por último, el inglés. Un
`clawdline guide <tag>` explícito anula esa elección; una etiqueta no admitida muestra el inglés.
`clawdline guide -list` nombra las nueve etiquetas incluidas. Esta preferencia cambia el texto de
la CLI legible por personas, no los campos del protocolo ni el idioma de un Agent.

Los comandos de orquestación de arriba (no `webhook fire`) imprimen el JSON del daemon cuando tienen
éxito; ante un rechazo imprimen
`refused, <status> <code>: <message>`, luego cada valor escalar que trae el rechazo como
`key: value`, uno por línea, y su remedio al final, y salen con 1. Los comandos de Cloud usan su
propia salida de éxito y de error legible por personas. `--port` anula el puerto.

`clawdline usage` es el libro de tokens (`docs/token-ledger.md` en el repositorio): en qué se gastó
cada token — `board`, `protocol`, `rules`, `impl`, `delegate`, `harness`, `talk`, `compaction`,
`other`. Sin ningún flag lee tu propia sesión, nombrada por `CLAUDE_CODE_SESSION_ID` o
`CODEX_THREAD_ID`. Imprime una línea de encabezado (llamadas, contexto máximo, coste), una línea
por categoría ordenada por coste —proporción, tokens, coste— y después cada hueco; `--json`
imprime la respuesta del daemon. `rules` es una cota superior, y lo dice: una comprobación de guarda
(guard) ejecutada en un mismo comando de shell junto con otro trabajo se queda con el coste de
todo ese comando.
Las rutas son `GET /v1/usage/sessions/<conversation>`, `GET /v1/usage/tasks/<task id>` y
`GET /v1/usage/items/<item id>`, que se leen con un dispositivo emparejado o con el token del
orquestador. Una sesión que el libro aún no ha leído, o que ya no puede leer, responde
`not_yet_read`, `transcript_missing` o `transcript_unreadable` —nunca un total vacío—; un id que
nadie conoce es 404 `unknown_session`, `unknown_task` o `unknown_item`. Si el libro sigue leyendo
lo indica `usage` en `/v1/diagnostics`.

**Esperar un comando largo.** `clawdline heavy`, `clawdline dispatch` y una ejecución de pruebas
larga no imprimen nada mientras esperan, y terminan por sí solos. Espera a uno con **una sola
espera larga**, no comprobándolo cada pocos segundos: cada comprobación es un turno que vuelve a
leer todo tu contexto, y una revisión de tokens contó 520 de esos turnos (72,2M tokens) en diez
ítems, sobre todo en ejecuciones de `heavy` en cola.

- **Claude Code:** una sola llamada a Bash con un `timeout` largo (hasta `600000` ms), o
  `run_in_background` y luego nada hasta que llegue su notificación de finalización. No un bucle de
  `sleep` y `tail`.
- **Codex (codex-cli 0.157.1, modo código):** pon `// @exec: {"yield_time_ms": 600000}` en la
  primera línea de la celda `functions.exec`. Después de que `exec_command` devuelva un ID de
  sesión, espera `write_stdin` con `chars` vacío y `yield_time_ms: 300000`; si sigue en marcha,
  repítelo dentro de esa misma celda. Medido: la celda externa siguió abierta 330 segundos con
  `600000`, mientras que un `write_stdin` vacío esperó hasta 300 segundos. Si la celda externa
  cede, usa `wait` con un `yield_time_ms` largo para recogerla.

  Usa una sola celda para esperar a un child o un build. Sustituye solo el comando; mantén el
  sondeo de la sesión dentro de la celda para que un retorno normal de 30 segundos de
  `exec_command` no despierte al agente para lanzar otra vez la misma espera:

  ```js
  // @exec: {"yield_time_ms": 600000}
  let r = await tools.exec_command({cmd: "clawdline task wait --timeout 9m TASK_ID", yield_time_ms: 30000});
  while (r.session_id) {
    r = await tools.write_stdin({session_id: r.session_id, chars: "", yield_time_ms: 300000});
  }
  text(r.output);
  text(`exit ${r.exit_code}`);
  ```

  Para un build, sustituye el comando por `tools/heavy.sh …` y conserva su
  código de salida: 75 significa que la espera del slot de compilación o de memoria expiró antes de
  que corriera el build.

`clawdline heavy` espera como máximo `--max-wait` (30m por defecto) y luego sale con 75 sin
ejecutar el comando; una espera más larga que la que permite tu herramienta se hace en segundo
plano.

**Curl a una ruta del orquestador.** Lee `<state dir>/orchestrator-token` y envíalo en la cabecera
`X-Clawdline-Orchestrator`. Mantén el token fuera de los argumentos del comando: usa
`DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"` y luego
`-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")`.
Usa `curl --fail-with-body`; además, todo POST con cuerpo JSON necesita
`-H 'Content-Type: application/json'` (si no, `415 unsupported_media_type`).

### Emparejar un navegador de Cloud

Emparejar cambia quién puede leer esta máquina. Un navegador emparejado puede leerla de inmediato
y, cuando los `commands` de Cloud están activados, puede manejarla. Ejecuta un comando de
emparejamiento solo cuando la persona pida explícitamente emparejar ese navegador o te dé el
comando u oferta de emparejamiento exactos. Emparejar no activa los comandos; eso sigue siendo un
ajuste aparte.

Hay dos direcciones admitidas:

1. **El navegador muestra una oferta.** Ejecuta en la máquina la línea exacta que te da:

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

   Conserva las comillas simples. La oferta es un secreto opaco, de corta duración y de un solo
   uso: no la decodifiques, edites ni guardes, ni la repitas en la respuesta final. Si caducó o ya
   se usó, pide una oferta nueva al navegador en lugar de reintentarla o modificarla.
2. **La máquina hace la invitación.** Ejecuta `clawdline cloud pair`. Imprime un enlace de un solo
   uso `https://app.clawdline.com/#pair=…` y espera. La persona abre ese enlace completo en el
   navegador que quiere emparejar, con la sesión iniciada en la misma cuenta de Clawdline Cloud.
   Trata el enlace como la oferta: no lo publiques ni lo conserves.

Si tiene éxito, imprime tres líneas: `paired` nombra el id de dispositivo del navegador, `browser`
es la huella del navegador y `machine` es la huella de la máquina. Compara la huella del navegador
con la que se muestra en el navegador, y la huella de la máquina con la que se muestra para esta
máquina. Una discrepancia no es un éxito: ejecuta de inmediato `clawdline cloud revoke <device-id>`
con el id de `paired` y luego informa de la discrepancia. `clawdline cloud devices` lista los navegadores
registrados actualmente y su estado de confianza local; también es la comprobación de solo lectura
que debes usar después de emparejar.

Estos comandos pasan por el daemon local en ejecución. Si uno falla, informa de su stderr exacto. No
actives Cloud, no inicies sesión, no habilites los comandos, no rotes claves ni sustituyas la
oferta recibida a menos que la persona haya pedido ese cambio por separado.

**Dónde está cada cosa.**

- Puerto: `CLAWDLINE_NEXT_PORT`; si no, **7727**. Solo loopback: `http://127.0.0.1:<port>`.
- Directorio de estado: `CLAWDLINE_NEXT_DIR`; si no, `$XDG_CONFIG_HOME/clawdline-next`; si no,
  `~/.config/clawdline-next` (`%APPDATA%\clawdline-next` en Windows).
- `GET /v1/health` no necesita credencial y responde `served_by: "clawdline-go"`. Úsalo para
  distinguir "no está en ejecución" de "rechazado".

**Credenciales.** Existen tres, y una sesión usa la primera:

| Credencial | Dónde | Se envía como | Abre |
|---|---|---|---|
| Token del orquestador | `<state dir>/orchestrator-token` | cabecera `X-Clawdline-Orchestrator` | Todo lo que hay bajo `/v1/orchestrator/`, `/v1/work/`, `/v1/board`, `GET /v1/places`, `POST /v1/artifacts/images` |
| Secreto de tarea | lo elige el root al despachar | cabecera `X-Clawdline-Task-Secret` | Las rutas propias de un child bajo `/v1/orchestrator/tasks/<id>/`, y `POST /v1/orchestrator/proposals` |
| Token de dispositivo | `<state dir>/local-token`, o el de un dispositivo emparejado | `Authorization: Bearer` | Las rutas de la consola (`/v1/sessions/…`). Una sesión no lo necesita |

El token del orquestador enviado como `Bearer` se compara con los dispositivos y se rechaza. Uno
incorrecto o ausente recibe `401 unauthorized`, que lo dice: falta el token o no es de este daemon,
o el dispositivo no está emparejado. `clawdline doctor` imprime el directorio y el puerto que lee la
CLI.

**Cuando tengas que usar curl**, mantén el token fuera de la línea de comandos:

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

- `--fail-with-body`: sin él, un rechazo sale con 0 y parece un éxito.
- **Todo POST con cuerpo necesita `-H 'Content-Type: application/json'`**, o se rechaza con
  `415 unsupported_media_type`. `curl -d` por sí solo envía un tipo de formulario.
- Los cuerpos tienen un límite de 2 MiB, salvo que una ruta indique menos.
- Un id de terminal de tmux como `%47` va en una ruta escapado como un solo segmento: `%2547`.

**Los rechazos tienen dos formas.** Ramifica según el código, nunca según la frase:

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}` — la compuerta y el broker.
  Los extras como `retry_after` van dentro de `error`.
- `{"error":"<code>","detail":"…"}` — rutas inexistentes, métodos equivocados y algunas lecturas.

Una ruta que no es de este daemon se rechaza como `501 not_implemented`, y el rechazo nombra la
ruta. Esa es la respuesta en cualquier máquina normal. Solo se reenvía cuando alguien puso a
propósito otro daemon detrás de este con `CLAWDLINE_NEXT_UPSTREAM_PORT`, y entonces un `502
upstream_unreachable` nombra la dirección que no respondió. Ninguna de las dos es una respuesta de
este daemon. Antes del 2026-09-19 el reenvío venía activado por defecto e iba a la app Swift en el
7717, así que una nota escrita entonces dirá que una ruta sin dueño llega a esa app; no es así.

### Configurar la presentación de un Project en Clawdline

Usa esta parte cuando la persona te pida que el Project en el que trabajas se vea con claridad en
Clawdline. El resultado no es "que existan algunos archivos"; es que el Project tenga un nombre y
una marca fieles, que el trabajo de larga duración pueda informar de su progreso y que sus
servidores de desarrollo se puedan ver sin que Clawdline los arranque.

Empieza leyendo las instrucciones de este repositorio, el README, los scripts de despliegue y de
build, y la configuración existente del gestor de procesos. Conserva los comandos que el Project ya
usa. No añadas una segunda vía de despliegue ni un supervisor de procesos solo por Clawdline, y no
arranques, detengas, reinicies ni despliegues nada a menos que la persona haya pedido ese cambio
operativo. La configuración y un despliegue real son trabajos distintos.

Recorre estas cuatro comprobaciones, y sáltate una solo cuando de verdad no aplique:

1. **Project.** Ejecuta `clawdline project list`. Si este checkout no aparece, añade la raíz de su
   repositorio con `clawdline project add <absolute-root>` y vuelve a listar. Esto registra un lugar
   desde el que puede arrancar una Session; no cambia el repositorio.
2. **Nombre e icono.** Clawdline deriva un icono estable cuando no hay ninguno configurado. Si la
   persona quiere un nombre o una marca de píxeles elegidos a propósito, conserva todas las demás
   entradas de `~/.claude/project-icons.json` y edita solo la ruta contenedora más larga de este
   Project. El formato está documentado en `docs/project-status.md` del repositorio de Clawdline;
   la página Projects también puede copiar un icono ya resuelto sin editar JSON a mano. Un archivo
   global del usuario no es contenido del repositorio: muestra la entrada exacta que propones antes
   de cambiar ese archivo si la petición no autorizaba ya esa edición.
3. **Despliegue y trabajo largo.** Clawdline solo lee recibos de estado; nunca ejecuta un
   despliegue. Para un repositorio de GitHub, un recibo de despliegue es
   `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`, donde owner y repo salen de `origin`.
   El productor que ya conoce la ejecución escribe `state` (`running`, `ok`, `fail` o `none`),
   `label`, `url`, `started_at` y un `typical_seconds` medido, de forma atómica. Para un comando
   local de build, prueba, importación o despliegue, usa
   `clawdline-progress run --label <label> -- <command>` cuando ese helper exista, o implementa el
   contrato `run-<path>.json` de `docs/project-status.md`. Nunca inventes una duración; omítela
   hasta que se haya medido. Un productor terminado a la fuerza no debe dejar un estado de
   ejecución permanente.
4. **Servidores de desarrollo.** Añade o actualiza `.devstack.json` en la raíz desplegable más
   cercana. El daemon de Go lee hoy los `processes` declarados y sondea su `port` de loopback o abre
   su `url`; **no** ejecuta comandos `status`, `up`, `down`, `restart` ni `logs` desde el navegador.
   Prefiere el archivo Tier 0 más pequeño que sea fiel a la realidad, por ejemplo:

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

   Un proceso sin un puerto o una URL estables no debe meterse en el archivo a base de suposiciones.
   No sondees ni reinicies producción mientras verificas una declaración de desarrollo.

Verifica cada capa cambiada por separado: `clawdline project list` nombra el checkout; cada archivo
JSON se puede analizar; pasan las pruebas propias del repositorio para los scripts cambiados;
`GET /v1/devstacks` muestra los servidores declarados como en ejecución, detenidos o desconocidos en
lugar de omitirlos en silencio; y una Session del Project muestra un recibo de progreso/despliegue
reciente. Si una lectura no está disponible, está mal formada o está desactualizada, di cuál y
déjala como desconocida: nunca informes de una ausencia como si fuera un éxito. Termina listando
qué se configuró, qué se consideró deliberadamente no aplicable y cualquier archivo propiedad del
usuario que se haya cambiado fuera de git.

**Unificar: un solo conjunto de reglas y skills para Claude y Codex** (`/clawdline unify`). Codex
lee `AGENTS.md` y `.agents/skills/<name>/`; Claude lee `CLAUDE.md` (y `AGENTS.md` solo cuando
`CLAUDE.md` no existe o tiene una línea `@AGENTS.md`) y `.claude/skills/<name>/`. Un Project está
unificado cuando sus reglas están en `AGENTS.md`, con `CLAUDE.md` ausente o importándolo, y cada
skill vive en `.agents/skills/<name>/` con `.claude/skills/<name>` como enlace relativo a ella.
Cuando la persona lo pida, o invoque `/clawdline unify`:

1. Ejecuta `clawdline project unify` en el Project de la Session (el nivel superior de git; añade
   antes el Project con `clawdline project add` si no aparece en la lista). No cambia nada. Muestra
   a la persona, en su idioma, lo que Claude y Codex leen ahora y lo que leerán después, cada fila de
   skill, cada frase de acción y cada conflicto, incluidas las líneas de `CLAUDE.md` que Codex no
   ve.
2. Ejecuta `clawdline project unify --apply` solo después de que un mensaje de la propia persona en
   esta conversación apruebe ese plan. Envía la versión que mostraste; si el disco cambió desde
   entonces, responde `plan_changed` y no aplica nada: imprime el plan otra vez y vuelve a
   preguntar.
3. Muestra el resultado de `clawdline project unify --check` (sale con 0 si está unificado, 1 si se
   está desviando, 3 si es desconocido). No se hace commit de nada; di qué archivos cambiaron para
   que la persona o una Session hagan el commit.

Nunca resuelvas un conflicto por tu cuenta editando `AGENTS.md`, `CLAUDE.md` o una skill sin un
mensaje de la persona que lo diga: una skill que difiere entre los dos directorios, un enlace que
apunta a otro sitio o unas reglas que Codex no ve son decisiones suyas. Las rutas son
`GET /v1/projects/{place}/unify` (el plan) y `POST /v1/projects/{place}/unify` con
`{"version"}` e `Idempotency-Key`; los rechazos son `plan_changed`, `plan_unknown` (no se pudo
leer una parte del Project, así que no cambió nada) y `name_taken` (ya existe un nombre que unify
crearía; no se sobrescribe nada).

## 2a. El camino habitual de un Feature Root

Esto es todo lo que ejecuta, en orden, un Feature Root normal: una Session que es dueña de un ítem
del Board. Cada paso es un comando: los comandos llevan la credencial, y un curl armado a mano a la
misma ruta se rechaza. Cualquier cosa menos frecuente está a un `clawdline guide <part>` de
distancia; las referencias están al final.

**1. Lee el ítem.** `clawdline item show <item id>` imprime su tipo, fase, criterios de aceptación,
compuertas capturadas, la versión de aceptación (`acceptance vN`), en una Feature el interruptor
Needs independent review de la persona, sus pasos y cada documento con su cuerpo. Ese es
el registro con el que trabajas. `clawdline item show <item id> --doc <doc id>` imprime solo el
cuerpo de un documento, para volcarlo a un archivo; `clawdline item steps <item id>` es el mismo
registro sin los cuerpos. Cada escritura sobre el ítem
imprime `wrote …; item <id> is at version N`, un resumen breve del ítem y una sugerencia de
`item show`. Lee la aceptación, los pasos y los documentos completos con `item show`. Las
escrituras actúan sobre la versión actual del ítem salvo que pases
`--expected-version`.

Si tu ASSIGNMENT.md tiene un encabezado **HANDOFF**, estás tomando el relevo de un ítem que otra
Session dejó a medias (reasignado después de empezar la implementación y antes de done). Lee primero
el paquete que nombra, antes de planificar. El daemon lo construyó a partir de sus propios
registros y de git, sin preguntar al dueño anterior: las tareas vinculadas al ítem y sus
resultados, los commits aún sin landing, los cambios sin commit de cada worktree guardados como un
patch con su sha256 y el comando `git apply` que lo restaura sobre su base, el último mensaje del
dueño anterior (o por qué no se pudo leer), y la fase y los pasos abiertos. Los patches viven junto
a ASSIGNMENT.md y sobreviven a los worktrees. Continúa desde ahí; no empieces de cero.

**2. Ponle nombre a tu Session**, si se abrió para este ítem:
`clawdline item name <item id> "<task name>"`, una sola vez, después de leer el objetivo y el
alcance. Renombra la Session, no el ítem.

**3. Antes de implementar.**

- Planificación capturada activada y sin criterios de aceptación: escribe criterios observables con
  `clawdline item acceptance <item id> --body-file acceptance.md`.
- Needs independent review marcado, o una Epic: primero va el camino del plan revisado de
  `clawdline guide epic`. Sin marcar: sin plan y sin child de revisión.
- Trabajo de varias etapas sin pasos: `clawdline item step-add <item id> "first" "second" …` (de dos
  a ocho pasos que puedas verificar de uno en uno; un cambio único no lleva ninguno).
- Después, `clawdline item phase <item id> implementing`.

**4. Por defecto, trabaja en esta Session.** Investiga, implementa, verifica y haz el landing de la
Feature tú mismo. Despacha solo cuando una necesidad concreta haga útil una Session aparte: trabajo
en paralelo realmente independiente, herramientas o permisos distintos, o una revisión
independiente obligatoria. Di por qué antes de despachar; una investigación o implementación
rutinaria por sí sola no es motivo.

Cuando haga falta despachar, mantén aquí la síntesis, la integración y el landing:

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id` vincula el child al ítem, de modo que su landing cuenta como el del ítem. Repítelo
  cuando un child haga varios ítems: el primero es la línea del child, y el landing cuenta para
  cada uno.
- El título es una línea de 60 caracteres como máximo que dice qué será distinto. Se rechaza
  cualquier dos puntos (`:` o `：`), porque une una observación con una explicación; también "the
  user" como sujeto, o un título que empiece por un identificador con formato de código. Cada caso
  responde `bad_task` con `title: …`, que dice cuál.
- El brief se sostiene por sí solo. Pon en él los hechos que ya verificaste, cada uno con su
  `file:line` o el comando que lo mostró, para que el child no tenga que redescubrirlos.
- El brief de un child de investigación o Explore también indica su condición de parada —la
  pregunta que, una vez respondida, termina la tarea— y un límite de turnos.
- El trabajo de solo lectura es `--claims ""`. Cada flag, y cada código de rechazo, está en
  `clawdline guide dispatch`.

**5. Si un child termina**, se escribe una línea `<clawdline-notice>` en tu compositor. Ejecuta
`clawdline task show <task id>` y luego integra la entrega; leerla cierra el aviso, así que no hay
un ACK aparte. **Después de despachar, termina tu turno**: el aviso te despierta, y un turno que se
mantiene abierto para esperar vuelve a leer todo tu contexto en cada sondeo. Solo cuando no queda
nada más por hacer y tienes que bloquearte, ejecuta `clawdline task wait <task id>…` (`--timeout 9m`
por defecto, `--any` para el primero). Integra un child con worktree **haciendo merge de su rama**
en el destino. **El merge registra el
landing por sí solo** en pocos minutos: no publiques un landing a mano. `clawdline landings` lista
lo que todavía se debe. Un child despachado con `--claims ""` que no escribió nada queda registrado
por el broker como `nothing_to_land`. Cualquier otro caso es `clawdline task land <task id> <state>`
(`clawdline guide landing`).

**6. Informe de finalización**, cuando encontrar la causa exigió una investigación considerable (un
arreglo directo y observado no lo necesita). Añádelo antes de `done`: una vez que el ítem está done
queda sin asignar, y el informe responde `409 not_item_owner`.

```sh
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Escríbelo para la persona que informó del problema, en Markdown y sin datos privados.

**7. Termina el ítem.** Completa cada paso una vez verificado con
`clawdline item step-done <item id> <step id>`. Haz commit y push del trabajo directo desde un
worktree desechable; haz merge de la rama de un child cuando se usó uno. Una vez hecho el landing,
un solo comando lleva el ítem a `done`. El landing registrado de un child aporta el commit, el
destino y el remoto; el trabajo directo los nombra:

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --commit <sha> --target main --remote origin \
  --deployment "what went live, where, which version"      # omit landing flags for a landed child
```

O avanza una fase cada vez:

```sh
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> deploying --no-landing-reason "why there is no code"    # work with no code
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

`done` acepta `--deployment` o `--no-deployment-reason` según lo que diga la política de despliegue
del ítem. Cuando `clawdline item steps <item id>` imprime una línea de compuerta, este ítem sigue el
camino más largo al que apunta esa línea.

**Esperar un despliegue.** No mantengas el turno abierto mientras un despliegue se ejecuta o se
propaga. Arranca el despliegue y su comprobación como un solo callback, termina el turno y termina
el ítem cuando llegue su aviso:

```sh
clawdline callback --title "The hosted console serves <sha>" --work-id <item id> --timeout 20m -- \
  sh -c './deploy.sh <sha> && tools/wait-hosted-console.sh <sha>'
# … the notice: clawdline task show <callback id>, then
clawdline item finish <item id> … --deployment "what went live, where, which version (callback <callback id>)"
```

**8. Informa del turno**: `clawdline session report --summary "…"` (§7).

**9. Deja abierta la Session dueña.** Completar o cancelar un ítem del Board libera su asignación;
no termina la Session que era su dueña. Después de `session report`, deja la Session disponible
para trabajo posterior. No ejecutes `clawdline session close` solo porque el ítem llegó a `done` o
`cancelled`. Una persona puede pedir explícitamente cerrar la Session más adelante. Por separado,
el broker cierra la pestaña de un child despachado por un agente después de que termine la tarea de
ese child, según la regla de pestañas de child de `clawdline guide child`.

**Cuando algo se rechaza.** `version_conflict`: ejecuta el mismo comando otra vez; vuelve a leer la
versión. `steps_incomplete`: un paso sigue abierto. Cualquier otro código: §12, y después la parte
que lo cubre.

**Trabajo menos frecuente, una parte cada uno:** `clawdline guide board`: propuestas, decisiones,
tareas pendientes, reabrir un ítem done, esperar a la persona, compuertas y cada rechazo de fase;
`clawdline guide epic`: planes, revisión de planes, los ítems hijos de una Epic, perfiles de persona (personas);
`clawdline guide landing`: landing a mano, traspasos (también el traspaso de hito de un Root
largo), asignaciones de Root; `clawdline guide running`: children atascados, pendientes
(leftovers), respawn.

## 3. Antes de despachar: lee lo que ya existe

Puede que el trabajo de otra sesión ya esté haciendo lo tuyo, y desde el árbol compartido no se ve:
una entrega terminada en una rama sin merge no aparece en ningún `git status`. Lee primero.

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- Responde `generation`, `task_root` y cuatro listas: `live`, `unlanded`, `droppable`,
  `unreadable`. Cada fila trae un `do` que el daemon aceptaría. Con `claims`, cada fila viva dice
  con qué se solapa (`overlaps`).
- **`generation` es obligatorio para despachar** (§4). Son 16 caracteres hexadecimales calculados
  sobre los campos sellados de las filas; cambia cuando una fila empieza, termina o cambia sus
  escrituras.
- **`task_root` es donde va tu `task.json`.** Es un campo propio de este daemon; el broker de Swift
  no tenía ninguno porque fijaba `/tmp/.clawdline` en el código.
- `400 bad_request` cuando `project` no es una ruta absoluta dentro de un repositorio Git.

También vale la pena leer una vez:

- `GET /v1/orchestrator/inflight?project=…`: cada línea de trabajo pendiente en el repositorio,
  quién la tiene y qué reclamó.
- `clawdline assistants`: por asistente, `availability` (`ok`, `low`, `exhausted`, `unknown`),
  `windows`, `stale`, `resets_at`. Elige a quién despachar después de leerlo; nada rechaza un
  despacho por cuota.

**¿Hay que despacharlo siquiera?** El trabajo que se divide en piezas independientes avanza más
rápido en paralelo. Una cadena en la que cada paso depende del anterior sale peor si se divide,
porque cada traspaso la rompe. El diagnóstico, el trabajo más pequeño que su propio briefing y
cualquier cosa que alguien esté esperando se quedan en tu propia sesión. Las reglas de la casa de
esta máquina están en `<state dir>/dispatch-policy.md` (y en el `dispatch-policy.local.md` de la
persona); cada child las recibe en su briefing.

## 4. Despachar un child propio

Un child propio es una tarea acotada que depende de ti. **Tú te quedas con la síntesis, la
integración y el landing.**

**Un solo comando hace los cuatro pasos de abajo**, con el brief por stdin o en un archivo:

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--persona <id>] [--work-id uuid …] [--task-id uuid] [--label "…"] [--project-dir D] < brief.md     # or --instructions-file brief.md
```

Genera el id y el secreto, lee el inventario para obtener `generation` y `task_root`, escribe
`task.json`, publica la tarea y, ante un `stale_inventory`, vuelve a leer el inventario y reenvía
una vez. Imprime `dispatched <id> <state> [worktree <path>]` y luego una línea por advertencia: las
del daemon y una por cada tarea viva cuyas escrituras se solapan con las tuyas. `--json` imprime en
su lugar la respuesta del daemon. Un rechazo es `refused, <status> <code>: <message>` en stderr,
luego sus extras y su remedio, uno por
línea, y sale con 1; la tabla del
final de esta parte dice qué significa cada código. El root es tu conversación, tomada de
`CLAUDE_CODE_SESSION_ID` o `CODEX_THREAD_ID`, o si no de `--conversation`; el asistente del child
es el tuyo salvo que `--assistant` diga otra cosa; el proyecto es el nivel superior de git de este
directorio salvo que `--project-dir` diga otra cosa. `--claims ""` declara un child que no escribe
nada. Mientras el daemon abre el worktree y la pestaña del child, el comando no imprime
nada; es una sola
petición que responde cuando el child ya existe, así que espérala una vez (§2, "Esperar un comando
largo"). El secreto nunca está en argv, en `task.json` ni en lo que imprime, y el token se lee como
lo lee cada comando ligero.

Para una revisión que quizá haya que reintentar, elige un UUID en minúsculas antes de la primera
llamada y pásalo como `--task-id` en cada intento. El comando guarda una copia privada de la
intención de despacho original junto a `task.json`, para que un reintento idéntico pueda reenviarse
incluso después de que el daemon reescriba el
brief. El broker devuelve el id de tarea original con `(replayed)`; un brief cambiado se rechaza
localmente. Si
el comando agota su tiempo o se pierde su salida, consulta `GET /v1/orchestrator/tasks/<id>` antes
de dar por hecho que falló. Una tarea que no existe se puede reintentar con el mismo id y el mismo
brief. Un rechazo explícito no creó ninguna tarea y también se puede reintentar después de
corregir su causa.

`--persona <id>` lanza el child como un perfil de persona integrado (`persona` en `task.json`); un
id que este build no tiene se rechaza localmente. Ningún tipo recibe uno por defecto, `plan_review`
incluido: nombra tú mismo `code-reviewer` cuando lo quieras. `GET /v1/personas` lista los ids que
trae este build; qué es una persona se explica en la parte Epic
de §10 (`clawdline guide epic`).

Los pasos que sigue, para quien llame sin el binario:

**1. Elige un id y un secreto.**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 characters, lowercase
SECRET=$(openssl rand -hex 32)          # 64 lowercase hex
```

El secreto va de ti al daemon en el cuerpo del POST, y del daemon al child en la única línea que
escribe allí. No está en `task.json` ni en la respuesta del despacho, y no vuelves a necesitarlo.
(Un respawn es la única respuesta que trae un secreto: el nuevo de su copia).

**2. Lee el inventario** (§3) para obtener `generation` y `task_root`.

**3. Escribe `<task_root>/<TASK_ID>/task.json`.** El daemon lee el brief de este archivo, no de la
petición. Al admitirlo lo valida, reescribe `task.json` a partir de lo que admitió y escribe el
`CHILD.md` del child a partir del mismo registro —título, instrucciones, escrituras, entregables,
tipo y tiempo límite incluidos—, de modo que la tarea que lee el child es la que se validó, y el
child no lee `task.json`.

| Campo | Regla |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | el mismo id |
| `assistant` | `claude` o `codex` |
| `project_dir` | ruta absoluta a un directorio existente |
| `title` | se muestra en pantalla: una línea de 60 caracteres como máximo que dice qué será distinto. Unos dos puntos (`:` o `：`), "the user" como sujeto o un identificador con formato de código al principio se rechazan como `bad_task` (`title: …`) |
| `instructions` | obligatorio, 16 KiB como máximo. Deben sostenerse por sí solas: el child no sabe nada más. Incluye los hechos que ya verificaste, cada uno con su `file:line` o su comando; un child de investigación recibe además una condición de parada y un límite de turnos |
| `claims` | **obligatorio**: 32 rutas relativas como máximo que el child puede escribir. `[]` significa que no escribe nada y genera una advertencia (`claims_missing`) |
| `isolation` | `none` (por defecto) o `worktree` para un checkout privado en su propia rama |
| `permission_mode` | `ask`, `edits` o `full` |
| `timeout_minutes` | 1–240, 30 por defecto |
| `kind`, `deliverables`, `model` | opcionales; `model` es `[a-z0-9._-]`, 64 caracteres como máximo |
| `work_id` | UUID opcional del ítem del tablero al que sirve |
| `persona` | id opcional de un perfil de persona integrado (`GET /v1/personas`); ninguno por defecto |
| `auto_compact_window` | opcional, solo Claude: el tamaño de contexto en tokens (50000–1000000) en el que el child compacta, o `null` para ninguno. Si falta, sigue el `claude_auto_compact_window` de la máquina, que está desactivado salvo que la persona lo haya fijado. Sirve para comparar ejecuciones, no para los briefs de cada día: una compactación puede perder detalle |
| `root` | **obligatorio**: `{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "project_dir": "<the same absolute repository path as project_dir>", "label": "…"}`. Un root con alcance de rol necesita `root.project_dir` para que el daemon verifique el alcance de su Project. |

**Antes de despacharlo, haz que la superficie de trabajo y el modo de lanzamiento cubran cada
herramienta que el child tenga que usar.**
El brief nombra las herramientas necesarias y el root demuestra que la superficie elegida las
ofrece. Un child de la CLI de Codex no obtiene el `@Browser` integrado de la app de escritorio de
ChatGPT solo por un flag de permisos. Para una revisión de UI, de accesibilidad o de diseño
adaptable, dirige el trabajo a una superficie que de verdad tenga Browser/Computer Use, o nombra un
arnés de navegador local equivalente para la aceptación, como Playwright/Chrome CDP, y demuestra
que está instalado. Cuando esa superficie pueda pedir acceso a una app, a un origen o a la GUI,
despacha con `--permission-mode ask`: en Codex, `full` significa un lanzamiento de shell no
interactivo (`--ask-for-approval never`), no todas las herramientas, y Auto-review no puede revisar
una petición que nunca se crea. Al empezar la tarea, el child usa de verdad cada herramienta
necesaria, no solo comprueba el nombre de un comando. Si falta alguna, informa de inmediato del
hueco exacto y el root restaura el acceso o vuelve a despachar. El child no da por terminada como no
verificada una comprobación de aceptación que depende de una herramienta por el hecho de que el root eligiera
un worker incompatible.

**`root.session_id` es el id de tu conversación, nunca un id de terminal.** Claude Code lo exporta
como `CLAUDE_CODE_SESSION_ID`; Codex, como `CODEX_THREAD_ID`. Es como el daemon agrupa el child
bajo tu sesión y te avisa cuando termina. Para comprobar que nombra esta pestaña:
`GET /v1/orchestrator/whoami?conversation_id=<id>` responde `terminal_id`.

**4. Despacha**, con el token del orquestador:

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

Envía el cuerpo por stdin (`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`)
para que el secreto quede fuera de argv.
La respuesta es `{ok, task, warnings?}`. Lee `warnings`: `claims_overlap`, `claims_missing`,
`claims_ignored_for_worktree`, `dirty_worktree_base` y `work_not_placed` (el ítem nombrado todavía
no se pudo colocar en el tablero; el barrido del tablero lo hace en un tick). Publicar otra vez el
mismo id responde la tarea guardada
con `replayed: true`, así que reintentar es seguro.

Una pestaña que no logra abrirse responde igualmente 200, con `task.state: "spawn_failed"`.
`POST /v1/orchestrator/tasks/<id>/respawn` (token del orquestador) abre una copia con un secreto
nuevo, como máximo dos veces por original.

¿Despachaste el child equivocado: el brief equivocado, el alcance equivocado o el mismo trabajo dos
veces? No esperes a que termine o agote su tiempo mientras ocupa un slot y retiene sus escrituras:
`clawdline task cancel <id>
--reason "…"` lo detiene ya (§5).

**Rechazos con los que te encontrarás**, en el orden en que se comprueban:

| Estado | Código | Qué hacer |
|---|---|---|
| 409 | `task_unreadable` | Hay una tarea guardada con este id pero no se puede leer; no reenvíes con el mismo id |
| 422 | `bad_task` | El mensaje nombra el campo. Incluye "No readable task.json under …": comprueba `task_root` |
| 422 | `claims_required` | Añade `claims` |
| 422 | `root_session_required`, `root_assistant_required` | Añade `root.session_id` y `root.assistant` |
| 403 | `session_scope_mismatch` | Comprueba que `root.project_dir` está presente y coincide con el Project y la instantánea de rol de la Session root. Corrige el brief o la CLI; no pidas a la persona que cambie la configuración del Project. |
| 422 | `detached_route_required` | Enviaste `root.poll_only`; eso es automatización desacoplada (§6) |
| **409** | **`stale_inventory`** | Tu `generation` falta o es antiguo. El inventario actual completo va dentro del error: léelo, vuelve a decidir y reenvía con su `generation` |
| 422 | `work_not_found`, `work_other_project`, `work_closed` | El `work_id` que nombraste no es ningún ítem, es de otro Project o está cerrado |
| 422 | `also_work_not_found` | Un id de `also_work_ids` no es ningún ítem del Board; después se comprueba como `work_id` |
| 503 | `store_unavailable` | No se pudo leer el tablero para comprobar el ítem nombrado; no se arrancó nada, envíalo otra vez |
| 409 | `graph_*` | Una regla de admisión del grafo de tareas (el campo `graph`) |
| 409 | `no_child_capability` | Esta plataforma no puede abrir un child; `missing` dice qué falta |
| 429 | `squad_launch_capacity` | Demasiados lanzamientos de personas siguen esperando su Session; vuelve a intentarlo más tarde |
| 429 | `rate_limited` | Demasiados despachos en diez minutos |
| 422 / 409 | `root_unresolved`, `conversation_ambiguous` | El id de tu conversación no coincide con ninguna sesión viva, o coincide con más de una. Corrígelo; no te pases al modo desacoplado |
| 403 | `session_actor_required` | Un root abierto con un rol debe despachar con la capacidad de squad de su propia Session, desde esa Session |
| 403 | `session_scope_mismatch` | También aquí: el Project de la Session root no coincide con su instantánea de rol |
| 503 | `squad_policy_unavailable` | No se pudo leer la configuración de asignación de roles; no se arrancó nada |
| 409 | `persona_disabled_for_auto_assignment` | Ese perfil de persona está desactivado para la asignación automática en el Project de destino |
| 429 | `over_capacity` | Tus slots de child (5 por defecto) o los de la máquina están llenos; `retry_after` |
| 409 | `workspace_busy` | Las escrituras de otro root se solapan; el error nombra la tarea que bloquea |
| 409 | `worktree_unavailable` | No se pudo crear el checkout privado |
| 429 | `terminal_busy` | Todos los carriles de escritura en terminal están ocupados; `retry_after: 5` |

## 5. Mientras se ejecuta, y cuando termina

El child firma el recibo de su briefing (`clawdline task accept`, que publica `/accepted` o deja
`accepted.json`), puede enviar una nota de progreso cuando cambia su plan (`/progress`), puede
enviar hasta cinco notificaciones push (`/notify`) y termina escribiendo `result.json` y ejecutando
`clawdline task finish`. Tú no llamas a esas rutas.

- `clawdline task show <id>`: una tarea, con su estado (`GET /v1/orchestrator/tasks/<id>`).
  `GET /v1/orchestrator/tasks` las lista (`?state=`, `?limit=` hasta 500).
- **Cuando termina, el daemon escribe una línea `<clawdline-notice>` en tu compositor.** Su `body`
  es una frase corta: la tarea, cómo terminó, los hechos que son solo de esta entrega (un atasco,
  escrituras liberadas, su rama, cuántos pendientes) y el único comando que hay que ejecutar,
  `clawdline task show <id>`, que cierra el aviso una vez que ha impreso la tarea. Su JSON
  (versión 3) trae `task`, `state` y `notice_id`, y `outstanding`, `leftovers` y
  `claims_released` solo cuando dicen algo; el resultado es lo que imprime `task show`. Una línea
  que no se pudo escribir se reintenta en una escalera de 5→300 segundos. Una vez que está en tu
  pantalla y no has leído la tarea, no se vuelve a escribir entera: se escribe una línea corta
  `task_reminder` que nombra el mismo comando, en una escalera de 2→30 minutos —ocho escrituras en
  total, y luego se rinde—. Nunca escribe mientras muestras un menú. Un menú no consume esas ocho:
  la línea espera, hasta 12 horas, y se escribe en cuanto el menú desaparece. La ruta que envía
  `task show`:

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  `clawdline task show <id>` y `clawdline task wait <id>…` la envían para una tarea terminada
  después de imprimirla; `clawdline task ack <id> <notice_id>` la envía a mano e imprime una línea.
  Un segundo ACK responde
  `changed: false`. Los avisos sin acuse se listan en
  `GET /v1/orchestrator/completions`; `POST /v1/orchestrator/completions/reconcile` los vuelve a
  armar. Un aviso que se rindió se escribe una vez más la próxima vez que tu sesión esté inactiva.
- **Puede que nunca veas la línea, y aun así te enteras.** Tu propio
  `GET /v1/work/v2/agent/session-todos/<conversation id>`, que lees en cada límite de turno, lista
  `unacknowledged_completions`: cada child tuyo que terminó y del que no has acusado recibo, con
  `task_id`, `title`, `state`, `kind`, `result_path`, `notice_id` y `ack_path`, tanto si su aviso
  sigue pendiente como si se rindió. `clawdline session report` los imprime después de su recibo.
  Para cada uno: `clawdline task show <id>` y luego intégralo; la lectura es el ACK y lo saca de
  ambas listas.
  `task show` imprime el resumen entero y cuenta lo que deja fuera; `--json` es la respuesta
  completa del daemon, símbolos y artefactos incluidos. Lee `result.json` directamente solo cuando
  eso no baste.
- **Una entrega que nombra pendientes (leftovers)** —cosas que el child dice que no hizo— no cambia
  nada por sí sola. `task show` lista sus títulos. Para plantearle uno a la persona,
  `POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}`;
  la persona responde seguirlo, más tarde (Backlog) o no, y nada llega a su tablero hasta que
  responda.
- **A un child que se para justo después de su briefing se le da un empujón y luego se informa.**
  Si un child cuyo briefing se escribió no ha firmado el recibo y su pantalla se ve inactiva —prompt
  dibujado, compositor vacío, sin menú, sin línea de trabajo— durante 5 minutos, el daemon le
  escribe una línea que nombra su
  `CHILD.md` (nunca el secreto). Si 5 minutos después sigue sin firmar e inactivo, la tarea termina
  como `spawn_failed` con un veredicto que dice que se atascó, y recibes un aviso de `"kind":
  "task_stalled"` en lugar de `task_finished`. Haz respawn
  (`POST /v1/orchestrator/tasks/<id>/respawn`) o despacha otra vez, y luego acusa recibo. A un child
  que está trabajando, mostrando un menú o que ya firmó nunca se le escribe nada.
- **Cancela un child que despachaste por error** —el brief equivocado, el alcance equivocado, un
  duplicado—:
  `clawdline task cancel <id> --reason "wrong brief"`
  (`POST /v1/orchestrator/tasks/<id>/cancel`, `{"reason":"…"}`). El motivo es obligatorio, 500
  bytes como máximo. La tarea termina como `cancelled` con el motivo como veredicto, se cierra su
  pestaña, se liberan sus escrituras y su slot de child, y recibes un aviso que dice que se canceló
  y por qué. **Los commits no se tiran:** un child que hizo commits conserva su rama y su checkout, y
  su landing queda pendiente con una nota que dice cuántos commits hay en ella; `task show` y
  `clawdline landings` lo muestran. Haz merge de lo que quieras de ella, o regístralo con
  `clawdline task land <id> abandoned`. Solo la Session root que despachó la tarea, o la persona
  desde la consola, puede cancelarla; a cualquier otro se le rechaza con `403 not_task_root`, y una
  Session abierta con un rol debe enviar su propia capacidad (`session_actor_required`; el comando
  lo hace por ti). Una tarea que ya terminó responde
  `409 task_already_terminal` con su `state`; ejecutar otra vez la misma cancelación responde el
  mismo éxito con `replayed: true`. `clawdline task wait` sale con 5 cuando una tarea que esperaba
  se canceló.
  En cualquier otro caso, una tarea termina al finalizar, fallar o agotar su tiempo.
- **Un child terminado no es código con landing.** Su trabajo está en el árbol compartido o en su
  rama hasta que lo integres.

## 5a. Esperar un comando largo: un callback

Un despliegue, una ejecución de CI, una release que se propaga, un build largo: cualquier cosa cuya
respuesta sea "más tarde". No la esperes en tu turno ni la sondees desde turnos posteriores.
Pásasela al daemon:

```sh
clawdline callback --title "CI is green on <sha>" --timeout 45m -- gh run watch <run id> --exit-status
```

Imprime `callback <id> briefed` y vuelve. **Termina tu turno.** Cuando el comando sale, el daemon
escribe un `<clawdline-notice>` cuyo cuerpo dice
`callback <first 8 of id> finished: success (exit 0 after 6m) — run clawdline task show <id>`;
`task show` imprime cómo terminó —su estado de salida y las últimas líneas de su salida— y cierra
el aviso, exactamente igual que con un child.

- Es una tarea tuya sin pestaña: `clawdline task cancel <id> --reason "…"` detiene todo el grupo de
  procesos del comando; pasado el `--timeout` (de 1m a 4h, 30m por defecto) se detiene y queda
  resuelto como `timeout`. Un callback en ejecución impide que tu Session se cierre, igual que un
  child en ejecución.
- El comando se ejecuta tal cual, palabra por palabra, sin shell; escribe `sh -c '…'` si necesitas
  uno. Se ejecuta en este directorio (`--dir` para otro) con solo PATH, HOME, locale, USER, SHELL,
  TMPDIR y TERM de tu entorno: nunca una credencial. Un comando que necesite una la lee de su propio
  archivo.
- Su salida está en el directorio de la tarea, `output.log`, y se conserva 7 días después de que
  termine.
- Se ejecuta una sola vez. Si el daemon se reinicia entretanto, retoma el comando; un comando que
  terminó mientras ningún daemon lo vigilaba y no dejó estado de salida queda resuelto como
  `failure` con el resultado desconocido, y **no** se vuelve a ejecutar. Arráncalo otra vez tú mismo
  si es seguro hacerlo.
- Un arranque dudoso —la CLI no pudo llegar al daemon— se reintenta con
  `--task-id <the id it printed>`: el mismo id nunca se arranca dos veces.
- Un callback no ocupa un slot de child. Se ejecutan como máximo 8 por Session y 16 por máquina; uno
  más se rechaza con `429 callback_capacity` y un `retry_after`. Un despacho no puede declararse
  `kind callback` (`bad_task`). Windows rechaza con `501 no_callback_capability`.

## 6. Landing, y los otros tres tipos de trabajo

**Después de hacer merge de la rama de un child, no hagas nada más**: el broker registra `landed`
por sí mismo (ver abajo). Un child que no declaró escrituras (`--claims ""`), terminó por sí solo y
no dejó nada en su rama ni en su checkout queda registrado por el broker como `nothing_to_land`
antes de que se escriba su aviso. Un landing registrado a mano es para lo que ninguno de los dos
cubre —un cherry-pick, una entrega `incorporated`, `nothing_to_land`, `abandoned`— y es un solo
comando, enviado con el token del orquestador:

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

Es esta ruta, que un script puede llamar con la cabecera `X-Clawdline-Orchestrator`:

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- Solo estas claves, más `delivery`, que se acepta y no se usa; cualquier otra clave se rechaza.
  `pending` y `abandoned` aceptan el secreto de tarea o el token del orquestador; `landed`,
  `incorporated` y `nothing_to_land` aceptan solo el token del orquestador.
- `landed` necesita `target` y `commit`; `incorporated` necesita `target`, `commit` y
  `carrier_task`, la otra tarea cuyo landing verificado llevó esta entrega. El daemon **comprueba en
  Git cualquiera de los dos estados**; si no cuadra, `409 unverified_landing` con uno de estos `reason`:
  `commit_unresolved`, `target_unresolved`, `not_on_target`, `base_unknown`, `predates_dispatch`,
  `delivery_unknown`, `nothing_delivered`, `not_the_delivery`, y para `incorporated`
  `carrier_required`, `carrier_is_delivery`, `carrier_unresolved`, `carrier_not_landed`,
  `carrier_repository_mismatch`, `carrier_target_mismatch`, `carrier_commit_mismatch`,
  `delivery_is_ancestor`.
- `nothing_to_land` se rechaza con `409 wrote_to_repository` cuando la tarea sí escribió.
- Un landing ya resuelto no puede cambiar: `409 invalid_transition`, o `409 landing_conflict` para
  un valor distinto.
- **Un merge se registra solo.** Una vez que la rama de una tarea terminada se fusiona en su
  destino, el broker registra `landed` por su cuenta en pocos minutos, mediante la misma
  comprobación de Git, con el head del destino como commit. Si no hay ningún destino registrado,
  solo nombra uno cuando la rama del checkout principal es la única rama que contiene la entrega.
  Un cherry-pick, una entrega `incorporated` y `nothing_to_land` te siguen tocando a ti registrarlos.
- **El aviso de finalización nombra el estado de la rama cuando terminó la tarea**, y cada estado
  pide una sola cosa, en forma de comando. *No hay nada con commit en su rama*: un landing se
  demuestra a partir de esa rama, así que, tal como está, nunca se podría registrar nada como
  landed; haz commit en su checkout, en esa rama, mientras el checkout siga en disco, o ejecuta
  `clawdline task land <id> abandoned`. *Hay commits en su rama*: haz merge de esa rama en su
  destino; el merge registra el landing. *No se pudo leer*: mira la rama y luego regístralo.
  *Escribió en el checkout compartido*: `clawdline task land <id>
  landed` con el commit que lleva ese trabajo a su destino, o `abandoned`. *No escribió nada, y el
  broker registró nothing_to_land*: solo queda el ACK.

`clawdline landings` (`GET /v1/orchestrator/landings`) es cada landing pendiente de la máquina, cada
uno con un `ownership.status`. `unknown` no es "nadie": significa que no se pudo leer la evidencia.
`503 landings_incomplete` significa que algunas filas no se pudieron leer, y no se ofrece una lista
más corta en su lugar.

`clawdline landings --work-id <item id>` (`GET /v1/orchestrator/landings?work_id=<item id>`) es, en
cambio, cada landing **registrado** para un ítem del Board: `{"work_id", "landings": [...], "at"}`,
cada fila con su `id` y su `source`: `task` (el registro landed o incorporated de un child
vinculado; el id es el id de la tarea), `root` (el registro que escribió
`item phase deploying --commit`) o `phase_event` (una copia que un daemon más antiguo guardó en el
historial del ítem). Una tarea vinculada cuyo registro no se puede leer es una fila con
`state: "unknown"`, nunca se omite. La lectura del ítem trae las mismas filas como `landings`. Un
ítem que no existe es `404 work_not_found`.

**Dos roots que hacen landing en un mismo checkout** toman primero un lease de landing (§11).

Los otros tres tipos de trabajo tienen cada uno su propia ruta. Cuál toca es un límite, no un
detalle:

| Tipo | Ruta | Qué es |
|---|---|---|
| **Traspaso (handoff)** | `POST /v1/orchestrator/handoffs` | Entregas una línea de trabajo existente, con todo su estado, a una sesión nueva |
| **Asignación de Root** | `POST /v1/orchestrator/root-assignments` | Un Root nuevo, con dueño independiente, para una funcionalidad nueva |
| **Automatización desacoplada** | `POST /v1/orchestrator/detached-tasks` | Trabajo desatendido sin nadie a quien informar |

**Traspaso.** Escribe primero `<state dir>/handoffs/<handoff_id>/handoff.md` (la ruta de lista
responde `package_root`). Debe llevar tres encabezados: **REFERENCES** (todo lo que el receptor debe
leer), **VERIFICATION** (preguntas que responde a partir de esas fuentes antes de continuar) y
**OPEN THREADS** (dónde retomar). Después publica, con un cuerpo cerrado:

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

Al receptor se le indica que lea el archivo, recorra sus referencias, responda sus preguntas de
verificación y continúe. Al abrirse, el traspaso captura los ítems abiertos del Board del emisor en
ese Project. Una vez que el receptor tiene un id de conversación y se observa su primer registro de
conversación, las asignaciones activas y los dueños de esos ítems pasan al receptor en una sola
transacción. Un traspaso fallido deja la propiedad en manos del emisor. Un ítem ya cerrado, pasado
a otro dueño o que se está asignando por separado se deja como está. Un ítem con compuerta de
verificación que está en verifying o merging vuelve a implementing, para que su nuevo dueño tenga
que verificarlo otra vez. Recibes un aviso `handoff_receipt` cuando se escribe la línea del
traspaso; ese aviso por sí solo no demuestra que se hiciera la transferencia en el Board. Rechazos:
`bad_task` (incluido un `handoff.md` ausente o vacío), `sender_not_found`, `sender_ambiguous`,
`rate_limited`, `terminal_busy`, y `succession_required` si tienes el rol de coordinador de la
máquina: la sucesión no está disponible en este daemon (`501`), así que esa sesión no puede hacer
un traspaso.

**Traspaso de hito.** Un Root de larga duración que ha alcanzado un hito hace el relevo con
`clawdline handoff --summary summary.md`, para que el trabajo posterior no vuelva a leer en cada
llamada todo lo anterior. El resumen tiene exactamente cinco encabezados `## ` —Goal, Verified
decisions, Blockers, Evidence (rutas, commits, ids o comandos `clawdline` que abrir, no su
contenido), Next step—, 6 KiB como máximo, sin credenciales ni texto de la conversación; `--check`
lista cada problema sin abrir nada, y el daemon rechaza esos mismos problemas como
`bad_milestone_summary`. El daemon escribe `obligations.md` a su lado: tus ítems del Board (se
mueven, incluida una decisión que la persona no ha respondido) y tus children en ejecución, avisos
sin acuse y landings pendientes (siguen siendo tuyos). Así que, después del relevo, sigue acusando
recibo y haciendo el landing de esos, y luego ejecuta `clawdline session close` cuando indique
`safe`. Hacer el relevo es decisión tuya: `clawdline usage --compare-handoff` dice si ha salido a
cuenta en esta máquina, y nunca es obligatorio.

**Asignación de Root.** La cabecera `Idempotency-Key` debe ser igual a `request_id`:

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

Cada campo de la asignación ocupa de 1 a 8192 bytes, 32 KiB en total. El daemon escribe el brief
él mismo y abre la sesión. **No tiene padre, secreto, tiempo límite, resultado ni landing**: a nadie
se le avisa cuando termina, porque no responde ante nadie. Rechazos: `bad_root_assignment`,
`idempotency_mismatch`, `request_conflict`, `rate_limited`. Nunca la simules con un child, una
tarea desacoplada o un traspaso.

**Automatización desacoplada.** Como un despacho (§4) —`task.json` bajo `task_root` y luego
`{"task_id", "secret", "inventory_generation"}`—, pero el root del brief debe ser
`{"session_id": null, "poll_only": true}`, o se rechaza como `detached_task_required`. No se avisa a
nadie; sondea `GET /v1/orchestrator/tasks/<id>` y lee `result.json`. Nunca es un Root ni el dueño
de una funcionalidad.

### Programar trabajo futuro

Clawdline Next gestiona por sí mismo el trabajo programado. No uses la app retirada, `cron` ni una
cadena de tareas desacopladas. Lee `GET /v1/orchestrator/schedules`; lee una completa en
`GET /v1/orchestrator/schedules/<id>`. `GET /v1/places` proporciona el `place_id` que nombra una
escritura.

Una programación única (`on`) se puede crear directamente con el token del orquestador. Una
programación repetitiva (`days`) es una instrucción permanente y necesita la instrucción explícita
de la persona desde esta sesión:

1. Lee `GET /v1/orchestrator/sessions/<conversation>/run`. Es la ejecución (run) reciente que se
   emitió cuando la persona envió su mensaje a esta sesión a través de Clawdline.
2. `POST /v1/orchestrator/schedules` con un `Idempotency-Key` y el cuerpo normal de la programación
   más esa conversación y ese run:

```json
{"title":"Morning sweep","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"Inspect the overnight failures and report actionable findings.",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

La misma prueba autoriza `PATCH /v1/orchestrator/schedules/<id>` (envía el cuerpo completo de la
programación) y `DELETE /v1/orchestrator/schedules/<id>` (envía los dos campos de prueba como su
cuerpo JSON) cuando la persona pidió explícitamente ese cambio.
`POST /v1/orchestrator/schedules/<id>/run` ejecuta una ahora. Vuelve a leer la programación creada
o cambiada antes de informar de un éxito.

Sin `via`, el token del orquestador sigue limitado a una programación única `on`. Un run inventado,
caducado o de otra sesión se rechaza como `run_unknown`, `run_expired` o `run_other_session`; una
prueba mal formada es `invalid_user_authorization`. Si la persona escribió directamente en una
terminal, no hay run: pídele que envíe la instrucción a través de Clawdline. Nunca reutilices un
run como permiso general para trabajo que el mensaje de la persona no pidió. Esta prueba hace
auditable la retransmisión; no convierte el token del orquestador, válido para toda la máquina, en
una credencial específica de una sesión.

Una programación puede no tener hora: envía `"trigger_only": true` en lugar de `at`, `days` y `on`.
El reloj nunca la ejecuta; solo se ejecuta mediante `…/run` o mediante su webhook, y solo mientras
esté `enabled`. Es una instrucción permanente, como una repetitiva, y necesita la misma prueba.

**Arrancar una tarea en otra máquina y saber cómo terminó.** No hay ningún canal de máquina a
máquina. Convierte la tarea en una programación solo por disparo (trigger-only) en la máquina de
destino, vincúlale un webhook de Cloud y guarda la URL donde quien llama pueda leerla (es una
credencial: un archivo que solo tú puedas leer, o `CLAWDLINE_WEBHOOK_URL`; nunca un argumento de
la línea de comandos). Después, en la máquina que llama:

```sh
clawdline webhook fire --url-file <path>
```

Envía `{"deliver_within_seconds": 60}` (`--deliver-within`), para que un destino apagado no la
ejecute más tarde; sigue el estado de la entrega hasta que termina o pasa el `--timeout` (60m),
imprimiendo los cambios en stderr y una línea final en stdout. Sale con `0` si tuvo éxito · `1` si
terminó sin éxito (failure, timed_out, cancelled, spawn_failed) · `2` si nunca llegó a la máquina
(expired, canceled, unreachable) · `3` si fue rechazada (dispatch_refused y su código, la URL no
disponible, límite de frecuencia) · `4` si dejó de esperar, con el último estado visto. `--no-wait`
imprime el id de la entrega y vuelve después del `202`. Informa del código de salida y de la línea
final tal como son: `2` significa que no se ejecutó nada, no que fallara.

Una tarea programada que lee datos para algo que espera verificación (el 驗收 de la barra lateral,
docs/verifications.md) escribe su lectura como una nota en ese registro con su propio secreto de
tarea, no con el token del orquestador, que no debería tener:

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<readout>"}'
```

Solo se asienta en un registro cuyo `schedule_id` sea la programación que arrancó esta tarea (si
no, `schedule_mismatch`), va firmada como `task:<task id>`, y se rechaza como `not_scheduled` para
una tarea que no arrancó ninguna programación. Encuentra el id del registro con
`clawdline verify list`.

## 7. Informar de tu propio turno terminado

Cuando tu turno haya terminado de verdad —el trabajo hecho, verificado y con commit cuando
corresponda—, haz de esto tu última acción antes de la respuesta final:

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

Dibuja una marca en la fila de tu sesión: **entregado, pendiente de aprobación**. Es más débil que
un landing y no pide ninguna revisión. Se muestra mientras el daemon lee la sesión como inactiva
—trabajando, esperando o una pantalla ilegible tienen prioridad sobre ella— y solo mientras esa
terminal tenga la misma conversación.

- **Solo para un turno terminado.** No para trabajo parcial, un diagnóstico, un bloqueo ni una
  pregunta de vuelta a la persona. Un child nunca lo envía (`409 child_session`).
- El comando encuentra tu conversación a partir de `CLAUDE_CODE_SESSION_ID` o `CODEX_THREAD_ID`
  (si no, `--conversation`), pregunta a `GET /v1/orchestrator/whoami` por la terminal y publica
  `{"summary"}` en `POST /v1/orchestrator/sessions/<terminal>/complete`.
- El resumen tiene de 1 a 500 caracteres. Cada llamada es un recibo nuevo; cuenta el más reciente.
- La respuesta también trae `open_todos`: las tareas pendientes directas de esta Session que se
  enviaron o se leyeron y no están completadas, de la más antigua a la más reciente, 20 como máximo
  (`open_todos_truncated` cuando hay más). El comando las imprime en stderr después del recibo, un
  id y un texto por línea. Marca como hecha cada una que hayas terminado con
  `clawdline todo done <id>`. El recibo se registra en cualquier caso y el estado de salida no
  cambia; `open_todos_unknown: true` significa que no se pudieron leer, no que no haya ninguna
  abierta.
- Rechazos: `conversation_id_malformed` (no es un UUID en minúsculas), `conversation_not_found`,
  `conversation_ambiguous`, `registry_stale`, `session_not_found`, `session_unbound`,
  `child_session`. Informa del rechazo con honestidad; una frase en el chat no es un recibo.

**Deja un informe de estado para la persona.** Cuando el turno cambió archivos en un proyecto git,
entrega a la persona una página donde pueda ver en qué punto está el trabajo y leer cada archivo
que el turno añadió o cambió. Es un archivo HTML local: no se sube nada y no carga nada.

```sh
clawdline report --repo <project> --status status.md [--notes notes.txt] [--lang zh-TW] [--open] <commit>…
```

- Nombra **los commits propios de este turno, del más antiguo al más reciente**. Cada uno se lee
  por separado, así que los commits de otra Session que queden entre ellos se quedan fuera; nunca
  pases un rango.
- `status.md`: un `# Title` opcional, una línea opcional debajo, y luego un encabezado `## ` por
  tarjeta —empiézalo con ✅, 🟡 o ❌— y un cuerpo breve en Markdown.
- `--notes`: un `path: sentence` por línea, que se muestra encima de ese archivo. `--pin`
  (repetible) pone un archivo primero; `CLAUDE.md` y `AGENTS.md` se fijan cuando el turno los tocó.
  `--exclude` deja fuera una ruta y lo dice. `--at` es la revisión cuyo contenido se muestra
  (`HEAD` por defecto).
- Guarda el informe en `<state dir>/reports/<date>-<id>/report.html`, fuera de todo repositorio,
  e imprime dos direcciones: **primero la dirección `file://`**, que una terminal sí puede abrir, **y luego
  `http://127.0.0.1:<port>/reports/<id>`**, que responde el daemon de esta máquina. Pon las dos en
  tu respuesta final: la consola muestra una dirección `file://` como texto que no puede abrir, y
  convierte en enlace la `http://`. Esa dirección solo se abre en un navegador de esta máquina que
  tenga la sesión iniciada en su consola; un teléfono o un visor de Cloud se rechazan
  (`report_not_over_cloud`, `report_local_only`).
- `--out` escribe en su lugar otro archivo o directorio, solo con la dirección `file://`. stderr
  dice qué dejó fuera o acortó. `--open` además abre el archivo en el navegador de esta máquina.

## 8. Hablar con otra sesión

**Encuéntrala.** `GET /v1/orchestrator/sessions` es la libreta de direcciones: el `id` de cada
sesión (su id de terminal), `label`, `assistant`, `cwd`, `state`, `work_state`, y `taskId` para un
child vivo.

**Envía.**

```sh
clawdline send --to <terminal id> "text"        # or text on stdin
```

Esto es `POST /v1/orchestrator/messages` con `{from_session, to_session, text}` y un
`Idempotency-Key`. El daemon lo escribe en el compositor del destinatario dentro de un sobre
`<clawdline-message>` que te nombra como origen.

- `to_session` es un **id de terminal**: un mensaje sigue a la pestaña que quisiste decir, no a una
  conversación. `from_session` es tu id de terminal o de conversación (el comando lo rellena).
- Solo texto, 100.000 caracteres como máximo. No hay campo `images`: si envías uno, se descarta en
  silencio.
- `ok` significa que los bytes llegaron a un compositor, no que alguien los leyera.
- Puede tardar decenas de segundos: el daemon lee todas las sesiones de la máquina antes de escribir
  (unos 30 s por retransmisión en un Mac, medido el 2026-09-19). El comando espera hasta dos
  minutos. No lo cortes antes: una retransmisión cortada a mitad de camino puede escribirse y no
  quedar registrada, y entonces la misma clave responde `409 request_in_progress`.
- El comando imprime primero su `Idempotency-Key`. Si la llamada falla por el camino, ejecútalo otra
  vez con `--key <that key>`: la misma clave y el mismo cuerpo se escriben una sola vez. La misma
  clave con un cuerpo distinto es `409 idempotency_key_reused`.
- Rechazos: `source_not_found`, `target_not_found`, `same_session`, `target_busy` (el destinatario
  muestra un menú; no se escribió nada), `terminal_busy`, `delivery_failed`.

**Muéstrale una imagen a la persona.** No pegues una ruta local: en un teléfono no abre nada.

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- Solo con el token del orquestador. De uno a seis archivos locales; cada uno debe ser un archivo
  normal, de 12 MiB como máximo y 12.000 px por lado. PNG, JPEG y GIF se leen directamente; los
  demás formatos pasan por `sips` en macOS.
- La respuesta lista `artifacts`, cada uno con un `marker` como `<clawdline-image id="…">`. **Pon el
  marcador en tu respuesta**; la consola muestra la imagen donde está el marcador. Las imágenes se
  conservan 24 horas.

## 9. Avisar a la persona

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

`POST /v1/orchestrator/notify`. Solo para algo que la persona está esperando: el valor de una
notificación push es que sea poco frecuente. `--session <terminal>` hace que al tocarla se abra esa
sesión.

- `409 agent_notify_disabled`: la persona desactivó las notificaciones de agentes. No es culpa
  tuya; no reintentes.
- `409 not_subscribed`: ningún dispositivo está suscrito a las notificaciones push.
- `429 rate_limited`: 30 por hora para toda la máquina, compartidas con las notificaciones de cada
  child.
- `502 push_failed`: el servicio push lo rechazó; `sent` y `failed` están en el error.

## 9a. Dejar una nota de intervención humana

Usa una nota cuando un Agent de larga duración tenga una cosa concreta que la persona deba leer, hacer o decidir y un mensaje de chat normal pueda perderse en el flujo. La nota permanece en el área de atención plegada de la Session de destino, marcada con un punto rojo hasta que la persona la pase a atendida. Después puedes seguir con trabajo independiente; la persona puede volver en un momento natural de pausa. Una nota no es un registro de progreso, un recordatorio privado, una notificación ni una autorización para una decisión del Board. Evita notas duplicadas para la misma petición.

**Antes de pedirle a la persona que elija en el chat**, crea una nota `answer` que contenga la pregunta real, las ventajas y desventajas necesarias para decidir, y de dos a cuatro respuestas sugeridas completas. Tocar un botón envía su respuesta como un mensaje de la conversación, así que haz que cada `draft` sea inequívoco por sí solo. Tras crearla, basta con una breve referencia en el chat. No tomes la creación de la nota ni una nota marcada como atendida como la respuesta de la persona; espera al mensaje de la conversación —una respuesta tocada o una que la persona escribió— antes de actuar en consecuencia. Si la creación falla, dilo y haz la pregunta directamente. Reserva las notas para decisiones que requieran juicio humano, no para elecciones rutinarias que el Agent puede tomar.

Crea una con un archivo de cuerpo JSON. Sin `--target`, la CLI resuelve el id de terminal propio de este Root vivo mediante `whoami`. Para otra Session, usa como `--target` su **id de terminal** vivo de la libreta de direcciones (`clawdline guide send`). `--from` toma por defecto el id de conversación de este Root vivo a partir del entorno. La CLI lee la credencial de la máquina sin ponerla en la línea de comandos, inyecta los ids de origen y destino, e imprime el id duradero de la nota que da el daemon. Reutiliza el `--key` impreso tras un resultado dudoso.

```json
{"kind":"answer","title":"Choose a date","summary":"One release date needs your choice.","action":"Choose a date when you have a moment.","reason":"Only you can choose it.","options":[{"label":"Tuesday","draft":"Tuesday works for me."},{"label":"Wednesday","draft":"Wednesday works for me."}]}
```

```sh
clawdline note create --body-file note.json
# For another Session: clawdline note create --target <terminal-id> --body-file note.json
```

`kind` es `read`, `answer`, `action` o `report`; `title`, `summary`, `action` y `reason` son obligatorios. Un `answer` puede ofrecer de dos a cuatro opciones. Cada `draft` es la respuesta sugerida que se muestra en su botón. Cuando la persona lo toca, la Console lo envía de inmediato como un mensaje de la conversación a la Session de la nota, seguido de una línea de contexto con el ID, el título y la acción de la nota, para que la Session que lo recibe sepa a qué petición respondió la persona; el contexto no se muestra en el botón. Solo después de que el envío tenga éxito pasa la nota a atendidas recientemente. Si el envío falla, la nota sigue pendiente y el control de atención dice que la respuesta no se envió. `detail` puede contener un texto más largo. `document_url` puede enlazar a un documento real y legible de Cloud; verifica la ruta del documento y el archivo antes de publicarla. Actúa según el mensaje de la conversación que llegue, no según el estado de la nota: una nota que la persona marcó a mano como atendida no envió nada. Si tu trabajo está realmente bloqueado esperando la respuesta, registra el estado de espera al usuario y envía una vez la notificación de atención existente. Una nota visible por sí sola no envía ninguna notificación push ni despierta a un Agent.

## 10. El tablero

El tablero tiene tres estructuras —los ítems del tablero, el Backlog y la lista de tareas pendientes
propia de cada sesión— y **lo que entra en él lo decide una persona**. Una sesión crea un ítem del
Board solo cuando el propio mensaje de la persona, enviado a través de Clawdline, se lo indica; si
no, lo propone. Nunca abre una tarjeta por iniciativa propia. La única excepción es el dueño de una
Epic: después del plan revisado de la Epic puede dividirla en ítems Feature e Issue y asignarlos a
Sessions (`clawdline guide epic`).

**TODO / 待辦 / 土度 dicho junto con un ítem del Board significa los pasos de ese ítem.** Ponlos en
el ítem con `--step`. **No** los escribas además con `clawdline todo add`. `clawdline todo add` es
solo para una lista que la persona te pide seguir como tareas pendientes propias de esta Session,
sin ningún ítem del Board.

**Cuando la persona te dice que crees un ítem del Board.** Solo cuando su mensaje —enviado a través
de Clawdline, de modo que tiene un run— pida explícitamente uno, créalo tú mismo:

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin] [--assign-self]
```

Ejemplo práctico. La persona escribe: *"Make a Board item to clean up the release notes, TODO: draft
them, check the links, publish."* Eso es un solo comando y nada más:

```
echo "Clean up the release notes before the next release." | \
  clawdline item add --project <place id> --kind feature --title "Clean up the release notes" \
  --step "Draft the notes" --step "Check the links" --step "Publish"
```

- `item add` lee el run más reciente de esta conversación
  (`GET /v1/orchestrator/sessions/<conversation>/run`) salvo que `--run` nombre uno, imprime su
  Idempotency-Key antes de preguntar (`--key` reintenta la misma escritura) e imprime el ítem
  creado con el id de cada paso. Es
  `POST /v1/work/v2/agent/items` con `{"session_id", "via": {"run"}, "project_id", "kind",
  "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`,
  y se responde `201` con `{"item", "assigned", "assignment_state"}`.
- Una Feature, una Issue o una Epic llega **sin asignar** por defecto, al lugar del Board donde
  esperan los ítems sin asignar de ese tipo de la persona, con sus pasos: las filas de `--step` en
  orden o, si no das ninguna, dos o más filas de lista Markdown de primer nivel de la descripción.
  La persona a menudo te pide que dejes trabajo anotado para más tarde; crear el ítem no lo hace
  tuyo. `assignment_state` es `not_requested`.
- Añade `--assign-self` (`"assign": {"mode": "self"}`) **solo cuando el mensaje de la persona pida
  que esta Session haga el trabajo ahora** ("crea un ítem para esto y hazlo"). El ítem llega
  entonces **asignado a ti**, en `assigned`, en la misma escritura. No se escribe nada en tu
  terminal; tú lo pediste. Trabaja los pasos en orden, completa cada uno cuando esté verificado
  (`clawdline item steps <item id>`, `clawdline item step-done <item id> <step id>`; `clawdline item step-add`
  añade uno que el trabajo resulte necesitar), y avanza las fases con `clawdline item phase` como en
  cualquier ítem asignado (ver abajo). Una Epic tomada así sigue después el procedimiento de Epic
  (ver abajo) antes de poder implementarse. Si la persona te pide más tarde que tomes un ítem que
  creaste sin asignar, usa `clawdline item claim` (ver abajo).
  Un Refactor es trabajo ejecutable que cambia la estructura interna pero no el comportamiento
  externo: se asigna, lleva pasos y sigue las fases, la compuerta y el interruptor de revisión de
  una Feature. Un Plan se crea sin asignar, en Planning, con o sin `--assign-self`, y no lleva pasos
  (`planning_has_no_steps`).
- El Clawdfather registrado es la excepción para el trabajo ejecutable de un Project: nunca es
  dueño del código del Project ni lo edita. Cuando el mensaje de la persona pide explícitamente un
  ítem nuevo, puede usar
  `clawdline item add --project <place id> --kind feature --title "…" --assign-new` (o
  `--assign-terminal <id>`) para crear primero el ítem y luego delegarlo en una Session del Project.
  `assignment_state` dice si se registró un dueño en el Project (`assigned`), si una Session nueva
  necesita que se responda su primer diálogo (`awaiting_user`), si la asignación falló (`failed`
  con `assignment_error`) o si no se pidió ninguna asignación (`not_requested`). En los dos últimos
  casos, la persona puede asignarlo desde el Board. Una repetición con `pending` nombra el ítem
  original después de una delegación interrumpida; revisa el Board antes de intentar otra
  asignación. Sin ese mensaje explícito, propón el ítem y espera a que se acepte.
- La persona ve la tarjeta marcada con "Created by the Session from your message at HH:MM", con sus
  palabras citadas.
- Rechazos, ninguno de los cuales escribe nada: `run_unknown` (no se nombró ningún run, o no se
  emitió ninguno), `run_expired` (de más de un día), `run_other_session` (un mensaje a otra
  Session), `session_not_found`, `child_session` (un child informa a través de `result.json`),
  `project_not_found`, `project_mismatch` (un ítem ejecutable normal debe estar en el Project en el
  que trabajas), `coordinator_required` (una Session de máquina sin el rol vivo),
  `machine_delegation_required` (una Session normal pidió la asignación combinada, exclusiva de la
  máquina, a otra Session), `invalid_assignment` (un `assign` mal formado, o el Clawdfather pidiendo
  `self`), `too_many_steps` (más de 128), `run_items_exhausted` (un mensaje respalda como máximo
  cinco ítems).
- **Sin run** —la persona escribió directamente en la terminal, así que `item add` responde
  `no_run` o `run_unknown`—: recurre a una propuesta (ver abajo) y dile a la persona que la acepte
  en las Agent proposals del Board.

Nunca crees un ítem del Board por iniciativa propia, y nunca varios para planificar trabajo
especulativo.

**Reclamar un ítem del Board que la persona te señaló.** Cuando el mensaje de la persona a través
de Clawdline te dice que tomes un ítem concreto que ya está en el Board —*"toma el ítem de las notas de la
versión"*, *"reclama <item id>"*—, reclámalo; es un solo comando:

```
clawdline item claim <item id>
```

- `item claim` lee el run más reciente de esta conversación salvo que `--run` nombre uno, lee el
  ítem para obtener su versión, imprime su Idempotency-Key antes de preguntar (`--key` reintenta la
  misma escritura) e imprime el ítem después. Es `POST /v1/work/v2/agent/items/<id>/claim` con
  `{"expected_version", "session_id", "via": {"run"}}`, y asigna el ítem a **ti, la Session a la
  que se envió ese mensaje**: nada en él nombra otra Session ni otra terminal.
- El ítem queda entonces exactamente como si la persona te lo hubiera asignado desde el Board: eres
  su dueño, pasa a `assigned` y, si no tenía pasos, se siembran a partir de la lista de la
  descripción. No se escribe nada en tu terminal. Trabájalo como cualquier ítem asignado (ver
  abajo).
- La persona ve la tarjeta marcada con "Claimed by the Session from your message at HH:MM", con sus
  palabras citadas.
- Rechazos, ninguno de los cuales escribe nada: `run_unknown`, `run_expired`, `run_other_session`,
  `session_not_found`, `child_session` (como en `item add`); `work_not_found`; `project_mismatch`
  (el ítem está en un Project en el que no trabajas); `item_assigned` (ya tiene una Session, o se
  está abriendo una para él; solo la persona mueve un ítem entre Sessions); `item_terminal` (done o
  cancelled); `planning_not_assignable` (un Plan se queda en Planning; una Epic o un Refactor sí se
  pueden reclamar); `version_conflict` (cambió; ejecuta el comando otra vez);
  `run_claims_exhausted` (un mensaje respalda como máximo cinco usos).
- **Sin run** se responde `no_run` o `run_unknown`: deja el ítem para que la persona lo asigne.

**Asignar un ítem del Board a una Session nueva que pidió la persona.** Cuando el mensaje de la
persona a través de Clawdline te pide que entregues una Feature o una Issue concreta sin asignar a
una Session nueva —*"abre una Session de seguridad para <item id>"*—, asígnala; es un solo comando:

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- En un ítem que no es hijo de ninguna Epic, `item assign` lee el run más reciente de esta
  conversación salvo que `--run` nombre uno, igual que `item claim`. Es
  `POST /v1/work/v2/agent/items/<id>/assign` con
  `{"expected_version", "session_id", "mode": "new_session", "assistant"?, "model"?, "persona"?,
  "via": {"run"}}`, y abre la misma Session nueva que abre la opción "New Session" de la propia
  persona.
- La tarjeta dice "Assigned by a Session from your message at HH:MM", con sus palabras citadas y el
  perfil de persona con el que se ejecuta la Session nueva.
- Rechazos, ninguno de los cuales escribe nada: los de `item claim` (`run_unknown`, `run_expired`,
  `run_other_session`, `session_not_found`, `child_session`, `project_mismatch`, `item_assigned`,
  `item_terminal`, `version_conflict` y `run_claims_exhausted`: `item claim` e `item assign`
  comparten los cinco usos de un mensaje); `kind_person_assigns` (solo una Feature o una Issue);
  `new_session_only` (para tomarlo tú, reclámalo); `unknown_persona`;
  `persona_disabled_for_auto_assignment` (el rol está desactivado para la asignación automática en
  ese Project).

Nunca reclames ni asignes un ítem por iniciativa propia —solo el que nombra el mensaje de la
persona— y nunca uses el `POST /v1/work/v2/items/<id>/assign` de la persona, que rechaza a una
Session (`session_cannot_create_item`). El dueño de una Epic también asigna los hijos de la propia
Epic con `clawdline item assign` (`clawdline guide epic`).

**Ponerle nombre a una Session nueva abierta para un ítem del Board.** Después de leer su objetivo y
su alcance, elige un nombre corto que describa tu tarea real y ejecuta
`clawdline item name <item id> "<task name>"`. Esto cambia el nombre de tu Session una sola vez, sin
cambiar el título del ítem del Board ni iniciar otro turno del modelo. Solo puede hacerlo el dueño
activo de la Session nueva. Enviar otra vez el mismo nombre es seguro; un nombre distinto se
rechaza, y la persona todavía puede fijar un título manual para la Session. Un ítem del Board
entregado a una Session existente deja intacto el nombre de esa Session.

**Proponer un ítem del Board.** La cola **Agent proposals** del Board se alimenta de una sola ruta:

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id` es el `id` de una fila de `GET /v1/places`.
- Se requiere un origen, y debe ser tuyo: un ítem del Board del que esta Session es dueña, o una de
  las tareas pendientes propias de esta Session (`proposal_source_required`,
  `proposal_source_invalid`). Una propuesta que surgió de algo que pidió la persona cita la tarea
  pendiente de la que salió; así que el camino es: la persona lo pide, tú lo añades con
  `clawdline todo add` (ver abajo), y propones a partir del id de esa tarea pendiente.
- Redacta la propuesta en un lenguaje llano que una persona entienda directamente: `title` nombra
  el resultado que podrá notar, `description` dice qué cambia, `reason` dice por qué vale la pena
  hacerlo ahora, y `suggested_acceptance` dice qué podrá observar cuando esté hecho. Los cuatro son
  obligatorios. No hagas que la explicación principal sean siglas sin explicar, identificadores
  internos, rutas de código o jerga de implementación. El Board muestra primero el título, el
  origen y el motivo; **Explain / 詳細說明** despliega qué cambia y qué verá la persona cuando esté
  hecho.
- **Para proponer un ítem con pasos**, escribe la lista como dos o más filas de lista Markdown de
  primer nivel en `description`. Cuando la persona acepta y asigna el ítem, cada fila se convierte
  en uno de sus `steps` (ver abajo).
- `201` responde la propuesta pendiente. La persona la acepta, la edita o la rechaza en la cola
  Agent proposals del Board; nada se convierte en un ítem del Board hasta que lo haga. Rechazos:
  `invalid_proposal`, `proposal_too_large`, `project_not_found`, `proposals_full`.

**La ruta de propuestas antigua.** `POST /v1/orchestrator/proposals` (con `…/<id>/asked` después de
preguntar en la conversación) todavía se sirve: es donde un child presenta un pendiente con su
secreto de tarea y su `task_id`, y donde la propuesta de línea de trabajo de un root recibe sus
`instructions` de preguntar ahora o esperar. Sus filas aparecen en la antigua área "to confirm",
**no** en la cola Agent proposals del Board v2, así que no es la forma de poner un ítem delante de
la persona en el Board.

**Pedirle una decisión a la persona.**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "work_id": "<the Board item this is about>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

El ítem debe estar abierto y ser de esta Session: su tarjeta es el contexto en el que el Board
muestra la pregunta (`decision_source_required`, `decision_source_not_found`,
`decision_source_invalid`, `decision_source_closed`). De dos a cuatro opciones; `default` debe ser
una de ellas y es lo que ocurre si nadie responde (pasados 7 días, salvo que `due_in_minutes` diga
60–10080). Solo una decisión `blocking` se envía como notificación push. Lee la respuesta con
`GET /v1/orchestrator/decisions/<id>`.

**La persona responde; una sesión solo retransmite lo que dijo.** Las propuestas, las decisiones y
los ítems del tablero se responden bajo `/v1/work/…`. Una sesión que escribe ahí debe nombrar el run
que llevó las palabras de la persona, `"via": {"run": "<id>"}`, y sin él se rechaza (`403
session_cannot_decide`). Lee el run más reciente en
`GET /v1/orchestrator/sessions/<conversation>/run`; un run inventado, caducado, de otra sesión o
anterior a la pregunta se rechaza por su nombre. Una persona que escribe directamente en una
terminal no tiene run, así que pídele que responda a través de Clawdline o en la consola.

**Tus children aún por recoger.** `GET /v1/orchestrator/sessions/<conversation id>/todos`, nombrado
por id de conversación, no de terminal (si no, `409 session_id_is_terminal`). El broker los abre y
los cierra a partir de los hechos de las tareas; no hay nada que escribir.

En cada límite de turno, antes de declararte inactivo, lee también
`GET /v1/work/v2/agent/session-todos/<conversation id>`. Sus `assigned_items` son los ítems del
Board que la persona ha dado a esta Session, sus `recent_items` son ítems que esta Session completó
recientemente, sus `direct_todos` son peticiones rápidas, y sus `unacknowledged_completions` son
children tuyos que terminaron sin tu ACK (§5). Esta consulta es la forma en que una asignación hecha
mientras trabajabas espera sin interrumpir el turno actual. Termina el turno actual, toma después el
ítem asignado como tu siguiente trabajo propio y lee su registro completo, cuerpos de los
documentos incluidos, con `clawdline item show <id>`.

**Una tarea pendiente que envió la persona.** Un mensaje cuya última línea dice
`(Clawdline to-do <id>. When it is done: clawdline todo done <id>)` es una de las `direct_todos` de
esta Session que la persona envió desde Clawdline; las palabras por encima de esa línea son la
petición. Haz el trabajo y, una vez verificado que está hecho, ejecuta `clawdline todo done <id>`
con ese id **antes** de informar del turno; si no, la fila sigue abierta en la lista de la persona
aunque el trabajo esté terminado. Una que no esté terminada sigue abierta. `clawdline session report`
lista en stderr cada tarea pendiente que se envió a esta Session y que todavía no está marcada
(§7).

**Tus propias tareas pendientes, cuando la persona lo pide.** Solo cuando la persona pida
explícitamente a esta Session que registre su trabajo como tareas pendientes de Clawdline —o le
entregue una lista de varios elementos y le diga que los siga ahí— escríbelas en la lista propia de
esta Session:

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add` es `POST /v1/work/v2/agent/session-todos/<conversation id>` con
`{"todos": [{"text": "…"}, …]}` y un Idempotency-Key que imprime primero (`--key` reintenta la misma
escritura). Una llamada lleva de 1 a 20 filas de 8 KiB como máximo cada una, dentro del cuerpo de
petición de 96 KiB, y las añade todas o ninguna; una lista que llevaría a esta Session por encima de
500 tareas pendientes abiertas se rechaza entera (`direct_todos_full`). Responde `201` con las filas,
en el orden dado. La conversación debe ser una Session viva que este daemon conozca
(`conversation_id_malformed`, `session_not_found`); un child de Clawdline se rechaza
(`child_session`) y sigue informando a través de `result.json`.

Nunca hagas esto por iniciativa propia, ni para planificar trabajo especulativo. Completa cada fila
con `clawdline todo done <id>` solo una vez verificado que está hecha. La persona ve estas filas
marcadas como añadidas por la Session, y solo la persona puede enviarlas o borrarlas. No son ítems
del Board y nunca aparecen en el Board. Las tareas pendientes son una lista de quehaceres de la
Session actual; un ítem del Board es trabajo que la persona quiere seguir en el Board —cuando pida
eso, usa `clawdline item add` (ver arriba), y su lista entra como las filas `--step` del ítem, nunca
además como tareas pendientes—.

Si lo que la persona quiere decir indica claramente que el ítem que acabas de completar sigue sin
terminar, corrige el Board tú mismo; no lo dejes en Recently Done, no crees un ítem de reemplazo ni
le pidas a la persona que lo reabra. Vuelve a leer el ítem para obtener su versión actual y luego
usa:

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "The concrete behavior or acceptance claim that remains unfinished"}
```

Usa esto solo cuando la referencia al ítem que acabas de completar sea clara. La ruta solo acepta
trabajo `done` cuya asignación final liberó esta misma Session; no puede revertir una cancelación
de la persona ni tomar la finalización de otra Session. Conserva la evidencia anterior, inicia un
nuevo ciclo en `implementing`, restaura esta Session como dueña y registra el motivo en el historial
inmutable del ítem. El motivo ocupa 8 KiB como máximo. Un seguimiento ambiguo no es autoridad para
cambiar un ítem del Board.

Cuando el Agent dueño necesita que la persona actúe o elija, le plantea una decisión y espera a que
la responda. Primero abre una decisión sobre este ítem (`POST /v1/orchestrator/decisions` con el
`work_id` de este ítem, de dos a cuatro opciones, un `default` y un plazo; para una acción, opciones
como `{"id": "done", "label": "I've done it"}` y `{"id": "cannot", "label": "I can't"}`), y luego
haz que el ítem apunte a ella en la ruta autenticada por la máquina:

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

La decisión debe existir (`decision_not_found`), ser de esta Session (`decision_other_session`),
tratar sobre este ítem (`decision_other_item`) y seguir abierta (`decision_not_open`); `decision_id`
con cualquier otra condición es `decision_requires_waiting_user`. Un `waiting_user` sin decisión se
rechaza con `waiting_user_requires_decision`. La persona responde en la tarjeta del Board o en
"Waiting on you"; cuando la decisión se responde, o su valor por defecto queda firme al vencer el
plazo, el daemon borra el `waiting_user` del ítem en la misma escritura, registra la respuesta en
el ítem y escribe el id y la etiqueta de la opción elegida en esta Session cuando está inactiva.
Para dejar de esperar por tu cuenta, pon `condition` a la cadena vacía (o a otra condición) en la
misma ruta; la decisión pasa entonces a `withdrawn` y sale de "Waiting on you", y lo mismo ocurre
cuando el ítem se libera, se reasigna, se cancela o queda done. Responder a una decisión retirada se
rechaza con `decision_withdrawn`.

**Compuertas capturadas de planificación y de verificación.** `planning_gate` está activada por
defecto y `verify_gate` desactivada; `clawdline setting get|set planning_gate|verify_gate` acepta
`on/off` o `true/false`. La primera asignación con éxito de un ciclo de ejecución congela ambos
valores. Las reasignaciones y los cambios posteriores del ajuste global no cambian ese ciclo. Una
Epic o una Feature con la planificación capturada activada necesita criterios de aceptación antes de
implementar. Una Epic necesita además un plan y una revisión independiente; una Feature los
necesita solo cuando la persona marcó su interruptor Needs independent review (ver abajo). Una Issue
nunca tiene compuerta de planificación. Con la planificación desactivada también se omite la
planificación forzada de las Epic. Ambas activadas significa planificación y luego verificación
independiente; solo planificación conserva la verificación de merge normal; solo verificación omite
la planificación pero sigue comprobando el candidato exacto; ambas desactivadas usa el ciclo de vida
normal. La persona no tiene que rellenar la aceptación en el Board. Si un ítem con compuerta llega
sin ella, escribe criterios observables con
`clawdline item acceptance <item id> --body-file <file>` después de la asignación y antes de la
transición con compuerta. La Session dueña puede rellenar un contrato vacío una sola vez. Cuando la
persona le diga explícitamente a este Root dueño, a través de Clawdline, que revise la aceptación de
este ítem, escribe en un archivo el Markdown de reemplazo completo y ejecuta
`clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>`.
La versión del ítem es la línea `item version N` que imprime `clawdline item show <id>`
(`item steps` imprime solo la versión de aceptación); lee el run del mensaje en
`GET /v1/orchestrator/sessions/<conversation>/run`. El extracto del mensaje que se conserva debe
pedir explícitamente un cambio de aceptación; una prohibición, una discusión o una simple pregunta
no son autorización.
El mensaje puede referirse al ítem por el contexto de la conversación si este Root
es dueño de exactamente un ítem abierto; si no, debe identificar el ítem por su ID o su título. El
run debe ser más reciente que la versión de aceptación actual. Un rechazo tipado significa que no
cambió nada. Reintenta una respuesta dudosa con
el mismo `--key`, `--run`, `--expected-version` y los mismos bytes de archivo. La persona también
puede editarla directamente.
Cambiarla antes del merge invalida los PASS y las anulaciones anteriores; una vez que empieza el
merge, queda bloqueada.

Con la verificación capturada activada, ejecuta `clawdline item phase <id> verifying` desde un
worktree registrado y limpio, en su candidato con commit: la CLI envía la rama actual y el HEAD
completo, y el daemon comprueba el Project, la base del ciclo, el árbol y el digest de la
aceptación. Un verificador de Codex desacoplado y de solo lectura usa `code-reviewer` para una
Issue, `reality-checker` para una Epic y `evidence-collector` para una Feature con imágenes de
referencia o un documento de diseño (si no, `reality-checker`). Su veredicto tipado es
`PASS`, `FAIL` o `NEEDS_WORK`; las afirmaciones no verificadas dicen por qué y nunca autorizan el
merge. Un resultado ausente o mal formado es un fallo técnico, con un reintento acotado y luego
escalado. La ronda final de extremo a extremo de una Epic espera a que todos los hijos estén en un
estado terminal y a que los componentes afectados estén integrados en un único candidato
ejecutable. Primero van las pruebas enfocadas de los hijos y las comprobaciones de humo de
integración; no despaches el trabajo final de extremo a extremo con navegador o con varias cuentas
contra una UI simulada, ramas desconectadas o APIs incompletas. Antes de despachar, demuestra que el
worker elegido puede abrir de verdad la URL de destino con un navegador autorizado o una
automatización local equivalente, y que tiene las cuentas de prueba, los fixtures y los permisos de
origen que necesita. Nombra esa vía en el brief; `--permission-mode full` por sí solo no es acceso
a un navegador. Resuelve una comprobación previa de herramientas fallida antes de reintentar, en
lugar de mandar otro verificador contra el mismo bloqueo; una comprobación previa fallida no es un
intento de extremo a extremo. Planifica una ronda completa de extremo a extremo por Epic, no una por
hijo o por revisión. Después de arreglar un defecto, vuelve a ejecutar solo los escenarios
afectados. Repite la ronda completa solo cuando el alcance de la aceptación o el límite de
integración cambien de forma sustancial, y registra por qué. `verifying → merging` necesita un PASS
vigente para el candidato y los criterios exactos, o una anulación explícitamente razonada; una
frase de verificación por sí sola no puede concederlo. Tres FAIL consecutivos se escalan al dueño
vivo de la Epic padre y, si ese dueño no está disponible, a la persona; un fallo técnico se escala
por separado. Solo el dueño padre designado usa `POST /v1/work/v2/agent/items/<id>/gate-decision`;
una persona usa `POST /v1/work/v2/items/<id>/gate-decision`. Nunca uses la ruta de la persona
como Agent. El Board nombra por separado las anulaciones de la IA, de la persona y técnicas, nunca
como un PASS del verificador. Cuando se alcanza la capacidad de detalle retenido, la persona primero
descarga `GET /v1/work/v2/items/<id>/gate-export`, verifica el digest del manifiesto y luego
confirma `POST /v1/work/v2/items/<id>/gate-purge` con ese digest y la versión del ítem. La purga
elimina solo el detalle cerrado que cumple los requisitos; los agregados, los hechos más recientes
y la auditoría se conservan.

**Avanzar la fase.** La Session dueña hace pasar su ítem por las fases de ejecución ella misma;
nadie más lo hace, y tampoco lo hacen un recibo de turno ni una condición borrada. La fase no es un
campo de `…/edit` (`phase_not_editable`). Ejecuta cada transición cuando el trabajo que nombra haya
ocurrido de verdad:

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

El comando lee la versión del ítem, imprime su Idempotency-Key (`--key` reintenta la misma
escritura) e imprime el ítem. Es

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "no_landing_reason"?: "…", "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Un paso cada vez: `assigned → implementing → verifying → merging → deploying → done`.
  Desde `verifying` puedes volver a `implementing`; desde `merging`, a `implementing` o a
  `verifying`. Un ítem cuya compuerta de verificación capturada está desactivada también puede ir de
  `implementing` directamente a `deploying` con su evidencia de landing (ver abajo), con
  `verification` opcional; a un ítem con compuerta se le rechaza ese paso con
  `verification_gate_on` y recorre la línea entera. Nada más se salta una fase, y a `done` solo se
  llega desde `deploying`.
- `merging` necesita `verification`. `deploying` necesita un landing: un child del broker de este
  ítem que hizo landing, o `landing` nombrando un commit que el daemon encuentre tanto en la rama
  local `target` del Project como en `refs/remotes/<remote>/<target>`: haz push primero. Cuando el
  trabajo hizo landing en otro repositorio (un ítem de backend cuyo cambio fue un commit de
  frontend), `landing.project` (`--landing-project`) nombra el id de ese Project de
  `GET /v1/places`, y el commit se busca allí en su lugar; un repositorio anidado dentro del
  directorio del Project (`cloud/`) es aquí su propio Project. La prueba se escribe como el
  **registro de landing del root** del broker, una sola vez: el mismo ítem, repositorio, commit y
  destino registrados otra vez son el mismo registro. El historial del ítem lo nombra como
  `landing_id`, y `clawdline landings --work-id <item id>` lo lista. Antes de `deploying`, el daemon
  pregunta de inmediato a git si se ha hecho merge de la rama de un child vinculado, así que un
  merge hecho hace un momento cuenta sin esperar a la siguiente revisión del broker. El trabajo sin
  código lleva `no_landing_reason` (`--no-landing-reason`) en lugar de un landing; se rechaza junto
  a un `landing` (`invalid_landing_evidence`), mientras un child vinculado todavía deba su landing
  (`landing_owed`, que nombra la tarea), y junto a un child que hizo landing (`landing_recorded`).
  `done` necesita `deployment` o `no_deployment_reason`; la `deployment_policy` del ítem decide
  cuál (`required` solo acepta `deployment`, `not_required` solo `no_deployment_reason`,
  `agent_decides` cualquiera de los dos). Antes de pasar a done, todos los pasos deben estar
  completos.
- `done` libera tu asignación y lleva el ítem a la fila de completados recientemente de la Session.
  Añade antes un informe de finalización (ver abajo) cuando se deba uno.
- Rechazos: `invalid_transition` (no es una fase siguiente, o falta su evidencia),
  `steps_incomplete`, `not_item_owner`, `item_unassigned`, `item_terminal` (una persona lo reabre),
  `evidence_unknown`, `direct_landing_not_applicable`, `invalid_landing_evidence`,
  `landing_project_not_found`, `landing_commit_unresolved`,
  `landing_target_unresolved`, `landing_not_on_target`, `landing_remote_unresolved`,
  `landing_not_published`, `landing_owed`, `landing_recorded`, `landings_full` (el ítem guarda 64
  landings de root) y `version_conflict`: vuelve a leer y envía otra vez. Un rechazo por falta de
  landing termina con lo que el broker encontró al mirar en ese momento (una rama todavía sin merge,
  un repositorio que no pudo leer).

**Terminar con un solo comando.** Después de que el trabajo haya hecho landing,
`clawdline item finish <item id>` lleva el ítem desde `implementing`, `verifying`, `merging` o
`deploying` hasta `done` en una sola transacción:

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?}, "no_landing_reason"?: "…",
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Cada paso es el mismo que da `item phase`, a través de las mismas compuertas, y escribe su propio
  `item.phase_changed`; si se rechaza cualquier paso, se rechaza el conjunto y no se escribe nada.
- El landing se lee, no se escribe a mano: el commit es el candidato autorizado de la compuerta en
  un ítem con compuerta y, si no, el commit con landing de los children vinculados; el destino es la
  rama que nombran sus landings; el remoto es el que sigue esa rama. Cualquier campo que des tiene
  prioridad, y el resultado se demuestra contra git igual que lo demuestra `item phase deploying`.
  Una compuerta de verificación capturada sigue necesitando su PASS: desde `implementing`, a un ítem
  con compuerta se le rechaza con `verification_candidate_required`, así que entra en `verifying`
  con `item phase` desde el worktree del candidato, espera el PASS y luego termina.
- Un child vinculado cuya rama se fusionó hace un momento queda registrado como landed por el
  propio finish: el daemon pregunta a git antes de leer los landings, así que `landing_required`
  justo después de un merge significa que la rama no está en ningún destino, y el rechazo dice qué
  encontró el broker.
- Trabajo sin código:
  `clawdline item finish <item id> --no-landing-reason "…" --no-deployment-reason "…"`,
  con las mismas reglas que en `item phase`.
- Un ítem que ya está `done` se responde tal como está y no se escribe nada, así que el mismo landing
  visto dos veces no mueve nada.
- Los rechazos añaden `verification_required`, `landing_required`, `deployment_required` (la nota
  que le faltaba a ese paso), `landing_target_unknown`, `landing_remote_unknown`,
  `landing_remote_unreadable`, `landing_ambiguous` (nómbralo con el flag), `landing_owed`,
  `landing_recorded` y `finish_not_started` (todavía antes de `implementing`).

Un ítem asignado puede contener `steps`. Una asignación con éxito puede sembrarlos a partir de dos o
más filas de lista Markdown de primer nivel de la descripción, y un ítem que creaste con
`clawdline item add` lleva sus filas `--step`. Cada paso es una entrada de la lista de verificación
de ese ítem, no otro ítem del Board.
Completa un paso verificado con `clawdline item step-done <item id> <step id>`; envía
`POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete` con `{"session_id"}`. En las
rutas de Agent, `expected_version` es opcional: si se omite, la escritura actúa sobre la versión
actual; si se nombra (`--expected-version`), se compara, y una versión desfasada responde
`version_conflict`. Una transición a `done` se rechaza con `steps_incomplete` mientras quede algún
paso abierto; Clawdline nunca marca uno solo porque haya avanzado la fase padre.

**Dividir tu propio ítem en pasos.** Cuando un ítem tuyo no tiene pasos y el trabajo tiene varias
etapas —varios cambios que se verifican por separado, o más de una parte del sistema—, divídelo tú
mismo en sus pasos ordenados antes de implementar: de dos a ocho pasos concretos, cada uno
verificable por sí solo. Un cambio único y sencillo **no** lleva pasos; no rellenes una lista solo
por tener una. Cuando el trabajo resulte más grande de lo que parecía, añade el paso en ese momento.

```
clawdline item step-add <item id> "Wire the route" "Cover it with a test" "Say it in the guide"
```

Los títulos son argumentos, o uno por cada línea no vacía de stdin. El comando vuelve a leer el ítem
antes de cada título, imprime cada Idempotency-Key antes de su escritura e imprime un recibo breve
con una sugerencia de `item show`. Es
una petición, reservada al dueño, por cada título,

```
POST /v1/work/v2/agent/items/<id>/steps     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

con `"position"` uno más que el del último paso existente, ya que los pasos se ordenan por
posición. Después completa cada uno con `clawdline item step-done` una vez verificado. Esto no es la
"iniciativa propia" que está prohibida para los ítems del Board y las tareas pendientes: el ítem ya
es tuyo, y sus pasos son la forma de mostrar a la persona las etapas del trabajo que se te
encargó.

Cuando resolver una issue o un incidente exigió una investigación considerable para descubrir la
causa raíz o para distinguir el arreglo real de alternativas verosímiles, añade un informe de
finalización legible para la persona antes de llevar el ítem a `done`. Una corrección sencilla y
observada directamente no lo necesita. Escribe en un archivo qué pasó, la causa raíz, qué cambió,
cómo se verificó y cualquier límite que quede, y luego:

```
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Envía `POST /v1/work/v2/agent/items/<id>/documents` por ti (la parte Epic de §10,
`clawdline guide epic`, lista sus campos). Un curl armado a mano a esa ruta sin la credencial que
lee el comando responde `401 unauthorized`. El cuerpo es Markdown, de 64 KiB como máximo. Escríbelo
para la persona que informó del problema, no como un log de depuración en bruto, y deja fuera los
datos privados. El dueño activo debe añadirlo antes de que el ítem llegue a un estado terminal;
vuelve a leer después de un conflicto de versión. Un informe de finalización es una narración con
autoría y nunca sustituye a la evidencia de verificación, de landing ni de despliegue. Cuando
existe, permanece en el ítem cerrado del Board y se abre directamente desde la fila Recently Done
de la Session.

`/v1/board` son las tarjetas antiguas de la app Swift, de solo lectura. El landing es un hecho del
broker: un ítem nunca se marca como landed a mano (`422 landing_is_broker_fact`).

### Epic y Feature: respeta el interruptor de revisión de la persona antes de implementar

Una Epic cuyo ciclo capturó la planificación activada necesita un plan y una revisión
independiente. Una Feature lleva el interruptor **Needs independent review** de la persona
(`review_required` en el ítem; `clawdline item steps
<id>` lo imprime). Solo la persona lo fija, en el Board; tú no puedes, y no juzgas por tu cuenta el
riesgo de la Feature. El daemon lee el interruptor cuando pides entrar en `implementing`.

- **Sin marcar** (por defecto): escribe los criterios de aceptación breves de la Feature,
  implementa y ejecuta pruebas enfocadas. No escribas un plan para revisión, no despaches un child
  `plan_review` y no registres una evaluación de riesgos.
- **Marcado**, y para toda Epic con la planificación activada: usa el camino del plan revisado.

Si crees que una Feature sin marcar merece revisión, díselo a la persona y deja que la marque; no
hay forma de que el Agent le pida una al daemon.

1. Planifícala con cuidado y escribe el plan en el ítem:
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. Despacha un child de solo lectura cuyo brief sea revisar ese plan con espíritu crítico —qué
   falta, qué está mal o qué es arriesgado—:
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. Espera a que termine el child. Un child de revisión con éxito despachado con `--work-id` registra
   por sí solo su recibo de revisión en el ítem como el documento `plan_review`; comprueba
   `.documents` en `GET /v1/work/v2/items/<id>`. Solo si no está ahí —por ejemplo, porque el child
   se despachó sin `--work-id`— regístralo a mano:
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
   Ejecutar otra vez ese comando para la misma tarea es inofensivo: responde el documento que ya
   está. Un resumen breve para la persona de lo que cambió el plan en respuesta es un documento
   `other` aparte, no una segunda revisión.
   Si el plan de una Feature cambia después de la revisión, escribe un documento `other` titulado
   `Review boundary assessment` después del plan revisado, con el JSON
   `{"new_risk_boundary":false,"reason":"..."}`, solo cuando el cambio se mantenga dentro del
   límite de riesgo de la revisión anterior. Un límite nuevo o incierto recibe una revisión nueva y
   enfocada. Sigue vigente el tope existente de dos revisiones por Epic.
4. Divide el trabajo en pasos con `clawdline item step-add <item id> …`.
5. Solo entonces, `clawdline item phase <item id> implementing`.

Planifica la verificación como una secuencia. Cada child de implementación comprueba su propio
código con pruebas enfocadas; el dueño de la Epic integra los componentes afectados y ejecuta la
comprobación de humo entre componentes más pequeña que sea útil. Solo después de que funcione el
candidato integrado debe el dueño despachar la verificación real de extremo a extremo y la revisión
independiente de UX/producto que corresponda. Comprueba antes de despachar la vía de navegador del
verificador, la URL de destino, las cuentas de prueba, los fixtures y los permisos. No uses tareas
repetidas de verificación de solo lectura para descubrir o sortear la falta de un navegador:
arregla primero el acceso o elige un arnés de navegador local equivalente. Una comprobación previa
fallida no es un intento de extremo a extremo. Planifica una ronda completa de extremo a extremo
por Epic para el candidato estable, no una por hijo o por revisión; después de un arreglo enfocado,
vuelve a ejecutar solo las rutas afectadas. Repite la ronda entera solo después de un cambio
sustancial de la aceptación o de la integración, y registra ese motivo.

`clawdline item doc` lee el ítem para obtener su versión y la posición del último documento,
imprime su Idempotency-Key (`--key` reintenta la misma escritura) e imprime el ítem. El cuerpo sale
de `--body-file` o de stdin. Es `POST /v1/work/v2/agent/items/<id>/documents` con
`{"expected_version", "session_id", "role", "title", "body", "reference", "position"}`; los roles
son `spec`, `design`, `test`, `deploy`, `completion_report`, `other`, `plan` y `plan_review`.

Para revisar un documento, escríbelo otra vez con el mismo `--role` y el mismo `--title`: el daemon
sustituye su cuerpo, su referencia y su posición, conserva su id, sube su versión en uno y registra
`document.revised`. La CLI dice `added … at v1` o `revised … to vN`, y `clawdline item show`
imprime el `vN` de cada documento. Enviar otra vez el mismo texto no cambia nada y responde el
documento tal como está, así que reintentar es seguro. El texto antiguo no se conserva; usa un
título distinto para conservar ambos.

- `plan`, `plan_review` y el límite de revisión nunca se revisan en el sitio: cada escritura añade
  un documento nuevo, porque la compuerta de planificación los lee en orden y una revisión nombra el
  plan que leyó.
- Un ítem contiene como máximo 32 documentos, y un `completion_report` no cuenta entre ellos:
  siempre cabe, incluso en un ítem lleno. Un ítem contiene un solo `completion_report`; escribir
  otro, con cualquier título, lo revisa y adopta el título nuevo.
- Un documento número 33 se rechaza con `documents_full` y no se escribe nada; el mensaje nombra en
  su lugar el comando `clawdline item doc` que revisa un documento existente.

- `plan` y `plan_review` pertenecen a una Epic o a una Feature (`document_role_not_applicable` para
  otros tipos).
- La `reference` de un `plan_review` es el id de tarea del child de Clawdline que revisó el plan. El
  daemon solo la acepta cuando esa tarea existe (`plan_review_task_unknown`), la despachó la Session
  dueña del ítem (`plan_review_task_not_owned`), está en la línea de este ítem si nombra una
  (`plan_review_task_other_item`), es de tipo `plan_review` (`plan_review_task_wrong_kind`), terminó
  con `success` (`plan_review_task_unfinished`) y no se despachó antes del plan más reciente
  (`plan_review_task_stale`). Una revisión sin un plan anterior se rechaza con `epic_plan_required`.
  El documento automático de un child de revisión pasa las mismas comprobaciones, y una escritura
  repetida para la misma tarea es idempotente.
- `clawdline item phase <item id> implementing` en una Epic con la planificación activada se rechaza
  sin su plan revisado (`epic_plan_required` o `epic_plan_review_required`). Una Feature con la
  planificación activada en la que la persona marcó
  Needs independent review se rechaza de la misma forma (`feature_plan_required` o
  `feature_plan_review_required`); una sin marcar solo necesita sus criterios de aceptación. Un plan
  revisado en una Feature marcada también necesita evidencia de que el límite no cambió, u otra
  revisión.
  Una Epic con la planificación desactivada puede entrar directamente en implementing.
- La compuerta lee el recibo del `plan_review` más reciente a partir de su tarea de revisión. Cada
  hallazgo tiene una `severity` de `blocking` o `non_blocking` (`important` y `minor`, de la
  plantilla antigua, cuentan como no bloqueantes). El veredicto es `safe_to_land` sin hallazgos,
  `proceed_with_findings` cuando todos los hallazgos son `non_blocking`, y `changes_required` cuando
  alguno es `blocking`. Si la revisión más reciente tiene un hallazgo bloqueante, se
  rechazan `item phase implementing` y cualquier despacho con `--work-id` sobre el ítem todavía asignado,
  salvo `--kind plan_review`
  (`epic_plan_review_blocking` o `feature_plan_review_blocking`); el rechazo lista los hallazgos
  bloqueantes y los comandos siguientes: revisa el plan y luego despacha una revisión nueva. Solo
  los hallazgos no bloqueantes, o un recibo antiguo cuyos hallazgos no llevan severidad, dejan que el
  ítem avance. Una Epic recibe como máximo dos revisiones: cuando la segunda sigue bloqueando,
  revisa el plan para responder a sus hallazgos, y el plan revisado pasa a implementing sin una
  tercera revisión; no despaches una.

**Divide la Epic en ítems hijos y repártelos.** Esta es la única excepción a "una sesión crea un
ítem del Board solo cuando el mensaje de la persona se lo indica" y a "solo la persona asigna
ítems": la persona te asignó la Epic, y eso es la autoridad para dividirla. Después de que el plan
revisado haya llevado la Epic a `implementing`, cuando otras Sessions puedan hacer mejor algunas
partes, crea ítems Feature o Issue bajo ella y asígnalos:

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | description on stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- Los ids de terminal están en la libreta de direcciones de sesiones, `GET /v1/orchestrator/sessions`
  (`clawdline guide
  send`); la Session debe trabajar en el Project de la Epic. Puedes asignarte un hijo a ti mismo, y
  `--assign-new` abre una Session nueva con una Root Assignment que nombra la Epic. Sin un flag
  `--assign`, el hijo queda sin asignar hasta que la persona lo asigne.
- `item child` lee la Epic para obtener su versión, imprime su Idempotency-Key (`--key` reintenta
  la misma escritura) e imprime el hijo. Es `POST /v1/work/v2/agent/items/<epic id>/children` con
  `{"expected_version", "session_id", "kind", "title", "description", "steps"?, "deployment_policy"?,
  "assign"?: {"mode": "existing_session", "terminal_id"} | {"mode": "new_session", "assistant"?,
  "model"?, "persona"?}}`, y se responde `201` con `{"item", "assigned", "assignment_error"?: {"code", "message"}}`.
  El hijo está en el Project de la Epic, lleva `parent_id` (la Epic), y su tarjeta dice que lo creó
  la Session dueña de la Epic. Sus pasos son tus filas `--step` o, si no das ninguna, la lista de su
  descripción una vez asignado.
- El hijo se crea primero y se asigna después. Cuando la asignación falla, el hijo **se queda, sin
  asignar**, la respuesta trae `assignment_error` con el código de la asignación
  (`session_unavailable`, `project_mismatch`, `assignment_failed`, …), y el comando sale con 1:
  asígnalo otra vez con `item assign`, o déjalo para la persona.
- `item assign` es `POST /v1/work/v2/agent/items/<child id>/assign` con `{"expected_version",
  "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}`; mueve un hijo
  abierto de tu Epic a otra Session, la misma asignación que hace la elección de una persona.
- Rechazos, ninguno de los cuales escribe nada: `not_epic_owner` (no eres el dueño de la Epic),
  `parent_not_epic` (el padre no es una Epic), `epic_not_planned` (la Epic sigue antes de
  `implementing`: los hijos salen de un plan revisado), `item_terminal` (la Epic está terminada),
  `child_kind_not_allowed` (solo `feature` o `issue`), `epic_children_full` (una Epic contiene como
  máximo 32 hijos, abiertos o cerrados), `not_epic_child` (`item assign` de un ítem que no es hijo
  de ninguna Epic: lo asigna la persona, salvo que su mensaje te pida hacerlo:
  `clawdline guide board`), `invalid_assignment`, `version_conflict`, `persona_not_applicable`
  (422: un perfil de persona con una Session existente) y `unknown_persona` (400: un id que no está
  en el catálogo).
- **Una persona (perfil de persona)** es un rol con el que se lanza una Session nueva: texto añadido
  a su prompt de sistema que hace que trabaje como trabaja ese rol, durante toda la conversación.
  Es solo para una Session nueva (`--assign-new`, `--new`, `dispatch`); una Session existente
  conserva la que tenía al abrirse. Ninguna por defecto. Una persona nunca anula
  `CLAUDE.md`/`AGENTS.md`, el brief, `CHILD.md` ni este protocolo. `GET /v1/personas` las lista; los
  ids (`teams` en cada una lista todos los equipos en los que está una persona, y puede estar en
  varios):
  - `architect`: planificar una Epic;
  - `backend`: una funcionalidad del daemon, de la API o del almacén;
  - `frontend`: una funcionalidad de la consola o de la maquetación para teléfono;
  - `minimal-change`: una issue; el arreglo más pequeño que se sostenga;
  - `code-reviewer`: children de revisión y de `plan_review`;
  - `reality-checker`: verificación; evidencia antes de "funciona";
  - `security`: trabajo que toca permisos, emparejamiento o Cloud;
  - `technical-writer`: documentación y guías;
  - marketing, para un blog, un sitio o un repositorio de documentación: `seo` (páginas y
    metadatos), `content-writer` (artículos redactados en archivos), `ai-search` (páginas que los
    motores de respuesta de IA pueden citar), `social-media`, `instagram`, `email` (boletines),
    `growth` (experimentos medidos) y `pr` (anuncios).
  - producto, calidad y operaciones: `product-manager`, `sprint-prioritizer`, `feedback-synthesizer`,
    `trend-researcher`, `ux-researcher`; `test-automation`, `accessibility`, `performance`,
    `api-tester`, `evidence-collector` (dictamina PASS o FAIL por cada afirmación a partir de
    pruebas capturadas); `sre`, `devops`, `incident-commander`, `finops` y `secrets`.
  - diseño y negocio: `ui-designer` (pantallas en el sistema de diseño del proyecto), `ux-architect`
    (flujos y estructura de la maquetación), `brand-guardian` (coherencia de marca),
    `ui-finish-gate` (la comprobación visual antes de publicar), `image-prompt` (prompts de
    generación de imágenes), `pricing`, `customer-success`, `support` (respuestas redactadas),
    `analytics` (respuestas a partir de datos reales), `devrel` (ejemplos que funcionan) y
    `privacy` (comprobaciones de datos personales; no es asesoramiento legal).
  - `zero-review-lead`: es dueño de una Epic de revisión que vuelve a examinar desde cero una
    funcionalidad o un proceso existentes: planifica los enfoques por rol, los despacha como
    children revisores de solo lectura con un único paquete de hechos compartido, y convierte su
    evidencia en un diseño objetivo; su skill es `zero-based-review`.
- **Añade una revisión independiente de UX/producto cuando la Epic cambie una experiencia que ven
  las personas.** En el plan, clasifica si la Epic cambia una interfaz de cara a las personas, un
  recorrido de usuario o una política de producto. Si lo hace, antes del merge despacha al menos un
  child especialista de solo lectura, usando `ux-architect` por defecto para la maquetación, la
  interacción y el flujo de producto de extremo a extremo:

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

  Su brief nombra el candidato integrado y pide evidencia en escritorio y en el móvil más pequeño
  admitido, el comportamiento con teclado y con lector de pantalla, los callejones sin salida, el
  encaje con el producto, la severidad y una recomendación concreta. Debe **marcar como no
  verificado y decir por qué** donde no haya evidencia disponible. Usa `product-manager` en su
  lugar cuando el riesgo dominante sea la política y el alcance, no la maquetación; añade
  `ui-finish-gate` cuando una pasada visual previa a la publicación sea relevante. Resuelve cada
  hallazgo bloqueante y registra el id de la tarea, el veredicto y la disposición en la evidencia de
  verificación o en el informe de finalización de la Epic. Si no hay impacto de cara a las
  personas, di por qué en el plan y no añadas ceremonia de revisión.
  Registra el alcance que cubrió esta revisión. Normalmente, despacha cada especialista pertinente
  una sola vez para la Epic integrada; no envíes por rutina UX, marca, seguridad y otros roles como
  una lista de verificación. Cierra tú mismo, con comprobaciones enfocadas, las correcciones
  pequeñas de textos, espaciado, pruebas o hallazgos dentro del alcance. Vuelve a despachar solo
  cuando un cambio posterior altere de forma sustancial el recorrido de usuario, la política de
  producto, la dirección de marca, el límite de seguridad u otro riesgo fuera de ese alcance
  registrado; nombra el límite que cambió y pide solo su especialista pertinente. Esta revisión
  nunca sustituye a la compuerta del plan capturada ni al PASS del verificador del candidato exacto
  de la compuerta de verificación.
- **Sigues siendo responsable de la Epic después del merge de cada hijo.** Vuelve a leer de
  inmediato el ítem de ese hijo y `clawdline item steps <child id>`; comprueba que todos los pasos
  estén completos. Un merge no cierra el hijo, y `merging` no es un estado de reposo. La Session
  dueña del hijo debe completar los pasos que queden, registrar un recibo de landing para el commit
  exacto que ya es alcanzable desde el destino local y desde `origin/main`, y luego avanzar
  `deploying` → `done` con evidencia de despliegue o un motivo de no despliegue que se ajuste a su
  política de despliegue. Si el hijo es tuyo, haz tú mismo esas acciones. Si es de otra Session,
  haz el seguimiento con ese dueño o usa la vía autorizada de reasignación de hijos; no suplantes a
  su dueño (`not_item_owner`). Acusa recibo del aviso de finalización del broker cuando exista, y
  luego clasifica los restos del worktree y elimina solo el material del que se haya demostrado que es
  idéntico a lo que ya hizo landing o que es temporal de la tarea. Conserva los bytes sin landing, mixtos o
  desconocidos para el siguiente dueño. No declares done la Epic padre hasta que todos los hijos
  estén `done` o `cancelled`: `epic_children_open` dice cuántos quedan. No crees ningún ítem del
  Board aparte de los hijos de la Epic.
- **Un hijo completado no cierra su Session de Feature Root independiente.** Para cada Root abierto
  por la Epic con `--assign-new`, usa `clawdline session close --dry-run --terminal <id>` (responde
  `closing as epic_owner` solo para un Root que abrió esta Epic) para leer su `closeability`; no
  deduzcas la propiedad a partir de una etiqueta o de la posición de una terminal,
  y no tomes `clawdline session report` como un cierre. Después de que el hijo llegue a `done`,
  pide al dueño de ese Root que audite sus propias tareas, landings, avisos, tareas pendientes y
  worktree, y que luego complete su informe de cierre. Obtén una atestación solo a través de una
  ruta que admita el daemon actual; la ruta de cierre retirada de Swift no es una de ellas. Si esa
  ruta o un cierre protegido no están disponibles, registra el bloqueo del producto y el siguiente
  dueño, y conserva la Session. Solo cuando la
  identidad y el trabajo estén verificados y
  `closeability.state=safe` puede `clawdline session close --terminal <id>` terminarla; antes vuelve
  a leer el inventario, y una segunda ejecución responde `session_not_found` una vez que ya no
  existe. No te saltes la protección con `clawdline close <terminal id>`. Haz el seguimiento de
  `blocked` con el responsable que nombra. Para `unknown` (incluido `terminal_unreadable`), conserva
  la Session y registra la evidencia que falta y el siguiente dueño; no la cierres a la fuerza, no la
  archives ni afirmes que quedó despejada. Antes de declarar terminada la coordinación de la Epic,
  enumera el resultado de cierre de cada Root o el bloqueo que lo impide. Un `done` en el Board no
  sustituye a este inventario.

## 11. Coordinación

**El coordinador de la máquina ("Clawdfather").** Trabaja desde un espacio de trabajo de la máquina propiedad del daemon, fuera de los Projects, para informar sobre las Sessions y gestionar las operaciones de máquina admitidas. Nunca edita el código fuente de un Project, Clawdline incluido. Ante una petición explícita de la persona de trabajo de ingeniería, crea primero un ítem del Board del Project con `clawdline item add --project … --assign-new` y luego delega en una Session del Project. Sin esa petición, propón un ítem para que la persona lo acepte. El dueño asignado del Project se encarga del despacho de children, la verificación y el landing. El espacio de trabajo es un límite organizativo, no un sandbox del sistema de archivos. Los vínculos nuevos deben venir de ese espacio de trabajo; los existentes siguen siendo legibles. Abre la Session desde la acción Clawdfather separada de la consola y luego registra su ID de conversación. Consulta `docs/clawdfather-role.md` para el límite del producto.
Ejecuta `clawdline coordinator bind` dentro de esa Session nueva para registrarla o para volver a vincular un predecesor del que se haya demostrado que está desconectado. El comando lee su propio ID de conversación y se niega a sustituir a un titular que esté conectado o que no se pueda leer.
`GET /v1/orchestrator/coordinator` inspecciona el rol;
`/coordinator/bearings` es la máquina de un vistazo (tareas activas, landings pendientes, esperas
abiertas, mensajes muertos, leases retenidos y lo que es `unknown`). `POST …/coordinator/register`
con `{"session_id": "<conversation id>"}` toma el rol; `POST …/coordinator/rebind` lo mueve una
vez que la sesión vinculada está desconectada (`expected_coordinator_id`, `expected_generation`).
La sucesión responde `501 succession_unavailable`.

**Esperas de archivos.** Una espera dice "avísame cuando el dueño haya terminado con estas rutas".
Es un registro y un mensaje, no un bloqueo ni una vigilancia de archivos.

- `POST /v1/orchestrator/waits` —
  `{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}`
  (los ids de sesión son ids de conversación). Se avisa al dueño una vez, en su compositor.
- El dueño la termina: `POST /v1/orchestrator/waits/<id>/release` con `{"owner_session_id",
  "commit"?, "note"?}`; se avisa a cada uno de los que esperan. Nada libera una espera por
  temporizador.
- Quien espera se retira: `POST …/waits/<id>/cancel` con `{"waiter_session_id"}`.
- `409 owner_busy` y `502 request_delivery_failed` significan que **la espera se registró** pero
  todavía no se avisó al dueño. `502 release_incomplete` lista quién sigue pendiente: envía otra vez
  la liberación.

**Leases.** Dos recursos: `heavy_compile` (el único slot de compilación de la máquina) y `landing`
(uno por checkout).

**Ejecuta un build o una batería de pruebas a través de `clawdline heavy -- <command>`**, no a
pelo. Se pone en cola para `heavy_compile`, espera a que la máquina tenga memoria disponible (una
cuarta parte, 1 GB como máximo, y sin atascos de memoria por encima del 10%), ejecuta el comando con
una prioridad más baja —en Linux, además, como lo primero que mata el kernel si se queda sin
memoria—, renueva el lease mientras se ejecuta y lo libera después. Conserva el
estado de salida del comando. Nunca se niega a compilar por la falta del daemon o por un rechazo que
no conoce: ejecuta el comando de todos modos con una frase en stderr. Cuando pasa el `--max-wait`
(30m por defecto) antes de tener el slot y la memoria, cede su lugar, no ejecuta el comando y sale
con **75**, un código que no se confunde con el fallo propio de ningún comando; ejecútalo otra vez
más tarde. Mientras espera imprime una línea cuando empieza la espera y otra cuando termina, nada
entre medias: espéralo una sola vez, con una espera larga (§2, "Esperar un comando largo"). Un
`heavy` dentro de un `heavy` se ejecuta directamente. `--min-available 1500M` pide más; `--no-slot`
solo comprueba la memoria. En un repositorio
que lo tenga, `tools/heavy.sh <command>` encuentra el binario por ti.

- `POST /v1/orchestrator/leases` —
  `{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`.
  Responde `granted`, o `queued` con una `position` y `retry_after_seconds`. Una petición en cola
  vuelve a preguntar con el mismo `request_id`.
- `POST …/leases/renew | release | cancel` con `{"request_id", "resource", "checkout"}`. El titular
  renueva con `/renew` (volver a preguntar a `POST /v1/orchestrator/leases` con su propio
  `request_id` también renueva).
- Renueva en menos de 60 segundos o el lease se da por perdido. `409 lease_lost` significa que se
  perdió. `429 queue_full` con 32 en espera.

**Los grafos** (`GET /v1/orchestrator/graphs`) son vistas de solo lectura calculadas a partir de los
campos `graph` de las tareas despachadas. **Reclaim** (`/v1/orchestrator/reclaim`) barre los
checkouts terminados; un POST es una ejecución de prueba salvo que el cuerpo diga
`{"dry_run": false}`.

## 12. Qué hacer cuando algo se rechaza

- Ramifica según `error.code` (o `error` en la forma plana). El mensaje es para las personas.
- `retry_after` significa que es una respuesta de capacidad: espera ese tiempo y luego envía la
  misma petición.
- `409 stale_write`, `503 orchestrator_store_busy`: el almacén estaba ocupado; volver a enviar la
  misma petición es seguro.
- `unknown` en cualquier sitio —una propiedad, una señal de vida, un origen— significa que el daemon
  no pudo leerlo. No significa "ausente", y no se debe borrar nada ni declarar nada muerto a partir
  de eso.
- Si una ruta que esperabas responde `404 not_found` o `501`, no está en este daemon. Dilo; no
  recurras en su lugar a las rutas de la app Swift ni a subagentes nativos del proveedor.
