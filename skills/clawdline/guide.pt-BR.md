# Guia do Clawdline

Para uma sessão de assistente — Claude Code ou Codex — em uma máquina onde **Clawdline Next** é executado. Ele cobre o que esse daemon serve hoje, e nada mais: cada rota abaixo é registrada pela compilação que imprimiu este guia, e um teste falha quando não o é. Imprima novamente com `clawdline guide` em vez de confiar em uma cópia; `clawdline guide zh-Hant` mostra o guia em chinês tradicional de Taiwan (`zh-TW` continua sendo um alias). `clawdline guide` imprime o núcleo e nomeia as outras partes; imprima uma parte (`clawdline guide pt-BR dispatch`) quando chegar ao trabalho que ela cobre, ou `clawdline guide pt-BR all` para o texto completo. O que quer que seja impresso, incluindo o núcleo, começa com `guide-version: <sha256>`; execute o mesmo comando com `--since <hash>` e, quando o texto permanecer inalterado, ele imprimirá uma linha `unchanged <hash>`. `clawdline guide pt-BR refused <code>` imprime a parte que explica um código de recusa e sai de 1 sem nada no stdout quando nenhuma parte o nomeia.

Neste guia, uma **etapa** é uma entrada da lista de verificação em um item, as **gravações** de uma tarefa são os caminhos que ela pode alterar, a **atribuição** é quem possui um item e uma **parte** é uma parte nomeada deste guia.

## 0. Se você aprendeu Clawdline com o aplicativo Swift, leia isto primeiro

O aplicativo Swift foi descontinuado em 19/09/2026: ele foi interrompido, não inicia mais no login e nada responde à porta 7717. Seu diretório, `~/.config/clawdline`, ainda está no disco e ainda é lido - apenas lido - para o histórico que este daemon nunca manteve. Este daemon não é uma cópia desse aplicativo, e cinco diferenças costumam causar erros:

1. **Os diretórios de tarefas são `<state dir>/tasks`, não `/tmp/.clawdline`.** `/tmp/.clawdline` era do broker Swift; dois brokers escrevendo no mesmo diretório de IDs de tarefas teriam colidido onde ninguém olha. Não fixe nenhum desses caminhos no código: leia `task_root` do inventário (§3) e escreva `task.json` abaixo dele.
2. **Não há envelope de fluxo de trabalho nem rota de fluxo de trabalho para chamar.** As mensagens não carregam mais uma classificação de Board e uma sessão nunca abre cartões de Board sozinha. `POST /v1/orchestrator/sessions/<terminal>/workflow` ainda existe apenas para que um auxiliar antigo não falhe no meio do turno: ele responde `workflow_retired`, não registra nada e o novo código não deve chamá-lo.
3. **A participação do Board passa por propostas e decisões** (§10): uma sessão propõe, uma pessoa responde. Não há nada em `begin` ou `deliver`.
4. **Uma porta diferente.** Porta 7727 (ou `CLAWDLINE_NEXT_PORT`), estado em `~/.config/clawdline-next` (ou `CLAWDLINE_NEXT_DIR`). Nunca leia `~/.config/clawdline`: seu token não é deste daemon e é recusado com `401 unauthorized`.
5. **Coisas que o aplicativo Swift tinha e este daemon não:** promoção de relatório durável (respostas `501 durable_report_promotion_unsupported`), sucessão de coordenador (respostas `501 succession_unavailable`) e os campos breves `serialize` e `attach_session` (cada um recusado pelo nome como `bad_task`). `reasoning_effort` é suportado: `high` ou `xhigh`, apenas em uma tarefa `codex`.

## 1. Sessão raiz ou agente filho

Se sua primeira mensagem dizia *"Você é um agente filho do Clawdline para a tarefa …"*, você é um **agente filho**. O `CHILD.md` que ele nomeia rege você: você não despacha, não envia um recibo de turno, assina com `clawdline task accept` e finaliza com `clawdline task finish`. Pare de ler aqui.

Caso contrário, você é uma **sessão raiz**: uma sessão comum com a qual uma pessoa está conversando. O resto é para você.

Se ela dizia *"Você é uma Feature Root independente do Clawdline …"*, ou um item do Board foi atribuído a você, imprima `clawdline guide pt-BR feature-root` a seguir: é todo o caminho comum desde a leitura do item até `done` e indica qual parte imprimir para os casos menos comuns.

## 2. Acessando o daemon

**Use os comandos, e não o curl criado manualmente, onde houver.** Eles leem a credencial dentro de seu próprio processo, para que ela nunca apareça em uma linha de comando, em `ps`, em sua saída ou em sua transcrição. Um curl criado à mão sem essa credencial responde `401 unauthorized` ("Nenhuma credencial válida acompanhou esta solicitação…"): é a credencial que está faltando, não uma permissão. Em vez disso, execute o comando.

| Comando | O que faz |
|---|---|
| `clawdline guide [lang]` | Este guia. Nenhum daemon necessário |
| `clawdline session report --summary "…"` | Registra seu turno concluído (§7) |
| `clawdline session close [--dry-run] [--terminal id]` | Audita e encerra uma Sessão finalizada, nunca à força (§2a) |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | Despacha um agente filho sob sua responsabilidade (§4) |
| `clawdline item show\|steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | Lê e avança um item do Board que você possui (`clawdline guide pt-BR feature-root`, §10) |
| `clawdline todo add\|list\|done` | Tarefas próprias desta Sessão, somente quando a pessoa solicitar (§10) |
| `clawdline heavy -- <command…>` | Executa um conjunto de construção ou teste no único slot de compilação da máquina (§11) |
| `clawdline send --to <terminal> "…"` | Retransmite uma mensagem para outra sessão (§8) |
| `clawdline notify --title "…" --body "…"` | Envia uma notificação para a pessoa (§9) |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | Deixa uma nota acionável acima de uma Sessão (§9a) |
| `clawdline assistants` | O que resta na conta de cada assistente |
| `clawdline landings` | Todas as integrações ainda pendentes nesta máquina; `--work-id <item id>`: cada integração registrada para um item do Board |
| `clawdline leases [--json]` | Quem detém o slot de compilação e cada reserva para integração e quem espera atrás deles |
| `clawdline sessions [--json]` | As sessões que um envio, uma espera ou um handoff podem nomear, com seu estado e tarefa |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | Quanto gastou uma sessão, tarefa filha ou item do Board, por categoria; seu por padrão |
| `clawdline cloud pair [--offer <code>]` | Emparelha um navegador Cloud com esta máquina |
| `clawdline task show [--json] <task id>` | Um resumo compacto de uma tarefa filha: estado, veredicto, resumo, títulos das pendências, verificação, integração, checkout (§5) |
| `clawdline task wait <task id>… [--timeout 9m] [--any]` | Aguarda até que os filhos terminem (todos, ou `--any` um), mostra cada um como `task show` faz e fecha seu aviso. Saída 0 todas bem-sucedidas, 1 uma falhou, 5 uma foi cancelada e nenhuma falhou, 3 expirou, 4 uma tarefa não pôde ser lida; 4 sobre 3 sobre 1 sobre 5 (§5) |
| `clawdline callback --title "…" [--timeout 30m] [--work-id <item>] -- <command…>` | Executa um comando longo — um deploy e sua verificação, uma espera no CI — sob o daemon e retorna imediatamente; termina seu turno e sua saída digita o mesmo `<clawdline-notice>` que um agente filho finalizado faz (§5a, `clawdline guide pt-BR callback`) |
| `clawdline task cancel <task id> --reason "…"` | Interrompe um agente filho que você despachou por engano: sua aba é fechada, suas escritas e slot são liberados, um branch com commits é mantido para você (§5) |
| `clawdline task ack <task id> <notice id>` | Fecha um aviso de conclusão manualmente; raramente necessário, já que `task show` e `task wait` fecham-no (§5) |
| `clawdline task accept <task dir>` | Um agente filho assinando seu briefing. Sessões raiz nunca executam esse comando |
| `clawdline task finish <task dir>` | Conclusão de um agente filho. Sessões raiz nunca executam esse comando |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | Inicia um agendamento através de seu webhook Cloud, em qualquer máquina, e aguarda seu resultado; o código de saída indica como terminou ("Agendar trabalhos futuros"). Nenhum daemon necessário |

Sem uma tag explícita do guia, o idioma da CLI segue `--lang <tag>` antes do comando, depois
`CLAWDLINE_LANG`, o `product_language` salvo e, por fim, o inglês. Um `clawdline guide <tag>`
explícito substitui essa escolha; uma tag sem suporte mostra o guia em inglês.
`clawdline guide -list` lista as nove tags publicadas. Essa preferência muda apenas o texto
legível da CLI, sem alterar os campos do protocolo nem o idioma de um Agent.

Os comandos de orquestração acima (não `webhook fire`) imprimem o JSON do daemon em caso de sucesso; em uma recusa, eles imprimem `refused, <status> <code>: <message>`, depois cada valor simples incluído na recusa como `key: value`, um por linha, e sua correção por último, e saem com código 1. Os comandos Cloud usam sua própria saída de sucesso e erro legível por humanos. `--port` substitui a porta.

`clawdline usage` é o livro-razão do token (`docs/token-ledger.md` no repositório): em que cada token foi gasto - `board`, `protocol`, `rules`, `impl`, `delegate`, `harness`, `talk`, `compaction`, `other`. Sem sinalizador, ele lê sua própria sessão, nomeada por `CLAUDE_CODE_SESSION_ID` ou `CODEX_THREAD_ID`. Ele imprime uma linha de cabeçalho (chamadas, contexto de pico, custo), uma linha por categoria por custo — compartilhamento, tokens, custo — e depois cada lacuna; `--json` imprime a resposta do daemon. `rules` é um limite superior e diz o seguinte: uma execução de guarda em um comando shell com outro trabalho leva todo esse comando. As rotas são `GET /v1/usage/sessions/<conversation>`, `GET /v1/usage/tasks/<task id>` e `GET /v1/usage/items/<item id>`, lidas com um dispositivo emparelhado ou o token do orquestrador. Uma sessão que o livro-razão ainda não leu, ou não pode mais ler, responde `not_yet_read`, `transcript_missing` ou `transcript_unreadable` — nunca um total vazio; um ID que ninguém conhece é 404 `unknown_session`, `unknown_task` ou `unknown_item`. O andamento da leitura do livro-razão aparece como é `usage` em `/v1/diagnostics`.

**Aguardando um comando longo.** `clawdline heavy`, `clawdline dispatch` e uma longa execução de teste não imprimem nada enquanto esperam e terminam por conta própria. Espere por um com **uma longa espera**, não verificando-o a cada poucos segundos: cada verificação é um turno que relê todo o seu contexto, e uma revisão de token contou 520 desses turnos (72,2 milhões de tokens) em dez itens, principalmente em execuções `heavy` enfileiradas.

- **Claude Code:** uma chamada Bash com um `timeout` longo (até `600000` ms) ou `run_in_background` e nada até que sua notificação de conclusão chegue. Não é um loop de `sleep` e `tail`.
- **Codex (codex-cli 0.157.1, modo de código):** coloque `// @exec: {"yield_time_ms": 600000}` na primeira linha da célula `functions.exec`. Depois que `exec_command` retornar um ID de sessão, aguarde `write_stdin` com `chars` e `yield_time_ms: 300000` vazios; se ainda funcionar, repita dentro da mesma célula. Medido: a célula externa permaneceu aberta por 330 segundos com `600000`, enquanto uma `write_stdin` vazia esperou até 300 segundos. Se a célula externa ceder, use `wait` com um `yield_time_ms` longo para coletá-la.

  Use uma célula para uma espera filho ou uma construção. Substitua apenas o comando; mantenha a sondagem da sessão dentro da célula para que um retorno `exec_command` normal de 30 segundos não desperte o agente para emitir a mesma espera novamente:

  ```js
  // @exec: {"yield_time_ms": 600000}
  let r = await tools.exec_command({cmd: "clawdline task wait --timeout 9m TASK_ID", yield_time_ms: 30000});
  while (r.session_id) {
    r = await tools.write_stdin({session_id: r.session_id, chars: "", yield_time_ms: 300000});
  }
  text(r.output);
  text(`exit ${r.exit_code}`);
  ```

  Para uma compilação, substitua o comando por `tools/heavy.sh …` e retenha seu código de saída: 75 significa que o slot de compilação ou a espera de memória expirou antes da execução da compilação.

`clawdline heavy` espera no máximo `--max-wait` (padrão 30m) e então sai de 75 sem executar o comando; uma espera mais longa do que a sua ferramenta permite é uma execução em segundo plano.

**Curl para uma rota do orquestrador.** Leia `<state dir>/orchestrator-token` e envie-o no cabeçalho `X-Clawdline-Orchestrator`. Mantenha o token fora dos argumentos de comando: use `DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"` e depois `-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")`. Utilize `curl --fail-with-body`; todo POST com corpo JSON também precisa de `-H 'Content-Type: application/json'` (caso contrário, `415 unsupported_media_type`).

### Emparelhar um navegador na nuvem

O emparelhamento altera quem pode ler esta máquina. Um navegador emparelhado pode lê-lo imediatamente e, quando o Cloud `commands` estiver ativado, pode conduzi-lo. Execute um comando de emparelhamento somente quando a pessoa solicitar explicitamente o emparelhamento desse navegador ou fornecer o comando ou oferta de emparelhamento exato. O emparelhamento não ativa os comandos; isso continua sendo uma configuração separada.

Existem duas direções suportadas:

1. **O navegador mostra uma oferta.** Execute a linha exata fornecida na máquina:

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

   Mantenha as aspas simples. A oferta é um segredo opaco, de curta duração e de uso único: não decodifique, edite, armazene ou repita na resposta final. Se expirar ou já tiver sido usado, receba uma nova oferta do navegador em vez de tentar novamente ou modificá-la.
2. **A máquina faz o convite.** Execute `clawdline cloud pair`. Ele imprime um link `https://app.clawdline.com/#pair=…` único e aguarda. A pessoa abre o link completo no navegador que deseja emparelhar, enquanto está conectada à mesma conta Clawdline Cloud. Trate o link como uma oferta: não o publique nem guarde.

O sucesso imprime três linhas: `paired` nomeia o ID do dispositivo do navegador, `browser` é a impressão digital do navegador e `machine` é a impressão digital da máquina. Compare a impressão digital do navegador com a mostrada no navegador e a impressão digital da máquina com a mostrada para esta máquina. Uma incompatibilidade não é bem-sucedida: execute imediatamente `clawdline cloud revoke <device-id>` usando o ID `paired` e relate a incompatibilidade. `clawdline cloud devices` lista a lista atual do navegador e seu status de confiança local; é também a verificação somente leitura para usar após o emparelhamento.

Esses comandos passam pelo daemon local em execução. Se um falhar, relate seu stderr exato. Não ative a nuvem, faça login, ative comandos, gire chaves ou substitua a oferta fornecida, a menos que a pessoa solicite separadamente essa alteração.

**Onde estão as coisas.**

- Porta: `CLAWDLINE_NEXT_PORT`, caso contrário **7727**. Apenas loopback: `http://127.0.0.1:<port>`.
- Diretório de estado: `CLAWDLINE_NEXT_DIR`, senão `$XDG_CONFIG_HOME/clawdline-next`, senão `~/.config/clawdline-next` (`%APPDATA%\clawdline-next` no Windows).
- `GET /v1/health` não precisa de credencial e responde `served_by: "clawdline-go"`. Use-o para diferenciar "não executando" de "recusado".

**Credenciais.** Existem três, e uma sessão usa a primeira:

| Credencial | Onde | Enviado como | Abre |
|---|---|---|---|
| Token do orquestrador | `<state dir>/orchestrator-token` | cabeçalho `X-Clawdline-Orchestrator` | Tudo em `/v1/orchestrator/`, `/v1/work/`, `/v1/board`, `GET /v1/places`, `POST /v1/artifacts/images` |
| Segredo da tarefa | escolhido pela raiz no despacho | cabeçalho `X-Clawdline-Task-Secret` | Rotas próprias de um agente filho sob `/v1/orchestrator/tasks/<id>/` e `POST /v1/orchestrator/proposals` |
| Token do dispositivo | `<state dir>/local-token` ou um dispositivo emparelhado | `Authorization: Bearer` | As rotas do console (`/v1/sessions/…`). Uma sessão não precisa disso |

O token do orquestrador enviado como `Bearer` é comparado com dispositivos e recusado. Um errado ou ausente recebe `401 unauthorized`, que diz: o token está faltando ou não é deste daemon, ou o dispositivo não está emparelhado. `clawdline doctor` imprime o diretório e a porta que a CLI lê.

**Quando você deve usar curl**, mantenha o token fora da linha de comando:

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

- `--fail-with-body`: sem isso, uma recusa sai 0 e é lida como sucesso.
- **Todo POST com corpo precisa de `-H 'Content-Type: application/json'`** ou é recusado com `415 unsupported_media_type`. `curl -d` envia sozinho um tipo de formulário.
- Os corpos são limitados a 2 MiB, a menos que uma rota diga menos.
- Um ID de terminal tmux como `%47` entra em um caminho com escape como um segmento: `%2547`.

**As recusas vêm em duas formas.** Ramificação no código, nunca na frase:

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}` — o mecanismo de admissão e o broker. Extras como `retry_after` ficam dentro do `error`.
- `{"error":"<code>","detail":"…"}` — erros de rota, métodos errados e algumas leituras.

Uma rota que este daemon não possui é recusada como `501 not_implemented` e a recusa nomeia a rota. Essa é a resposta em qualquer máquina comum. Ele só é encaminhado quando alguém deliberadamente coloca outro daemon atrás deste com `CLAWDLINE_NEXT_UPSTREAM_PORT`, e então um `502 upstream_unreachable` nomeia o endereço que não respondeu. Nem é uma resposta deste daemon. Antes de 19/09/2026, o encaminhamento estava ativado por padrão e ia para o aplicativo Swift em 7717, portanto, uma nota escrita então dirá que uma rota sem proprietário chega a esse aplicativo; isso não acontece.

### Configurar o display Clawdline de um projeto

Utilize esta parte quando a pessoa solicitar que você torne legível o Projeto no qual está trabalhando em Clawdline. O resultado não é “existem alguns arquivos”; é que o projeto tem um nome e uma marca verdadeiros, o trabalho de longa duração pode relatar o progresso e seus servidores de desenvolvimento podem ser vistos sem que Clawdline os inicie.

Comece lendo as instruções deste repositório, README, scripts de implantação/construção e configuração existente do gerenciador de processos. Preserva os comandos que o Projeto já utiliza. Não adicione um segundo caminho de implantação ou supervisor de processo apenas para Clawdline e não inicie, pare, reinicie ou implante nada, a menos que a pessoa solicite essa alteração operacional. A configuração e uma implantação real são trabalhos diferentes.

Execute estas quatro verificações, ignorando uma verificação somente quando ela realmente não se aplicar:

1. **Projeto.** Execute `clawdline project list`. Se este checkout estiver ausente, adicione sua raiz de repositório com `clawdline project add <absolute-root>` e liste novamente. Registra um local a partir do qual uma Sessão pode começar; isso não altera o repositório.
2. **Nome e ícone.** Clawdline deriva um ícone estável quando nenhum está configurado. Se o usuário precisar de um nome deliberado ou marca de pixel, preserve todas as outras entradas em `~/.claude/project-icons.json` e edite apenas o caminho mais longo para este projeto. O formato está documentado no `docs/project-status.md` do repositório Clawdline; a página Projetos também pode copiar um ícone resolvido existente sem editar manualmente o JSON. Um arquivo de usuário global não é conteúdo de repositório: mostre a entrada proposta exata antes de alterá-la quando a solicitação ainda não autorizou essa edição.
3. **Implantação e trabalho prolongado.** Clawdline lê apenas recibos de status; ele nunca executa uma implantação. Para um repositório GitHub, um recibo de implantação é `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`, onde o proprietário e o repositório vêm de `origin`. O produtor que já conhece a execução escreve `state` (`running`, `ok`, `fail` ou `none`), `label`, `url`, `started_at` e um `typical_seconds` medido, atomicamente. Para um comando local de construção, teste, importação ou implementação, use `clawdline-progress run --label <label> -- <command>` quando esse auxiliar existir ou implemente o contrato `run-<path>.json` de `docs/project-status.md`. Nunca invente uma duração; omita-o até que tenha sido medido. Um produtor morto não deve deixar um estado de funcionamento permanente.
4. **Servidores de desenvolvimento.** Adicione ou atualize `.devstack.json` na raiz implementável mais próxima. O daemon Go atualmente lê o `processes` declarado e testa seu loopback `port` ou abre seu `url`; ele **não** executa os comandos `status`, `up`, `down`, `restart` ou `logs` do navegador. Prefira o menor arquivo verdadeiro de Nível 0, por exemplo:

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

   Um processo sem porta ou URL estável não deve ser adivinhado no arquivo. Não sonde nem reinicie a produção ao verificar uma declaração de desenvolvimento.

Verifique cada camada alterada separadamente: `clawdline project list` nomeia o checkout; cada arquivo JSON é analisado; os próprios testes do repositório para scripts alterados são aprovados; `GET /v1/devstacks` mostra servidores declarados como em execução, parados ou desconhecidos, em vez de omiti-los silenciosamente; e uma Sessão no Projeto mostra um novo recibo de progresso/implantação. Se uma leitura não estiver disponível, estiver malformada ou obsoleta, diga qual e deixe-a desconhecida – nunca relate a ausência como sucesso. Termine listando o que foi configurado, o que não foi intencionalmente aplicável e qualquer arquivo de propriedade do usuário alterado fora do git.

**Unificar: um conjunto de regras e habilidades para Claude e Codex** (`/clawdline unify`). Codex lê `AGENTS.md` e `.agents/skills/<name>/`; Claude lê `CLAUDE.md` (e `AGENTS.md` somente quando `CLAUDE.md` está ausente ou tem uma linha `@AGENTS.md`) e `.claude/skills/<name>/`. Um projeto é unificado quando suas regras estão em `AGENTS.md` com `CLAUDE.md` ausente ou importando-o, e cada habilidade reside em `.agents/skills/<name>/` com `.claude/skills/<name>` um link relativo a ele. Quando a pessoa pede isso, ou invoca `/clawdline unify`:

1. Execute `clawdline project unify` no Projeto da Sessão (o git de nível superior; adicione o Projeto com `clawdline project add` primeiro se não estiver listado). Isso não muda nada. Mostre à pessoa, em seu idioma, o que Claude e Codex leram agora e lerão depois, cada linha de habilidade, cada frase de ação e cada conflito - incluindo as linhas `CLAUDE.md` que Codex não vê.
2. Execute `clawdline project unify --apply` somente depois que a própria mensagem da pessoa nesta conversa aprovar esse plano. Envia a versão que você mostrou; se o disco tiver sido alterado desde então, ele responderá `plan_changed` e não aplicará nada – imprima o plano novamente e pergunte novamente.
3. Mostrar o resultado de `clawdline project unify --check` (saída 0 unificada, 1 à deriva, 3 desconhecida). Nada está comprometido; diga quais arquivos foram alterados para que a pessoa ou uma sessão os confirme.

Nunca resolva um conflito editando `AGENTS.md`, `CLAUDE.md` ou uma habilidade sem que a mensagem da pessoa diga isso: uma habilidade que difere entre os dois diretórios, um link apontando para outro lugar ou regras que Codex não vê são deles para decidir. As rotas são `GET /v1/projects/{place}/unify` (o plano) e `POST /v1/projects/{place}/unify` com `{"version"}` e `Idempotency-Key`; as recusas são `plan_changed`, `plan_unknown` (parte do Projeto não pôde ser lida, então nada mudou) e `name_taken` (um nome que a unificação criaria já existe; nada é substituído).

## 2a. O fluxo habitual de uma Feature Root

Isso é tudo que uma Feature Root comum — uma sessão que possui um item do Board — executa, em ordem. Cada etapa é um comando: os comandos carregam a credencial e um curl criado manualmente para a mesma rota é recusado. Qualquer coisa mais rara está a um `clawdline guide <part>` de distância; os ponteiros estão no final.

**1. Leia o item.** `clawdline item show <item id>` imprime seu tipo, fase, critérios de aceitação, regras capturadas, a versão de aceitação (`acceptance vN`), para um recurso de revisão independente das Necessidades da pessoa, suas etapas e cada documento com seu corpo. Esse é o registro a partir do qual você trabalha. `clawdline item show <item id> --doc <doc id>` imprime apenas o corpo de um documento, para canalizar para um arquivo; `clawdline item steps <item id>` é o mesmo registro sem os corpos. Cada gravação de item imprime `wrote …; item <id> is at version N`, um breve resumo do item e uma dica `item show`. Leia a aceitação completa, etapas e documentos com `item show`. As gravações atuam na versão atual do item, a menos que você passe `--expected-version`.

Se seu ASSIGNMENT.md tiver um título **HANDOFF**, você está assumindo um item que outra Sessão deixou no meio do andamento (reatribuído após o início da implementação, antes de terminar). Leia o pacote que ele nomeia primeiro, antes de planejar. O daemon o construiu a partir de seus próprios registros e git, sem perguntar ao proprietário anterior: as tarefas vinculadas ao item e seus resultados, commits ainda não concluídos, as alterações não confirmadas de cada árvore de trabalho salvas como um patch com seu sha256 e o ​​comando `git apply` que o restaura em sua base, a última mensagem do proprietário anterior (ou por que não pôde ser lida) e as etapas de fase e abertura. Os patches ficam ao lado de ASSIGNMENT.md e duram mais que as árvores de trabalho. Continue a partir daí; não comece de novo.

**2. Nomeie sua Sessão**, caso ela tenha sido aberta para este item: `clawdline item name <item id> "<task name>"`, uma vez, após leitura do objetivo e escopo. Ele renomeia a Sessão, não o item.

**3. Antes de implementar.**

- Planejamento capturado e nenhum critério de aceitação: escreva aqueles observáveis com `clawdline item acceptance <item id> --body-file acceptance.md`.
- Precisa de uma revisão independente verificada ou de um Epic: o caminho do plano revisado em `clawdline guide pt-BR epic` vem primeiro. Desmarcado: sem plano, sem revisão, filho.
- Trabalho em vários estágios sem etapas: `clawdline item step-add <item id> "first" "second" …` (duas a oito etapas que você pode verificar uma de cada vez; uma única alteração não leva nenhuma).
- Então `clawdline item phase <item id> implementing`.

**4. Trabalhe nesta sessão por padrão.** Investigue, implemente, verifique e obtenha o recurso você mesmo. Envio somente quando uma necessidade concreta torna útil uma Sessão separada: trabalho paralelo genuinamente independente, ferramentas ou permissões diferentes ou revisão independente necessária. Diga o porquê antes de despachar; uma investigação ou implementação de rotina por si só não é uma razão.

Quando for necessário despacho, mantenha aqui a síntese, integração e integração:

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id` vincula o filho ao item, portanto, seu destino conta como o do item. Repita quando um agente filho fizer vários itens: o primeiro é a linha do agente filho, e a integração conta para cada um.
- O título é uma linha de no máximo 60 caracteres dizendo o que será diferente. Qualquer dois-pontos (`:` ou `：`) é recusado, porque une uma observação a uma explicação; assim como "o usuário" como assunto, ou uma abertura de título com um identificador formatado em código. Cada um responde `bad_task` com `title: …` dizendo qual.
- O documento é independente. Coloque nele os fatos que você já verificou, cada um com seu `file:line` ou o comando que o mostrou, para que o agente filho não os redescubra.
- Um briefing de agente filho de investigação ou Exploração também declara sua condição de parada — a pergunta que, uma vez respondida, encerra a tarefa — e um limite de turno.
- O trabalho somente leitura é `--claims ""`. Cada sinalizador e cada código de recusa estão em `clawdline guide pt-BR dispatch`.

**5. Se um filho terminar**, uma linha `<clawdline-notice>` será digitada em seu compositor. Execute `clawdline task show <task id>` e integre a entrega; lê-lo fecha o aviso, portanto não há ACK separado. **Após um despacho, termine seu turno**: o aviso acorda você, e um turno aberto para espera relê todo o seu contexto em cada enquete. Somente quando não houver mais nada a fazer e você precisar bloquear, execute `clawdline task wait <task id>…` (padrão `--timeout 9m`, `--any` para o primeiro). Integre um filho da árvore de trabalho **mesclando sua ramificação** no destino. **A mesclagem registra a integração por si só** dentro de alguns minutos: não publique uma integração manualmente. `clawdline landings` lista o que ainda é devido. Um filho despachado com `--claims ""` que não escreveu nada é registrado como `nothing_to_land` pelo broker. Qualquer outra coisa é `clawdline task land <task id> <state>` (`clawdline guide pt-BR landing`).

**6. Relatório de conclusão**, quando a causa foi encontrada, foi necessária uma investigação substancial (uma correção direta, cujo efeito foi observado não precisa de nenhuma). Adicione-o antes de `done`: uma vez concluído o item, ele não será atribuído e o relatório responderá `409 not_item_owner`.

```sh
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Escreva para quem relatou o problema, em Markdown, sem dados privados.

**7. Conclua o item.** Conclua cada etapa assim que for verificado com `clawdline item step-done <item id> <step id>`. Faça commit e push do trabalho direto de uma árvore de trabalho descartável; mesclar o branch de um filho quando um foi usado. Depois da integração, um comando leva o item a `done`. Para uma entrega de agente filho, o registro de integração fornece commit, destino e remoto; para trabalho direto, informe esses dados:

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --commit <sha> --target main --remote origin \
  --deployment "what went live, where, which version"      # omit landing flags for a landed child
```

Ou avance uma fase de cada vez:

```sh
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> deploying --no-landing-reason "why there is no code"    # work with no code
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

`done` leva `--deployment` ou `--no-deployment-reason` conforme diz a política de implantação do item. Quando `clawdline item steps <item id>` imprime uma linha sobre a regra de verificação, este item mantém o caminho mais longo para o qual a linha aponta.

**Aguardando uma implementação.** Não mantenha o turno aberto enquanto uma implementação é executada ou propagada. Inicie a implantação e seu teste como um callback, termine o turno e termine o item quando seu aviso chegar:

```sh
clawdline callback --title "The hosted console serves <sha>" --work-id <item id> --timeout 20m -- \
  sh -c './deploy.sh <sha> && tools/wait-hosted-console.sh <sha>'
# … the notice: clawdline task show <callback id>, then
clawdline item finish <item id> … --deployment "what went live, where, which version (callback <callback id>)"
```

**8. Relate o turno**: `clawdline session report --summary "…"` (§7).

**9. Deixe a Sessão proprietária aberta.** Concluir ou cancelar um item do Board libera sua atribuição; não encerra a sessão que o possuía. Após `session report`, deixe a Sessão disponível para trabalho de acompanhamento. Não execute `clawdline session close` apenas porque o item atingiu `done` ou `cancelled`. Uma pessoa pode pedir explicitamente para encerrar a Sessão mais tarde. O broker fecha separadamente uma guia filho despachada pelo agente após o término da tarefa desse filho, de acordo com a regra da guia filho em `clawdline guide child`.

**Quando algo é recusado.** `version_conflict`: execute o mesmo comando novamente; ele relê a versão. `steps_incomplete`: uma etapa ainda está aberta. Qualquer outro código: §12, depois a parte que o abrange.

**Trabalho mais raro, uma parte cada:** `clawdline guide pt-BR board` — propostas, decisões, tarefas, reabertura de item concluído, espera da pessoa, portões e recusa de todas as fases; `clawdline guide pt-BR epic` — planos, revisão do plano, itens filhos de uma Epic, personas; `clawdline guide pt-BR landing` — integração manual, transferências (uma longa transferência de marco do Root também), atribuições de Root; `clawdline guide pt-BR running` — filhos paralisados, sobras, reaparecimento.

## 3. Antes de despachar: veja o trabalho existente

O trabalho de outra sessão pode já estar fazendo o seu trabalho e é invisível na árvore compartilhada: uma entrega concluída em uma ramificação não mesclada aparece em nenhum `git status`. Leia primeiro.

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- Responde `generation`, `task_root` e quatro listas: `live`, `unlanded`, `droppable`, `unreadable`. Cada linha carrega um `do` que o daemon aceitaria. Com `claims`, cada linha ativa diz o que é `overlaps`.
- **`generation` é necessário para despacho** (§4). São 16 caracteres hexadecimais nos campos selados das linhas; ele se move quando uma linha inicia, termina ou altera as gravações.
- **`task_root` é para onde vai seu `task.json`.** É o campo próprio deste daemon; o broker Swift não tinha nenhum porque codificou `/tmp/.clawdline`.
- `400 bad_request` quando `project` não é um caminho absoluto dentro de um repositório Git.

Também vale a pena ler:

- `GET /v1/orchestrator/inflight?project=…` — cada linha de trabalho pendente no repositório, quem a possui e o que ela reivindica.
- `clawdline assistants` — por assistente: `availability` (`ok`, `low`, `exhausted`, `unknown`), `windows`, `stale`, `resets_at`. Escolha para quem enviar após lê-lo; nada recusa um despacho para cota.

**Deveria ser despachado?** O trabalho que se divide em partes independentes é mais rápido em paralelo. Uma cadeia onde cada passo depende do último se divide pior, porque cada transferência a quebra. Diagnóstico, trabalho menor que seu próprio briefing e qualquer coisa que alguém esteja esperando fica em sua própria sessão. As regras internas da máquina estão em `<state dir>/dispatch-policy.md` (e `dispatch-policy.local.md` da pessoa); cada agente filho os recebe em seu briefing.

## 4. Despachar um agente filho sob sua responsabilidade

Um agente filho sob sua responsabilidade é uma tarefa limitada sob sua responsabilidade. **Você mantém síntese, integração e landing.**

**Um comando executa todas as quatro etapas abaixo**, com o resumo em stdin ou em um arquivo:

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--persona <id>] [--work-id uuid …] [--task-id uuid] [--label "…"] [--project-dir D] < brief.md     # or --instructions-file brief.md
```

Ele faz o id e o segredo, lê o inventário para `generation` e `task_root`, escreve `task.json`, posta a tarefa e em um `stale_inventory` lê o inventário novamente e reenvia uma vez. Ele imprime `dispatched <id> <state> [worktree <path>]` e, em seguida, uma linha por aviso - o daemon e cada tarefa ativa cujas gravações se sobrepõem às suas. `--json` imprime a resposta do daemon. Uma recusa é `refused, <status> <code>: <message>` em stderr, depois seus extras e remediação uma linha cada e saem com código 1; a tabela no final desta parte diz o que cada código significa. A raiz é a sua conversa, de `CLAUDE_CODE_SESSION_ID` ou `CODEX_THREAD_ID`, caso contrário, `--conversation`; o assistente do agente filho é seu, a menos que `--assistant` indique o contrário; o projeto é o git de nível superior deste diretório, a menos que `--project-dir` diga o contrário. `--claims ""` declara um filho que não escreve nada. Enquanto o daemon abre a árvore de trabalho e a guia filho, ele não imprime nada; é um pedido que responde quando o agente filho existe, então espere uma vez (§2, “Aguardando um comando longo”). O segredo nunca está em argv, em `task.json` ou no que ele imprime, e o token é lido conforme cada comando fino o lê.

Para uma revisão que pode precisar de uma nova tentativa, escolha um UUID minúsculo antes da primeira chamada e passe-o como `--task-id` em cada tentativa. O comando mantém uma cópia privada da intenção de despacho original ao lado de `task.json`, para que uma nova tentativa idêntica possa ser reenviada mesmo depois que o daemon reescrever o briefing. O broker retorna o ID da tarefa original com `(replayed)`; um resumo alterado é recusado localmente. Se o comando expirar ou sua saída for perdida, verifique `GET /v1/orchestrator/tasks/<id>` antes de assumir a falha. Uma tarefa ausente pode ser repetida com o mesmo ID e brief. Uma recusa explícita não criou uma tarefa e também pode ser tentada novamente após a correção da sua causa.

`--persona <id>` lança o filho como uma persona integrada (`persona` em `task.json`); um ID que falta nesta compilação é recusado localmente. Nenhum tipo recebe um por padrão, `plan_review` incluído: nomeie `code-reviewer` você mesmo quando quiser. `GET /v1/personas` lista os ids que esta compilação carrega; o que é uma persona está na parte épica do §10 (`clawdline guide pt-BR epic`).

As etapas necessárias para um chamador sem o binário:

**1. Escolha um ID e um segredo.**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 characters, lowercase
SECRET=$(openssl rand -hex 32)          # 64 lowercase hex
```

O segredo vai de você para o daemon no corpo do POST, e do daemon para o filho na única linha que ele digita lá. Não está em `task.json` e nem na resposta do despacho, e você não precisa dele novamente. (Um reaparecimento é a única resposta que carrega um segredo: o novo de sua cópia.)

**2. Leia o inventário** (§3) para `generation` e `task_root`.

**3. Escreva `<task_root>/<TASK_ID>/task.json`.** O daemon lê o resumo deste arquivo, não da solicitação. Na admissão, ele o valida, reescreve `task.json` a partir do que admitiu e escreve o `CHILD.md` do agente filho a partir do mesmo registro - título, instruções, escritas, entregas, tipo e tempo limite incluídos - então a tarefa que o agente filho lê é aquela que foi validada e não lê `task.json`.

| Campo | Regra |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | o mesmo ID |
| `assistant` | `claude` ou `codex` |
| `project_dir` | caminho absoluto para um diretório existente |
| `title` | mostrado na tela: uma linha de no máximo 60 caracteres informando o que será diferente. Dois pontos (`:` ou `：`), "o usuário" como assunto ou um identificador formatado com código de abertura é recusado como `bad_task` (`title: …`) |
| `instructions` | necessário, no máximo 16 KiB. Eles devem permanecer por conta própria: o agente filho não sabe mais nada. Leve os fatos que você já verificou, cada um com seu `file:line` ou comando; um agente filho investigada também recebe uma condição de parada e um limite de giro |
| `claims` | **obrigatório**: no máximo 32 caminhos relativos que o filho pode escrever. `[]` significa que não escreve nada e é avisado (`claims_missing`) |
| `isolation` | `none` (padrão) ou `worktree` para um checkout privado em sua própria filial |
| `permission_mode` | `ask`, `edits` ou `full` |
| `timeout_minutes` | 1–240, padrão 30 |
| `kind`, `deliverables`, `model` | opcional; `model` é `[a-z0-9._-]`, no máximo 64 caracteres |
| `work_id` | UUID opcional do item do Board que serve |
| `persona` | ID de persona integrado opcional (`GET /v1/personas`); nenhum por padrão |
| `auto_compact_window` | opcional, apenas Claude: o tamanho do contexto em tokens (50000–1000000) no qual o filho compacta, ou `null` para nenhum. Ausente segue o `claude_auto_compact_window` da máquina, que fica desligado a menos que a pessoa o configure. Para comparação de execuções, não para resumos do dia a dia: uma compactação pode perder detalhes |
| `root` | **obrigatório**: `{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "project_dir": "<the same absolute repository path as project_dir>", "label": "…"}`. Uma raiz com escopo de função precisa de `root.project_dir` para que o daemon verifique o escopo do projeto. |

**Corresponda a superfície do trabalhador e o modo de inicialização a cada ferramenta que o agente filho deve usar antes de despachá-la.** O resumo nomeia as ferramentas necessárias e a raiz prova que a superfície escolhida as fornece. Um filho da CLI Codex não obtém o `@Browser` integrado do aplicativo de desktop ChatGPT apenas por meio de um sinalizador de permissão. Para uma revisão de UI, acessibilidade ou layout responsivo, direcione o trabalho para uma superfície que realmente tenha uso de navegador/computador ou nomeie um equipamento de navegador local equivalente à aceitação, como Playwright/Chrome CDP e prove que ele está instalado. Quando essa superfície pode solicitar acesso ao aplicativo, origem ou GUI, despache com `--permission-mode ask`: Codex `full` significa um lançamento de shell não interativo (`--ask-for-approval never`), nem todas as ferramentas, e a revisão automática não pode revisar uma solicitação que nunca é criada. No início da tarefa, o agente filho exercita cada ferramenta necessária e não apenas verifica o nome de um comando. Se um não estiver disponível, ele reporta imediatamente a lacuna exata e o root restaura o acesso ou reenvia. Ele não conclui uma verificação de aceitação dependente de ferramenta como não verificada porque o root escolheu um trabalhador incompatível.

**`root.session_id` é o seu ID de conversa, nunca um ID de terminal.** Claude Code o exporta como `CLAUDE_CODE_SESSION_ID`; Codex como `CODEX_THREAD_ID`. É como o daemon agrupa o agente filho abaixo de você e avisa quando termina. Para verificar nomeia esta aba: `GET /v1/orchestrator/whoami?conversation_id=<id>` responde `terminal_id`.

**4. Despacho**, com o token do orquestrador:

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

Envie o corpo através do stdin (`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`) para que o segredo fique fora do argv. A resposta é `{ok, task, warnings?}`. Leia `warnings`: `claims_overlap`, `claims_missing`, `claims_ignored_for_worktree`, `dirty_worktree_base` e `work_not_placed` (o item nomeado ainda não pôde ser movido para o Board; o a varredura do Board faz isso dentro de um tick). Postar o mesmo ID novamente responde à tarefa armazenada com `replayed: true`, portanto, uma nova tentativa é segura.

Uma aba que não abre ainda responde 200, com `task.state: "spawn_failed"`. `POST /v1/orchestrator/tasks/<id>/respawn` (token orquestrador) abre uma cópia com um novo segredo, no máximo duas vezes por original.

Despachou o agente filho errada — o briefing errado, o escopo errado ou o mesmo trabalho duas vezes? Não espere que ele termine ou expire enquanto ele mantém um slot e suas gravações: `clawdline task cancel <id> --reason "…"` o interrompe agora (§5).

**Recusas que você atenderá**, na ordem em que são verificadas:

| Estado | Código | O que fazer |
|---|---|---|
| 409 | `task_unreadable` | Uma tarefa com este id é armazenada, mas não pode ser lida; não reenvie com o mesmo id |
| 422 | `bad_task` | A mensagem nomeia o campo. Inclui "Nenhum task.json legível em…" - verifique `task_root` |
| 422 | `claims_required` | Adicionar `claims` |
| 422 | `root_session_required`, `root_assistant_required` | Adicionar `root.session_id` e `root.assistant` |
| 403 | `session_scope_mismatch` | Verifique se `root.project_dir` está presente e corresponde ao projeto da sessão raiz e ao instantâneo da função. Corrija o briefing ou CLI; não peça à pessoa para alterar as configurações do projeto. |
| 422 | `detached_route_required` | Você enviou `root.poll_only`; isso é automação separada (§6) |
| **409** | **`stale_inventory`** | Seu `generation` está faltando ou é antigo. Todo o estoque atual está dentro do erro: leia, decida novamente, reenvie com seu `generation` |
| 422 | `work_not_found`, `work_other_project`, `work_closed` | O `work_id` que você nomeou não é nenhum item, é de outro projeto ou está fechado |
| 422 | `also_work_not_found` | Um id em `also_work_ids` não é um item do Board; é verificado como `work_id` depois disso |
| 503 | `store_unavailable` | Não foi possível ler o Board para verificar o item nomeado; nada foi iniciado, envie novamente |
| 409 | `graph_*` | Uma regra de admissão do gráfico de tarefas (o campo `graph`) |
| 409 | `no_child_capability` | Esta plataforma não pode abrir um filho; `missing` diz o que |
| 429 | `squad_launch_capacity` | Muitos lançamentos de persona ainda estão aguardando sua Sessão; tente novamente mais tarde |
| 429 | `rate_limited` | Muitos despachos em dez minutos |
| 422/409 | `root_unresolved`, `conversation_ambiguous` | Seu ID de conversa não corresponde a nenhuma sessão ao vivo ou a mais de uma. Corrija; não mude para desanexado |
| 403 | `session_actor_required` | Uma raiz aberta com um role deve despachar com sua própria capacidade de esquadrão da Sessão, a partir dessa Sessão |
| 403 | `session_scope_mismatch` | Também aqui: o projeto da sessão raiz não corresponde ao snapshot da sua função |
| 503 | `squad_policy_unavailable` | As configurações de atribuição de função não puderam ser lidas; nada foi iniciado |
| 409 | `persona_disabled_for_auto_assignment` | Essa persona está desativada para atribuição automática no Projeto alvo |
| 429 | `over_capacity` | Seus slots filho (padrão 5) ou os da máquina estão cheios; `retry_after` |
| 409 | `workspace_busy` | Sobreposição de gravações de outra raiz; o erro nomeia a tarefa de bloqueio |
| 409 | `worktree_unavailable` | Não foi possível realizar o checkout privado |
| 429 | `terminal_busy` | Cada pista de gravação de terminal está ocupada; `retry_after: 5` |

## 5. Durante a execução e quando terminar

O agente filho assina seu briefing (`clawdline task accept`, que publica `/accepted` ou sai de `accepted.json`), pode enviar uma nota de progresso quando seu plano mudar (`/progress`), pode enviar até cinco notificações (`/notify`) e finaliza escrevendo `result.json` e executando `clawdline task finish`. Você não chama essas rotas.

- `clawdline task show <id>` — uma tarefa, com seu estado (`GET /v1/orchestrator/tasks/<id>`). `GET /v1/orchestrator/tasks` os lista (`?state=`, `?limit=` até 500).
- **Quando termina, o daemon digita uma linha `<clawdline-notice>` em seu compositor.** Seu `body` é uma frase curta: a tarefa, como terminou, os fatos que são apenas desta entrega (uma parada, gravações liberadas, sua ramificação, quantas sobras) e o único comando a ser executado, `clawdline task show <id>`, que fecha o aviso após imprimir a tarefa. Seu JSON (versão 3) carrega `task`, `state` e `notice_id`, e `outstanding`, `leftovers` e `claims_released` apenas quando dizem algo; o resultado é o que `task show` imprime. Uma linha que não pôde ser digitada é tentada novamente em uma escada de 5→300 segundos. Uma vez na tela e você não leu a tarefa, ela não é digitada novamente inteira: uma linha curta `task_reminder` nomeando o mesmo comando está, em uma escada de 2 → 30 minutos - oito digitações no total, então ele desiste. Ele nunca digita enquanto você mostra um menu. Um menu não esgota esses oito: a fila espera, por até 12 horas, e é digitada assim que o menu acaba. A rota `task show` envia:

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  `clawdline task show <id>` e `clawdline task wait <id>…` enviam-no para uma tarefa concluída após imprimi-lo; `clawdline task ack <id> <notice_id>` envia manualmente e imprime uma linha. Um segundo ACK responde `changed: false`. Os avisos não confirmados estão listados em `GET /v1/orchestrator/completions`; `POST /v1/orchestrator/completions/reconcile` os rearma. Um aviso de que desisti será digitado mais uma vez na próxima vez que sua sessão estiver ociosa.
- **Você pode nunca ver a linha e ainda assim aprender sobre ela.** Seu próprio `GET /v1/work/v2/agent/session-todos/<conversation id>`, que você lê ao final de cada turno, lista `unacknowledged_completions` - cada filho seu que terminou e que você não reconheceu, com `task_id`, `title`, `state`, `kind`, `result_path`, `notice_id` e `ack_path`, quer seu aviso ainda esteja pendente ou desistido. `clawdline session report` os imprime após seu recebimento. Para cada um: `clawdline task show <id>`, depois integre-o; a leitura é o ACK e retira ambas as listas. `task show` imprime o resumo inteiro e conta o que deixa de fora; `--json` é a resposta completa do daemon, incluindo símbolos e artefatos. Leia o próprio `result.json` apenas quando isso não for suficiente.
- **Uma entrega que nomeia sobras** — coisas que o agente filho diz que não fez — por si só não muda nada. `task show` lista seus títulos. Para colocar um para a pessoa, `POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}`; eles respondem normalmente, mais tarde (Backlog) ou não, e nada chega ao seu Board até que o façam.
- **Um agente filho que para logo após seu briefing é cutucada e então denunciada.** Se um agente filho cujo briefing foi digitado não tiver assinado e sua tela estiver inativa - prompt desenhado, compositor vazio, sem menu, sem linha de trabalho - por 5 minutos, o daemon digita uma linha nomeando seu `CHILD.md` (nunca o segredo). Ainda não assinado e inativo 5 minutos depois, a tarefa termina `spawn_failed` com um veredicto que diz que ela foi interrompida e você recebe um aviso de `"kind": "task_stalled"` em vez de `task_finished`. Renasça-o (`POST /v1/orchestrator/tasks/<id>/respawn`) ou envie-o novamente e, em seguida, faça ACK. Um agente filho que está trabalhando, mostrando um menu ou que assinou nunca recebe digitação.
- **Cancelar um filho que você despachou por engano** — o resumo errado, o escopo errado, uma duplicata: `clawdline task cancel <id> --reason "wrong brief"` (`POST /v1/orchestrator/tasks/<id>/cancel`, `{"reason":"…"}`). O motivo é obrigatório, no máximo 500 bytes. A tarefa termina `cancelled` com o motivo como seu veredicto, sua guia é fechada, suas gravações e slot filho são liberados e você recebe um aviso informando que foi cancelado e por quê. **Commits não são jogados fora:** um filho que fez commit mantém seu branch e checkout, e seu landing fica pendente com uma nota informando quantos commits há nele; `task show` e `clawdline landings` mostram isso. Mescle o que quiser ou grave com `clawdline task land <id> abandoned`. Somente a Sessão raiz que despachou a tarefa, ou a pessoa do console, poderá cancelá-la; qualquer outra pessoa é recusada `403 not_task_root`, e uma Sessão aberta com uma função deve enviar seu próprio recurso (`session_actor_required`; o comando faz isso para você). Uma tarefa que já foi finalizada responde `409 task_already_terminal` com seu `state`; executar o mesmo cancelamento novamente responde ao mesmo sucesso com `replayed: true`. `clawdline task wait` sai de 5 quando uma tarefa que esperava foi cancelada. Caso contrário, uma tarefa termina terminando, falhando ou expirando.
- **Um filho finalizado não é um código de destino.** Seu trabalho fica na árvore compartilhada ou em sua ramificação até que você o integre.

## 5a. Aguardar um comando demorado: callback

Uma implantação, uma execução de CI, uma propagação de release, uma construção longa: qualquer coisa cuja resposta seja "mais tarde". Não espere por ele na sua vez e não faça pesquisas em turnos posteriores. Entregue ao daemon:

```sh
clawdline callback --title "CI is green on <sha>" --timeout 45m -- gh run watch <run id> --exit-status
```

Imprime `callback <id> briefed` e retorna. **Termine seu turno.** Quando o comando termina, o daemon digita um `<clawdline-notice>` cujo corpo lê `callback <first 8 of id> finished: success (exit 0 after 6m) — run clawdline task show <id>`; `task show` imprime como terminou — seu status de saída e as últimas linhas de sua saída — e fecha o aviso, exatamente como para um filho.

- É uma tarefa sua sem aba: `clawdline task cancel <id> --reason "…"` para todo o grupo de processos do comando; após `--timeout` (1m a 4h, padrão 30m) ele é parado e liquidado `timeout`. Um callback em execução evita que sua sessão feche, como faz um filho em execução.
- O comando é executado como suas palavras, sem shell; escreva `sh -c '…'` para um. Ele é executado neste diretório (`--dir` para outro) com apenas PATH, HOME, locale, USER, SHELL, TMPDIR e TERM do seu ambiente: nunca uma credencial. Um comando que precisa lê-lo em seu próprio arquivo.
- Sua saída fica no diretório da tarefa, `output.log`, mantido 7 dias após seu término.
- É executado uma vez. Se o daemon for reiniciado nesse meio tempo, ele pegará o comando novamente; um comando que terminou enquanto nenhum daemon o estava observando e não deixou nenhum status de saída é resolvido `failure` com o resultado desconhecido e **não** é executado novamente. Inicie você mesmo novamente, se for seguro.
- Um início incerto — a CLI não pôde alcançar o daemon — é tentado novamente com `--task-id <the id it printed>`: o mesmo ID nunca é iniciado duas vezes.
- Um callback não ocupa um slot filho. No máximo 8 execuções por Sessão e 16 por máquina; mais um `429 callback_capacity` é recusado com um `retry_after`. Um despacho não pode chamar a si mesmo de `kind callback` (`bad_task`). O Windows recusa `501 no_callback_capability`.

## 6. Integração e os outros três tipos de trabalho

**Depois de mesclar uma ramificação filha, não faça mais nada**: o próprio broker registra `landed` (abaixo). Um filho que não declarou nenhuma gravação (`--claims ""`), finalizou sozinho e não deixou nenhuma alteração em sua branch ou no checkout é registrado como `nothing_to_land` pelo broker antes que seu aviso seja digitado. Um registro manual de integração é para o que nenhum deles cobre - um cherry-pick, uma entrega `incorporated`, `nothing_to_land`, `abandoned` - e é um comando, enviado com o token do orquestrador:

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

É esta rota, que um script pode chamar com o cabeçalho `X-Clawdline-Orchestrator`:

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- Somente estas chaves, mais `delivery`, que é aceita e não utilizada; qualquer outra chave é recusada. `pending` e `abandoned` aceitam o segredo da tarefa ou o token do orquestrador; `landed`, `incorporated` e `nothing_to_land` aceitam apenas o token do orquestrador.
- `landed` precisa de `target` e `commit`; `incorporated` precisa de `target`, `commit` e `carrier_task`, a outra tarefa cuja integração verificada realizou esta entrega. O daemon **verifica no Git**; caso contrário, `409 unverified_landing` com um destes `reason`s: `commit_unresolved`, `target_unresolved`, `not_on_target`, `base_unknown`, `predates_dispatch`, `delivery_unknown`, `nothing_delivered`, `not_the_delivery`, e para `incorporated` `carrier_required`, `carrier_is_delivery`, `carrier_unresolved`, `carrier_not_landed`, `carrier_repository_mismatch`, `carrier_target_mismatch`, `carrier_commit_mismatch`, `delivery_is_ancestor`.
- `nothing_to_land` é recusado com `409 wrote_to_repository` quando a tarefa foi gravada.
- Uma integração registrada não pode mudar: `409 invalid_transition` ou `409 landing_conflict` para um valor diferente.
- **Uma mesclagem registra a si mesma.** Depois que a ramificação de uma tarefa concluída é mesclada em seu destino, o broker registra `landed` por conta própria dentro de alguns minutos, por meio da mesma verificação do Git, com o cabeçalho do alvo como o commit. Sem nenhum alvo registrado, ele nomeia um somente quando a branch do checkout principal é a única que realiza a entrega. Um cherry-pick, uma entrega `incorporated` e `nothing_to_land` ainda são seus para registrar.
- **O aviso de conclusão nomeia o estado da ramificação quando a tarefa terminou**, e cada um pede uma coisa, como um comando. *Nenhum commit em seu branch*: uma integração é comprovada a partir desse branch, então nada poderia ser registrado como integrado — commit em seu checkout, naquele branch, enquanto o checkout ainda está no disco, ou `clawdline task land <id> abandoned`. *Há commits em seu branch*: mescla esse branch em seu alvo; a mesclagem registra a integração. *Não foi possível ler*: inspecione a branch e grave-o. *Escreveu no checkout compartilhado*: `clawdline task land <id> landed` com o commit que carrega esse trabalho para seu destino, ou `abandoned`. *Não escreveu nada e o broker registrou `nothing_to_land`*: resta apenas o ACK.

`clawdline landings` (`GET /v1/orchestrator/landings`) são todas as integrações pendentes na máquina, cada uma com um `ownership.status`. `unknown` não é “ninguém”: significa que a evidência não pôde ser lida. `503 landings_incomplete` significa que algumas linhas não puderam ser lidas e nenhuma lista mais curta é oferecida em seu lugar.

`clawdline landings --work-id <item id>` (`GET /v1/orchestrator/landings?work_id=<item id>`) é cada integração **registrada** para um item do Board: `{"work_id", "landings": [...], "at"}`, cada linha com seu `id` e `source` — `task` (registro integrado ou incorporado de um agente filho vinculado; o id é o id da tarefa), `root` (o registro que `item phase deploying --commit` escreveu) ou `phase_event` (uma cópia de um daemon mais antigo mantido no histórico do item). Uma tarefa vinculada cujo registro não pode ser lido é uma linha com `state: "unknown"`, nunca deixada de fora. O item lido carrega as mesmas linhas que `landings`. Um item que não existe é `404 work_not_found`.

**Duas sessões raiz integrando trabalho no mesmo checkout** faça primeiro uma reserva de integração (§11).

Os outros três tipos de trabalho têm, cada um, seu próprio roteiro. Qual deles é um limite, não um detalhe:

| Tipo | Rota | O que é |
|---|---|---|
| **Transferência** | `POST /v1/orchestrator/handoffs` | Você fornece uma linha de trabalho existente, com seu estado completo, para uma nova sessão |
| **Atribuição raiz** | `POST /v1/orchestrator/root-assignments` | Um novo Root independente para um novo recurso |
| **Automação separada** | `POST /v1/orchestrator/detached-tasks` | Trabalho autônomo sem ninguém a quem reportar |

**Handoff.** Escreva `<state dir>/handoffs/<handoff_id>/handoff.md` primeiro (a rota da lista responde `package_root`). Deve conter três títulos: **REFERÊNCIAS** (tudo o que o receptor deve ler), **VERIFICAÇÃO** (perguntas que ele responde dessas fontes antes de continuar) e **TÓPICOS ABERTOS** (onde pegar). Depois poste, com corpo fechado:

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

O receptor é instruído a ler o arquivo, percorrer suas referências, responder às perguntas de verificação e continuar. Na abertura, a transferência captura os itens abertos do Board do remetente naquele Projeto. Depois que o destinatário tiver um ID de conversa e seu primeiro registro de conversa for observado, as atribuições e proprietários ativos desses itens são transferidos para o destinatário em uma transação. Uma transferência com falha deixa a propriedade com o remetente. Um item já fechado, movido para outra pessoa ou atribuído separadamente é deixado sozinho. Um item bloqueado por verificação na verificação ou mesclagem retorna à implementação, portanto seu novo proprietário deve verificá-lo novamente. Você recebe um aviso `handoff_receipt` quando a linha de transferência é digitada; esse aviso por si só não prova que a transferência do Board ocorreu. Recusas: `bad_task` (um `handoff.md` ausente ou vazio incluído), `sender_not_found`, `sender_ambiguous`, `rate_limited`, `terminal_busy` e `succession_required` se você tiver a função de coordenador de máquina — a sucessão não está disponível neste daemon (`501`), portanto, essa sessão não pode ser transferida.

**Transferência de marco.** Um Root de longa execução que atingiu um marco é transferido com `clawdline handoff --summary summary.md`, para que o trabalho posterior não precise reler tudo o que veio antes em cada chamada. O resumo tem exatamente cinco títulos `## ` — Objetivo, Decisões verificadas, Bloqueadores, Evidência (caminhos, commits, ids ou comandos `clawdline` para abrir, não seu conteúdo), Próxima etapa — no máximo 6 KiB, sem credencial e sem texto de conversa; `--check` lista todos os problemas sem abrir nada, e o daemon recusa os mesmos que `bad_milestone_summary`. O daemon escreve `obligations.md` ao lado: os itens do seu Board (eles se movem, incluindo uma decisão que a pessoa não respondeu) e seus filhos em execução, avisos não reconhecidos e integrações devidos (eles permanecem seus). Portanto, depois de entregar, continue reconhecendo e aterrando-os e, em seguida, `clawdline session close` quando estiver escrito `safe`. A entrega é sua escolha: `clawdline usage --compare-handoff` diz se foi pago nesta máquina e nunca é forçado.

**Atribuição raiz.** O cabeçalho `Idempotency-Key` deve ser igual a `request_id`:

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

Cada campo de atribuição tem de 1 a 8.192 bytes, 32 KiB no total. O daemon escreve o briefing e abre a sessão. Não possui **pai, segredo, tempo limite, resultado ou integração**: ninguém é avisado quando termina, porque não responde a ninguém. Recusas: `bad_root_assignment`, `idempotency_mismatch`, `request_conflict`, `rate_limited`. Nunca finja um com um agente filho, uma tarefa independente ou uma transferência.

**Automação separada.** Como um despacho (§4) — `task.json` sob `task_root`, depois `{"task_id", "secret", "inventory_generation"}` — mas a raiz do resumo deve ser `{"session_id": null, "poll_only": true}`, ou será recusado como `detached_task_required`. Ninguém é notificado; pesquise `GET /v1/orchestrator/tasks/<id>` e leia `result.json`. Nunca é um Root ou proprietário de recurso.

### Agendar trabalhos futuros

Clawdline Next possui o próprio trabalho agendado. Não use o aplicativo descontinuado, `cron`, ou uma cadeia de tarefas desanexadas. Leia `GET /v1/orchestrator/schedules`; leia um completo em `GET /v1/orchestrator/schedules/<id>`. `GET /v1/places` fornece ao `place_id` nomes de gravação.

Um agendamento único (`on`) pode ser feito diretamente com o token do orquestrador. Um cronograma de repetição (`days`) é uma instrução permanente e precisa da instrução explícita da pessoa nesta sessão:

1. Leia `GET /v1/orchestrator/sessions/<conversation>/run`. É a execução recente emitida quando a pessoa enviou sua mensagem para esta sessão por meio de Clawdline.
2. `POST /v1/orchestrator/schedules` com um `Idempotency-Key` e o corpo da programação comum mais aquela conversa e execução:

```json
{"title":"Morning sweep","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"Inspect the overnight failures and report actionable findings.",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

A mesma prova autoriza `PATCH /v1/orchestrator/schedules/<id>` (enviar todo o corpo do cronograma) e `DELETE /v1/orchestrator/schedules/<id>` (enviar os dois campos de prova como seu corpo JSON) quando a pessoa solicitou explicitamente essa alteração. `POST /v1/orchestrator/schedules/<id>/run` executa um agora. Leia o cronograma criado ou alterado antes de relatar o sucesso.

Sem `via`, o token do orquestrador permanece limitado a um planejamento `on` único. Uma execução de sessão inventada, expirada ou de outra sessão é recusada como `run_unknown`, `run_expired` ou `run_other_session`; a prova malformada é `invalid_user_authorization`. Se a pessoa digitou direto em um terminal, não há execução disponível: peça para ela enviar a instrução através de Clawdline. Nunca reutilize uma execução como permissão geral para um trabalho que a mensagem da pessoa não solicitou. Esta prova torna o relé auditável; ele não transforma o token do orquestrador de toda a máquina em uma credencial específica da sessão.

Um agendamento pode não ter horário: envie `"trigger_only": true` em vez de `at`, `days` e `on`. O relógio nunca funciona; ele é executado apenas por `…/run` ou por seu webhook e somente enquanto `enabled`. É uma instrução permanente como uma instrução repetida e precisa da mesma prova.

**Inicie uma tarefa em outra máquina e saiba como ela terminou.** Não há canal máquina a máquina. Torne a tarefa um agendamento somente de gatilho na máquina de destino, vincule um webhook da nuvem a ela e armazene a URL onde o chamador pode lê-la (é uma credencial: um arquivo que pode ser lido apenas por você ou `CLAWDLINE_WEBHOOK_URL`; nunca um argumento de linha de comando). Então, na máquina de chamada:

```sh
clawdline webhook fire --url-file <path>
```

Ele envia `{"deliver_within_seconds": 60}` (`--deliver-within`), então um destino que está desligado não o executa posteriormente; segue o status da entrega até o final ou passagem de `--timeout` (60m), imprimindo as alterações no stderr e uma linha final no stdout. Saída `0` bem-sucedida · `1` finalizado sem sucesso (falha, timed_out, cancelado, spawn_failed) · `2` nunca alcançou a máquina (expirado, cancelado, inacessível) · `3` recusado (dispatch_refused e seus código, URL indisponível, taxa limitada) · `4` parou de esperar, com o último estado visto. `--no-wait` imprime o ID de entrega e retorna após `202`. Relate o código de saída e a linha final como estão: `2` significa que nada foi executado, não que falhou.

Uma tarefa agendada que lê dados de algo aguardando para ser verificado (o 驗收 da barra lateral, docs/verifications.md) grava sua leitura como uma nota nesse registro com seu próprio segredo de tarefa - não o token do orquestrador, que não deve conter:

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<readout>"}'
```

Ele chega apenas em um registro cujo `schedule_id` é o planejamento que iniciou esta tarefa (caso contrário, `schedule_mismatch`), é assinado como `task:<task id>` e é recusado como `not_scheduled` para uma tarefa sem planejamento iniciado. Encontre o ID do registro com `clawdline verify list`.

## 7. Relate seu próprio turno concluído

Quando seu turno estiver genuinamente concluído — o trabalho realizado, verificado e comprometido quando aplicável — faça desta sua última ação antes da resposta final:

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

Coloca um cheque na linha da sua sessão: **entregue, aguardando aprovação**. É mais fraco que uma integração e não exige revisão. Ele mostra enquanto o daemon lê a sessão como ociosa – trabalhando, aguardando ou uma tela ilegível supera-a – e somente enquanto o terminal mantém a mesma conversa.

- **Apenas para um turno concluído.** Não para trabalho parcial, diagnóstico, bloqueador ou pergunta para a pessoa. Um agente filho nunca envia (`409 child_session`).
- O comando encontra sua conversa de `CLAUDE_CODE_SESSION_ID` ou `CODEX_THREAD_ID` (`--conversation` caso contrário), solicita `GET /v1/orchestrator/whoami` pelo terminal e posta `{"summary"}` para `POST /v1/orchestrator/sessions/<terminal>/complete`.
- O resumo tem de 1 a 500 caracteres. Cada chamada é um novo recibo; as contagens mais recentes.
- A resposta também traz `open_todos`: as tarefas diretas desta Sessão que foram enviadas ou lidas e não foram concluídas, as mais antigas primeiro, no máximo 20 (`open_todos_truncated` quando houver mais). O comando os imprime no stderr após o recebimento, um id e texto por linha. Marque cada um que você finalizou com `clawdline todo done <id>`. O recebimento é registrado de qualquer forma e o status de saída não muda; `open_todos_unknown: true` significa que eles não puderam ser lidos, não que nenhum esteja aberto.
- Recusas: `conversation_id_malformed` (não um UUID minúsculo), `conversation_not_found`, `conversation_ambiguous`, `registry_stale`, `session_not_found`, `session_unbound`, `child_session`. Relate a recusa honestamente; uma frase no chat não é um recibo.

**Deixe um relatório de status para a pessoa.** Quando o turno alterou os arquivos em um projeto git, entregue à pessoa uma página onde ela possa ver onde está o trabalho e ler todos os arquivos adicionados ou alterados no turno. É um arquivo HTML local: nada é carregado e não carrega nada.

```sh
clawdline report --repo <project> --status status.md [--notes notes.txt] [--lang zh-TW] [--open] <commit>…
```

- Nome **os commits deste turno, os mais antigos primeiro**. Cada um é lido por si só, então os commits de outra Session entre eles ficam de fora; nunca ultrapasse um intervalo.
- `status.md`: um `# Title` opcional, uma linha opcional abaixo dele e, em seguida, um cabeçalho `## ` por cartão - abra-o com ✅, 🟡 ou ❌ - e um corpo Markdown curto.
- `--notes`: um `path: sentence` por linha, mostrado acima desse arquivo. `--pin` (repetível) coloca um arquivo primeiro; `CLAUDE.md` e `AGENTS.md` são imobilizados quando o turn os toca. `--exclude` deixa um caminho e diz isso. `--at` é a revisão cujo conteúdo é mostrado (padrão `HEAD`).
- Ele mantém o relatório em `<state dir>/reports/<date>-<id>/report.html`, fora de cada repositório, e imprime dois endereços: **primeiro o endereço `file://`**, que um terminal abre, **depois `http://127.0.0.1:<port>/reports/<id>`**, que o daemon desta máquina responde. Coloque ambos em sua resposta final: o console mostra um endereço `file://` como texto que não pode ser aberto e transforma o `http://` em um link. Esse endereço abre apenas em um navegador nesta máquina que esteja conectado ao seu console; um telefone ou visualizador de nuvem é recusado (`report_not_over_cloud`, `report_local_only`).
- `--out` grava outro arquivo ou diretório, apenas com o endereço `file://`. stderr diz o que foi deixado de fora ou encurtado. `--open` também abre o arquivo no navegador desta máquina.

## 8. Fale com outra sessão

**Encontre.** `GET /v1/orchestrator/sessions` é o catálogo de endereços: `id` de cada sessão (seu ID de terminal), `label`, `assistant`, `cwd`, `state`, `work_state` e `taskId` para um filho vivo.

**Enviar.**

```sh
clawdline send --to <terminal id> "text"        # or text on stdin
```

Este é `POST /v1/orchestrator/messages` com `{from_session, to_session, text}` e um `Idempotency-Key`. O daemon digita no compositor do destinatário dentro de um envelope `<clawdline-message>` que nomeia você como a fonte.

- `to_session` é um **ID de terminal**: uma mensagem segue a guia que você quis dizer, não uma conversa. `from_session` é o seu terminal ou ID de conversa (o comando o preenche).
- Somente texto, no máximo 100.000 caracteres. Não há campo `images`: um enviado é descartado silenciosamente.
- `ok` significa que os bytes chegaram a um compositor, não que alguém os tenha lido.
- Pode levar dezenas de segundos: o daemon lê cada sessão na máquina antes de digitar (cerca de 30 s por retransmissão em um Mac, medido em 19/09/2026). O comando aguarda até dois minutos. Não o interrompa - um relé cortado no meio do caminho pode ser digitado e não ser gravado, e a mesma chave responde `409 request_in_progress`.
- O comando imprime seu `Idempotency-Key` primeiro. Se a chamada falhar no caminho, execute-a novamente com `--key <that key>`: a mesma chave e corpo são digitados uma vez. A mesma chave com corpo diferente é `409 idempotency_key_reused`.
- Recusas: `source_not_found`, `target_not_found`, `same_session`, `target_busy` (o destinatário mostra um menu; nada foi digitado), `terminal_busy`, `delivery_failed`.

**Mostre uma foto à pessoa.** Não cole um caminho local: em um telefone ele não abre nada.

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- Somente token do orquestrador. Um a seis arquivos locais; cada um deve ser um arquivo simples, com no máximo 12 MiB e 12.000 px por lado. PNG, JPEG e GIF são lidos diretamente; outros formatos passam por `sips` no macOS.
- A resposta lista `artifacts`, cada um com um `marker`, como `<clawdline-image id="…">`. **Coloque o marcador na sua resposta**; o console mostra a imagem onde está o marcador. As fotos são mantidas 24 horas.

## 9. Diga à pessoa

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

`POST /v1/orchestrator/notify`. Apenas por algo que a pessoa espera: o valor de um empurrão é que ele é raro. `--session <terminal>` abre essa sessão com um toque.

- `409 agent_notify_disabled`: a pessoa desativou as notificações do agente. Não é sua culpa; não tente novamente.
- `409 not_subscribed`: nenhum dispositivo está inscrito em pushes.
- `429 rate_limited`: 30 por hora para toda a máquina, compartilhado com as notificações de cada agente filho.
- `502 push_failed`: serviço push recusado; `sent` e `failed` estão no erro.

## 9a. Deixe uma nota de intervenção humana

Use uma nota quando um Agente de longa duração tiver algo concreto que a pessoa deva ler, fazer ou decidir e uma mensagem de bate-papo comum possa desaparecer no fluxo. A nota permanece na área de atenção recolhida da Sessão alvo, marcada com um ponto vermelho até que a pessoa a mova para manipulada. Você poderá então continuar o trabalho independente; a pessoa pode retornar em um ponto de parada natural. Uma nota não é um registro de progresso, um lembrete privado, uma notificação ou autorização para uma decisão do Board. Evite notas duplicadas para a mesma solicitação.

**Antes de pedir à pessoa que escolha no chat**, crie uma nota `answer` contendo a pergunta real, as compensações necessárias para decidir e duas a quatro respostas sugeridas completas. Tocar em um botão envia sua resposta como uma mensagem de conversa, portanto, torne cada `draft` inequívoco por si só. Um breve ponteiro de bate-papo é suficiente após a criação. Não trate a criação de uma nota ou uma nota marcada como tratada como a resposta da pessoa; aguarde a mensagem da conversa – uma resposta tocada ou digitada pela pessoa – antes de agir de acordo. Se a criação falhar, diga isso e faça a pergunta diretamente. Reserve notas para decisões que necessitam de julgamento humano, e não para escolhas rotineiras que o Agente pode fazer.

Crie um com um arquivo de corpo JSON. Sem `--target`, a CLI resolve o próprio ID de terminal do Root ativo por meio de `whoami`. Para outra sessão, use seu **ID de terminal** ativo do catálogo de endereços (`clawdline guide pt-BR send`) como `--target`. O padrão `--from` é o ID de conversação deste Root ativo do ambiente. A CLI lê a credencial da máquina sem colocá-la na linha de comando, injeta os IDs de origem e destino e imprime o ID da nota durável do daemon. Reutilize o `--key` impresso após um resultado incerto.

```json
{"kind":"answer","title":"Choose a date","summary":"One release date needs your choice.","action":"Choose a date when you have a moment.","reason":"Only you can choose it.","options":[{"label":"Tuesday","draft":"Tuesday works for me."},{"label":"Wednesday","draft":"Wednesday works for me."}]}
```

```sh
clawdline note create --body-file note.json
# For another Session: clawdline note create --target <terminal-id> --body-file note.json
```

`kind` é `read`, `answer`, `action` ou `report`; `title`, `summary`, `action` e `reason` são necessários. Um `answer` pode oferecer de duas a quatro opções. Cada `draft` é a resposta sugerida mostrada em seu botão. Quando a pessoa toca nele, o Console a envia imediatamente como uma mensagem de conversa para a Sessão da nota, seguida por uma linha de contexto com o ID, título e ação da nota para que a Sessão receptora saiba qual solicitação a pessoa atendeu; o contexto não é mostrado no botão. Somente após o envio ser bem-sucedido é que a nota passa para o manipulado recentemente. Se o envio falhar, a nota fica pendente e o controle de atenção informa que a resposta não foi enviada. `detail` pode conter texto mais longo. `document_url` pode ser vinculado a um documento real e legível na nuvem; verifique a rota e o arquivo do documento antes de publicá-lo. Aja de acordo com a mensagem de conversa que chega, não com base no estado da nota: uma nota que a pessoa marcada como tratada manualmente não enviou nada. Se o seu trabalho estiver realmente bloqueado na resposta, registre o estado do usuário em espera e envie a notificação de atenção existente uma vez. Uma nota visível por si só não envia push e não desperta um Agente.

## 10. O Board

O Board tem três estruturas — itens do Board, o Backlog e a lista de tarefas de cada sessão — e **uma pessoa decide o que acontece nele**. Uma sessão cria um item do Board somente quando a própria mensagem da pessoa, enviada por meio de Clawdline, solicita; caso contrário, ele propõe. Nunca apresenta um cartão por iniciativa própria. A única exceção é o proprietário de uma Épica: após o plano revisado da Épica, ele pode dividir a Épica em itens de Recurso e Emissão e atribuí-los a Sessões (`clawdline guide pt-BR epic`).

**TODO / 待辦 / 土度 dito junto com um item do Board significa as etapas desse item.** Coloque-as no item com `--step`. **Não** escreva-os também com `clawdline todo add`. `clawdline todo add` é apenas para uma lista que a pessoa pede para você acompanhar como tarefas da própria sessão, sem nenhum item do Board.

**Quando a pessoa lhe disser para criar um item do Board.** Somente quando a mensagem dela — enviada por meio de Clawdline, portanto, tiver uma execução — solicitar explicitamente um, crie você mesmo:

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin] [--assign-self]
```

Exemplo resolvido. A pessoa escreve: *"Faça um item no Board para limpar as notas de lançamento, TODO: rascunhe-as, verifique os links, publique."* Esse é um comando e nada mais:

```
echo "Clean up the release notes before the next release." | \
  clawdline item add --project <place id> --kind feature --title "Clean up the release notes" \
  --step "Draft the notes" --step "Check the links" --step "Publish"
```

- `item add` lê a última execução desta conversa (`GET /v1/orchestrator/sessions/<conversation>/run`), a menos que `--run` nomeie uma, imprima sua chave de idempotência antes de perguntar (`--key` tenta novamente a mesma gravação) e imprime o item criado com cada etapa identificação. É `POST /v1/work/v2/agent/items` com `{"session_id", "via": {"run"}, "project_id", "kind", "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`, respondeu `201` com `{"item", "assigned", "assignment_state"}`.
- Um Feature, Issue ou Epic chega **não atribuído** por padrão, onde os itens não atribuídos desse tipo da pessoa aguardam no Board, com suas etapas: as linhas `--step` em ordem, ou - quando você não fornece nenhum - duas ou mais linhas da lista Markdown de nível superior da descrição. A pessoa muitas vezes pede que você escreva um trabalho para mais tarde; criar o item não o torna seu. `assignment_state` é `not_requested`.
- Adicione `--assign-self` (`"assign": {"mode": "self"}`) **somente quando a mensagem da pessoa solicitar que esta Sessão faça o trabalho agora** ("faça um item para isso e faça"). O item então chega **atribuído a você**, em `assigned`, na mesma gravação. Nada é digitado no seu terminal; você pediu por isso. Trabalhe as etapas em ordem, conclua cada uma quando for verificada (`clawdline item steps <item id>`, `clawdline item step-done <item id> <step id>`; `clawdline item step-add` adiciona um que o trabalho for necessário) e avance as fases com `clawdline item phase` como para qualquer item atribuído (abaixo). Uma Épica tomada desta forma segue o procedimento da Épica (abaixo) antes de poder ser implementada. Se a pessoa pedir para você pegar um item que você criou não atribuído posteriormente, use `clawdline item claim` (abaixo). Um Refactor é um trabalho executável que altera a estrutura interna, mas não o comportamento externo: ele é atribuído, executa etapas e segue as fases, portão e chave de revisão de um recurso. Um Plano é criado sem atribuição, no Planning, com ou sem `--assign-self`, e não executa etapas (`planning_has_no_steps`).
- O Clawdfather registrado é a exceção para trabalhos executáveis ​​do Projeto: ele nunca possui ou edita o código do Projeto. Quando a mensagem da pessoa solicita explicitamente um novo item, ela pode usar `clawdline item add --project <place id> --kind feature --title "…" --assign-new` (ou `--assign-terminal <id>`) para criar o item primeiro e depois delegá-lo a uma Sessão de Projeto. `assignment_state` informa se um proprietário do projeto foi registrado (`assigned`), uma nova Sessão aguarda sua primeira mensagem (`awaiting_user`), falha na atribuição (`failed` com `assignment_error`) ou nenhuma atribuição foi solicitada (`not_requested`). Nestes dois últimos casos, a pessoa pode atribuir o item pelo Board. Uma repetição com `pending` nomeia o item original após uma delegação interrompida; inspecione o Board antes de tentar outra atribuição. Sem essa mensagem explícita, proponha o item e aguarde a aceitação.
- A pessoa vê o cartão marcado "Criado pela Sessão a partir de sua mensagem em HH:MM", com suas palavras entre aspas.
- Recusas, cada uma sem escrever nada: `run_unknown` (nenhuma execução nomeada ou nenhuma emitida), `run_expired` (mais de um dia), `run_other_session` (uma mensagem para outra sessão), `session_not_found`, `child_session` (um filho relata por meio de `result.json`), `project_not_found`, `project_mismatch` (um item executável comum deve estar no projeto em que você trabalha), `coordinator_required` (uma sessão de máquina sem a função ativa), `machine_delegation_required` (uma sessão comum solicitou a atribuição combinada somente de máquina para outra sessão), `invalid_assignment` (um `assign` malformado ou Clawdfather solicitando `self`), `too_many_steps` (mais de 128), `run_items_exhausted` (uma mensagem retorna no máximo cinco itens).
- **Sem execução** — a pessoa digitou diretamente no terminal, então `item add` responde `no_run` ou `run_unknown`: volte para uma proposta (abaixo) e diga à pessoa para aceitá-la nas propostas do Agente do Board.

Nunca crie um item do Board por iniciativa própria e nunca vários para planejar trabalhos especulativos.

**Reivindicar um item do Board que a pessoa indicou para você.** Quando a mensagem da pessoa através de Clawdline diz para você pegar um item específico já no Board — *"pegar o item das notas de lançamento"*, *"reclamar <item id>"* — reivindicá-lo; esse é um comando:

```
clawdline item claim <item id>
```

- `item claim` lê a última execução desta conversa, a menos que `--run` nomeie uma, leia o item para sua versão, imprima sua chave de idempotência antes de perguntar (`--key` tenta novamente a mesma gravação) e imprime o item depois. É `POST /v1/work/v2/agent/items/<id>/claim` com `{"expected_version", "session_id", "via": {"run"}}` e atribui o item a **você, a Sessão para a qual a mensagem foi enviada** — nada nele nomeia outra Sessão ou terminal.
- O item então é lido exatamente como se a pessoa o tivesse atribuído a você no Board: você o possui, ele se move para `assigned`, suas etapas são propagadas a partir da lista de descrição se não houver nenhuma. Nada é digitado em seu terminal. Trabalhe como qualquer item atribuído (abaixo).
- A pessoa vê o cartão marcado "Reivindicado pela Sessão a partir de sua mensagem em HH:MM", com suas palavras entre aspas.
- Recusas, cada uma escrevendo nada: `run_unknown`, `run_expired`, `run_other_session`, `session_not_found`, `child_session` (como para `item add`); `work_not_found`; `project_mismatch` (o item está em um Projeto que você não trabalha); `item_assigned` (já possui uma Sessão, ou está sendo aberta para ela — somente a pessoa movimenta um item entre Sessões); `item_terminal` (concluído ou cancelado); `planning_not_assignable` (um Plano permanece no Planejamento; um Epic ou um Refactor podem ser reivindicados); `version_conflict` (mudou; execute o comando novamente); `run_claims_exhausted` (uma mensagem retorna no máximo cinco usos).
- **Sem execução** respostas `no_run` ou `run_unknown`: deixe o item para a pessoa atribuir.

**Atribua um item do Board a uma nova Sessão solicitada pela pessoa.** Quando a mensagem da pessoa por meio de Clawdline solicita que você entregue um Recurso ou Problema específico não atribuído a uma nova Sessão — *"abra uma Sessão de segurança para <item id>"* — atribua-a; esse é um comando:

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- Em um item que não é filho da Epic, `item assign` lê a última execução desta conversa, a menos que `--run` nomeie um, como `item claim` faz. É `POST /v1/work/v2/agent/items/<id>/assign` com `{"expected_version", "session_id", "mode": "new_session", "assistant"?, "model"?, "persona"?, "via": {"run"}}` e abre a nova Sessão que a opção "Nova Sessão" da própria pessoa abre.
- O cartão diz "Atribuído por uma Sessão de sua mensagem em HH:MM", com suas palavras citadas e a persona como a nova Sessão é executada.
- Recusas, cada uma sem escrever nada: as de `item claim` (`run_unknown`, `run_expired`, `run_other_session`, `session_not_found`, `child_session`, `project_mismatch`, `item_assigned`, `item_terminal`, `version_conflict` e `run_claims_exhausted`: `item claim` e `item assign` compartilham uma mensagem cinco); `kind_person_assigns` (apenas um Recurso ou um Problema); `new_session_only` (para pegar você mesmo, reivindique); `unknown_persona`; `persona_disabled_for_auto_assignment` (a função está desativada para atribuição automática nesse Projeto).

Nunca reivindique ou atribua um item por sua própria iniciativa — apenas aquele com o nome da mensagem da pessoa — e nunca use o `POST /v1/work/v2/items/<id>/assign` da pessoa, que recusa uma Sessão (`session_cannot_create_item`). O proprietário de uma Epic também atribui `clawdline item assign` (`clawdline guide pt-BR epic`) aos próprios filhos da Epic.

**Nomeie uma nova Sessão aberta para um item do Board.** Depois de ler seu objetivo e escopo, escolha um nome abreviado que descreva sua tarefa real e execute `clawdline item name <item id> "<task name>"`. Isso altera o nome da sua sessão uma vez, sem alterar o título do item do Board ou iniciar outro turno de modelo. Somente o proprietário ativo da nova sessão pode fazer isso. Enviar novamente o mesmo nome é seguro; um nome diferente é recusado e a pessoa ainda pode definir um título de sessão manual. Um item do Board dado a uma Sessão existente deixa o nome dessa Sessão inalterado.

**Propor um item do Board.** A fila de **Propostas de Agentes** do Board é alimentada por uma rota:

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id` é o `id` de uma linha de `GET /v1/places`.
- Uma fonte é necessária e deve ser sua: um item do Board de propriedade desta Sessão ou uma das tarefas desta Sessão (`proposal_source_required`, `proposal_source_invalid`). Uma proposta que veio de algo que a pessoa pediu cita a tarefa de onde veio - então o caminho é: a pessoa pergunta, você `clawdline todo add` (abaixo), e você propõe a partir do id dessa tarefa.
- Escreva a proposta em uma linguagem simples que uma pessoa possa entender diretamente: `title` nomeia o resultado que ela pode notar, `description` diz o que muda, `reason` diz por que vale a pena fazer agora e `suggested_acceptance` diz o que ela pode observar quando estiver feito. Todos os quatro são obrigatórios. Não use acrônimos inexplicáveis, identificadores internos, caminhos de código ou jargões de implementação a explicação principal. O Board primeiro mostra o título, a fonte e o motivo; **Explicar/詳細說明** expande o que muda e o que a pessoa verá quando terminar.
- **Para propor um item com etapas**, escreva a lista como duas ou mais linhas de lista Markdown de nível superior em `description`. Quando a pessoa aceita e atribui o item, cada linha se torna um de seus `steps` (veja abaixo).
- `201` atende a proposta pendente. A pessoa aceita, edita ou rejeita na fila de propostas de Agentes do Board; nada se torna um item do Board até que isso aconteça. Recusas: `invalid_proposal`, `proposal_too_large`, `project_not_found`, `proposals_full`.

**A rota de proposta mais antiga.** `POST /v1/orchestrator/proposals` (com `…/<id>/asked` depois de perguntar na conversa) ainda é veiculada: é onde um agente filho arquiva uma sobra com seu segredo de tarefa e `task_id`, e onde a proposta de linha de trabalho de um root obtém seu pedido agora ou espera `instructions`. Suas linhas aparecem na antiga área "para confirmar", **não** na fila de propostas de agentes do Board v2, portanto não é como colocar um item na frente da pessoa no Board.

**Peça uma decisão à pessoa.**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "work_id": "<the Board item this is about>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

O item deve ser aberto e pertencer a esta Sessão: seu cartão é o contexto em que o Board mostra a questão (`decision_source_required`, `decision_source_not_found`, `decision_source_invalid`, `decision_source_closed`). Duas a quatro opções; `default` deve ser um deles e é o que acontece quando ninguém responde (após 7 dias, a menos que `due_in_minutes` diga 60–10080). Somente uma decisão `blocking` é enviada. Leia a resposta com `GET /v1/orchestrator/decisions/<id>`.

**A pessoa atende; uma sessão apenas transmite o que eles disseram.** Propostas, decisões e itens do Board são respondidos em `/v1/work/…`. Uma sessão escrita ali deve nomear a execução que continha as palavras da pessoa, `"via": {"run": "<id>"}`, e é recusada sem ela (`403 session_cannot_decide`). Leia a última execução em `GET /v1/orchestrator/sessions/<conversation>/run`; uma execução inventada, expirada, em outra sessão ou pré-pergunta é recusada nominalmente. Uma pessoa digitando direto em um terminal não tem corrida, então peça para ela responder através de Clawdline ou no console.

**Seus filhos ainda serão coletados.** `GET /v1/orchestrator/sessions/<conversation id>/todos` — nomeado pelo ID da conversa, não pelo terminal (caso contrário, `409 session_id_is_terminal`). O broker abre e fecha estes dados de tarefas; não há nada para escrever.

Ao fim de cada turno, antes de se declarar inativo, leia também `GET /v1/work/v2/agent/session-todos/<conversation id>`. Seus `assigned_items` são itens do Board que a pessoa deu nesta Sessão, seus `recent_items` são itens que esta Sessão concluiu recentemente, e seus `direct_todos` são solicitações rápidas, e seus `unacknowledged_completions` são filhos seus que terminaram sem seu ACK (§5). Esse pull é como uma tarefa feita enquanto você estava trabalhando espera sem interromper o turno atual. Termine o turno atual, então escolha o item atribuído como seu próximo trabalho de propriedade e leia seu registro completo, incluindo os corpos dos documentos, com `clawdline item show <id>`.

**Uma tarefa enviada pela pessoa.** Uma mensagem cuja última linha é `(Clawdline to-do <id>. When it is done: clawdline todo done <id>)` é uma das `direct_todos` desta Sessão que a pessoa enviou de Clawdline; as palavras acima dessa linha são o pedido. Faça o trabalho e, uma vez verificado, execute `clawdline todo done <id>` com esse ID **antes** de relatar o turno - caso contrário, a linha permanecerá aberta na lista da pessoa embora o trabalho esteja concluído. Aquele que não está concluído permanece aberto. `clawdline session report` lista no stderr todas as tarefas que foram enviadas para esta sessão e ainda não foram marcadas (§7).

**Suas próprias tarefas, quando a pessoa solicitar.** Somente quando a pessoa solicitar explicitamente a esta Sessão para registrar seu trabalho como tarefas Clawdline — ou entregar a ela uma lista de vários itens e disser para rastreá-los lá — escreva-os na própria lista desta Sessão:

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add` é `POST /v1/work/v2/agent/session-todos/<conversation id>` com `{"todos": [{"text": "…"}, …]}` e uma chave de idempotência que ele imprime primeiro (`--key` tenta novamente a mesma gravação). Uma chamada carrega de 1 a 20 linhas de no máximo 8 KiB cada, dentro do corpo da solicitação de 96 KiB, e adiciona todas elas ou nenhuma; uma lista que levaria esta sessão além de 500 tarefas abertas é totalmente recusada (`direct_todos_full`). Responde `201` com as linhas, na ordem indicada. A conversa deve ser uma sessão ao vivo que este daemon conhece (`conversation_id_malformed`, `session_not_found`); um filho Clawdline é recusado (`child_session`) e continua reportando por meio de `result.json`.

Nunca faça isso por sua própria iniciativa e nunca planeje trabalho especulativo. Preencha cada linha com `clawdline todo done <id>` somente depois de verificado. A pessoa vê essas linhas marcadas como adicionadas pela Sessão e somente ela pode enviá-las ou excluí-las. Eles não são itens do Board e nunca aparecem no Board. Tarefas são uma lista de tarefas na Sessão atual; um item do Board é um trabalho que a pessoa deseja rastrear no Board - quando ela solicitar isso, use `clawdline item add` (acima), e sua lista vai como as linhas `--step` do item, nunca como tarefas também.

Se o significado da pessoa diz claramente que o item que você acabou de completar ainda está inacabado, corrija você mesmo o Board; não deixe em Feito recentemente, crie um item de substituição ou peça à pessoa para reabri-lo. Releia o item para sua versão atual e use:

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "The concrete behavior or acceptance claim that remains unfinished"}
```

Use isto somente quando a referência ao seu item recém-concluído estiver clara. A rota aceita apenas trabalhos `done` cuja atribuição final foi liberada por esta mesma Sessão; não pode reverter o cancelamento de uma pessoa ou impedir a conclusão de outra Sessão. Preserva a evidência anterior, inicia um novo ciclo em `implementing`, restaura esta Sessão como proprietária e registra o motivo no histórico imutável do item. O motivo é no máximo 8 KiB. Um acompanhamento ambíguo não é autoridade para alterar um item do Board.

Quando o Agente proprietário precisa que a pessoa aja ou escolha, ele pede uma decisão e espera por ela. Primeiro abra uma decisão sobre este item (`POST /v1/orchestrator/decisions` com o `work_id` deste item, duas a quatro opções, um `default` e um devido - para uma ação, opções como `{"id": "done", "label": "I've done it"}` e `{"id": "cannot", "label": "I can't"}`), em seguida, aponte o item na rota autenticada por máquina:

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

A decisão deve existir (`decision_not_found`), ser desta Sessão (`decision_other_session`), ser sobre este item (`decision_other_item`) e ainda estar aberta (`decision_not_open`); `decision_id` em qualquer outra condição é `decision_requires_waiting_user`. Um `waiting_user` sem decisão é recusado com `waiting_user_requires_decision`. A pessoa responde no cartão Board ou em “Esperando por você”; quando a decisão é respondida, ou seu padrão está vencido, o daemon limpa o `waiting_user` do item na mesma gravação, registra a resposta no item e digita o id e o rótulo da opção escolhida nesta sessão quando ela está ociosa. Para parar de esperar, defina `condition` como a string vazia (ou outra condição) na mesma rota; a decisão é então `withdrawn` e sai "Esperando por você", e o mesmo acontece quando o item é liberado, reatribuído, cancelado ou concluído. A resposta a uma decisão retirada é recusada com `decision_withdrawn`.

**Portas de planejamento e verificação capturadas.** `planning_gate` é ativado por padrão e `verify_gate` desativado; `clawdline setting get|set planning_gate|verify_gate` aceita `on/off` ou `true/false`. A primeira atribuição bem-sucedida em um ciclo de execução congela ambos os valores. A reatribuição e alterações posteriores nas configurações globais não alteram esse ciclo. Um Epic ou Feature com a regra de planejamento capturada exige critérios de aceitação antes da implementação. Um Epic também precisa de um plano e de uma revisão independente; uma Feature precisa deles somente quando a pessoa marcou a opção Precisa de revisão independente (abaixo). Um Issue nunca passa pela regra de planejamento. O planejamento também forçou o planejamento da Epic. Com ambas as regras ativadas, há planejamento e verificação independente. Apenas o planejamento mantém a verificação comum da mesclagem; apenas a verificação dispensa o planejamento, mas ainda verifica o candidato exato. Com ambas desativadas, vale o ciclo de vida normal. A pessoa não precisa preencher aceitação no Board. Se um item bloqueado chegar sem ele, escreva critérios observáveis ​​com `clawdline item acceptance <item id> --body-file <file>` após a atribuição e antes da transição bloqueada. A Sessão proprietária pode preencher um contrato vazio uma vez. Quando a pessoa informar explicitamente a este Root proprietário por meio de Clawdline para revisar a aceitação deste item, grave o Markdown de substituição completo em um arquivo e execute `clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>`. A versão do item é a linha `item version N` que `clawdline item show <id>` imprime (`item steps` imprime apenas a versão de aceitação); leia a mensagem executada em `GET /v1/orchestrator/sessions/<conversation>/run`. O trecho da mensagem retida deve solicitar explicitamente uma alteração de aceitação; uma proibição, discussão ou pergunta simples não é autorização. Pode referir-se ao item por contexto de conversação se esta Raiz possuir exatamente um item em aberto; caso contrário, deverá identificar o item por ID ou título. A execução deve ser mais recente que a versão de aceitação atual. Uma recusa digitada significa que nada mudou. Tente novamente uma resposta incerta com os mesmos `--key`, `--run`, `--expected-version` e bytes de arquivo. A pessoa também pode editar diretamente. Alterá-lo antes da fusão invalida o PASS e as substituições antigas; assim que a fusão começar, ela será bloqueada.

Com a verificação capturada ativada, execute `clawdline item phase <id> verifying` a partir de uma árvore de trabalho registrada limpa em seu candidato confirmado: a CLI envia a ramificação atual e o HEAD completo, e o daemon verifica o projeto, a base do ciclo, a árvore e o resumo de aceitação. Um verificador Codex somente leitura desanexado usa `code-reviewer` para Issue, `reality-checker` para Epic e `evidence-collector` para um recurso com imagens de referência ou um documento de design (caso contrário, `reality-checker`). Seu veredicto digitado é `PASS`, `FAIL` ou `NEEDS_WORK`; declarações não verificadas dizem o porquê e nunca autorizam a fusão. Um resultado ausente ou malformado é uma falha técnica, com uma nova tentativa limitada e depois escalonamento. A rodada final de ponta a ponta de um Epic espera até que todos os filhos sejam terminais e os componentes afetados sejam integrados em um candidato executável. Os testes focados dos agentes filhos e as verificações de fumaça de integração acontecem primeiro; não envie trabalho final do navegador ou de várias contas de ponta a ponta em interfaces de usuário simuladas, ramificações desconectadas ou APIs incompletas. Antes do envio, prove que o trabalhador escolhido pode realmente abrir a URL de destino com um navegador autorizado ou automação local equivalente e tem as contas de teste, acessórios e permissões de origem necessárias. Nomeie essa rota no briefing; `--permission-mode full` por si só não é acesso ao navegador. Resolva uma simulação de ferramenta com falha antes de tentar novamente, em vez de enviar outro verificador para o mesmo bloqueador; uma simulação com falha não é uma tentativa de ponta a ponta. Planeje uma rodada abrangente de ponta a ponta por Epic, não uma por agente filho ou revisão. Depois de corrigir um defeito, execute novamente apenas os cenários afetados. Repita a rodada abrangente somente quando o escopo de aceitação ou o limite de integração mudar materialmente e registre o motivo. `verifying → merging` precisa de um PASS ativo para o candidato/critério exato ou uma substituição explicitamente fundamentada; uma sentença de verificação por si só não pode concedê-lo. Três FAILs consecutivos são transferidos para o proprietário da Epic pai ativo e, em seguida, para a pessoa, se esse proprietário estiver indisponível; uma falha técnica aumenta separadamente. Somente o proprietário pai designado usa `POST /v1/work/v2/agent/items/<id>/gate-decision`; uma pessoa usa `POST /v1/work/v2/items/<id>/gate-decision`. Nunca use a rota pessoal como Agente. O Board identifica separadamente IA, pessoa e substituições técnicas, nunca como verificador PASS. Na capacidade de detalhes retidos, a pessoa primeiro baixa `GET /v1/work/v2/items/<id>/gate-export`, verifica o resumo do manifesto e, em seguida, confirma `POST /v1/work/v2/items/<id>/gate-purge` com esse resumo e versão do item. A eliminação remove apenas detalhes fechados elegíveis; agregados, dados mais recentes e auditoria permanecem.

**Avançando a fase.** A Sessão proprietária move seu item pelas próprias fases de execução; ninguém mais o faz, e um recibo de turno ou uma condição liberada não. A fase não é um campo de `…/edit` (`phase_not_editable`). Execute cada transição quando o trabalho que ela nomeia realmente aconteceu:

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

O comando lê a versão do item, imprime sua Chave de Idempotência (`--key` tenta novamente a mesma gravação) e imprime o item. É

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "no_landing_reason"?: "…", "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Um passo de cada vez: `assigned → implementing → verifying → merging → deploying → done`. De `verifying` você pode voltar para `implementing`; de `merging`, de volta para `implementing` ou `verifying`. Um item cuja regra de verificação capturada está desligado também pode ir de `implementing` direto para `deploying` em sua evidência de integração (abaixo), com `verification` opcional; um item com a regra ativada é recusado nessa etapa com `verification_gate_on` e percorre toda a linha. Nada mais pula uma fase e `done` é alcançado somente a partir de `deploying`.
- `merging` precisa de `verification`. `deploying` precisa de um destino: uma entrega integrada de um agente filho vinculado a este item, ou `landing` nomeando um commit que o daemon encontra na ramificação `target` local do projeto e `refs/remotes/<remote>/<target>` - faça push antes. Quando o trabalho chega em outro repositório (um item de back-end cuja alteração foi um commit de front-end), `landing.project` (`--landing-project`) nomeia o ID do projeto de `GET /v1/places`, e o commit é procurado lá - um repositório aninhado dentro do diretório do projeto (`cloud/`) é seu próprio Projeto aqui. A prova é escrita como o **registro de destino raiz** do broker, uma vez que: o mesmo item, repositório, commit e destino registrado novamente é o mesmo registro. O histórico do item o nomeia como `landing_id` e `clawdline landings --work-id <item id>` o lista. Antes de `deploying`, o daemon pergunta ao git imediatamente se a ramificação de um filho vinculado foi mesclada, portanto, uma mesclagem feita há pouco conta sem esperar pela próxima olhada do broker. Trabalhar sem código leva `no_landing_reason` (`--no-landing-reason`) em vez de um patamar; é recusado ao lado de um `landing` (`invalid_landing_evidence`), enquanto um agente filho vinculado ainda deve sua integração (`landing_owed`, nomeando a tarefa), e ao lado de um agente filho cuja entrega foi integrada (`landing_recorded`). `done` precisa de `deployment` ou `no_deployment_reason`; o `deployment_policy` do item decide qual (`required` leva apenas `deployment`, `not_required` apenas `no_deployment_reason`, `agent_decides` também). Cada etapa deve ser concluída primeiro.
- `done` libera sua tarefa e move o item para a linha concluída recentemente da Sessão. Adicione um relatório de conclusão (abaixo) antes dele quando for devido.
- Recusas: `invalid_transition` (não há próxima fase ou falta sua evidência), `steps_incomplete`, `not_item_owner`, `item_unassigned`, `item_terminal` (uma pessoa o reabre), `evidence_unknown`, `direct_landing_not_applicable`, `invalid_landing_evidence`, `landing_project_not_found`, `landing_commit_unresolved`, `landing_target_unresolved`, `landing_not_on_target`, `landing_remote_unresolved`, `landing_not_published`, `landing_owed`, `landing_recorded`, `landings_full` (o item mantém 64 integrações de raiz), e `version_conflict`: releia e envie novamente. Uma recusa por falta de integração termina com o que o broker encontrou quando olhou agora há pouco (um branch ainda não mesclado, um repositório que não conseguiu ler).

**Concluindo em um comando.** Depois que o trabalho for concluído, `clawdline item finish <item id>` leva o item de `implementing`, `verifying`, `merging` ou `deploying` para `done` em uma transação:

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?}, "no_landing_reason"?: "…",
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- Cada etapa é aquela que `item phase` realiza, através das mesmas portas, e escreve seu próprio `item.phase_changed`; qualquer passo recusado recusa o todo e nada está escrito.
- A integração é inferida, não informada novamente: o commit é o candidato autorizado da verificação em um item bloqueado, caso contrário, o commit enviado dos filhos vinculados; o destino é a branch indicada pelos registros de integração; o controle remoto é aquele que rastreia. Qualquer campo que você fornecer vence, e o resultado é comprovado contra o git, como `item phase deploying` prova isso. Uma porta de verificação capturada ainda precisa de seu PASS: de `implementing` um item bloqueado é recusado `verification_candidate_required`, então insira `verifying` com `item phase` da árvore de trabalho candidata, aguarde o PASS e termine.
- Um filho vinculado cuja ramificação foi mesclada há pouco é registrado como finalizado pelo próprio final: o daemon pergunta ao git antes de ler as integrações, então `landing_required` logo após uma mesclagem significa que a ramificação não está em um alvo, e a recusa diz o que o broker encontrou.
- Trabalhar sem código: `clawdline item finish <item id> --no-landing-reason "…" --no-deployment-reason "…"`, sob as mesmas regras de `item phase`.
- Um item já `done` é respondido como está e nada está escrito, portanto a mesma integração visto duas vezes não move nada.
- As recusas adicionam `verification_required`, `landing_required`, `deployment_required` (a observação que faltava na etapa), `landing_target_unknown`, `landing_remote_unknown`, `landing_remote_unreadable`, `landing_ambiguous` (nomeie-o com a bandeira), `landing_owed`, `landing_recorded` e `finish_not_started` (ainda antes de `implementing`).

Um item atribuído pode conter `steps`. Uma atribuição bem-sucedida pode semeá-los a partir de duas ou mais linhas da lista Markdown de nível superior na descrição, e um item que você criou com `clawdline item add` carrega suas linhas `--step`. Cada etapa é uma entrada da lista de verificação daquele item, e não outro item do Board. Conclua uma etapa verificada com `clawdline item step-done <item id> <step id>`; ele envia `POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete` com `{"session_id"}`. Nas rotas do Agente `expected_version` é opcional: omitido, a escrita atua na versão atual; nomeado (`--expected-version`), ele é comparado e um obsoleto responde `version_conflict`. Uma transição para `done` é recusada com `steps_incomplete` enquanto qualquer etapa permanece aberta; Clawdline nunca verifica um apenas porque a fase pai avançou.

**Dividindo seu próprio item em etapas.** Quando um item que você possui não tem etapas e o trabalho é de vários estágios — diversas alterações que são verificadas separadamente, ou mais de uma parte do sistema — divida-o você mesmo em suas etapas ordenadas, antes de implementar: duas a oito etapas concretas, cada uma delas você pode verificar por si só. Uma única mudança direta **não** exige etapas; não preencha uma lista para ter uma. Quando o trabalho for maior do que parecia, adicione a etapa.

```
clawdline item step-add <item id> "Wire the route" "Cover it with a test" "Say it in the guide"
```

Os títulos são argumentos ou um por linha stdin não vazia. O comando relê o item antes de cada título, imprime cada chave de idempotência antes de sua gravação e imprime um recibo curto com uma dica `item show`. É uma solicitação exclusiva do proprietário por título,

```
POST /v1/work/v2/agent/items/<id>/steps     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

com `"position"` um após o último passo existente, já que os passos são ordenados por posição. Em seguida, preencha cada um com `clawdline item step-done` assim que for verificado. Esta não é a “iniciativa própria” que é proibida para itens e tarefas do Board: o item já é seu, e seus passos são como você mostra à pessoa as etapas do trabalho que lhe foram dadas.

Quando a resolução de um problema ou incidente exigiu investigação substancial para descobrir a causa raiz ou para distinguir a solução real de alternativas plausíveis, adicione um relatório de conclusão legível pelo usuário antes de avançar o item para `done`. Uma correção direta e diretamente observada não precisa de uma. Escreva o que aconteceu, a causa raiz, o que mudou, como foi verificado e qualquer limite restante em um arquivo e, em seguida:

```
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

Ele envia `POST /v1/work/v2/agent/items/<id>/documents` para você (a parte épica do §10, `clawdline guide pt-BR epic`, lista seus campos). Um curl criado manualmente para essa rota sem a credencial que o comando lê responde `401 unauthorized`. O corpo é Markdown, no máximo 64 KiB. Escreva para a pessoa que relatou o problema, não como um log de depuração bruto, e mantenha os dados privados fora dele. O proprietário ativo deve adicioná-lo antes que o item se torne terminal; reler após um conflito de versão. Um relatório de conclusão é atribuído a uma narrativa e nunca substitui evidências de verificação, integração ou implantação. Quando presente, ele permanece no item fechado do Board e abre diretamente na linha Concluído recentemente da sessão.

`/v1/board` são os cartões antigos do aplicativo Swift, somente leitura. A integração é um fato do broker: um item nunca é marcado como integrado manualmente (`422 landing_is_broker_fact`).

### Epic e Feature: siga a escolha de revisão antes de implementar

Uma Épica cujo ciclo capturou o planejamento precisa de um plano e de uma revisão independente. Uma Feature traz a opção **Precisa de revisão independente** da pessoa (`review_required` no item; `clawdline item steps <id>` imprime). Só quem define, no Board; você não pode e não julga o risco do recurso por conta própria. O daemon lê a opção quando você solicita a inserção de `implementing`.

- **Desmarcado** (o padrão): escreva os critérios curtos de aceitação do Recurso, implemente e execute testes focados. Não escreva um plano para revisão, não envie um filho `plan_review` e não registre uma avaliação de risco.
- **Verificado**, e para cada Epic planejado: use o caminho do plano revisado.

Se você acha que um Recurso desmarcado merece revisão, diga isso à pessoa e deixe-a verificar; não há maneira do agente solicitar um ao daemon.

1. Planeje cuidadosamente e escreva o plano no item:
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. Envie um agente filho somente leitura cuja missão é revisar esse plano criticamente - o que está faltando, errado ou arriscado:
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. Aguarde o agente filho terminar. Um filho de revisão bem-sucedido despachado com `--work-id` registra seu recibo de revisão no item como o próprio documento `plan_review`; verifique `.documents` em `GET /v1/work/v2/items/<id>`. Somente se não estiver lá — por exemplo, o agente filho foi despachado sem `--work-id` — registre-o manualmente:
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
   Executar esse comando novamente para a mesma tarefa é inofensivo: ele responde ao documento que já está lá. Um breve resumo para a pessoa sobre o que o plano mudou em resposta é um documento `other` separado, não uma segunda revisão. Se um plano de Feature for alterado após a revisão, escreva um documento `other` intitulado `Review boundary assessment` após o plano revisado com JSON `{"new_risk_boundary":false,"reason":"..."}` somente quando a alteração permanecer dentro do limite de risco da revisão anterior. Um limite novo ou incerto recebe uma nova revisão focada. O limite existente de duas revisões da Epic ainda se aplica.
4. Divida o trabalho em etapas com `clawdline item step-add <item id> …`.
5. Somente então `clawdline item phase <item id> implementing`.

Planeje a verificação como uma sequência. Cada filho da implementação verifica seu próprio código com testes focados; o proprietário do Epic integra os componentes afetados e executa a menor verificação útil de fumaça entre componentes. Somente depois que o candidato integrado funcionar o proprietário deverá enviar uma verificação real de ponta a ponta e a revisão independente de UX/produto aplicável. Verifique a rota do navegador do verificador, URL de destino, contas de teste, acessórios e permissões antes do envio. Não use tarefas repetidas de verificação somente leitura para descobrir ou solucionar um navegador ausente: corrija o acesso ou escolha primeiro um equipamento de navegador local equivalente. Uma simulação com falha não é uma tentativa de ponta a ponta. Planeje uma rodada abrangente de ponta a ponta por Epic para o candidato estável, não uma por agente filho ou revisão; após uma correção focada, execute novamente apenas os caminhos afetados. Repita toda a rodada somente após uma aceitação material ou alteração de integração e registre esse motivo.

`clawdline item doc` lê o item para sua versão e última posição do documento, imprime sua chave de idempotência (`--key` tenta novamente a mesma gravação) e imprime o item. O corpo vem de `--body-file` ou stdin. É `POST /v1/work/v2/agent/items/<id>/documents` com `{"expected_version", "session_id", "role", "title", "body", "reference", "position"}`; as funções são `spec`, `design`, `test`, `deploy`, `completion_report`, `other`, `plan` e `plan_review`.

Para revisar um documento, escreva-o novamente com os mesmos `--role` e `--title`: o daemon substitui seu corpo, referência e posição, mantém seu id, aumenta sua versão em um e registra `document.revised`. A CLI diz `added … at v1` ou `revised … to vN` e `clawdline item show` imprime o `vN` de cada documento. Enviar o mesmo texto novamente não altera nada e responde ao documento como está, portanto, uma nova tentativa é segura. O texto antigo não é mantido; use um título diferente para manter ambos.

- `plan`, `plan_review` e o limite de revisão nunca são revisados ​​no local: cada gravação adiciona um novo documento, porque a regra de planejamento os lê em ordem e uma revisão nomeia o plano que ele lê.
- Um item comporta no máximo 32 documentos, e um `completion_report` não é um deles: cabe sempre, mesmo em um item completo. Um item contém um `completion_report`; escrevendo outro, sob qualquer título, revisa-o e assume o novo título.
- Um 33º documento é recusado com `documents_full` e nada é escrito; a mensagem nomeia o comando `clawdline item doc` que revisa um documento existente.

- `plan` e `plan_review` pertencem a um Epic ou Feature (`document_role_not_applicable` para outros tipos).
- O `reference` de um `plan_review` é o ID da tarefa do filho Clawdline que revisou o plano. O daemon a aceita somente quando essa tarefa existe (`plan_review_task_unknown`), foi despachada pela Sessão proprietária do item (`plan_review_task_not_owned`), está na linha deste item se nomear uma (`plan_review_task_other_item`), tem o tipo `plan_review` (`plan_review_task_wrong_kind`), finalizado com `success` (`plan_review_task_unfinished`) e não foi despachado antes do plano mais recente (`plan_review_task_stale`). Uma revisão sem plano antes de ser recusada `epic_plan_required`. O documento automático de um filho de revisão passa pelas mesmas verificações, e uma gravação repetida para a mesma tarefa é idempotente.
- `clawdline item phase <item id> implementing` em um planejamento da Epic é recusado sem seu plano revisado (`epic_plan_required` ou `epic_plan_review_required`). Uma Feature sujeita à regra de planejamento que a pessoa marcou para revisão independente é recusado da mesma maneira (`feature_plan_required` ou `feature_plan_review_required`); um não verificado precisa apenas de seus critérios de aceitação. Um plano revisado sobre um recurso verificado também precisa de evidências de limites inalterados ou de outra revisão. Uma Epic planejada pode entrar em implementação diretamente.
- A regra lê o recibo `plan_review` mais recente de sua tarefa de revisão. Cada descoberta tem um `severity` de `blocking` ou `non_blocking` (`important` e `minor`, do modelo mais antigo, contam como sem bloqueio). O veredicto é `safe_to_land` sem descobertas, `proceed_with_findings` quando cada descoberta é `non_blocking` e `changes_required` quando qualquer é `blocking`. Uma análise mais recente com uma descoberta de bloqueio recusa `item phase implementing` e qualquer envio com `--work-id` no item ainda atribuído, exceto `--kind plan_review` (`epic_plan_review_blocking` ou `feature_plan_review_blocking`); a recusa lista as descobertas de bloqueio e os próximos comandos: revisar o plano e, em seguida, enviar uma nova revisão. Somente descobertas sem bloqueio, ou um recibo mais antigo cujas descobertas não tenham gravidade, permitem que o item prossiga. Um Epic recebe no máximo duas revisões: quando a segunda ainda bloqueia, revise o plano para responder às suas descobertas, e o plano revisado entra em implementação sem uma terceira revisão; não envie um.

**Divida a Épica em itens secundários e distribua-os.** Esta é a única exceção para "uma sessão cria um item do Board somente quando a mensagem da pessoa solicitar" e para "somente a pessoa atribui itens": a pessoa que lhe atribuiu a Épica, e essa é a autoridade para dividi-la. Depois que o plano revisado tiver levado o Epic para `implementing`, quando partes dele forem melhor executadas por outras sessões, crie itens de recurso ou problema abaixo dele e atribua-os:

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | description on stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- Os IDs de terminal estão no catálogo de endereços da sessão, `GET /v1/orchestrator/sessions` (`clawdline guide pt-BR send`); a Sessão deve funcionar no Projeto da Epic. Você pode atribuir um filho a si mesmo e `--assign-new` abre uma nova sessão com uma atribuição raiz que nomeia o épico. Sem um sinalizador `--assign`, o filho aguarda sem designação pela pessoa.
- `item child` lê o Epic para sua versão, imprime sua chave de idempotência (`--key` tenta novamente a mesma gravação) e imprime o filho. É `POST /v1/work/v2/agent/items/<epic id>/children` com `{"expected_version", "session_id", "kind", "title", "description", "steps"?, "deployment_policy"?, "assign"?: {"mode": "existing_session", "terminal_id"} | {"mode": "new_session", "assistant"?, "model"?, "persona"?}}`, respondeu `201` com `{"item", "assigned", "assignment_error"?: {"code", "message"}}`. O agente filho está no Projeto da Epic, carrega `parent_id` (a Epic), e seu cartão diz que o dono da Epic, Session, a criou. Suas etapas são suas linhas `--step` ou - quando você não fornece nenhuma - sua lista de descrições depois de atribuída.
- O filho é criado primeiro e atribuído em segundo lugar. Quando a atribuição falha o filho **permanece, não atribuído**, a resposta carrega `assignment_error` com o código da atribuição (`session_unavailable`, `project_mismatch`, `assignment_failed`, …), e o comando sai 1: atribua-o novamente com `item assign`, ou deixe para a pessoa.
- `item assign` é `POST /v1/work/v2/agent/items/<child id>/assign` com `{"expected_version", "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}`; ele move um filho aberto do seu Epic para outra Sessão, a mesma atribuição que a escolha de uma pessoa faz.
- Recusas, cada um sem escrever nada: `not_epic_owner` (você não é o dono do Epic), `parent_not_epic` (o pai não é um Epic), `epic_not_planned` (o Epic ainda é anterior a `implementing`: os filhos saem de um plano revisado), `item_terminal` (o Epic está concluído), `child_kind_not_allowed` (apenas `feature` ou `issue`), `epic_children_full` (um Epic comporta no máximo 32 filhos, abertos ou fechados), `not_epic_child` (`item assign` de um item que não é filho da Epic — a pessoa o atribui, a menos que a mensagem solicite: `clawdline guide pt-BR board`), `invalid_assignment`, `version_conflict`, `persona_not_applicable` (422: uma persona com uma sessão existente) e `unknown_persona` (400: um id que falta no catálogo).
- **Uma persona** é uma função com a qual uma nova Sessão é iniciada: texto adicionado ao prompt do sistema que a faz funcionar da maneira que a função funciona, durante toda a conversa. É apenas para uma nova Sessão (`--assign-new`, `--new`, `dispatch`); uma sessão existente mantém aquela com a qual foi aberta. Nenhum por padrão. Uma persona nunca substitui `CLAUDE.md`/`AGENTS.md`, o resumo `CHILD.md` ou este protocolo. `GET /v1/personas` os lista; os ids (`teams` em cada lista cada equipe em que uma persona está, e uma pode estar em várias):
  - `architect` — planejando um épico;
  - `backend` — um daemon, API ou recurso de armazenamento;
  - `frontend` — um recurso de console ou layout de telefone;
  - `minimal-change` — um problema: a menor correção válida;
  - `code-reviewer` — revisão e `plan_review` filhos;
  - `reality-checker` — verificando: evidência antes de "funciona";
  - `security` — permissões de toque de trabalho, emparelhamento ou nuvem;
  - `technical-writer` — documentos e guias;
  - marketing, para um blog, site ou repositório de documentos: `seo` (páginas e metadados), `content-writer` (artigos redigidos em arquivos), `ai-search` (páginas que os mecanismos de resposta de IA podem citar), `social-media`, `instagram`, `email` (boletins informativos), `growth` (experimentos medidos) e `pr` (anúncios).
  - produto, qualidade e operações: `product-manager`, `sprint-prioritizer`, `feedback-synthesizer`, `trend-researcher`, `ux-researcher`; `test-automation`, `accessibility`, `performance`, `api-tester`, `evidence-collector` (regras APROVADO ou REPROVADO por reivindicação da prova capturada); `sre`, `devops`, `incident-commander`, `finops` e `secrets`.
  - design e negócios: `ui-designer` (telas no sistema de design do projeto), `ux-architect` (fluxos e estrutura de layout), `brand-guardian` (consistência da marca), `ui-finish-gate` (verificação visual antes do envio), `image-prompt` (solicitações de geração de imagem), `pricing`, `customer-success`, `support` (respostas elaboradas), `analytics` (respostas de dados reais), `devrel` (amostras executadas) e `privacy` (verificações de dados pessoais; não aconselhamento jurídico).
  - `zero-review-lead` — possui um épico de revisão que reexamina um recurso ou processo existente do zero: planeja as lentes de função, despacha-as como filhos revisores somente leitura com um pacote de fatos compartilhado e transforma suas evidências em um design de destino; sua habilidade é `zero-based-review`.
- **Adicione uma análise independente de UX/produto quando a Epic alterar uma experiência pessoal.** No plano, classifique se a Epic altera uma interface humana, a jornada do usuário ou a política do produto. Em caso afirmativo, antes da fusão, envie pelo menos um filho especialista somente leitura, usando `ux-architect` por padrão para layout, interação e fluxo de produto de ponta a ponta:

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

  Seu resumo nomeia o candidato integrado e pede evidências de desktop e dispositivos móveis com menor suporte, comportamento do teclado e do leitor de tela, becos sem saída, adequação do produto, severidade e uma recomendação concreta. Deve **marcar como não verificado e dizer o porquê** quando a evidência não estiver disponível. Use `product-manager` quando a política e o escopo, e não o layout, forem o risco dominante; adicione `ui-finish-gate` quando uma passagem visual pré-envio separada for material. Resolva todas as descobertas de bloqueio e registre o ID da tarefa, o veredicto e a disposição na evidência de verificação ou no relatório de conclusão da Epic. Se não houver impacto na pessoa, diga o porquê no plano e não adicione cerimônia de revisão. Registre o escopo coberto por esta revisão. Normalmente despache cada especialista relevante apenas uma vez para o Epic integrado; não envie rotineiramente UX, marca, segurança e outras funções como uma lista de verificação. Feche você mesmo as correções de cópia pequena, espaçamento, teste ou localização no escopo com verificações focadas. Reenviar somente quando uma alteração posterior alterar materialmente a jornada do usuário, a política do produto, a direção da marca, o limite de segurança ou outro risco fora do escopo registrado; nomeie o limite alterado e solicite apenas o especialista relevante. Esta revisão nunca substitui o portão do plano capturado ou o verificador de candidato exato PASS da regra de verificação.
- **Você permanece responsável pela Epic depois que cada filho for mesclado.** Releia imediatamente o item desse filho e `clawdline item steps <child id>`; verifique se cada etapa foi concluída. Uma mesclagem não fecha o filho e `merging` não é um estado de repouso. A Sessão proprietária do agente filho deve concluir todas as etapas restantes, registrar um recibo de destino para o commit exato já acessível a partir do destino local e `origin/main` e, em seguida, avançar `deploying` → `done` com evidência de implantação ou um motivo de não implantação correspondente à sua política de implantação. Se você é o dono do agente filho, faça você mesmo essas ações. Se outra Sessão for proprietária, entre em contato com esse proprietário ou use o caminho autorizado de reatribuição de filho; não se faça passar por seu proprietário (`not_item_owner`). ACK o aviso de conclusão do broker onde houver, em seguida, classifique os resíduos da árvore de trabalho e remova apenas material comprovado idêntico ao que foi integrado ou temporário da tarefa. Mantenha alterações ainda não integradas, mistos ou desconhecidos para o próximo proprietário. Não declare o Epic pai concluído até que cada filho seja `done` ou `cancelled`: `epic_children_open` nomeia quantos restam. Não crie nenhum item do Board além dos filhos do Epic.
- **Um filho concluído não fecha sua sessão raiz de recurso independente.** Para cada Root aberto pelo Epic com `--assign-new`, use `clawdline session close --dry-run --terminal <id>` (ele responde `closing as epic_owner` apenas para um Root que este Epic abriu) para ler seu `closeability`; não infira propriedade a partir de um rótulo ou posição terminal e não trate `clawdline session report` como fechamento. Depois que o agente filho chegar a `done`, peça ao proprietário do Root para auditar suas próprias tarefas, integrações, avisos, tarefas e árvore de trabalho e, em seguida, conclua seu relatório de fechamento. Obtenha um atestado somente através de uma rota suportada pelo daemon atual; a rota de fechamento do Swift aposentada não é essa rota. Se essa rota ou um fechamento protegido não estiver disponível, registre o bloqueador do produto e o próximo proprietário e retenha a Sessão. Somente quando a identidade e o trabalho forem verificados e `closeability.state=safe` poderá `clawdline session close --terminal <id>` encerrá-lo; ele relê o inventário primeiro e uma segunda execução responde `session_not_found` quando ele desaparece. Não ignore a proteção com `clawdline close <terminal id>`. Acompanhe `blocked` com seu movedor nomeado. Para `unknown` (incluindo `terminal_unreadable`), preserve a Sessão e registre a evidência faltante e o próximo proprietário; não force o fechamento, arquive ou afirme que foi apagado. Antes de declarar a coordenação épica concluída, enumere o resultado próximo de cada Root ou bloqueador nomeado. O Board `done` não substitui este inventário.

## 11. Coordenação

**O coordenador da máquina ("Clawdfather").** Ele funciona em um espaço de trabalho da máquina de propriedade do daemon, fora dos Projetos, para relatar sessões e gerenciar operações de máquina suportadas. Ele nunca edita o código-fonte do projeto, incluindo Clawdline. Na solicitação explícita da pessoa para trabalho de engenharia, crie primeiro um item do Board do Projeto com `clawdline item add --project … --assign-new` e depois delegue para uma Sessão do Projeto. Sem esse pedido, proponha um item para a pessoa aceitar. O proprietário do projeto designado cuida do envio, verificação e integração dos filhos. O espaço de trabalho é um limite organizacional, não uma área restrita do sistema de arquivos. Novas ligações devem vir dessa área de trabalho; as ligações existentes permanecem legíveis. Abra a Sessão na ação Clawdfather separada do console e registre seu ID de conversa. Consulte `docs/clawdfather-role.md` para obter o limite do produto. Execute `clawdline coordinator bind` dentro dessa nova sessão para registrá-la ou religar um antecessor offline comprovado. O comando lê seu próprio ID de conversa e se recusa a substituir um suporte on-line ou ilegível. `GET /v1/orchestrator/coordinator` inspeciona a função; `/coordinator/bearings` é a máquina em resumo (tarefas ativas, integrações pendentes, esperas abertas, cartas mortas, arrendamentos retidos e o que é `unknown`). `POST …/coordinator/register` com `{"session_id": "<conversation id>"}` assume a função; `POST …/coordinator/rebind` o move quando a sessão vinculada está offline (`expected_coordinator_id`, `expected_generation`). A sucessão responde a `501 succession_unavailable`.

**Arquivo em espera.** Uma espera diz "avise-me quando o proprietário terminar com esses caminhos". É um registro e uma mensagem, não um bloqueio ou monitoramento de arquivo.

- `POST /v1/orchestrator/waits` — `{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}` (IDs de sessão são IDs de conversa). O proprietário é informado uma vez, em seu compositor.
- O proprietário finaliza: `POST /v1/orchestrator/waits/<id>/release` com `{"owner_session_id", "commit"?, "note"?}`; cada sessão em espera é informada. Nenhum temporizador libera uma espera.
- Uma sessão em espera desiste: `POST …/waits/<id>/cancel` com `{"waiter_session_id"}`.
- `409 owner_busy` e `502 request_delivery_failed` significam que **a espera foi registrada**, mas o proprietário ainda não foi informado. `502 release_incomplete` lista quem ainda está pendente: envie novamente a liberação.

**Reservas.** Dois recursos: `heavy_compile` (um slot de compilação da máquina) e `landing` (um por checkout).

**Execute uma compilação ou um conjunto de testes por meio de `clawdline heavy -- <command>`**, não vazio. Ele faz fila para `heavy_compile`, espera até que a máquina tenha memória disponível (um quarto dela, no máximo 1 GB, e nenhuma parada de memória acima de 10%), executa o comando com uma prioridade mais baixa - no Linux também como a primeira coisa que o kernel mata se a memória acabar - renova a reserva enquanto ele é executado e o libera depois. Mantém o status de saída do comando. Ele nunca se recusa a construir para um daemon ausente ou para uma recusa que ele não conhece: ele executa o comando de qualquer maneira com uma frase em stderr. Quando `--max-wait` (padrão 30m) passa antes de ter o slot e a memória, ele desiste de seu lugar, não executa o comando e sai **75** - um código com o qual nenhuma falha do próprio comando é confundida; execute-o novamente mais tarde. Enquanto espera, ele imprime uma linha quando a espera começa e outra quando ela termina, nada entre elas: espere uma vez, por muito tempo (§2, "Aguardando um comando longo"). Um `heavy` dentro de um `heavy` é executado diretamente. `--min-available 1500M` pede mais; `--no-slot` verifica apenas a memória. Em um repositório que o possui, `tools/heavy.sh <command>` encontra o binário para você.

- `POST /v1/orchestrator/leases` — `{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`. Responde `granted` ou `queued` com um `position` e `retry_after_seconds`. Uma solicitação na fila pergunta novamente com o mesmo `request_id`.
- `POST …/leases/renew | release | cancel` com `{"request_id", "resource", "checkout"}`. O detentor renova com `/renew` (pedindo novamente `POST /v1/orchestrator/leases` com seu próprio `request_id` renova também).
- Renove dentro de 60 segundos ou a reserva será considerada perdida. `409 lease_lost` significa que sim. `429 queue_full` com 32 solicitações em espera.

**Gráficos** (`GET /v1/orchestrator/graphs`) são visualizações somente leitura calculadas a partir dos campos `graph` das tarefas despachadas. **Reclaim** (`/v1/orchestrator/reclaim`) varre checkouts finalizados; um POST é uma simulação, a menos que o corpo diga `{"dry_run": false}`.

## 12. O que fazer quando algo é recusado

- Ramificação em `error.code` (ou `error` na forma plana). A mensagem é para as pessoas.
- `retry_after` significa que é uma resposta de capacidade: espere esse tempo e envie a mesma solicitação.
- `409 stale_write`, `503 orchestrator_store_busy`: o armazenamento estava ocupado; a mesma solicitação novamente é segura.
- `unknown` em qualquer lugar — uma propriedade, uma atividade, uma fonte — significa que o daemon não conseguiu lê-lo. Não está “ausente” e nada deve ser deletado ou declarado morto nele.
- Se uma rota que você esperava responder `404 not_found` ou `501`, ela não está neste daemon. Diga isso; não recorra às rotas do aplicativo Swift ou aos subagentes nativos do provedor em seu lugar.
