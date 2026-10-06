# Clawdline ガイド

**Clawdline Next** が実行されているマシン上のアシスタント セッション (Claude Code または Codex) の場合。ここでは、このデーモンが現在どのような役割を果たしているかを説明しますが、それ以外のことは説明しません。以下のすべてのルートは、このガイドを出力したビルドによって登録されており、登録されていないルートがある場合はテストは失敗します。コピーを信頼するのではなく、`clawdline guide` を使用して再度出力します。 `clawdline guide zh-TW` は繁体字中国語の同じガイドです。 `clawdline guide` はコアを出力し、他の部分に名前を付けます。対象となる作業に到達したらその一部 (`clawdline guide dispatch`) を出力するか、全文の場合は `clawdline guide all` を出力します。何を出力するにしても、コアも含めて、`guide-version: <sha256>` で始まります。 `--since <hash>` を使用して同じコマンドを実行すると、テキストが変更されていない場合は、代わりに `unchanged <hash>` という 1 行が出力されます。 `clawdline guide refused <code>` は、拒否コードを説明する部分を出力し、名前を付ける部分がない場合は stdout に何も出力せずに 1 を終了します。

このガイドでは、**ステップ**はアイテムのチェックリストの一項目、タスクの**書き込み**は変更する可能性のあるパス、**割り当て**はアイテムの所有者、**パート**はこのガイドの名前付き部分を指します。

## 0. Swift アプリから Clawdline を学習した場合は、最初にこれをお読みください

Swift アプリは 2026 年 9 月 19 日に廃止されました。アプリは停止され、ログイン時に開始されなくなり、ポート 7717 には何も応答しません。そのディレクトリ `~/.config/clawdline` はまだディスク上にあり、このデーモンが保持しなかった履歴については読み取り専用です。このデーモンはそのアプリのコピーではありません。特に次の 5 点に注意してください。

1. **タスク ディレクトリは `/tmp/.clawdline` ではなく、`<state dir>/tasks` です。** `/tmp/.clawdline` は Swift ブローカーのものでした。 2 つのブローカーがタスク ID の 1 つのディレクトリを書き込むと、誰も見ていないところで衝突してしまうでしょう。どちらもハードコーディングしないでください。インベントリ (§3) から `task_root` を読み取り、その下に `task.json` を書き込みます。
2. **ワークフロー エンベロープや呼び出すワークフロー ルートはありません。** メッセージにはボード分類が含まれなくなり、セッションが単独でボード カードを開くことはなくなりました。 `POST /v1/orchestrator/sessions/<terminal>/workflow` は、古いヘルパーがターンの途中で失敗しないようにするためにのみ存在します。これは `workflow_retired` に応答し、何も記録せず、新しいコードはそれを呼び出してはなりません。
3. **ボードへの参加は提案と決定を通じて行われます** (§10): セッションが提案し、人が回答します。 `begin` または `deliver` には何もありません。
4. **接続先が異なります。** ポートは 7727 (または `CLAWDLINE_NEXT_PORT`)、状態ディレクトリは `~/.config/clawdline-next` (または `CLAWDLINE_NEXT_DIR`) です。`~/.config/clawdline` は決して読まないでください。そのトークンはこのデーモンのものではなく、`401 unauthorized` で拒否されます。
5. **Swift アプリにはあり、このデーモンにはありません:** 永続レポートのプロモーション (`501 durable_report_promotion_unsupported` と回答)、コーディネーター継承 (`501 succession_unavailable` と回答)、およびブリーフ フィールド `serialize` および `attach_session` (それぞれ `bad_task` として名前で拒否されました)。 `reasoning_effort` は、`codex` タスクでのみサポートされます: `high` または `xhigh`。

## 1. ルートまたは子

最初のメッセージが *「あなたはタスク … の Clawdline CHILD エージェントです」* なら、あなたは **子エージェント** です。指定された `CHILD.md` の指示に従ってください。子はディスパッチもターンの完了記録も行いません。`clawdline task accept` で受諾し、`clawdline task finish` で終了します。ここで読むのをやめてください。

それ以外の場合、あなたは**ルート**、つまり誰かが話している通常のセッションです。残りはあなたのものです。

*「あなたは独立して所有されている Clawdline フィーチャー ルートです…」* と表示されている場合、またはボード アイテムがあなたに割り当てられている場合は、次に `clawdline guide feature-root` を出力します。これは、アイテムの読み取りから `done` までの通常のパス全体であり、よりまれな場合に出力する部分に名前を付けます。

## 2. デーモンへの到達

**手書きの curl ではなく、存在するコマンドを使用してください。** コマンドは独自のプロセス内で認証情報を読み取るため、コマンド ライン、`ps`、出力、トランスクリプトには認証情報が表示されることはありません。その認証情報を持たない手書きの curlは、`401 unauthorized` (「このリクエストには有効な認証情報がありませんでした…」) と応答します。これは、不足している認証情報であり、アクセス許可ではありません。代わりにコマンドを実行してください。

| コマンド | 機能 |
|---|---|
| `clawdline guide [zh-TW]` | このガイド。デーモンは必要ありません |
| `clawdline session report --summary "…"` | 終了したターンを記録します (§7) |
| `clawdline session close [--dry-run] [--terminal id]` | 終了したセッションを監査し、決して強制的に閉じません (§2a) |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | 所有する子をディスパッチする (§4) |
| `clawdline item show\|steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | 所有するボードアイテムを読み込んで進めます (`clawdline guide feature-root`、§10) |
| `clawdline todo add\|list\|done` | 本人が要求した場合のみ、このセッション独自の To-Do を実行します (§10) |
| `clawdline heavy -- <command…>` | マシンの 1 つのコンパイル スロットでビルドまたはテスト スイートを実行します (§11) |
| `clawdline send --to <terminal> "…"` | メッセージを別のセッションに中継します (§8) |
| `clawdline notify --title "…" --body "…"` | 通知を個人にプッシュ通知します (§9) |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | セッションの上に実用的なメモを 1 つ残します (§9a) |
| `clawdline assistants` | 各アシスタントのアカウントに残っているもの |
| `clawdline landings` | このマシンに残るすべてのランディング作業。`--work-id <item id>` で 1 つのボード アイテムに記録されたランディングを表示 |
| `clawdline leases [--json]` | 誰がコンパイル スロットと各ランディングリースを保持し、誰がその後ろで待機しているか |
| `clawdline sessions [--json]` | 送信、待機、またはハンドオフで名前を付けることができるセッションとその状態およびタスク |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | カテゴリ別のセッション、子タスク、またはボードアイテムの支出額。デフォルトであなたのもの |
| `clawdline cloud pair [--offer <code>]` | 1 つのクラウド ブラウザとこのマシンをペアリングします |
| `clawdline task show [--json] <task id>` | 1 つの子タスクをコンパクトに: 状態、評決、概要、残されたタイトル、検証、ランディング、チェックアウト (§5) |
| `clawdline task wait <task id>… [--timeout 9m] [--any]` | 子が終了するまで待機し (すべて、または `--any` の 1 つ)、`task show` が行うようにそれぞれを表示し、その通知を閉じます。終了 0 はすべて成功、1 1 つは失敗、5 1 つはキャンセルされ失敗なし、3 タイムアウト、4 タスクを読み取れませんでした。 4 オーバー 3 オーバー 1 オーバー 5 (§5) |
| `clawdline callback --title "…" [--timeout 30m] [--work-id <item>] -- <command…>` | 長いコマンド (デプロイとその確認、CI の待機) をデーモンの管理下で実行し、すぐに制御を返します。ターンを終えると、子タスクの完了通知と同じ形式の `<clawdline-notice>` が届きます (§5a、`clawdline guide callback`) |
| `clawdline task cancel <task id> --reason "…"` | 誤ってディスパッチした子を停止します。タブは閉じられ、書き込みとスロットは解放され、コミットのあるブランチは保持されます (§5) |
| `clawdline task ack <task id> <notice id>` | 完了通知を手動で閉じます。 `task show` と `task wait` がそれを閉じるため、ほとんど必要ありません (§5) |
| `clawdline task accept <task dir>` | 説明会に署名する子エージェント。ルートは決して実行しない |
| `clawdline task finish <task dir>` | 子エージェントの完成。ルートは決して実行しない |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | 任意のマシン上で Cloud Webhook を通じてスケジュールを開始し、その結果を待ちます。終了コードは、どのように終了したかを示します (「将来の作業をスケジュールする」)。デーモンは必要ありません |

上記のオーケストレーション コマンド (`webhook fire` ではない) は、成功するとデーモンの JSON を出力します。拒否の場合は、`refused, <status> <code>: <message>` を出力し、その後、拒否の各スカラーが `key: value` として 1 行に 1 つずつ表示され、修正が最後に行われ、1 で終了します。クラウド コマンドは、人間が判読できる独自の成功出力とエラー出力を使用します。 `--port` はポートをオーバーライドします。

`clawdline usage` はトークン台帳 (リポジトリ内の `docs/token-ledger.md`) です。各トークンが何に使われたか — `board`、`protocol`、`rules`、`impl`、`delegate`、`harness`、 `talk`、`compaction`、`other`。フラグを指定しないと、`CLAUDE_CODE_SESSION_ID` または `CODEX_THREAD_ID` という名前の独自のセッションが読み取られます。ヘッダー行が 1 行 (コール、ピーク コンテキスト、コスト)、コスト (シェア、トークン、コスト) ごとにカテゴリごとに 1 行、そしてすべてのギャップが出力されます。 `--json` はデーモンの応答を出力します。 `rules` は上限であり、そのとおりです。1 つのシェル コマンドで実行されるガードと他の作業は、そのコマンド全体を受け取ります。ルートは `GET /v1/usage/sessions/<conversation>`、`GET /v1/usage/tasks/<task id>`、および `GET /v1/usage/items/<item id>` で、ペアリングされたデバイスまたはオーケストレーター トークンで読み取られます。台帳が読み取っていない、または読み取れなくなったセッションは、`not_yet_read`、`transcript_missing`、または `transcript_unreadable` を返します。合計が空になることはありません。誰も知らない ID は 404 `unknown_session`、`unknown_task`、または `unknown_item` です。台帳がまだ読み取り中かどうかは、`/v1/diagnostics` の `usage` です。

**長いコマンドを待機しています。** `clawdline heavy`、`clawdline dispatch`、および長いテスト実行は、待機中に何も出力せず、自動的に終了します。数秒ごとにチェックするのではなく、**1 回の長い待機**で待ちます。各チェックはコンテキスト全体を再読み込みするターンであり、トークン レビューでは、10 項目にわたってそのようなターンが 520 回 (7,220 万トークン) カウントされ、そのほとんどがキューに入れられた `heavy` の実行でした。

- **Claude Code:** 長い `timeout` (最大 `600000` ミリ秒) または `run_in_background` を伴う Bash 呼び出しが 1 回あり、その後は完了通知が到着するまで何も行われません。 `sleep` と `tail` のループではありません。
- **Codex (codex-cli 0.157.1、コード モード):** `// @exec: {"yield_time_ms": 600000}` を `functions.exec` セルの最初の行に置きます。 `exec_command` がセッション ID を返した後、空の `chars` と `yield_time_ms: 300000` を指定して `write_stdin` を待ちます。それでも実行される場合は、同じセル内で繰り返します。測定値: `600000` では外側のセルが 330 秒間開いたままになりましたが、空の `write_stdin` では最大 300 秒待機しました。外側のセルが降伏した場合は、`wait` とロング `yield_time_ms` を使用して収集します。

  子の待機またはビルドには 1 つのセルを使用します。コマンドのみを置き換えます。通常の 30 秒間の `exec_command` リターンによってエージェントがウェイクアップして同じ待機を再度発行しないように、セル内でセッションのポーリングを維持します。

  ```js
  // @exec: {"yield_time_ms": 600000}
  let r = await tools.exec_command({cmd: "clawdline task wait --timeout 9m TASK_ID", yield_time_ms: 30000});
  while (r.session_id) {
    r = await tools.write_stdin({session_id: r.session_id, chars: "", yield_time_ms: 300000});
  }
  text(r.output);
  text(`exit ${r.exit_code}`);
  ```

  ビルドの場合は、コマンドを `tools/heavy.sh …` に置き換えて、その終了コードを保持します。75 は、ビルドの実行前にコンパイル スロットまたはメモリ待機の期限が切れたことを意味します。

`clawdline heavy` は最大 `--max-wait` (デフォルトは 30 分) 待機し、コマンドを実行せずに 75 で終了します。ツールが許容するよりも長い待ち時間は、バックグラウンド実行です。

**curl でオーケストレーターのルートを呼ぶ場合。** `<state dir>/orchestrator-token` を読み取り、`X-Clawdline-Orchestrator` ヘッダーで送信します。トークンをコマンド引数に含めないでください。`DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"`、次に `-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")` を使用します。`curl --fail-with-body` を指定してください。JSON 本文を含む POST には `-H 'Content-Type: application/json'` も必要です (指定しないと `415 unsupported_media_type`)。

### クラウドブラウザをペアリングする

ペアリングにより、このマシンを読み取れる人が変わります。ペアリングされたブラウザはそれをすぐに読み取り、Cloud `commands` がオンの場合はそれを駆動する可能性があります。ペアリング コマンドを実行するのは、相手がそのブラウザのペアリングを明示的に要求した場合、または正確なペアリング コマンドやオファーを提供した場合に限られます。ペアリングしてもコマンドは有効になりません。それは別の設定のままです。

サポートされている方向は 2 つあります。

1. **ブラウザにオファーが表示されます。** 表示される行をそのままマシン上で実行してください。

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

   一重引用符はそのままにしておきます。このオファーは不透明で、有効期間が短く、1 回限りのシークレットです。最終的な回答では、解読、編集、保存、または繰り返しを行わないでください。有効期限が切れているか、すでに使用されている場合は、再試行または変更する代わりに、ブラウザから新しいオファーを取得します。
2. **マシンが招待を作成します。** `clawdline cloud pair` を実行します。ワンタイム `https://app.clawdline.com/#pair=…` リンクを出力して待機します。ユーザーは、同じ Clawdline クラウド アカウントにサインインしながら、ペアリングするブラウザーで完全なリンクを開きます。リンクをオファーと同様に扱い、公開したり保持したりしないでください。

成功すると 3 行が出力されます: `paired` はブラウザーのデバイス ID、`browser` はブラウザーのフィンガープリント、`machine` はマシンのフィンガープリントです。ブラウザのフィンガープリントをブラウザに表示されるフィンガープリントと比較し、マシンのフィンガープリントをこのマシンに表示されるフィンガープリントと比較します。不一致は成功ではありません。`paired` ID を使用して `clawdline cloud revoke <device-id>` をすぐに実行し、不一致を報告します。 `clawdline cloud devices` には、現在のブラウザーの名簿とそのローカルの信頼ステータスがリストされます。これは、ペアリング後に使用する読み取り専用チェックでもあります。

これらのコマンドは、実行中のローカル デーモンを通過します。いずれかが失敗した場合は、正確な stderr を報告します。担当者が別途変更を要求しない限り、クラウドをオンにしたり、ログインしたり、コマンドを有効にしたり、キーをローテーションしたり、提供されたオファーを置き換えたりしないでください。

**接続先と状態ディレクトリ**

- ポート: `CLAWDLINE_NEXT_PORT`、それ以外の場合は **7727**。ループバックのみ: `http://127.0.0.1:<port>`。
- 状態ディレクトリ: `CLAWDLINE_NEXT_DIR`、それ以外の場合は `$XDG_CONFIG_HOME/clawdline-next`、それ以外の場合は `~/.config/clawdline-next` (Windows では `%APPDATA%\clawdline-next`)。
- `GET /v1/health` は認証情報を必要とせず、`served_by: "clawdline-go"` に応答します。 「実行しない」と「拒否」を区別するために使用します。

**認証情報。** 3 つ存在し、セッションでは最初のものが使用されます。

| 認証情報 | どこで | として送信 | 開く |
|---|---|---|---|
| オーケストレーター トークン | `<state dir>/orchestrator-token` | ヘッダー `X-Clawdline-Orchestrator` | `/v1/orchestrator/`、`/v1/work/`、`/v1/board`、`GET /v1/places`、`POST /v1/artifacts/images` に基づくすべて |
| タスクシークレット | ディスパッチ時にルートによって選択される | ヘッダー `X-Clawdline-Task-Secret` | `/v1/orchestrator/tasks/<id>/` および `POST /v1/orchestrator/proposals` に基づく子自身のルート |
| デバイストークン | `<state dir>/local-token`、またはペアリングされたデバイスの | `Authorization: Bearer` | コンソールのルート (`/v1/sessions/…`)。セッションには必要ありません |

`Bearer` として送信されたオーケストレーター トークンはデバイスと比較され、拒否されます。間違っている、または欠落しているトークンは `401 unauthorized` を取得します。これは、トークンが欠落しているか、このデーモンのものではない、またはデバイスがペアリングされていないことを示します。 `clawdline doctor` は、CLI が読み取るディレクトリとポートを出力します。

**curl** を使用する必要がある場合は、コマンド ラインにトークンを含めないでください。

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

- `--fail-with-body`: これがないと、拒否は 0 で終了し、成功と同じように読み取られます。
- **本文のあるすべての POST には `-H 'Content-Type: application/json'` が必要です**。そうしないと、`415 unsupported_media_type` で拒否されます。 `curl -d` のみがフォーム タイプを送信します。
- ルートにそれ以下の記載がない限り、ボディは 2 MiB に制限されます。
- `%47` などの tmux 端末 ID は、1 つのセグメント `%2547` としてエスケープされたパスに入ります。

**拒否には 2 つの形があります。** 文ではなく、コードで分岐します。

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}` — ゲートとブローカー。 `retry_after` などの追加機能は `error` 内に収まります。
- `{"error":"<code>","detail":"…"}` — 存在しないルート、間違ったメソッド、および一部の読み取り。

このデーモンが所有していないルートは `501 not_implemented` として拒否され、拒否によりルートに名前が付けられます。それが普通のマシンの答えです。これは、誰かが意図的に `CLAWDLINE_NEXT_UPSTREAM_PORT` を使用してこのデーモンの背後に別のデーモンを配置し、応答しなかったアドレスに `502 upstream_unreachable` を指定した場合にのみ転送されます。どちらもこのデーモンからの応答ではありません。 2026 年 9 月 19 日より前は、転送がデフォルトでオンになっていて、7717 の Swift アプリに送信されていたため、その時に書かれたメモには、所有されていないルートがそのアプリに到達すると記載されます。そうではありません。

### プロジェクトの Clawdline 表示を設定する

作業中のプロジェクトを Clawdline で読みやすくしてほしいと依頼された場合にこの部分を使用します。結果は「いくつかのファイルが存在する」ということではありません。それは、プロジェクトには真実の名前とマークがあり、長期にわたる作業は進捗状況を報告でき、その開発サーバーは Clawdline が起動しなくても確認できるということです。

まず、このリポジトリの説明、README、デプロイ/ビルド スクリプト、および既存のプロセス マネージャー構成を読んでください。プロジェクトがすでに使用しているコマンドを保存します。 Clawdline のためだけに 2 番目のデプロイ パスやプロセス スーパーバイザを追加しないでください。また、操作上の変更を要求しない限り、何も開始、停止、再起動、またはデプロイしないでください。構成と実際の展開は別の作業です。

以下の 4 つのチェックを実行し、本当に適用されない場合にのみチェックをスキップします。

1. **プロジェクト。** `clawdline project list` を実行します。このチェックアウトがない場合は、リポジトリ ルートを `clawdline project add <absolute-root>` で追加し、再度リストします。これは、セッションが開始される可能性のある場所を記録します。リポジトリは変更されません。
2. **名前とアイコン。** 何も設定されていない場合、Clawdline は安定したアイコンを派生します。ユーザーが意図的な名前またはピクセル マークを必要とする場合は、`~/.claude/project-icons.json` 内の 1 つおきのエントリを保存し、このプロジェクトの最長のパスを含むパスのみを編集します。この形式は、Clawdline リポジトリの `docs/project-status.md` に文書化されています。 「プロジェクト」ページでは、JSON を手動で編集せずに、既存の解決済みアイコンをコピーすることもできます。グローバル ユーザー ファイルはリポジトリのコンテンツではありません。リクエストでその編集がまだ承認されていない場合は、変更する前に提案された正確なエントリを表示します。
3. **展開と長時間の作業。** Clawdline はステータスの受信のみを読み取ります。デプロイは決して実行されません。 GitHub リポジトリの場合、デプロイ受領書は `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json` で、所有者とリポジトリは `origin` から取得されます。実行をすでに認識しているプロデューサーは、`state` (`running`、`ok`、`fail`、または `none`)、`label`、`url`、`started_at`、および測定された `typical_seconds` をアトミックに書き込みます。ローカルのビルド、テスト、インポート、またはデプロイ コマンドの場合、そのヘルパーが存在する場合は `clawdline-progress run --label <label> -- <command>` を使用するか、`docs/project-status.md` から `run-<path>.json` コントラクトを実装します。期間を決して考え出さないでください。測定されるまで省略してください。強制終了されたプロデューサーは永続的な実行状態を離れてはなりません。
4. **開発サーバー。** 最も近いデプロイ可能なルートで `.devstack.json` を追加または更新します。 Go デーモンは現在、宣言された `processes` を読み取り、そのループバック `port` を調査するか、`url` を開きます。ブラウザから `status`、`up`、`down`、`restart`、または `logs` コマンドは**実行されません**。最小の真実の Tier 0 ファイルを優先します。次に例を示します。

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

   安定したポートまたは URL を持たないプロセスをファイルに推測すべきではありません。開発宣言を検証している間は、実稼働をプローブしたり再起動したりしないでください。

変更された各レイヤーを個別に検証します。`clawdline project list` はチェックアウトに名前を付けます。すべての JSON ファイルが解析されます。変更されたスクリプトに対するリポジトリ独自のテストに合格します。 `GET /v1/devstacks` は、宣言されたサーバーを黙って省略するのではなく、実行中、停止中、または不明として表示します。プロジェクトのセッションには、新しい進行状況/デプロイのレシートが表示されます。読み取りが利用できない場合、形式が間違っている場合、または古い場合は、どれであるかを答えて不明のままにしておきます。読み取りがないことを成功として報告しないでください。最後に、構成されたもの、意図的に適用されなかったもの、および git の外部で変更されたユーザー所有のファイルをリストします。

**統一: クロードと Codex** (`/clawdline unify`) のルールとスキルのセット。 Codex は `AGENTS.md` および `.agents/skills/<name>/` を読み取ります。クロードは、`CLAUDE.md` (および `CLAUDE.md` が存在しないか、`@AGENTS.md` 行がある場合のみ `AGENTS.md`) と `.claude/skills/<name>/` を読み取ります。プロジェクトは、ルールが `AGENTS.md` にあり、`CLAUDE.md` が存在しないかインポートされている場合に統合され、すべてのスキルが `.agents/skills/<name>/` に存在し、それに相対リンクされた `.claude/skills/<name>` が存在します。ユーザーがそれを要求するか、`/clawdline unify` を呼び出すと、次のようになります。

1. セッションのプロジェクトで `clawdline project unify` を実行します (git のトップレベル。リストにない場合は、最初に `clawdline project add` を持つプロジェクトを追加します)。それは何も変わりません。クロードと Codex がそれぞれ今読んでいる内容、今後読む予定の内容、すべてのスキルの行、すべてのアクション センテンス、すべての対立を、その人の言語で示します。これには、Codex が見ていない `CLAUDE.md` の行も含まれます。
2. `clawdline project unify --apply` は、この会話内のその人自身のメッセージがその計画を承認した後にのみ実行してください。あなたが示したバージョンを送信します。それ以降にディスクが変更された場合、`plan_changed` と応答し、何も適用されません。計画を再度出力して、もう一度尋ねます。
3. `clawdline project unify --check` (出口 0 統一、1 ドリフト、3 不明) の結果を表示します。何もコミットされていません。どのファイルが変更されたかを伝え、その人またはセッションがそれらをコミットします。

本人からのメッセージがない限り、`AGENTS.md`、`CLAUDE.md`、またはスキルを編集して競合を自分で解決しないでください。2 つのディレクトリ間で異なるスキル、他の場所を指しているリンク、または Codex が認識していないルールは、本人が決定します。ルートは `GET /v1/projects/{place}/unify` (計画) と `POST /v1/projects/{place}/unify`、`{"version"}` と `Idempotency-Key` です。拒否は、`plan_changed`、`plan_unknown` (プロジェクトの一部を読み取れなかったため、何も変更されません)、および `name_taken` (統合によって作成される名前が既に存在します。何も上書きされません) です。

## 2a. フィーチャールートの通常のパス

これは、通常の機能ルート (1 つのボード項目を所有するセッション) が順番に実行されるすべてです。すべてのステップはコマンドです。コマンドは認証情報を運びますが、同じルートへの手書きの curlは拒否されます。よりレアなものは 1 `clawdline guide <part>` 離れたところにあります。ポインタは最後にあります。

**1.項目を読みます。** `clawdline item show <item id>` は、その種類、フェーズ、受け入れ基準、キャプチャされたゲート、受け入れバージョン (`acceptance vN`)、その人の独立したレビュー スイッチが必要な機能、その手順、および本体付きのすべてのドキュメントを出力します。それがあなたが作業する記録です。 `clawdline item show <item id> --doc <doc id>` は、1 つのドキュメントの本文だけを出力して、ファイルにパイプします。 `clawdline item steps <item id>` は本体を除いた同じレコードです。アイテムを書き込むたびに、`wrote …; item <id> is at version N`、短いアイテムの概要、および `item show` ヒントが出力されます。 `item show` の完全な同意書、手順、およびドキュメントをお読みください。 `--expected-version` を渡さない限り、書き込みは項目の現在のバージョンに基づいて行われます。

ASSIGNMENT.md に **HANDOFF** という見出しがある場合、別のセッションが途中で終了した項目を引き継いでいることになります (実装が開始された後、完了する前に再割り当てされます)。計画を立てる前に、まずパック名を読んでください。デーモンは、前の所有者に尋ねることなく、独自のレコードと git からそれを構築しました。アイテムにバインドされたタスクとその結果、まだランディングされていないコミット、sha256 を使用してパッチとして保存された各ワークツリーのコミットされていない変更と、それをベースに復元する `git apply` コマンド、前の所有者の最後のメッセージ (または読み取れなかった理由)、およびフェーズとオープンのステップ。パッチは ASSIGNMENT.md の隣に存在し、ワークツリーよりも長持ちします。そこから続けてください。最初からやり直さないでください。

**2.目的と範囲を読んだ後、セッションに名前を付けます** (この項目に対して開かれた場合は `clawdline item name <item id> "<task name>"`)。項目ではなくセッションの名前が変更されます。

**3.導入前**

- キャプチャされた計画と受け入れ基準なし: `clawdline item acceptance <item id> --body-file acceptance.md` で観察可能なものを書き込みます。
- 独立したレビューのチェックが必要、またはエピック: `clawdline guide epic` のレビュー済みプラン パスが最初に来ます。チェックなし: 計画なし、子レビューなし。
- ステップのない多段階作業: `clawdline item step-add <item id> "first" "second" …` (一度に 2 ～ 8 つのステップを 1 つずつ検証できます。1 つの変更には何もかかりません)。
- じゃあ`clawdline item phase <item id> implementing`。

**4.デフォルトでは、このセッションで作業します。** 機能を自分で調査、実装、検証し、実現します。純粋に独立した並行作業、異なるツールや権限、または独立したレビューが必要な場合など、具体的なニーズによって別のセッションが役立つ場合にのみディスパッチします。ディスパッチする前に理由を述べてください。日常的な調査や実施だけでは理由にはなりません。

ディスパッチが必要な場合は、ここで合成、統合、ランディングを続けてください。

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id` は子をアイテムにバインドするため、そのランディングはアイテムのランディングとしてカウントされます。 1 人の子エージェントが複数の項目を実行するときにこれを繰り返します。最初の行は子エージェントの行であり、ランディングはすべての項目でカウントされます。
- タイトルは、何が異なるかを示す最大 60 文字の 1 行です。コロン (`:` または `：`) は、観察と説明を結合するものであるため、拒否されます。件名としての「ユーザー」や、コード形式の識別子で始まるタイトルも同様です。それぞれが `bad_task` に `title: …` がどちらかを答えます。
- ブリーフは自立します。そこには、あなたがすでに確認した事実を、その`file:line`またはそれを示したコマンドとともに入れて、子エージェントがそれらを再発見しないようにします。
- 子エージェントの調査または探索の概要には、停止条件 (回答するとタスクが終了する質問) とターン制限も記載されています。
- 読み取り専用作品は`--claims ""`です。すべてのフラグとすべての拒否コードは `clawdline guide dispatch` にあります。

**5.子が終了**すると、`<clawdline-notice>` 行がコンポーザーに入力されます。 `clawdline task show <task id>` を実行し、配信を統合します。これを読むと通知が閉じられるため、個別の ACK はありません。 **ディスパッチ後、ターンを終了します**: 通知で目が覚め、ターンを開いたまま待つと、ポーリングのたびにコンテキスト全体が再読されます。他に何もすることがなく、ブロックする必要がある場合にのみ、`clawdline task wait <task id>…` (デフォルトは `--timeout 9m`、最初の場合は `--any`) を実行します。 **ブランチをターゲットにマージ**して、ワークツリーの子を統合します。 **マージは数分以内にランディングを自動的に記録します**。手動でランディングを投稿しないでください。 `clawdline landings` には、まだランディングが必要な作業が表示されます。 `--claims ""` でディスパッチされた何も書き込まない子は、ブローカーによって `nothing_to_land` と記録されます。それ以外は`clawdline task land <task id> <state>`（`clawdline guide landing`）です。

**6.完了レポート**。原因を特定するには十分な調査が必要でした (直接の観察された修正には必要ありません)。 `done` の前に追加します。項目が完了すると割り当てが解除され、レポートの応答は `409 not_item_owner` になります。

```sh
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

問題を報告した人に向けて、マークダウンで個人データなしで書きます。

**7．項目を完了します。** `clawdline item step-done <item id> <step id>` で確認されたら、各ステップを完了します。使い捨てのワークツリーから直接作業をコミットしてプッシュします。子のブランチが使用されている場合は、そのブランチをマージします。ランディングしたら、コマンド 1 つでアイテムを `done` に移動します。子エージェントの記録されたランディング供給品のコミット、ターゲット、およびリモート。直接的な作業では、次のように名前が付けられます。

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --commit <sha> --target main --remote origin \
  --deployment "what went live, where, which version"      # omit landing flags for a landed child
```

または、一度に 1 フェーズずつ進みます。

```sh
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> deploying --no-landing-reason "why there is no code"    # work with no code
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

`done` は、アイテムの展開ポリシーに従って `--deployment` または `--no-deployment-reason` を受け取ります。 `clawdline item steps <item id>` がゲート ラインを出力する場合、この項目はそのラインが指す長いパスを保持します。

**デプロイを待機しています。** デプロイの実行中または伝播中にターンを開いたままにしないでください。デプロイとそのチェックを 1 つのコールバックとして開始し、ターンを終了し、通知が到着したらアイテムを終了します。

```sh
clawdline callback --title "The hosted console serves <sha>" --work-id <item id> --timeout 20m -- \
  sh -c './deploy.sh <sha> && tools/wait-hosted-console.sh <sha>'
# … the notice: clawdline task show <callback id>, then
clawdline item finish <item id> … --deployment "what went live, where, which version (callback <callback id>)"
```

**8.ターンを報告します**: `clawdline session report --summary "…"` (§7)。

**9.所有するセッションを開いたままにしておきます。** ボード項目を完了またはキャンセルすると、その割り当てが解放されます。それを所有していたセッションは終了しません。 `session report` の後は、フォローアップ作業のためにセッションを残しておいてください。項目が `done` または `cancelled` に達したからといって、`clawdline session close` を実行しないでください。後でセッションを閉じるように明示的に要求する場合があります。ブローカーは、`clawdline guide child` の子タブ ルールに基づいて、子のタスクが終了した後、エージェントによってディスパッチされた子のタブを個別に閉じます。

**何かが拒否された場合。** `version_conflict`: 同じコマンドを再度実行します。バージョンを再読み込みします。 `steps_incomplete`: ステップはまだ開いています。その他のコード: §12、その後、それをカバーする部分。

**珍しい作業、それぞれ 1 つのパート:** `clawdline guide board` — 提案、決定、やるべきこと、完了した項目の再開、人待ち、ゲート、およびあらゆる段階の拒否。 `clawdline guide epic` — 計画、計画レビュー、Epic の子アイテム、ペルソナ。 `clawdline guide landing` — 手動でのランディング、ハンドオフ (長いルートのマイルストーンハンドオフも)、ルートの割り当て。 `clawdline guide running` — 失速した子エージェント、残り物、リスポーン。

## 3. ディスパッチする前に: 既存の内容を読んでください

別のセッションの作業がすでにあなたのジョブを実行している可能性がありますが、共有ツリーからは見えません。マージされていないブランチでの完了した配信は、`git status` には表示されません。まず読んでください。

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- `generation`、`task_root`、および 4 つのリスト: `live`、`unlanded`、`droppable`、`unreadable` を答えます。すべての行には、デーモンが受け入れる `do` が含まれています。 `claims` では、各ライブ行が `overlaps` を表します。
- **ディスパッチには`generation`が必要です** (§4)。これは、行のシールされたフィールド全体の 16 進数文字です。行が開始、終了、または書き込みを変更すると移動します。
- **`task_root` は、`task.json` が置かれる場所です。** これは、このデーモン独自のフィールドです。 Swift ブローカーには `/tmp/.clawdline` がハードコーディングされているため、何もありませんでした。
- `400 bad_request` (`project` が Git リポジトリ内の絶対パスではない場合)。

こちらも一読の価値があります:

- `GET /v1/orchestrator/inflight?project=…` — リポジトリ内のすべての未処理の作業ライン、誰がそれを所有し、何を要求したか。
- `clawdline assistants` — アシスタントごと: `availability` (`ok`、`low`、`exhausted`、`unknown`)、`windows`、`stale`、`resets_at`。読んだ後、誰にディスパッチするかを選択してください。ノルマのためのディスパッチを拒否するものは何もありません。

**そもそもディスパッチすべきでしょうか?** 独立した部分に分割した作業は、並行して実行すると高速になります。すべてのステップが最後のステップに依存するチェーンは、ハンドオフのたびに壊れてしまうため、さらに分割が悪くなります。診断、ブリーフィングよりも小規模な作業、および誰かが待機しているものはすべて、自分のセッションに残ります。マシンのハウス ルールは `<state dir>/dispatch-policy.md` (およびユーザーの `dispatch-policy.local.md`) です。すべての子エージェントたちは説明会でそれらを受け取ります。

## 4. 所有する子エージェントをディスパッチする

所有される子は、あなたの下で制限されたタスクです。 **合成、統合、ランディングを続けます。**

**1 つのコマンドで、以下の 4 つのステップすべてを実行します**。その概要は標準入力またはファイルにあります。

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--persona <id>] [--work-id uuid …] [--task-id uuid] [--label "…"] [--project-dir D] < brief.md     # or --instructions-file brief.md
```

ID とシークレットを作成し、`generation` と `task_root` のインベントリを読み取り、`task.json` を書き込み、タスクをポストし、一方の `stale_inventory` でインベントリを再度読み取り、1 回再送信します。 `dispatched <id> <state> [worktree <path>]` を出力し、その後、警告ごとに 1 行、つまりデーモンの書き込みと、書き込みが重複する各ライブ タスクを出力します。 `--json` は代わりにデーモンの応答を出力します。拒否は標準エラー出力で `refused, <status> <code>: <message>` であり、その後、追加と修復がそれぞれ 1 行ずつ表示され、出口 1 になります。この部分の最後にある表に、各コードの意味が記載されています。ルートはあなたの会話です。`CLAUDE_CODE_SESSION_ID` または `CODEX_THREAD_ID`、それ以外の場合は `--conversation` です。 `--assistant` が別段の指示をしない限り、子エージェントのアシスタントはあなたのものです。 `--project-dir` が別途指定しない限り、プロジェクトはこのディレクトリの git トップレベルです。 `--claims ""` は、何も書き込まない子を宣言します。デーモンはワークツリーと子のタブを開いている間、何も出力しません。これは子が存在するときに応答する 1 つのリクエストなので、一度待ちます (§2、「長いコマンドの待機」)。シークレットは argv、`task.json`、またはそれが出力するものには決して含まれず、トークンはすべての Thin コマンドが読み取るときに読み取られます。

再試行が必要な可能性があるレビューの場合は、最初の呼び出しの前に小文字の UUID を選択し、試行のたびにそれを `--task-id` として渡します。このコマンドは、元のディスパッチ インテントのプライベート コピーを `task.json` の横に保持するため、デーモンがブリーフを書き換えた後でも、同一の再試行を再送信できます。ブローカーは元のタスク ID を `(replayed)` で返します。変更された概要はローカルで拒否されます。コマンドがタイムアウトするか、その出力が失われた場合は、失敗する前に `GET /v1/orchestrator/tasks/<id>` を確認してください。不足しているタスクは、同じ ID と概要を使用して再試行できます。明示的な拒否ではタスクは作成されず、原因を修正した後に再試行することもできます。

`--persona <id>` は、子を組み込みペルソナ (`task.json` の `persona`) として起動します。このビルドに欠けている ID はローカルで拒否されます。デフォルトで取得できるものはありません。`plan_review` が含まれています。必要なときに自分で `code-reviewer` という名前を付けます。 `GET /v1/personas` は、このビルドが持つ ID をリストします。ペルソナが何であるかは、§10 (`clawdline guide epic`) のエピック部分にあります。

バイナリを持たない呼び出し側の場合に実行される手順は次のとおりです。

**1. ID とシークレットを選択します。**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 characters, lowercase
SECRET=$(openssl rand -hex 32)          # 64 lowercase hex
```

シークレットは、POST 本文でユーザーからデーモンに送信され、そこに入力される 1 行でデーモンから子に送信されます。これは `task.json` にもディスパッチの回答にも含まれていないため、再度必要ありません。 (リスポーンは、秘密を運ぶ 1 つの答えです。つまり、そのコピーの新しいものです。)

**2. `generation` および `task_root` のインベントリ** (§3) を参照してください。

**3. `<task_root>/<TASK_ID>/task.json` と書き込みます。** デーモンはリクエストからではなく、このファイルから概要を読み取ります。許可時にそれを検証し、許可した内容から `task.json` を書き換え、同じレコード (タイトル、指示、書き込み、成果物、種類、タイムアウトを含む) から子の `CHILD.md` を書き込むため、子が読み取るタスクは検証されたタスクであり、`task.json` は読み取られません。

| フィールド | ルール |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | 同じID |
| `assistant` | `claude` または `codex` |
| `project_dir` | 既存のディレクトリへの絶対パス |
| `title` | が画面に表示されます。何が異なるかを示す最大 60 文字の 1 行。コロン (`:` または `：`)、件名としての「ユーザー」、または開始コード形式の識別子は、`bad_task` (`title: …`) として拒否されます。 |
| `instructions` | が必要、最大 16 KiB。子エージェントは自分たちで自立しなければなりません。子エージェントは他に何も知りません。すでに検証した事実を `file:line` またはコマンドとともに伝えます。調査の子にも停止条件とターン制限が適用されます |
| `claims` | **必須**: 子が記述できる相対パスは最大 32 個です。 `[]` は何も書き込まず、警告されることを意味します (`claims_missing`) |
| `isolation` | `none` (デフォルト) または独自のブランチでのプライベート チェックアウトの場合は `worktree` |
| `permission_mode` | `ask`、`edits`、または `full` |
| `timeout_minutes` | 1–240、デフォルトは 30 |
| `kind`、`deliverables`、`model` | オプション。 `model` は `[a-z0-9._-]`、最大 64 文字 |
| `work_id` | これが提供するボードアイテムのオプションの UUID |
| `persona` | オプションの組み込みペルソナ ID (`GET /v1/personas`);デフォルトではなし |
| `auto_compact_window` | オプション、クロードのみ: 子が圧縮するトークン単位のコンテキスト サイズ (50000 ～ 1000000)、または圧縮しない場合は `null`。不在はマシンの `claude_auto_compact_window` に従い、人が設定しない限りオフになります。日常の概要ではなく、実行の比較の場合: 圧縮により詳細が失われる可能性があります |
| `root` | **必須**: `{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "project_dir": "<the same absolute repository path as project_dir>", "label": "…"}`。デーモンがプロジェクト スコープを確認するには、ロール スコープのルートに `root.project_dir` が必要です。 |

**ワーカー サーフェスと起動モードを、子がディスパッチする前に使用する必要があるすべてのツールに一致させます。** ブリーフは必要なツールの名前を示し、ルートは選択したサーフェスがそれらを提供することを証明します。 Codex CLI の子は、許可フラグだけから ChatGPT デスクトップ アプリの組み込み `@Browser` を取得するわけではありません。 UI、アクセシビリティ、またはレスポンシブ レイアウトのレビューの場合は、実際にブラウザ/コンピュータの使用が許可されているサーフェスに作業をルーティングするか、Playwright/Chrome CDP などの承認に相当するローカル ブラウザ ハーネスに名前を付けて、それがインストールされていることを証明します。そのサーフェスがアプリ、オリジン、または GUI アクセスをリクエストする可能性がある場合、`--permission-mode ask` でディスパッチします: Codex `full` は、すべてのツールではなく、非対話型シェル起動 (`--ask-for-approval never`) を意味し、自動レビューは作成されていないリクエストをレビューできません。タスクの開始時に、子エージェントはコマンド名を確認するだけでなく、必要な各ツールを実行します。いずれかが利用できない場合は、正確なギャップが直ちに報告され、ルートはアクセスを復元するか、再ディスパッチします。ルートが互換性のないワーカーを選択したため、ツール依存の受け入れチェックが未検証として終了しません。

**`root.session_id` は会話 ID であり、端末 ID ではありません。** Claude Code はそれを `CLAUDE_CODE_SESSION_ID` としてエクスポートします。 Codex を `CODEX_THREAD_ID` として。これは、デーモンが子を下位にグループ化し、それが終了したときに通知する方法です。確認するには、このタブに名前を付けます: `GET /v1/orchestrator/whoami?conversation_id=<id>` は `terminal_id` と答えます。

**4.オーケストレーター トークンを使用してディスパッチ**:

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

シークレットが argv に含まれないように、標準入力 (`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`) を介して本体を送信します。答えは`{ok, task, warnings?}`です。 `warnings` を読み取ります: `claims_overlap`、`claims_missing`、`claims_ignored_for_worktree`、`dirty_worktree_base`、および `work_not_placed` (名前付きアイテムはまだボード上に移動できませんでした。ボードのスイープは 1 ティック以内に移動します)。同じ ID を再度投稿すると、保存されたタスクに `replayed: true` で応答するため、再試行は安全です。

タブが開かない場合でも、`task.state: "spawn_failed"` で 200 が返されます。 `POST /v1/orchestrator/tasks/<id>/respawn` (オーケストレーター トークン) は、オリジナルごとに最大 2 回、新しいシークレットを使用してコピーを開きます。

間違った子をディスパッチしました — 間違った概要、間違った範囲、または同じ作業を 2 回繰り返しましたか?スロットとその書き込みを保持している間は、終了するまで待ったりタイムアウトしたりしないでください。`clawdline task cancel <id> --reason "…"` はすぐに停止します (§5)。

**対応する拒否** (チェックされた順):

| ステータス | コード | どうするか |
|---|---|---|
| 409 | `task_unreadable` | この ID のタスクは保存されていますが、読み取ることができません。同じIDで再送信しないでください |
| 422 | `bad_task` | メッセージはフィールドに名前を付けます。 「…の下に読み取り可能な task.json がありません」が含まれています — `task_root` を確認してください |
| 422 | `claims_required` | `claims`を追加 |
| 422 | `root_session_required`、`root_assistant_required` | `root.session_id` および `root.assistant` を追加 |
| 403 | `session_scope_mismatch` | `root.project_dir` が存在し、ルート セッションのプロジェクトとロールのスナップショットと一致することを確認してください。概要または CLI を修正します。担当者にプロジェクト設定の変更を依頼しないでください。 |
| 422 | `detached_route_required` | `root.poll_only` を送信しました。それは切り離された自動化です (§6) |
| **409** | **`stale_inventory`** | `generation` が見つからないか、古いです。現在のインベントリ全体がエラーの中にあります。読んで、もう一度決定し、`generation` で再送信してください。 |
| 422 | `work_not_found`、`work_other_project`、`work_closed` | あなたが指定した `work_id` は項目ではありません、別のプロジェクトのもの、または閉じられています |
| 422 | `also_work_not_found` | `also_work_ids` の ID はボード項目ではありません。その後は`work_id`のようにチェックされます |
| 503 | `store_unavailable` | 指定されたアイテムを確認するためにボードを読み取ることができませんでした。何も始まっていません。もう一度送信してください |
| 409 | `graph_*` | タスクグラフ許可ルール (`graph` フィールド) |
| 409 | `no_child_capability` | このプラットフォームは子を開くことができません。 `missing` は何を言っていますか |
| 429 | `squad_launch_capacity` | セッションを待っているペルソナ起動が多すぎます。後でもう一度試してください |
| 429 | `rate_limited` | 10分間でのディスパッチが多すぎます |
| 422 / 409 | `root_unresolved`、`conversation_ambiguous` | 会話 ID がライブ セッションに一致しないか、複数のライブ セッションに一致しません。それを修正してください。分離型に切り替えないでください |
| 403 | `session_actor_required` | ロールで開かれたルートは、そのセッションから独自のセッションの分隊機能を使用してディスパッチする必要があります |
| 403 | `session_scope_mismatch` | ここにもあります: ルート セッションのプロジェクトがその役割のスナップショットと一致しません |
| 503 | `squad_policy_unavailable` | 役割の割り当て設定を読み取れませんでした。何も始まっていなかった |
| 409 | `persona_disabled_for_auto_assignment` | そのペルソナはターゲット プロジェクトでの自動割り当てがオフになっています |
| 429 | `over_capacity` | 子スロット (デフォルト 5) またはマシンがいっぱいです。 `retry_after` |
| 409 | `workspace_busy` | 別のルートの書き込みは重複します。エラーはブロックしているタスクに名前を付けます |
| 409 | `worktree_unavailable` | プライベートチェックアウトを行うことができませんでした |
| 429 | `terminal_busy` | すべての端末書き込みレーンがビジー状態です。 `retry_after: 5` |

## 5. 実行中および終了時

子はブリーフィングに署名し (`clawdline task accept`、`/accepted` を投稿するか、`accepted.json` から離れる)、計画が変更されたときに 1 つの進捗メモを送信することができ (`/progress`)、最大 5 つの通知をプッシュすることができ (`/notify`)、`result.json` を書いて `clawdline task finish` を実行することで終了します。それらのルートを呼び出すことはありません。

- `clawdline task show <id>` — 1 つのタスクとその状態 (`GET /v1/orchestrator/tasks/<id>`)。 `GET /v1/orchestrator/tasks` にはそれらがリストされます (`?state=`、`?limit=` は最大 500)。
- **終了すると、デーモンはコンポーザーに `<clawdline-notice>` 行を入力します。** その `body` は 1 つの短い文です。タスク、終了方法、この配信のみの事実 (ストール、書き込みの解放、ブランチ、残りの数)、および実行する 1 つのコマンド `clawdline task show <id>` であり、タスクの出力後に通知を閉じます。その JSON (バージョン 3) には、何かを言うときのみ `task`、`state`、`notice_id`、および `outstanding`、`leftovers`、`claims_released` が含まれます。結果は `task show` が出力するものです。入力できなかった行は 5→300 秒のラダーで再試行されます。タスクが画面上に表示されても、タスクを読んでいない場合は、再度完全に入力することはありません。同じコマンドに名前を付ける短い `task_reminder` 行は、2→30 分のラダーで、合計 8 回入力すると、諦めます。メニューを表示している間は決して入力されません。メニューはこれら 8 つを使い切ることはありません。ラインは最大 12 時間待機し、メニューがなくなると入力されます。 `task show` が送信するルート:

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  `clawdline task show <id>` および `clawdline task wait <id>…` は、出力後に完了タスクとして送信します。 `clawdline task ack <id> <notice_id>` は手動で送信し、1 行を出力します。 2 番目の ACK は `changed: false` に応答します。未承認の通知は `GET /v1/orchestrator/completions` にリストされます。 `POST /v1/orchestrator/completions/reconcile` はそれらを再武装します。放棄した通知は、次回セッションがアイドル状態になったときにもう一度入力されます。
- **あなたはそのラインを決して見ることができないかもしれません、そしてあなたはまだそれを知っています。** ターンの境界ごとに読むあなた自身の`GET /v1/work/v2/agent/session-todos/<conversation id>`には、`unacknowledged_completions`がリストされています - 終了し、あなたが承認していないあなたの各子は、`task_id`、`title`、`state`、`kind`、 `result_path`、`notice_id`、および `ack_path`。その通知がまだ保留中であるか放棄されているかどうか。 `clawdline session report` は受信後にそれらを出力します。それぞれ: `clawdline task show <id>`、それを統合します。読み取りは ACK であり、両方のリストから削除されます。 `task show` は概要全体を出力し、省略されたものをカウントします。 `--json` は、シンボルとアーティファクトを含むデーモンの完全な答えです。それでも不十分な場合にのみ、`result.json` 自体を読んでください。
- **食べ残しに名前を付ける配達** — 子エージェントがしなかったと言っていること — それ自体は何も変わりません。 `task show` にはタイトルがリストされています。 `POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}` という人に 1 つを贈ります。彼らはトラック、後で（バックログ）、またはいいえと答えますが、彼らがそうするまでボードには何も届きません。
- **ブリーフィングの直後に停止した子は、ナッジされてから報告されます。** ブリーフィングを入力された子が署名しておらず、その画面が 5 分間アイドル状態 (プロンプトが表示され、コンポーザーが空で、メニューがなく、作業行がない) を表示した場合、デーモンは `CHILD.md` (決してシークレットではありません) という名前を 1 行入力します。まだ署名されておらずアイドル状態のまま 5 分後、タスクは停止したという判定で `spawn_failed` を終了し、`task_finished` ではなく `"kind": "task_stalled"` の通知を受け取ります。それをリスポーン (`POST /v1/orchestrator/tasks/<id>/respawn`) するか、再度ディスパッチしてから ACK します。作業している子、メニューを表示している子、またはサインをしている子には決して入力されません。
- **誤ってディスパッチした子をキャンセル** — 間違った概要、間違った範囲、重複: `clawdline task cancel <id> --reason "wrong brief"` (`POST /v1/orchestrator/tasks/<id>/cancel`、`{"reason":"…"}`)。理由は必須であり、最大 500 バイトです。タスクは理由を判定として `cancelled` 終了し、タブが閉じられ、書き込みと子スロットが解放され、キャンセルされたこととその理由を示す 1 つの通知が表示されます。 **コミットは破棄されません:** コミットした子はブランチとチェックアウトを保持し、そのランディングは保留中のままになり、その上にコミットがいくつあるかを示すメモが表示されます。 `task show` と `clawdline landings` がそれを示します。必要なものをマージするか、`clawdline task land <id> abandoned` で記録します。タスクをディスパッチしたルート セッション、またはコンソールからのユーザーのみがタスクをキャンセルできます。他のユーザーは拒否されます `403 not_task_root`。ロールを使用して開かれたセッションは、独自の機能を送信する必要があります (`session_actor_required`。コマンドがそれを行います)。すでに終了したタスクは、`409 task_already_terminal` を `state` で返します。同じキャンセルを再度実行すると、`replayed: true` で同じ成功が返されます。 `clawdline task wait` は、待機していたタスクがキャンセルされたときに 5 を終了します。それ以外の場合、タスクは終了、失敗、またはタイムアウトによって終了します。
- **完成した子は、ランディングされたコードではありません。** その作業は、統合されるまで、共有ツリーまたはそのブランチに置かれます。

## 5a. 長いコマンドを待機しています: コールバック

デプロイ、CI の実行、リリースの伝播、長いビルドなど、答えが「後で」になるものはすべてです。自分のターンでそれを待たないでください。また、後のターンからポーリングしないでください。それをデーモンに渡します。

```sh
clawdline callback --title "CI is green on <sha>" --timeout 45m -- gh run watch <run id> --exit-status
```

`callback <id> briefed`を出力してリターンします。 **ターンを終了します。** コマンドが終了すると、デーモンは `<clawdline-notice>` を入力し、その本体は `callback <first 8 of id> finished: success (exit 0 after 6m) — run clawdline task show <id>` となります。 `task show` は、子の場合とまったく同じように、終了状況 (終了ステータスと出力の最後の行) を出力し、通知を閉じます。

- これはタブのないあなたのタスクです。`clawdline task cancel <id> --reason "…"` はコマンドのプロセス グループ全体を停止します。 `--timeout` (1 分～4 時間、デフォルト 30 分) を過ぎると、`timeout` が停止して決済されます。実行中のコールバックは、実行中の子と同様に、セッションが閉じることを防ぎます。
- コマンドはシェルなしでその言葉どおりに実行されます。 1 つは `sh -c '…'` と書き込みます。このディレクトリ (別のディレクトリでは `--dir`) で、環境からの PATH、HOME、ロケール、USER、SHELL、TMPDIR、および TERM のみを使用して実行されます。認証情報は使用されません。必要なコマンドは、それ自体のファイルからそれを読み取ります。
- その出力はタスクのディレクトリ `output.log` にあり、終了後 7 日間保存されます。
- 1 回実行されます。その間にデーモンが再起動すると、コマンドが再び選択されます。デーモンが監視していない間に終了し、終了ステータスが残されなかったコマンドは、結果が不明で `failure` に解決され、**再実行されません**。安全であれば自分でやり直してください。
- 不確実な開始 (CLI がデーモンに到達できなかった) は、`--task-id <the id it printed>` で再試行されます。同じ ID が 2 回開始されることはありません。
- コールバックは子スロットを取りません。セッションごとに最大 8 回、マシンごとに 16 回の実行。もう 1 つは `429 callback_capacity` で `retry_after` で拒否されます。ディスパッチはそれ自体を `kind callback` (`bad_task`) と呼ぶことはできません。 Windows は `501 no_callback_capability` を拒否します。

## 6. ランディング、その他3種類の作業

**子のブランチをマージした後は、それ以上何もする必要はありません**: ブローカーは `landed` 自体を記録します (下記)。書き込みを行わないと宣言し (`--claims ""`)、自動的に終了し、ブランチやチェックアウトに何も残さない子は、通知が入力される前にブローカーによって `nothing_to_land` として記録されます。手動で記録されたランディングは、どちらもカバーされていないもの (チェリーピック、`incorporated` 配信、`nothing_to_land`、`abandoned`) を対象としており、オーケストレーター トークンとともに送信される 1 つのコマンドです。

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

スクリプトが `X-Clawdline-Orchestrator` ヘッダーで呼び出すことができるのはこのルートです。

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- これらのキーと `delivery` のみが受け入れられ、使用されません。他のキーは拒否されます。 `pending` および `abandoned` は、タスク シークレットまたはオーケストレーター トークンを受け入れます。 `landed`、`incorporated`、および `nothing_to_land` はオーケストレーター トークンのみを受け入れます。
- `landed` には `target` と `commit` が必要です。 `incorporated` には、`target`、`commit`、および `carrier_task` が必要です。これらのタスクは、検証済みのランディングによってこの配信が実行されました。デーモンは **Git** のいずれかをチェックします。それ以外の場合は、`409 unverified_landing` と次の `reason` のいずれか: `commit_unresolved`、`target_unresolved`、`not_on_target`、`base_unknown`、`predates_dispatch`、`delivery_unknown`、`nothing_delivered`、`not_the_delivery`、および `incorporated` の場合`carrier_required`、`carrier_is_delivery`、`carrier_unresolved`、`carrier_not_landed`、`carrier_repository_mismatch`、`carrier_target_mismatch`、`carrier_commit_mismatch`、`delivery_is_ancestor`。
- タスクが書き込みを行った場合、`nothing_to_land` は `409 wrote_to_repository` で拒否されます。
- 決済済みのランディングは、`409 invalid_transition` または `409 landing_conflict` を別の値に変更することはできません。
- **マージはそれ自体を記録します。** 完了したタスクのブランチがターゲットにマージされると、ブローカーは数分以内に同じ Git チェックを通じて、ターゲットのヘッドをコミットとして独自に `landed` を記録します。記録にターゲットがない場合、プライマリ チェックアウトのブランチが配達を保持している単一のブランチである場合にのみターゲットを指定します。チェリーピック、`incorporated` 配信、および `nothing_to_land` はまだ記録できます。
- **完了通知は、タスクが終了したときのブランチの状態を示します**。それぞれが 1 つのことをコマンドとして要求します。 *そのブランチでは何もコミットされません*: ランディングはそのブランチから証明されるため、現状では何もランディングとして記録されることはありません。チェックアウトがまだディスク上にある間に、そのブランチでチェックアウトでコミットするか、`clawdline task land <id> abandoned` します。 *ブランチにコミット済み*: そのブランチをターゲットにマージします。マージはランディングを記録します。 *読み取れませんでした*: ブランチを見て、記録してください。 *共有チェックアウトを書き込みました*: `clawdline task land <id> landed` とその作業をターゲットに運ぶコミット、または `abandoned`。 *何も書き込まれず、ブローカーは nothing_to_land を記録しました*。ACK だけが残ります。

`clawdline landings` (`GET /v1/orchestrator/landings`) はマシン上で保留中のすべてのランディングであり、それぞれに `ownership.status` が付いています。 `unknown` は「誰でもない」ではありません。証拠を読み取ることができなかったことを意味します。 `503 landings_incomplete` は、一部の行を読み取ることができず、代わりに短いリストが提供されないことを意味します。

`clawdline landings --work-id <item id>` (`GET /v1/orchestrator/landings?work_id=<item id>`) は、代わりに 1 つのボード アイテムのすべてのランディング **記録**です: `{"work_id", "landings": [...], "at"}`、各行の `id` および `source` — `task` (バインドされた子のランディングまたは組み込まれたレコード。ID はタスク ID)、`root` (レコード) `item phase deploying --commit` が書き込みました) または `phase_event` (アイテムの履歴に保存されている古いデーモンのコピー)。レコードを読み取ることができないバインドされたタスクは、`state: "unknown"` の行であり、取り残されることはありません。読み取られた項目には、`landings` と同じ行が含まれます。存在しない商品は`404 work_not_found`です。

**1 つのチェックアウトにランディングする 2 つのルート**は、最初にランディングリースを取得します (§11)。

他の3種類の作品はそれぞれルートが異なります。どちらが境界であり、詳細ではありません。

| 種類 | ルート | それは何ですか |
|---|---|---|
| **ハンドオフ** | `POST /v1/orchestrator/handoffs` | 既存の一連の作業を完全な状態で新しいセッションに与える |
| **ルートの割り当て** | `POST /v1/orchestrator/root-assignments` | 新機能のための、独立して所有される新しいルート |
| **分離されたオートメーション** | `POST /v1/orchestrator/detached-tasks` | 報告する人がいない無人作業 |

**ハンドオフ。** 最初に `<state dir>/handoffs/<handoff_id>/handoff.md` を書き込みます (リスト ルートは `package_root` に応答します)。これには 3 つの見出しが含まれている必要があります。**参考** (受信者が必ず読む必要があるすべて)、**検証** (続行する前にこれらの情報源からの質問に答える)、**オープン スレッド** (どこから入手するか) です。次に、閉じた本文で投稿します。

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

受信者は、ファイルを読み取り、その参照をたどり、検証の質問に答えて続行するように指示されます。開始時に、ハンドオフは、そのプロジェクト内の送信者のオープン ボード アイテムをキャプチャします。受信者が会話 ID を取得し、その最初の会話レコードが観察されると、それらのアイテムのアクティブな割り当てと所有者が 1 つのトランザクションで受信者に移動します。ハンドオフが失敗すると、所有権は送信者に残ります。すでに閉じられている項目、他の人に移動されている項目、または個別に割り当てられている項目はそのまま残されます。検証またはマージ中の検証ゲート項目は実装に戻るため、新しい所有者は再度検証する必要があります。ハンドオフラインが入力されると、`handoff_receipt` 通知が 1 つ表示されます。この通知だけではボードの異動が行われたことを証明するものではありません。拒否: `bad_task` (欠落または空の `handoff.md` が含まれる)、`sender_not_found`、`sender_ambiguous`、`rate_limited`、`terminal_busy`、および `succession_required` (マシン コーディネーターの役割を保持している場合) — このデーモン (`501`) では継承が利用できないため、セッションは実行できません手を離す。

**マイルストーンのハンドオフ。** マイルストーンに到達した長期実行ルートは `clawdline handoff --summary summary.md` でハンドオーバーするため、その後の作業は呼び出しごとに以前のすべてを再読み込みする必要はありません。概要には、目標、検証済みの決定、ブロッカー、証拠 (コンテンツではなく、開くパス、コミット、ID または `clawdline` コマンド)、次のステップという 5 つの `## ` 見出しがあり、最大 6 KiB、認証情報や会話テキストはありません。 `--check` は何も開かずにすべての問題をリストし、デーモンは `bad_milestone_summary` と同じ問題を拒否します。デーモンはその横に `obligations.md` を書き込みます: あなたのボード項目 (移動します。その人が答えていない決定も含まれます) とあなたの実行中の子、未承認の通知と未承認のランディング (それらはあなたのもののままです)。したがって、引き渡し後、それらを承認してランディングさせ続け、`safe` が読み取られたら `clawdline session close` とします。引き渡しはあなたの選択です: `clawdline usage --compare-handoff` は、このマシンでハンドオフによる効果があったかを示しますが、強制されることはありません。

**ルート割り当て。** `Idempotency-Key` ヘッダーは `request_id` と等しくなければなりません。

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

各割り当てフィールドは 1 ～ 8192 バイトで、合計 32 KiB です。デーモンはそれ自体の概要を作成し、セッションを開きます。 **親、シークレット、タイムアウト、結果、またはランディングはありません**。誰にも応答しないため、いつ終了するかは誰にも通知されません。拒否: `bad_root_assignment`、`idempotency_mismatch`、`request_conflict`、`rate_limited`。子エージェント、独立したタスク、引き継ぎなどで偽装をしないでください。

**分離されたオートメーション。** ディスパッチ (§4) と同様 — `task_root` の下の `task.json`、次に `{"task_id", "secret", "inventory_generation"}` — ただし、ブリーフのルートは `{"session_id": null, "poll_only": true}` である必要があり、そうでない場合は `detached_task_required` として拒否されます。誰にも通知されません。 `GET /v1/orchestrator/tasks/<id>` をポーリングし、`result.json` を読み取ります。ルートまたは機能所有者ではありません。

### 今後の作業のスケジュール設定

Clawdline Next はスケジュールされた作業自体を所有します。廃止されたアプリ `cron` や、切り離されたタスクのチェーンは使用しないでください。`GET /v1/orchestrator/schedules` をお読みください。`GET /v1/orchestrator/schedules/<id>` で完全な内容をお読みください。`GET /v1/places` は `place_id` に書き込み名を提供します。

1 回限りのスケジュール (`on`) は、オーケストレータートークンを使用して直接作成できます。繰り返しスケジュール (`days`) は常時実行される指示であり、このセッションからユーザーによる明示的な指示が必要です。

1. `GET /v1/orchestrator/sessions/<conversation>/run` をお読みください。これは、ユーザーが Clawdline を介してこのセッションにメッセージを送信した際に発行された最新の実行です。
2. `POST /v1/orchestrator/schedules` は、`Idempotency-Key` と通常のスケジュール本文、および当該会話と実行情報とともに送信されます。

```json
{"title":"Morning sweep","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"Inspect the overnight failures and report actionable findings.",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

同じ証明により、ユーザーが明示的に変更を要求した場合、`PATCH /v1/orchestrator/schedules/<id>` (スケジュール本文全体を送信) と `DELETE /v1/orchestrator/schedules/<id>` (2 つの証明フィールドを JSON 本文として送信) の両方が承認されます。`POST /v1/orchestrator/schedules/<id>/run` は、今すぐ 1 つを実行します。成功を報告する前に、作成または変更されたスケジュールを読み取ってください。

`via` がない場合、オーケストレーター トークンは、1 回限りの `on` スケジュールに制限されます。捏造された、期限切れの、または他のセッションの実行は、`run_unknown`、`run_expired`、または `run_other_session` として拒否されます。形式が正しくない証明は `invalid_user_authorization` として拒否されます。ユーザーが端末に直接入力した場合、実行は行われません。Clawdline を介して指示を送信するように依頼してください。実行を、ユーザーが要求していない作業に対する包括的な許可として再利用しないでください。この証明によりリレーの監査が可能になりますが、マシン全体のオーケストレータートークンがセッション固有の認証情報になるわけではありません。

スケジュールには時間を指定できない場合があります。`at`、`days`、`on` の代わりに `"trigger_only": true` を送信してください。クロックはスケジュールを実行せず、`…/run` または Webhook によってのみ、`enabled` の間だけ実行されます。これは繰り返し実行される指示と同様に、常時実行される指示であり、同じ証明が必要です。

**別のマシンでタスクを開始し、その終了方法を取得します。** マシン間チャネルはありません。対象マシン上でタスクをトリガー専用スケジュールに設定し、クラウドWebhookをバインドして、呼び出し元が読み取れる場所にURLを保存します（これは認証情報です。つまり、あなただけが読み取れるファイル、または`CLAWDLINE_WEBHOOK_URL`です。コマンドライン引数には決して使用しないでください）。次に、呼び出し元マシンで以下の処理を実行します。

```sh
clawdline webhook fire --url-file <path>
```

`{"deliver_within_seconds": 60}`（`--deliver-within`）を送信するため、対象マシンがオフになっている場合は後で実行されません。配信ステータスが完了するか、`--timeout`（60分）が経過するまで配信状況を監視し、変更内容を標準エラー出力に、最後の1行を標準出力に出力します。終了コード `0` は成功しました。・ `1` は失敗しました (失敗、タイムアウト、キャンセル、生成失敗)。・ `2` はマシンに到達しませんでした (期限切れ、キャンセル、到達不能)。・ `3` は拒否されました (ディスパッチ拒否とそのコード、URL が利用不可、レート制限)。・ `4` は待機を停止し、最後に確認された状態になりました。`--no-wait` は配信 ID を出力し、`202` の後に処理を終了します。終了コードと最終行はそのまま報告してください。`2` は何も実行されなかったことを意味し、失敗したことを意味するものではありません。

検証待ちのデータを読み取るスケジュール済みタスク（サイドバーの「検証」、docs/verifications.md参照）は、その読み取り結果を、タスク自身のシークレット（オーケストレータートークンではなく、タスクが保持すべきではないトークン）とともに、該当レコードにメモとして書き込みます。

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<readout>"}'
```

このタスクは、`schedule_id`がこのタスクを開始したスケジュール（それ以外の場合は`schedule_mismatch`）であるレコードにのみ書き込まれ、`task:<task id>`で署名され、スケジュールが開始されていないタスクの場合は`not_scheduled`で拒否されます。レコードIDは`clawdline verify list`で確認できます。

## 7. 自分のターン完了を報告する

自分のターンが完全に完了したとき（作業が完了し、検証され、該当する場合はコミットされたとき）、最終回答の前に以下の操作を行ってください。

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

セッションの行に「配信済み、承認待ち」というチェックマークが1つ表示されます。これはランディングよりも弱いステータスであり、レビューは求められません。デーモンがセッションをアイドル状態（作業中、待機中、または画面が判読不能な状態よりも優先されます）と認識している間、そしてその端末で同じ会話が行われている間のみ表示されます。

- **ターン完了時のみ**。作業途中、診断、障害、または相手への質問には使用しないでください。子エージェントはこれを送信しません（`409 child_session`）。
- このコマンドは、`CLAUDE_CODE_SESSION_ID` または `CODEX_THREAD_ID` (それ以外の場合は `--conversation`) から会話を検索し、`GET /v1/orchestrator/whoami` に端末を要求し、`{"summary"}` を `POST /v1/orchestrator/sessions/<terminal>/complete` に転送します。
- 要約は 1～500 文字です。呼び出しごとに新しい完了記録が作成され、最新の記録がカウントされます。
- 回答には `open_todos` も含まれています。送信または読み取られて完了していない、このセッションの直接の To-Do が最も古いものから順に、最大 20 個 (それ以上ある場合は `open_todos_truncated`) です。このコマンドは、受信後に標準エラー出力にそれらを 1 行に 1 つの ID とテキストで出力します。 `clawdline todo done <id>` で完了した各項目にチェックを入れます。レシートはどちらの方法でも記録され、終了ステータスは変わりません。 `open_todos_unknown: true` は、どれも開いていないということではなく、読み取れなかったことを意味します。
- 拒否: `conversation_id_malformed` (小文字の UUID ではない)、`conversation_not_found`、`conversation_ambiguous`、`registry_stale`、`session_not_found`、`session_unbound`、`child_session`。拒否を正直に報告してください。チャット内の文章は領収書ではありません。

**その人にステータス レポートを残します。** git プロジェクトでファイルを変更したときは、その人に 1 ページを手渡し、作業の状況を確認して、そのターンが追加または変更したすべてのファイルを読むことができます。これはローカル HTML ファイルです。何もアップロードされず、何も読み込まれません。

```sh
clawdline report --repo <project> --status status.md [--notes notes.txt] [--lang zh-TW] [--open] <commit>…
```

- 名前 **このターン自身のコミット、最も古いものから**。それぞれは独自に読み取られるため、それらの間の別のセッションのコミットは行われません。決して範囲を超えないでください。
- `status.md`: オプションの `# Title`、その下のオプションの行、カードごとに 1 つの `## ` 見出し — ✅、🟡 または ❌ で開きます — と短い Markdown 本文。
- `--notes`: 1 行に 1 つの `path: sentence`、そのファイルの上に表示されます。 `--pin` (反復可能) ファイルを最初に置きます。 `CLAUDE.md` と `AGENTS.md` は、ターンがタッチしたときに固定されます。 `--exclude` はパスを残してそう言います。 `--at` は、内容が表示されるリビジョンです (デフォルトは `HEAD`)。
- すべてのリポジトリの外側にある `<state dir>/reports/<date>-<id>/report.html` にレポートを保持し、2 つのアドレスを出力します。**最初は `file://` アドレス**。これは端末が開きます。**次に `http://127.0.0.1:<port>/reports/<id>`**。これはこのマシンのデーモンが応答します。最終的な答えには両方を含めてください。コンソールには、開けないテキストとして `file://` アドレスが表示され、`http://` アドレスがリンクになります。このアドレスは、コンソールにサインインしているこのマシン上のブラウザでのみ開きます。電話またはクラウド ビューアは拒否されます (`report_not_over_cloud`、`report_local_only`)。
- `--out` は、代わりに `file://` アドレスのみを使用して別のファイルまたはディレクトリを書き込みます。 stderr は、省略または短縮された内容を示します。 `--open` は、このマシンのブラウザでもファイルを開きます。

## 8. 別のセッションと話す

**見つけてください。** `GET /v1/orchestrator/sessions` はアドレス帳です。生きている子のすべてのセッションの `id` (端末 ID)、`label`、`assistant`、`cwd`、`state`、`work_state`、および `taskId`。

**送信**

```sh
clawdline send --to <terminal id> "text"        # or text on stdin
```

これは、`{from_session, to_session, text}` と `Idempotency-Key` を組み合わせた `POST /v1/orchestrator/messages` です。デーモンは、ソースとして名前を指定する `<clawdline-message>` エンベロープ内の受信者のコンポーザーにそれを入力します。

- `to_session` は **端末 ID** です。会話ではなく、メッセージが意図したタブに続きます。 `from_session` は端末または会話 ID です (コマンドによって入力されます)。
- テキストのみ、最大 100,000 文字。 `images` フィールドはありません。送信されたフィールドは通知なくドロップされます。
- `ok` は、バイトが作曲家に到達したことを意味し、誰かがそれを読んだということではありません。
- これには数十秒かかる場合があります。デーモンは、タイプする前にマシン上のすべてのセッションを読み取ります (1 台の Mac でのリレーに約 30 秒、2026 年 9 月 19 日に測定)。コマンドは最大 2 分間待機します。短くしないでください。リレーが途中で切れると、入力しても記録されず、同じキーが `409 request_in_progress` と応答する可能性があります。
- コマンドは最初に `Idempotency-Key` を出力します。呼び出しが途中で失敗した場合は、`--key <that key>` を使用して呼び出しを再実行します。同じキーと本文が 1 回入力されます。本体が異なる同じキーは `409 idempotency_key_reused` です。
- 拒否: `source_not_found`、`target_not_found`、`same_session`、`target_busy` (受信者にはメニューが表示され、何も入力されませんでした)、`terminal_busy`、`delivery_failed`。

**その人に写真を見せてください。** ローカル パスを貼り付けないでください。電話では何も開きません。

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- オーケストレーター トークンのみ。 1 ～ 6 個のローカル ファイル。それぞれは、最大 12 MiB、一辺 12,000 ピクセルのプレーン ファイルである必要があります。 PNG、JPEG、GIF は直接読み込まれます。他の形式は、macOS では `sips` を通過します。
- 回答には `artifacts` がリストされており、それぞれに `<clawdline-image id="…">` などの `marker` が付いています。 **返信にマーカーを入れてください**;コンソールにはマーカーがある場所の画像が表示されます。写真は24時間保存されます。

## 9. 本人に伝える

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

`POST /v1/orchestrator/notify`。その人が待っているものに対してのみ、プッシュの価値は、それがまれであることにあります。 `--session <terminal>` をタップすると、そのセッションが開きます。

- `409 agent_notify_disabled`: その人はエージェントの通知をオフにしました。あなたのせいではありません。再試行しないでください。
- `409 not_subscribed`: プッシュに登録されているデバイスはありません。
- `429 rate_limited`: マシン全体で 1 時間に 30 回、すべての子エージェントの通知と共有されます。
- `502 push_failed`: プッシュ サービスが拒否されました。 `sent` と `failed` がエラーになります。

## 9a. 人間の介入メモを残す

長期にわたるエージェントが読んだり、実行したり、決定したりするべき具体的な事柄が 1 つあり、通常のチャット メッセージがストリームに表示されなくなる可能性がある場合は、メモを使用します。メモはターゲット セッションの折りたたまれたアテンション エリアに残り、ユーザーがそれを処理対象に移動するまで赤い点でマークされます。その後、独立した仕事を続けることができます。人は自然な停止点で戻ることができます。メモは、進捗ログ、個人的なリマインダー、通知、またはボードの決定の承認ではありません。同じリクエストに対してメモが重複しないようにしてください。

**チャットで相手に選択を求める前に**、実際の質問、決定に必要なトレードオフ、および 2 ～ 4 つの完全な返信候補を含む `answer` メモを 1 つ作成します。ボタンをタップすると、その応答が会話メッセージとして送信されるため、各 `draft` はそれ自体で明確になります。作成後に簡単なチャット ポインタを入力するだけで十分です。メモの作成または処理済みとしてマークされたメモをその人の回答として扱わないでください。会話メッセージ (タップされた返信またはユーザーが入力したメッセージ) を待ってから、それに基づいて行動します。作成が失敗した場合は、その旨を伝えて直接質問してください。エージェントが行う日常的な選択ではなく、人間の判断が必要な決定についてメモを取っておきます。

JSONボディファイルを使用して作成します。 `--target` がない場合、CLI は `whoami` を通じてこのライブ ルート自体の端末 ID を解決します。別のセッションの場合は、アドレス帳 (`clawdline guide send`) のライブ **端末 ID** を `--target` として使用します。 `--from` のデフォルトは、環境からのこのライブ ルートの会話 ID です。 CLI は、コマンド ラインに入力せずにマシンの認証情報を読み取り、ソース ID とターゲット ID を挿入し、デーモンの永続的なメモ ID を出力します。結果が不確かな場合は、出力された `--key` を再利用します。

```json
{"kind":"answer","title":"Choose a date","summary":"One release date needs your choice.","action":"Choose a date when you have a moment.","reason":"Only you can choose it.","options":[{"label":"Tuesday","draft":"Tuesday works for me."},{"label":"Wednesday","draft":"Wednesday works for me."}]}
```

```sh
clawdline note create --body-file note.json
# For another Session: clawdline note create --target <terminal-id> --body-file note.json
```

`kind` は、`read`、`answer`、`action`、または `report` です。 `title`、`summary`、`action`、`reason`が必要です。 `answer` には 2 ～ 4 つの選択肢があります。各 `draft` は、そのボタンに表示される返信候補です。ユーザーがそれをタップすると、コンソールはそれを会話メッセージとしてメモのセッションにすぐに送信し、その後にメモ ID、タイトル、アクションを含むコンテキスト行が送信されるため、受信側のセッションはそのユーザーがどのリクエストに応答したかを知ることができます。コンテキストはボタンには表示されません。送信が成功した後にのみ、メモは最近処理されたものに移動します。送信が失敗した場合、メモは保留状態のままになり、アテンション コントロールは応答が送信されなかったことを示します。 `detail` は長いテキストを保持できます。 `document_url` は、実際の読み取り可能なクラウド ドキュメントにリンクできます。投稿する前にドキュメントのルートとファイルを確認してください。メモの状態ではなく、到着した会話メッセージに基づいて行動します。マークされた人が手書きで処理したメモには何も送信されません。実際に回答で作業がブロックされている場合は、ユーザーの待機状態を記録し、既存のアテンション通知を 1 回送信します。表示されたメモだけではプッシュは送信されず、エージェントはウェイクアップされません。

## 10. ボード

ボードには 3 つの構造 (ボード項目、バックログ、各セッション独自の ToDo リスト) があり、**その内容を決定するのは人です**。セッションは、Clawdline 経由で送信されたその人自身のメッセージが指示した場合にのみボード アイテムを作成します。それ以外の場合は提案します。自らカードを提出することは決してありません。 1 つの例外はエピックの所有者です。エピックの計画をレビューした後、エピックを機能項目と問題項目に分割し、それらをセッション (`clawdline guide epic`) に割り当てることができます。

**ボードアイテムと一緒に言うTODO/待辦/土度はそのアイテムのステップを意味します。** `--step`でアイテムに付けてください。 `clawdline todo add` とも書き込まないでください**。 `clawdline todo add` は、ボード項目のない、このセッション独自の To-Do として追跡するようその人が依頼したリストのみです。

**その人がボード アイテムを作成するように指示した場合。** そのメッセージ (Clawdline 経由で送信され、実行される) がボード アイテムを明示的に要求した場合にのみ、自分で作成します。

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin] [--assign-self]
```

動作例。その人は次のように書きます: *「リリース ノートをクリーンアップするためのボード アイテムを作成します。TODO: ドラフトを作成し、リンクを確認し、公開します。」* これは 1 つのコマンドであり、他には何もありません。

```
echo "Clean up the release notes before the next release." | \
  clawdline item add --project <place id> --kind feature --title "Clean up the release notes" \
  --step "Draft the notes" --step "Check the links" --step "Publish"
```

- `item add` は、`--run` が名前を指定しない限り、この会話の最新の実行 (`GET /v1/orchestrator/sessions/<conversation>/run`) を読み取り、要求する前にその冪等性キーを出力し (`--key` は同じ書き込みを再試行します)、作成された項目を各ステップの ID で出力します。 `{"session_id", "via": {"run"}, "project_id", "kind", "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`で`POST /v1/work/v2/agent/items`、`201`で`{"item", "assigned", "assignment_state"}`と答えました。
- フィーチャー、課題、またはエピックは、デフォルトでは**未割り当て**で到着します。そこでは、その種類のユーザーの未割り当てアイテムがボード上で待機します。そのステップは、順番に `--step` 行、または — 何も指定しない場合は — 説明の最上位マークダウン リストの 2 行以上です。その人はよくあなたに、後で書くために準備を書くように頼みます。アイテムを作成しても、それがあなたのものになるわけではありません。 `assignment_state`は`not_requested`です。
- `--assign-self` (`"assign": {"mode": "self"}`) を追加します。**その人のメッセージがこのセッションに今すぐ作業を行うように要求した場合のみ** (「このためのアイテムを作成してそれを実行する」)。その後、アイテムは同じ書き込みで **あなたに割り当てられた**、`assigned` で届きます。端末には何も入力されません。あなたがそれを求めたのです。手順を順番に実行し、検証されたら各手順を完了し (`clawdline item steps <item id>`、`clawdline item step-done <item id> <step id>`、`clawdline item step-add` は作業に必要と判明したものを 1 つ追加します)、割り当てられた項目 (下記) と同様に `clawdline item phase` でフェーズを進めます。この方法で取得されたエピックは、実装される前にエピックの手順 (下記) に従います。作成したアイテムを後で割り当て解除するように頼まれた場合は、`clawdline item claim` (下記) を使用してください。リファクタリングは、内部構造を変更するが、外部の動作は変更しない実行可能な作業です。リファクタリングは、割り当てられ、ステップを実行し、フィーチャーのフェーズ、ゲート、およびレビューのスイッチに従います。計画は、`--assign-self` の有無にかかわらず、Planning で割り当てられていない状態で作成され、手順は実行されません (`planning_has_no_steps`)。
- 登録された Clawdfather は、実行可能なプロジェクト作業の例外です。プロジェクト コードを所有したり編集したりすることはありません。ユーザーのメッセージが新しいアイテムを明示的に要求する場合、`clawdline item add --project <place id> --kind feature --title "…" --assign-new` (または `--assign-terminal <id>`) を使用して最初にアイテムを作成し、次にそれをプロジェクト セッションに委任することがあります。 `assignment_state` は、プロジェクト所有者が記録されたか (`assigned`)、新しいセッションで最初のダイアログに応答する必要があるか (`awaiting_user`)、割り当てが失敗したか (`failed` と `assignment_error`)、割り当てが要求されなかったのか (`not_requested`) を示します。後者の 2 つの場合、その人はボードから任命することができます。 `pending` を使用したリプレイでは、委任が中断された後に元のアイテムに名前が付けられます。別の割り当てを試みる前にボードを調べてください。明示的なメッセージがなければ、アイテムを提案し、承認されるまで待ちます。
- その人には、「HH:MM のあなたのメッセージからセッションによって作成されました」とマークされ、言葉が引用されたカードが表示されます。
- それぞれ何も書き込まない拒否: `run_unknown` (実行名なし、または何も発行されない)、`run_expired` (1 日より古い)、`run_other_session` (別のセッションへのメッセージ)、`session_not_found`、`child_session` (子は `result.json` 経由でレポートする)、`project_not_found`、 `project_mismatch` (通常の実行可能項目は、作業しているプロジェクト内に存在する必要があります)、`coordinator_required` (ライブ ロールのないマシン セッション)、`machine_delegation_required` (通常のセッションが別のセッションへのマシンのみの結合割り当てを要求しました)、`invalid_assignment` (`self` を要求する不正な形式の `assign`、または Clawdfather)、 `too_many_steps` (128 を超える)、`run_items_exhausted` (1 つのメッセージで最大 5 つの項目が返されます)。
- **実行なし** — 担当者が端末に直接入力したため、`item add` は `no_run` または `run_unknown` と応答します。提案 (下記) にフォールバックし、ボードのエージェント提案でそれを受け入れるようにその担当者に指示します。

決して自分の主導でボード項目を作成したり、投機的な作業を計画するために複数の項目を作成したりしないでください。

**その人があなたに指摘したボード項目を請求します。** Clawdline を介したその人のメッセージが、すでにボード上にある特定の項目を取るように指示した場合 — *「リリースノート項目を取る」*、*「<item id> を請求する」* — それを請求します。それは 1 つのコマンドです。

```
clawdline item claim <item id>
```

- `item claim` は、`--run` が名前を指定しない限り、この会話の最新の実行を読み取り、そのバージョンの項目を読み取り、要求する前に冪等性キーを出力し (`--key` は同じ書き込みを再試行します)、その後項目を出力します。これは `POST /v1/work/v2/agent/items/<id>/claim` と `{"expected_version", "session_id", "via": {"run"}}` であり、項目を **あなた、つまりメッセージが送信されたセッション** に割り当てます。その中には別のセッションや端末を指定するものはありません。
- アイテムは、その人がボードからあなたに割り当てたかのように正確に読み取られます。あなたがそれを所有し、`assigned` に移動し、そのステップが説明のリストにない場合はシードされます。端末には何も入力されません。割り当てられたアイテムとして作業します (下記)。
- その人には、「HH:MM のあなたのメッセージからセッションによって要求されました」とマークされ、その言葉が引用されたカードが表示されます。
- 拒否、それぞれ何も書かない: `run_unknown`、`run_expired`、`run_other_session`、`session_not_found`、`child_session` (`item add` と同様)。 `work_not_found`; `project_mismatch` (アイテムはあなたが作業していないプロジェクトにあります); `item_assigned` (すでにセッションがあるか、セッションが開かれています。セッション間でアイテムを移動できるのはその人だけです); `item_terminal` (完了またはキャンセル); `planning_not_assignable` (プランはプランニングのままです。エピックまたはリファクタリングを要求できます)。 `version_conflict` (変更されました。コマンドを再度実行します); `run_claims_exhausted` (1 つのメッセージで最大 5 つの使用が返されます)。
- **実行なし** は `no_run` または `run_unknown` と答えます: 項目はその人に割り当てておいてください。

**その人が要求した新しいセッションにボード項目を割り当てます。** Clawdline を介したその人のメッセージが、特定の未割り当ての機能または問題を新しいセッションに渡すように要求した場合 — *「<item id> のセキュリティ セッションを開く」* — それを割り当てます。それは 1 つのコマンドです。

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- Epic の子ではないアイテムでは、`item claim` のように `--run` が名前を指定しない限り、`item assign` はこの会話の最新の実行を読み取ります。 `{"expected_version", "session_id", "mode": "new_session", "assistant"?, "model"?, "persona"?, "via": {"run"}}` では `POST /v1/work/v2/agent/items/<id>/assign` となり、ユーザー自身の「新しいセッション」を選択すると新しいセッションが開きます。
- カードには「HH:MM のメッセージからセッションによって割り当てられました」と書かれており、引用された言葉と新しいセッションが実行されるペルソナが示されています。
- それぞれ何も書かない拒否: `item claim` (`run_unknown`、`run_expired`、`run_other_session`、`session_not_found`、`child_session`、`project_mismatch`、`item_assigned`、`item_terminal`、 `version_conflict`、および `run_claims_exhausted`: `item claim` と `item assign` は 1 つのメッセージの 5 つを共有します)。 `kind_person_assigns` (機能または問題のみ); `new_session_only` (自分で受け取るには、請求してください); `unknown_persona`; `persona_disabled_for_auto_assignment` (そのプロジェクトではロールは自動割り当てではオフになっています)。

決して自分の意思で項目を要求したり割り当てたりしないでください (その人のメッセージ名のみです)。また、セッション (`session_cannot_create_item`) を拒否するその人の `POST /v1/work/v2/items/<id>/assign` を決して使用しないでください。エピックの所有者は、エピック自身の子にも `clawdline item assign` (`clawdline guide epic`) を割り当てます。

**ボード アイテムに対して開いた新しいセッションに名前を付けます。** その目的と範囲を読んだ後、実際のタスクを説明する短い名前を選択し、`clawdline item name <item id> "<task name>"` を実行します。これにより、ボード アイテムのタイトルを変更したり、別のモデル ターンを開始したりすることなく、セッション名が 1 回変更されます。アクティブな新しいセッションの所有者だけがそれを行うことができます。同じ名前を再度送信しても安全です。別の名前は拒否されますが、手動でセッション タイトルを設定することはできます。既存のセッションにボード アイテムを指定すると、そのセッションの名前はそのまま残ります。

**ボードアイテムを提案します。** ボードの **エージェント提案** キューは 1 つのルートによって供給されます。

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id` は、`GET /v1/places` からの行の `id` です。
- ソースは 1 つ必要で、それは自分のものである必要があります: このセッションが所有するボード アイテム、またはこのセッション独自の To-Do の 1 つ (`proposal_source_required`、`proposal_source_invalid`)。その人が要求したものから来た提案は、その To Do の元になったものを引用します。つまり、パスは次のとおりです。人が尋ね、あなたはそれを `clawdline todo add` (下記)、そしてあなたはその To Do の ID から提案します。
- 人が直接理解できる平易な言葉で提案書を書きます。`title` は気づくことができる結果を示し、`description` は何が変化するかを示し、`reason` はなぜそれを今行う価値があるのか​​を示し、`suggested_acceptance` はそれが完了したときに観察できることを示します。 4 つすべてが必要です。説明のつかない頭字語、内部識別子、コード パス、実装専門用語を主な説明にしないでください。ボードは最初にタイトル、出典、理由を示します。 **説明/詳細說明** では、何が変更され、それが完了すると何が表示されるかを説明します。
- **ステップのある項目を提案するには**、リストを `description` の 2 つ以上のトップレベルのマークダウン リスト行として記述します。ユーザーが項目を受け入れて割り当てると、各行は `steps` の 1 つになります (下記を参照)。
- `201` は保留中の提案に回答します。その人は、ボードのエージェント提案キューでそれを承認、編集、または拒否します。そうするまでは何もボードアイテムにはなりません。拒否: `invalid_proposal`、`proposal_too_large`、`project_not_found`、`proposals_full`。

**古い提案ルート。** `POST /v1/orchestrator/proposals` (会話で質問した後は `…/<id>/asked`) が引き続き提供されます。これは、子がタスク シークレットと `task_id` を使用して残りをファイルする場所であり、ルートの作業ライン提案がその質問するか保留する `instructions` を取得する場所です。その行は古い「確認用」領域に表示され、v2 ボードのエージェント提案キューには**表示されません**。そのため、ボード上のユーザーの前に項目を置く方法ではありません。

**本人に決断を求めてください。**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "work_id": "<the Board item this is about>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

アイテムはこのセッションによってオープンされ、所有されている必要があります。そのカードは、ボードが質問を表示するコンテキストです (`decision_source_required`、`decision_source_not_found`、`decision_source_invalid`、`decision_source_closed`)。 2 ～ 4 つのオプション。 `default` はそれらの 1 つである必要があり、誰も応答しない場合に何が起こります (`due_in_minutes` が 60 ～ 10080 を示さない限り 7 日後)。 `blocking` の決定のみがプッシュされます。 `GET /v1/orchestrator/decisions/<id>` で答えを読んでください。

**その人は答える。セッションは彼らの発言を中継するだけです。** 提案、決定、およびボードの項目は、`/v1/work/…` に基づいて回答されます。そこに書き込むセッションは、その人の言葉を伝えるランに `"via": {"run": "<id>"}` という名前を付ける必要があり、それがないと拒否されます (`403 session_cannot_decide`)。 `GET /v1/orchestrator/sessions/<conversation>/run` で最新の実行を読んでください。発明されたもの、有効期限が切れたもの、他のセッションで実行されたもの、または事前質問の実行は名指しで拒否されます。端末に直接入力している人は実行されないため、Clawdline またはコンソールで応答するように依頼してください。

**あなたの子エージェントたちはまだ収集する必要があります。** `GET /v1/orchestrator/sessions/<conversation id>/todos` — 端末ではなく、会話 ID によって名前が付けられます (それ以外の場合は `409 session_id_is_terminal`)。ブローカーはタスク ファクトからこれらをオープンおよびクローズします。書くことは何もありません。

ターン境界ごとに、アイドル状態を宣言する前に、`GET /v1/work/v2/agent/session-todos/<conversation id>` も読んでください。その `assigned_items` はその人がこのセッションに与えたボード アイテム、その `recent_items` はこのセッションが最近完了したアイテム、その `direct_todos` はクイック リクエスト、その `unacknowledged_completions` はあなたの ACK なしで終了したあなたの子です (§5)。このプルは、作業中に作成された割り当てが、現在のターンを中断することなく待機する方法です。現在のターンを終了し、割り当てられたアイテムを次の所有作品として取得し、`clawdline item show <id>` を使用して、文書本体を含むその完全な記録を読み取ります。

**その人が送信した To-Do。** 最後の行が `(Clawdline to-do <id>. When it is done: clawdline todo done <id>)` であるメッセージは、その人が Clawdline から送信したこのセッションの `direct_todos` の 1 つです。その行の上の言葉がリクエストです。作業を実行し、完了が確認されたら、順番を報告する **前**にその ID で `clawdline todo done <id>` を実行します。そうしないと、作業は終了しても、その行はその人のリスト上で開いたままになります。完了していないものは開いたままになります。 `clawdline session report` は、このセッションに送信され、まだチェックされていないすべての ToDo を標準エラー出力にリストします (§7)。

**ユーザーが尋ねたときの、あなた自身の To-Do 。** ユーザーがこのセッションにその作業を Clawdline To-Do として記録するよう明示的に要求した場合、またはいくつかの項目のリストを渡してそこで追跡するように指示した場合にのみ、それらをこのセッション独自のリストに書き込みます。

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add` は、`{"todos": [{"text": "…"}, …]}` と最初に出力されるべき冪性キーを持つ `POST /v1/work/v2/agent/session-todos/<conversation id>` です (`--key` は同じ書き込みを再試行します)。 1 回の呼び出しで、96 KiB のリクエスト本文内にそれぞれ最大 8 KiB の 1 ～ 20 行が含まれ、それらをすべて追加するか、まったく追加しません。このセッションのオープン To-Do が 500 を超えるリストは全体として拒否されます (`direct_todos_full`)。指定された順序で行を `201` に返します。会話は、このデーモンが認識しているライブ セッション (`conversation_id_malformed`、`session_not_found`) である必要があります。 Clawdline の子は拒否され (`child_session`)、`result.json` を通じてレポートを続けます。

決して自分の意志でこれを行ったり、投機的な作業を計画したりしないでください。完了が確認された場合のみ、各行に `clawdline todo done <id>` を入力します。このユーザーには、セッションによって追加されたものとしてマークされたこれらの行が表示され、そのユーザーのみがこれらの行を送信または削除できます。これらはボード項目ではないため、ボードに表示されることはありません。 To-Do は、現在のセッション内の雑務のリストです。ボード アイテムは、その人がボード上で追跡したい作業です。これを要求する場合は、`clawdline item add` (上記) を使用します。そのリストは、ToDo としてではなく、アイテムの `--step` 行として入力されます。

完了したばかりの項目がまだ未完成であることが相手の意味で明確に示されている場合は、ボードを自分で修正してください。 「最近完了した項目」のままにしたり、代わりの項目を作成したり、再度開くように依頼したりしないでください。現在のバージョンのアイテムを再読み込みし、次を使用します。

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "The concrete behavior or acceptance claim that remains unfinished"}
```

これは、完成したばかりの項目への参照が明確な場合にのみ使用してください。ルートは、最終割り当てがこの同じセッションによって解放された `done` 作業のみを受け入れます。ユーザーのキャンセルを取り消したり、別のセッションの完了を取り消したりすることはできません。以前の証拠を保存し、`implementing` で新しいサイクルを開始し、このセッションを所有者として復元し、その理由を不変アイテム履歴に記録します。理由は最大 8 KiB です。曖昧なフォローアップにはボードの項目を変更する権限はありません。

所有エージェントが人に行動または選択を求めるとき、決定を求め、それを待ちます。まず、このアイテムに関する決定 (`POST /v1/orchestrator/decisions` とこのアイテムの `work_id`、2 ～ 4 つのオプション、`default` および期限 — アクションの場合は、`{"id": "done", "label": "I've done it"}` や `{"id": "cannot", "label": "I can't"}` などのオプション) を開き、次にマシン認証ルート上でそのアイテムをポイントします。

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

決定は存在し (`decision_not_found`)、このセッションのものであり (`decision_other_session`)、この項目に関するものであり (`decision_other_item`)、まだオープン中 (`decision_not_open`) である必要があります。他の条件の `decision_id` は `decision_requires_waiting_user` です。決定のない `waiting_user` は、`waiting_user_requires_decision` で拒否されます。その人はボードカードまたは「お待ちしています」で答えます。決定が回答されるか、そのデフォルトが期限に達すると、デーモンは同じ書き込みで項目の `waiting_user` をクリアし、項目に回答を記録し、アイドル状態のこのセッションに選択したオプションの ID とラベルを入力します。自分自身を待つのをやめるには、同じルート上で `condition` を空の文字列 (または別の条件) に設定します。その場合、決定は `withdrawn` となり、「待機中」のままになります。アイテムが解放、再割り当て、キャンセル、または完了したときにも同様です。撤回された決定に対する回答は、`decision_withdrawn` で拒否されます。

**計画ゲートと検証ゲートをキャプチャしました。** `planning_gate` はデフォルトでオン、`verify_gate` はオフです。 `clawdline setting get|set planning_gate|verify_gate` は `on/off` または `true/false` を受け入れます。実行サイクルで最初に割り当てが成功すると、両方の値が固定されます。再割り当てとその後のグローバル設定の変更によって、そのサイクルは変更されません。計画がキャプチャされたエピックまたはフィーチャーには、実装する前に受け入れ基準が必要です。エピックには計画と独立したレビューも必要です。機能は、ユーザーが [独立したレビューが必要] スイッチ (下記) をオンにした場合にのみそれらを必要とします。問題には計画ゲートがありません。計画ゲートがオフなら、エピックに対する計画の強制もありません。両方とも、計画を立ててから独立して検証することを意味します。計画では通常のマージ検証のみが保持されます。検証では計画がスキップされるだけで、正確な候補がチェックされます。どちらのオフも通常のライフサイクルを使用します。その人はボードに承認を記入する必要はありません。ゲート項目がそれなしで到着した場合は、割り当て後、ゲート遷移の前に、監視可能な基準を `clawdline item acceptance <item id> --body-file <file>` で書き込みます。所有するセッションは、空のコントラクトを 1 回だけ埋めることができます。ユーザーが Clawdline を通じてこの所有ルートにこの項目の承認を改訂するよう明示的に指示した場合、完全な置換マークダウンをファイルに書き込み、`clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>` を実行します。アイテムのバージョンは、`clawdline item show <id>` が出力する `item version N` 行です (`item steps` は受け入れバージョンのみを出力します)。 `GET /v1/orchestrator/sessions/<conversation>/run` から実行されたメッセージを読みます。保持されるメッセージの抜粋は、受け入れの変更を明示的に要求する必要があります。禁止、議論、または単純な質問は許可ではありません。このルートが開いているアイテムを 1 つだけ所有している場合、会話コンテキストによってアイテムを参照できます。それ以外の場合は、ID またはタイトルでアイテムを識別する必要があります。実行は現在の承認バージョンよりも新しい必要があります。拒否を入力すると、何も変更されないことを意味します。同じ `--key`、`--run`、`--expected-version`、およびファイル バイトを使用して、不確実な応答を再試行します。本人が直接編集することも可能です。マージ前にこれを変更すると、古い PASS が無効になり、上書きされます。マージが開始されるとロックされます。

キャプチャされた検証をオンにして、コミットされた候補でクリーンな登録ワークツリーから `clawdline item phase <id> verifying` を実行します。CLI は現在のブランチと完全な HEAD を送信し、デーモンはプロジェクト、サイクル ベース、ツリー、および受け入れダイジェストをチェックします。分離された読み取り専用 Codex チェッカーは、問題に `code-reviewer`、エピックに `reality-checker`、参照画像または設計ドキュメント (それ以外の場合は `reality-checker`) を含むフィーチャーに `evidence-collector` を使用します。型指定された判定は `PASS`、`FAIL`、または `NEEDS_WORK` です。未確認の声明ではその理由が述べられており、決してマージを許可しません。結果が欠落しているか不正な形式である場合は技術的な障害であり、1 回の制限付き再試行とその後のエスカレーションが発生します。 Epic の最後のエンドツーエンド ラウンドは、すべての子が終了し、影響を受けるコンポーネントが 1 つの実行可能な候補に統合されるまで待機します。集中的な子テストと統合スモーク チェックが最初に行われます。モック UI、切断されたブランチ、または不完全な API に対して、最終的なブラウザーまたはマルチアカウントのエンドツーエンド作業をディスパッチしないでください。ディスパッチする前に、選択したワーカーが承認されたブラウザまたは同等のローカル オートメーションを使用してターゲット URL を実際に開くことができ、必要なテスト アカウント、フィクスチャ、およびオリジンのアクセス許可を持っていることを証明します。概要でそのルートに名前を付けます。 `--permission-mode full` だけではブラウザ アクセスではありません。別のベリファイアを同じブロッカーに送信するのではなく、再試行する前に失敗したツール プリフライトを解決します。失敗したプリフライトはエンドツーエンドの試行ではありません。子またはリビジョンごとに 1 つではなく、エピックごとに 1 つの包括的なエンドツーエンド ラウンドを計画します。欠陥を修正した後は、影響を受けるシナリオのみを再実行します。受け入れ条件の範囲または統合境界が大幅に変更された場合にのみ包括的なラウンドを繰り返し、その理由を記録します。 `verifying → merging` には、正確な候補/基準、または明示的に理由付けされたオーバーライドに対するライブ PASS が必要です。検証文だけでは許可されません。 3 回連続で FAIL が発生した場合は、生きている親 Epic オーナーにエスカレーションされ、そのオーナーが不在の場合はその人にエスカレーションされます。技術的な障害は別途エスカレートします。指定された親所有者のみが `POST /v1/work/v2/agent/items/<id>/gate-decision` を使用します。ある人は`POST /v1/work/v2/items/<id>/gate-decision`を使用しています。個人ルートをエージェントとして使用しないでください。ボードは、AI、本人、および技術的な理由によるオーバーライドを別々に表示し、決してチェッカー PASS として命名することはありません。詳細記録の保存上限に達した場合は、ユーザーはまず `GET /v1/work/v2/items/<id>/gate-export` をダウンロードし、マニフェスト ダイジェストを検証し、次にそのダイジェストとアイテム バージョンで `POST /v1/work/v2/items/<id>/gate-purge` を確認します。パージでは、対象となる閉じられた詳細のみが削除されます。集計、最新の事実、監査が残ります。

**フェーズの進行** 所有するセッションは、そのアイテムを実行フェーズ自体を通じて移動します。他の誰もそうしませんし、ターンの受け取りやクリアされた条件もそうではありません。位相は `…/edit` (`phase_not_editable`) のフィールドではありません。指定された作業が実際に発生したときに、各トランジションを実行します。

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

コマンドは項目のバージョンを読み取り、冪等性キーを出力し (`--key` は同じ書き込みを再試行します)、項目を出力します。それは

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "no_landing_reason"?: "…", "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- 一度に 1 ステップずつ: `assigned → implementing → verifying → merging → deploying → done`。 `verifying` から `implementing` に戻ることができます。 `merging` から `implementing` または `verifying` に戻ります。キャプチャされた検証ゲートがオフになっているアイテムは、ランディング証拠 (下記) で `implementing` から `deploying` に直接移行することもありますが、`verification` はオプションです。ゲートされたアイテムは `verification_gate_on` でそのステップを拒否され、ライン全体を歩きます。他にフェーズをスキップするものはなく、`done` には `deploying` からのみ到達します。
- `merging` には `verification` が必要です。 `deploying` にはランディングが必要です。ランディングしたこのアイテムの子ブローカー、またはプロジェクトのローカル `target` ブランチと `refs/remotes/<remote>/<target>` の両方でデーモンが見つけたコミットに `landing` という名前を付けて、最初にプッシュします。作業が別のリポジトリ (変更がフロントエンド コミットであるバックエンド アイテム) に到達すると、`landing.project` (`--landing-project`) は `GET /v1/places` からそのプロジェクトの ID に名前を付け、代わりにコミットがそこで検索されます。ここでは、プロジェクト ディレクトリ (`cloud/`) 内のネストされたリポジトリが独自のプロジェクトです。証拠は、ブローカーの**ルート ランディング レコード**として一度書き込まれます。同じアイテム、リポジトリ、コミット、およびターゲットが再び記録されると、同じレコードになります。アイテムの履歴では `landing_id` という名前が付けられ、`clawdline landings --work-id <item id>` にリストされます。 `deploying` より前に、デーモンはバインドされた子のブランチがマージされているかどうかを git にすぐに尋ねるため、ブローカーの次の検索を待たずに、直前に行われたマージがカウントされます。コードなしで作業すると、ランディングの代わりに `no_landing_reason` (`--no-landing-reason`) がかかります。それは、`landing` (`invalid_landing_evidence`) の横で拒否されますが、バインドされた子はまだランディングする義務があります (`landing_owed`、タスクに名前を付けます)、およびランディングした子の横 (`landing_recorded`)。 `done` には `deployment` または `no_deployment_reason` が必要です。アイテムの `deployment_policy` がどれかを決定します (`required` は `deployment` のみ、`not_required` は `no_deployment_reason`、`agent_decides` のいずれかを受け取ります)。すべての手順を最初に完了する必要があります。
- `done` は割り当てを解放し、項目をセッションの最近完了した行に移動します。期限がある場合は、その前に完了報告書 (下記) を追加してください。
- 拒否: `invalid_transition` (次の段階ではない、またはその証拠が欠落している)、`steps_incomplete`、`not_item_owner`、`item_unassigned`、`item_terminal` (人が再度開く)、`evidence_unknown`、`direct_landing_not_applicable`、`invalid_landing_evidence`、 `landing_project_not_found`、`landing_commit_unresolved`、`landing_target_unresolved`、`landing_not_on_target`、`landing_remote_unresolved`、`landing_not_published`、`landing_owed`、`landing_recorded`、`landings_full` (アイテムは 64 個のルートランディングを保持します)、および `version_conflict`: 再読み込みして再送信します。ランディング希望の拒否は、ブローカーが今調べたときに見つけたもの (まだマージされていないブランチ、読み取れなかったリポジトリ) で終了します。

**1 つのコマンドで終了。** 作業が完了すると、`clawdline item finish <item id>` は 1 つのトランザクションで項目を `implementing`、`verifying`、`merging` または `deploying` から `done` まで移動します。

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?}, "no_landing_reason"?: "…",
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- 各ステップは、`item phase` が同じゲートを通過し、独自の `item.phase_changed` を書き込むステップです。どのステップでも拒否されると全体が拒否され、何も書き込まれません。
- ランディングは入力ではなく読み取られます。コミットはゲート項目に対するゲートの承認された候補であり、それ以外の場合はバインドされた子のランドコミットです。ターゲットはブランチのランディング名です。リモコンはトラックを分岐するものです。指定したフィールドがすべて勝ち、`item phase deploying` が証明しているように、結果は git に対して証明されます。キャプチャされた検証ゲートには依然として PASS が必要です。`implementing` からゲート項目は `verification_candidate_required` で拒否されるため、候補ワークツリーから `item phase` を使用して `verifying` を入力し、PASS を待ってから終了します。
- 直前にブランチがマージされたバインドされた子は、フィニッシュ自体によってランディングされたと記録されます。デーモンはランディングを読み取る前に git に問い合わせます。そのため、マージ直後の `landing_required` はブランチがターゲット上にないことを意味し、拒否はブローカーが見つけた内容を示します。
- コードなしで動作します: `clawdline item finish <item id> --no-landing-reason "…" --no-deployment-reason "…"`、`item phase` と同じルールに基づいて。
- すでに `done` の項目はそのまま応答され、何も書き込まれないため、同じランディングが 2 回見られても何も動きません。
- 拒否は、`verification_required`、`landing_required`、`deployment_required` (ステップが欠けていたメモ)、`landing_target_unknown`、`landing_remote_unknown`、`landing_remote_unreadable`、`landing_ambiguous` (フラグを付けて名前を付けます)、`landing_owed`、 `landing_recorded` および `finish_not_started` (まだ `implementing` より前)。

割り当てられた項目には `steps` が含まれる場合があります。割り当てが成功すると、説明内の 2 つ以上のトップレベルのマークダウン リスト行からそれらをシードすることができ、`clawdline item add` で作成した項目には `--step` 行が含まれます。各ステップは、その項目に関するチェックリストのエントリであり、ボードの別の項目ではありません。 `clawdline item step-done <item id> <step id>` で検証済みの手順を完了します。 `POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete` を `{"session_id"}` とともに送信します。エージェント ルートでは、`expected_version` はオプションです。省略すると、書き込みは現在のバージョンに基づいて行われます。 (`--expected-version`) という名前で比較すると、古いものは `version_conflict` と答えます。いずれかのステップが開いたままである間、`done` への移行は `steps_incomplete` で拒否されます。 Clawdline は、単に親フェーズが進んだという理由だけでチェックを行うことはありません。

**独自の項目をステップに分割する。** 所有する項目にステップがなく、作業が多段階である場合 (個別に検証されるいくつかの変更、またはシステムの複数の部分)、実装する前に、項目を順序付けられたステップに自分で分割します。2 ～ 8 つの具体的なステップで、各ステップは単独で検証できます。単一の簡単な変更には**何も** の手順は必要ありません。リストを追加するためにリストを埋めないでください。見た目よりも大きな作品になった場合は、ステップを追加してください。

```
clawdline item step-add <item id> "Wire the route" "Cover it with a test" "Say it in the guide"
```

タイトルは引数、または空ではない stdin 行ごとに 1 つです。このコマンドは、各タイトルの前に項目を再読み込みし、書き込み前に各冪等キーを出力し、`item show` ヒントを含む短いレシートを出力します。 1タイトルにつき1オーナー限定のリクエストとなりますが、

```
POST /v1/work/v2/agent/items/<id>/steps     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

では、ステップは位置によって順序付けされているため、最後の既存ステップの 1 つ先の `"position"` になります。検証されたら、それぞれを `clawdline item step-done` で完了します。これは、ボード項目や To-Do で禁止されている「自分自身の主導権」ではありません。その項目はすでにあなたのものであり、そのステップは、あなたが与えられた仕事の段階をその人に示す方法です。

問題やインシデントの解決に、根本原因を発見したり、実際の修正と妥当な代替案を区別したりするための大幅な調査が必要な場合は、項目を `done` に進める前に、ユーザーが判読できる完了レポートを追加してください。単純で直接観察された修正には必要ありません。何が起こったのか、根本原因、何が変わったのか、どのように検証されたのか、残っている境界をファイルに書き込み、次のようにします。

```
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

`POST /v1/work/v2/agent/items/<id>/documents` を送信します (§10 の Epic 部分、`clawdline guide epic` にフィールドがリストされています)。コマンドが読み取る認証情報なしでそのルートへの手書きの curlは、`401 unauthorized` と応答します。ボディは Markdown で、最大 64 KiB です。生のデバッグ ログとしてではなく、問題を報告した人に向けて書き込み、個人データが含まれないようにします。アクティブな所有者は、アイテムが終了する前にそれを追加する必要があります。バージョンの競合後に再読み込みします。完了レポートは説明的なものであり、検証、ランディング、展開の証拠に代わることはありません。存在すると、閉じたボード アイテムに残り、セッションの最近完了した行から直接開きます。

`/v1/board` は、Swift アプリの古いカードで、読み取り専用です。ランディングは重要な事実です。アイテムが手動でランディングしたとマークされることはありません (`422 landing_is_broker_fact`)。

### エピックとフィーチャー: 実装前にユーザーのレビュー スイッチに従ってください

サイクルが計画を捕捉したエピックには、計画と独立したレビューが必要です。機能には、その人の **独立したレビューが必要** スイッチが含まれます (項目には `review_required`、`clawdline item steps <id>` はそれを出力します)。ボード上でそれを設定するのはその人だけです。機能のリスクを自分で判断することはできませんし、判断することもできません。 `implementing` の入力を要求すると、デーモンはスイッチを読み取ります。

- **チェックなし** (デフォルト): 機能の短い受け入れ基準を作成し、焦点を絞ったテストを実装および実行します。レビュー用の計画を作成したり、`plan_review` の子をディスパッチしたり、リスク評価を記録したりしないでください。
- **チェック済み**、および計画中のエピックごとに: レビュー済み計画パスを使用します。

チェックされていない機能がレビューに値すると思う場合は、その人にそう言ってチェックしてもらいます。エージェント側でデーモンに要求する方法はありません。

1. 慎重に計画を立て、その計画をアイテムに書き込みます。
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. 読み取り専用の子をディスパッチします。その要旨は、その計画を批判的にレビューすることです。つまり、不足しているもの、間違っているもの、または危険なものは何か。
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. 子エージェントが終わるまで待ちます。 `--work-id` でディスパッチされた成功したレビューの子は、アイテムのレビュー受領書を `plan_review` ドキュメントとして単独で記録します。 `GET /v1/work/v2/items/<id>`の`.documents`をチェックしてください。それが存在しない場合 (たとえば、子エージェントが `--work-id` なしでディスパッチされた場合など) にのみ、手動で記録します。
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
   同じタスクに対してそのコマンドを再度実行しても無害です。すでに存在するドキュメントに応答します。これに応じて計画が変更された内容についての担当者向けの短い概要は、2 回目のレビューではなく、別の `other` 文書です。レビュー後に機能計画が変更された場合は、変更が以前のレビューのリスク境界内にとどまる場合にのみ、改訂された計画の後に JSON `{"new_risk_boundary":false,"reason":"..."}` を使用して `Review boundary assessment` というタイトルの `other` ドキュメントを作成します。新しい境界または不確実な境界については、焦点を絞った新たなレビューが行われます。既存の Epic 2 レビュー上限は引き続き適用されます。
4. `clawdline item step-add <item id> …` を使用して作業をステップに分割します。
5. その場合のみ、`clawdline item phase <item id> implementing`。

一連の検証を計画します。各実装の子は、焦点を絞ったテストで独自のコードをチェックします。 Epic 所有者は、影響を受けるコンポーネントを統合し、最小の有用なコンポーネント間のスモーク チェックを実行します。統合された候補が機能した後でのみ、所有者は実際のエンドツーエンド検証と該当する独立した UX/製品レビューを送信する必要があります。ディスパッチ前に、検証者のブラウザ ルート、ターゲット URL、テスト アカウント、フィクスチャ、権限を確認してください。見つからないブラウザを検出したり回避したりするために、読み取り専用ベリファイア タスクを繰り返し使用しないでください。最初にアクセスを修正するか、同等のローカル ブラウザ ハーネスを選択してください。失敗したプリフライトはエンドツーエンドの試行ではありません。子またはリビジョンごとに 1 つではなく、安定した候補のエピックごとに 1 つの包括的なエンドツーエンド ラウンドを計画します。集中的な修正の後、影響を受けるパスのみを再実行します。重要な受け入れまたは統合の変更が行われた後にのみラウンド全体を繰り返し、その理由を記録します。

`clawdline item doc` は、そのバージョンと最後のドキュメント位置のアイテムを読み取り、冪等性キーを出力し (`--key` は同じ書き込みを再試行します)、アイテムを出力します。本体は `--body-file` または標準入力です。 `{"expected_version", "session_id", "role", "title", "body", "reference", "position"}`とは`POST /v1/work/v2/agent/items/<id>/documents`です。役割は、`spec`、`design`、`test`、`deploy`、`completion_report`、`other`、`plan`、および `plan_review` です。

ドキュメントを改訂するには、同じ `--role` および `--title` を使用してドキュメントを再度書き込みます。デーモンはその本体、参照、および位置を置き換え、その ID を保持し、バージョンを 1 つ上げて、`document.revised` を記録します。 CLI では `added … at v1` または `revised … to vN` と表示され、`clawdline item show` は各ドキュメントの `vN` を出力します。同じテキストを再度送信しても何も変更されず、文書がそのまま返信されるため、再試行しても安全です。古いテキストは保存されません。両方を保持するには、別のタイトルを使用してください。

- `plan`、`plan_review` およびレビュー境界は、その場で改訂されることはありません。プランニング ゲートはそれらを順番に読み取り、レビューは読み取った計画に名前を付けるため、書き込みのたびに新しいドキュメントが追加されます。
- アイテムには最大 32 個のドキュメントが保持されますが、`completion_report` はその 1 つではありません。完全なアイテムであっても、常に適合します。アイテムには `completion_report` が 1 つ含まれます。別のタイトルを作成すると、それを改訂して新しいタイトルが付けられます。
- 33 番目の文書は `documents_full` で拒否され、何も書き込まれません。メッセージでは、代わりに既存のドキュメントを改訂する `clawdline item doc` コマンドが指定されています。

- `plan` および `plan_review` はエピックまたはフィーチャーに属します (他の種類の場合は `document_role_not_applicable`)。
- `plan_review` の `reference` は、計画をレビューした Clawdline 子のタスク ID です。デーモンは、そのタスクが存在し (`plan_review_task_unknown`)、項目を所有するセッションによってディスパッチされ (`plan_review_task_not_owned`)、名前が指定されている場合はこの項目の行にあり (`plan_review_task_other_item`)、種類が `plan_review` (`plan_review_task_wrong_kind`)、`success` (`plan_review_task_unfinished`) で終了し、最新プラン（`plan_review_task_stale`）。拒否される前に計画性のないレビュー `epic_plan_required`。レビューの子からの自動ドキュメントは同じチェックに合格し、同じタスクに対する繰り返しの書き込みは冪等です。
- 計画中の Epic の `clawdline item phase <item id> implementing` は、レビューされた計画 (`epic_plan_required` または `epic_plan_review_required`) なしで拒否されます。 「独立したレビューが必要」にチェックを入れた計画中の機能も、同様に拒否されます (`feature_plan_required` または `feature_plan_review_required`)。チェックされていないものには、その受け入れ基準のみが必要です。チェックされたフィーチャの修正された計画には、変更されていない境界の証拠または別のレビューも必要です。計画中の Epic が直接実装に入る可能性があります。
- ゲートは、レビュー タスクから最新の `plan_review` のレシートを読み取ります。各検出結果には、`severity` または `blocking` または `non_blocking` があります (古いテンプレートの `important` および `minor` は、非ブロックとしてカウントされます)。評決は所見なしの場合は `safe_to_land`、すべての所見が `non_blocking` の場合は `proceed_with_findings`、いずれかの所見が `blocking` の場合は `changes_required` となります。ブロッキング発見のある最新のレビューでは、`item phase implementing` および `--kind plan_review` (`epic_plan_review_blocking` または `feature_plan_review_blocking`) を除く、まだ割り当てられているアイテムに関する `--work-id` によるディスパッチは拒否されます。拒否には、障害となる所見と次のコマンドがリストされます。つまり、計画を修正し、新しいレビューを送信します。障害のない所見、または所見に重大度がない古いレシートのみがアイテムを続行させます。エピックは最大 2 回のレビューを受けます。2 回目のレビューでもまだブロックされている場合は、その結果に答えるために計画を修正し、修正された計画は 3 回目のレビューなしで実装されます。ディスパッチしないでください。

**エピックを子アイテムに分割し、配ります。** これは、「セッションは、その人のメッセージが指示した場合にのみボード アイテムを作成する」および「その人だけがアイテムを割り当てる」の 1 つの例外です。つまり、その人があなたにエピックを割り当て、それがエピックを分割する権限です。レビューされた計画によってエピックが `implementing` に取り込まれた後、その一部が他のセッションでより適切に実行された場合は、その下に機能項目または問題項目を作成して割り当てます。

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | description on stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- 端末 ID はセッション アドレス帳 `GET /v1/orchestrator/sessions` (`clawdline guide send`) にあります。セッションはエピックのプロジェクトで動作する必要があります。子を自分自身に割り当てることができ、`--assign-new` はエピックに名前を付けるルート割り当てで新しいセッションを開きます。 `--assign` フラグがないと、子はその人を割り当てられずに待機します。
- `item child` は、そのバージョンのエピックを読み取り、そのべき冪性キーを出力し (`--key` は同じ書き込みを再試行します)、子を出力します。 `{"expected_version", "session_id", "kind", "title", "description", "steps"?, "deployment_policy"?, "assign"?: {"mode": "existing_session", "terminal_id"} | {"mode": "new_session", "assistant"?, "model"?, "persona"?}}`で`POST /v1/work/v2/agent/items/<epic id>/children`、`{"item", "assigned", "assignment_error"?: {"code", "message"}}`で`201`と答えました。その子はエピックのプロジェクトに属しており、`parent_id` (エピック) を持ち、そのカードにはエピックの所有者であるセッションが作成したことが記載されています。そのステップは、`--step` 行、または割り当てられた後の説明のリスト (何も指定しない場合) です。
- 子が最初に作成され、次に割り当てられます。割り当てが失敗すると、子は **未割り当てのまま**、答えには `assignment_error` と割り当てのコード (`session_unavailable`、`project_mismatch`、`assignment_failed`、…) が含まれ、コマンドは 1 で終了します。`item assign` で再度割り当てるか、その人にそのまま残します。
- `item assign` は `POST /v1/work/v2/agent/items/<child id>/assign` と `{"expected_version", "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}` です。これは、エピックの開いている子を別のセッションに移動します。これは、ユーザーの選択によって行われるのと同じ割り当てです。
- 拒否、それぞれ何も書いていません: `not_epic_owner` (あなたはエピックの所有者ではありません)、`parent_not_epic` (親はエピックではありません)、`epic_not_planned` (エピックはまだ `implementing` より前です: 子はレビューされた計画から出てきます)、`item_terminal` (エピックは終了しました)、 `child_kind_not_allowed` (`feature` または `issue` のみ)、`epic_children_full` (エピックはオープンまたはクローズにかかわらず最大 32 個の子を保持します)、`not_epic_child` (エピックの子ではないアイテムの `item assign` — メッセージで要求されない限り、その人が割り当てます: `clawdline guide board`)、 `invalid_assignment`、`version_conflict`、`persona_not_applicable` (422: 既存のセッションを持つペルソナ)、および `unknown_persona` (400: カタログにない ID)。
- **ペルソナ** は、新しいセッションが開始されるロールです。システム プロンプトに追加されるテキストで、会話全体でそのロールが機能するように機能します。これは新しいセッション (`--assign-new`、`--new`、`dispatch`) のみに適用されます。既存のセッションには、開いたセッションが保持されます。デフォルトではなし。ペルソナは、`CLAUDE.md`/`AGENTS.md`、概要、`CHILD.md`、またはこのプロトコルをオーバーライドすることはありません。 `GET /v1/personas` にそれらがリストされています。 ID (各リストの `teams` は、ペルソナが所属するすべてのチームを示します。1 つは複数のチームに所属する場合があります):
  - `architect` — エピックを計画中。
  - `backend` — デーモン、API、またはストア機能。
  - `frontend` — コンソールまたは電話レイアウト機能。
  - `minimal-change` — 問題: 維持される最小の修正。
  - `code-reviewer` — レビューと `plan_review` の子。
  - `reality-checker` — 検証: 「機能する」前の証拠。
  - `security` — 権限、ペアリング、またはクラウドに触れる作業。
  - `technical-writer` — ドキュメントとガイド。
  - マーケティング、ブログ、サイト、またはドキュメント リポジトリの場合: `seo` (ページとメタデータ)、`content-writer` (ファイルで作成された記事)、`ai-search` (AI 回答エンジンが引用できるページ)、`social-media`、`instagram`、`email` (ニュースレター)、 `growth` (測定実験) および `pr` (発表)。
  - 製品、品質および操作: `product-manager`、`sprint-prioritizer`、`feedback-synthesizer`、`trend-researcher`、`ux-researcher`; `test-automation`、`accessibility`、`performance`、`api-tester`、`evidence-collector` (キャプチャされた証拠からの主張ごとに合格または不合格のルール)。 `sre`、`devops`、`incident-commander`、`finops`、`secrets`。
  - デザインとビジネス: `ui-designer` (プロジェクトのデザイン システムの画面)、`ux-architect` (フローとレイアウト構造)、`brand-guardian` (ブランドの一貫性)、`ui-finish-gate` (出荷前の視覚チェック)、`image-prompt` (画像生成プロンプト)、`pricing`、 `customer-success`、`support` (返信草案)、`analytics` (実際のデータからの回答)、`devrel` (実行されるサンプル)、および `privacy` (個人データのチェック。法的アドバイスではありません)。
  - `zero-review-lead` — 既存の機能またはプロセスをゼロから再検討するレビュー Epic を所有します。役割レンズを計画し、1 つの共有ファクト パックを持つ読み取り専用レビュー担当者の子としてディスパッチし、その証拠をターゲット設計に変換します。スキルは`zero-based-review`。
- **エピックが対面エクスペリエンスを変更する場合は、独立した UX/製品レビューを追加します。** 計画では、エピックが対面インターフェイス、ユーザー ジャーニー、または製品ポリシーを変更するかどうかを分類します。その場合、マージする前に、少なくとも 1 つの読み取り専用スペシャリストの子をディスパッチし、レイアウト、インタラクション、エンドツーエンドの製品フローにデフォルトで `ux-architect` を使用します。

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

  その概要では、統合候補の名前を示し、デスクトップとサポートされる最小のモバイルの証拠、キーボードとスクリーン リーダーの動作、行き止まり、製品の適合性、重大度、および具体的な推奨事項を求めています。証拠が入手できない場合は**未検証とマークし、その理由を述べる**必要があります。レイアウトではなくポリシーとスコープが主なリスクである場合は、代わりに `product-manager` を使用してください。別個のプリシップビジュアルパスがマテリアルである場合は、`ui-finish-gate` を追加します。すべての障害となる発見を解決し、タスク ID、評決、および処分を Epic の検証証拠または完了レポートに記録します。人体に直接的な影響がない場合は、その理由を計画に明記し、レビューセレモニーを追加しないでください。このレビューがカバーした範囲を記録します。通常、統合された Epic に対して関連する各スペシャリストを 1 回だけディスパッチします。 UX、ブランド、セキュリティ、その他の役割をチェックリストとして定期的に送信しないでください。小さなコピー、間隔、テスト、または範囲内の修正を、焦点を絞ったチェックで自分で閉じます。後の変更により、ユーザー ジャーニー、製品ポリシー、ブランドの方向性、セキュリティ境界、または記録された範囲外の別のリスクが大幅に変更される場合にのみ再ディスパッチします。変更された境界に名前を付けて、その関連する専門家のみに依頼してください。このレビューは、キャプチャされたプラン ゲートや検証ゲートの正確候補チェッカー PASS の代わりになることはありません。
- **各子がマージされた後も、あなたはエピックに対する責任を負い続けます。** その子のアイテムと `clawdline item steps <child id>` をすぐに再読してください。すべての手順が完了していることを確認してください。マージによって子は閉じられず、`merging` は休止状態ではありません。子の所有セッションは残りの手順を完了し、ローカル ターゲットと `origin/main` からすでに到達可能な正確なコミットのランディング受信を記録し、デプロイメントの証拠またはデプロイメント ポリシーと一致するデプロイメントなしの理由を使用して `deploying` → `done` に進む必要があります。あなたが子エージェントを所有している場合は、それらのアクションを自分で行ってください。別のセッションがそれを所有している場合は、その所有者に問い合わせるか、承認された子の再割り当てパスを使用します。所有者 (`not_item_owner`) になりすまさないでください。ブローカー完了通知が存在する場合は、その通知に ACK を送り、ワークツリーの残留物を分類し、ランディング済みの同一またはタスクの一時的なマテリアルのみを削除します。次の所有者のために、未ランディングバイト、混合バイト、または不明なバイトを保持します。すべての子が `done` または `cancelled` になるまで、親エピックの完了を宣言しないでください: `epic_children_open` は残りの数を示します。エピックの子以外のボード アイテムを作成しないでください。
- **完了した子は、独立したフィーチャー ルート セッションを閉じません。** エピックによって `--assign-new` で開かれたすべてのルートについて、`clawdline session close --dry-run --terminal <id>` (このエピックが開いたルートに対してのみ `closing as epic_owner` と応答します) を使用して、その `closeability` を読み取ります。ラベルや端子の位置から所有権を推測したり、`clawdline session report` をクロージャとして扱ったりしません。子が `done` に到達したら、ルートの所有者に、ルート自身のタスク、ランディング、通知、ToDo、およびワークツリーを監査してから、終了レポートを完了するように依頼します。現在のデーモンがサポートするルートを通じてのみ確認記録を取得します。廃止されたSwift閉鎖ルートはそのようなルートではありません。そのルートまたはガードされたクローズが利用できない場合は、製品ブロッカーと次の所有者を記録し、セッションを保持します。セッションの識別情報と作業が検証され、`closeability.state=safe` の場合にのみ、`clawdline session close --terminal <id>` でそのセッションを終了できます。最初にインベントリを再読み込みし、インベントリがなくなると 2 回目の実行で `session_not_found` が応答されます。 `clawdline close <terminal id>` でガードをバイパスしないでください。名前付きムーバーを使用して `blocked` をフォローアップします。 `unknown` (`terminal_unreadable` を含む) の場合、セッションを保存し、不足している証拠と次の所有者を記録します。強制的に閉じたり、アーカイブしたり、クリアされたと主張したりしないでください。エピック調整の完了を宣言する前に、各ルートのクローズ結果または名前付きブロッカーを列挙します。ボード `done` はこの在庫を置き換えません。

## 11. コーディネート

**マシン コーディネーター ("Clawdfather")。** プロジェクト外にあるデーモン所有のマシン ワークスペースで動作し、セッションの状態を報告し、サポート対象のマシン操作を管理します。Clawdline を含め、プロジェクトのソース コードは編集しません。開発作業を本人から明示的に依頼されたら、まず `clawdline item add --project … --assign-new` でプロジェクトのボード アイテムを作成し、プロジェクト セッションに委任します。依頼がない場合は、本人が採用できるアイテムを提案します。割り当てられたプロジェクト所有者が子のディスパッチ、検証、ランディングを担当します。このワークスペースは組織上の境界であり、ファイルシステムのサンドボックスではありません。新しいバインドはそのワークスペースから行います。既存のバインドは引き続き読み取れます。コンソールの専用 Clawdfather アクションからセッションを開き、その会話 ID を登録します。製品上の境界については `docs/clawdfather-role.md` を参照してください。新しいセッション内で `clawdline coordinator bind` を実行して登録するか、オフラインであることを確認した前任セッションからバインドを引き継ぎます。このコマンドは自身の会話 ID を読み取り、オンラインまたは読み取り不能な所有者の置き換えを拒否します。`GET /v1/orchestrator/coordinator` で役割を確認できます。`/coordinator/bearings` では、実行中のタスク、保留中のランディング、待機、配信失敗、保持中のリース、`unknown` の項目を一覧できます。`POST …/coordinator/register` に `{"session_id": "<conversation id>"}` を渡すと役割を引き受けます。バインド中のセッションがオフラインになった後は、`POST …/coordinator/rebind` で移します (`expected_coordinator_id`、`expected_generation`)。後継機能は `501 succession_unavailable` を返します。

**ファイルの待機。** 待機は「所有者がこれらのパスの作業を終えたら知らせてください」という依頼です。記録とメッセージであり、ロックやファイル監視ではありません。

- `POST /v1/orchestrator/waits` — `{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}` (セッション ID は会話 ID)。所有者には、その入力欄で一度通知されます。
- 所有者は `POST /v1/orchestrator/waits/<id>/release` に `{"owner_session_id", "commit"?, "note"?}` を渡して待機を終了します。各待機者に通知されます。タイマーによって待機が解除されることはありません。
- 待機者は `POST …/waits/<id>/cancel` に `{"waiter_session_id"}` を渡して待機を取り消します。
- `409 owner_busy` と `502 request_delivery_failed` は、**待機は記録済み**だが所有者への通知はまだ届いていないことを意味します。`502 release_incomplete` は通知が残る待機者を列挙します。解除要求を再送してください。

**リース。** 2 つのリソース: `heavy_compile` (マシンの 1 つのコンパイル スロット) と `landing` (チェックアウトごとに 1 つ)。

**ビルドやテスト スイートは `clawdline heavy -- <command>` 経由で実行します。** `heavy_compile` のキューに入り、マシンに十分なメモリができるまで待ちます。条件はメモリの 4 分の 1 (最大 1 GB) が空いており、メモリ ストールが 10% 以下であることです。コマンドは低い優先度で実行され、Linux ではメモリ不足時にカーネルが最初に終了させる対象にもなります。実行中はリースを更新し、終了後に解放します。コマンドの終了ステータスはそのまま保持します。デーモンが見つからない場合や未知の拒否が返った場合でも、ビルドは拒否せず、stderr に説明を出してコマンドを実行します。スロットとメモリの確保前に `--max-wait` (デフォルトは 30m) を過ぎると、キューから離れ、コマンドを実行せずに **75** で終了します。このコードはコマンド自体の失敗と区別できます。後で再実行してください。待機の開始時と終了時にそれぞれ 1 行だけ出力されます。途中は出力されないため、長い待機を 1 回行います (§2「長いコマンドの待機」)。`heavy` 内から呼ばれた `heavy` は直接実行されます。`--min-available 1500M` は必要メモリを増やし、`--no-slot` はメモリだけを確認します。リポジトリに `tools/heavy.sh <command>` があれば、それがバイナリを見つけます。

- `POST /v1/orchestrator/leases` — `{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`。 `granted`、または `queued` を `position` および `retry_after_seconds` で答えます。キューに入れられたリクエストは、同じ `request_id` で再度要求します。
- `POST …/leases/renew | release | cancel` と `{"request_id", "resource", "checkout"}`。ホルダーは `/renew` で更新します (`POST /v1/orchestrator/leases` を自身の `request_id` で再度要求することも更新します)。
- 60 秒以内に更新しないと、リースは終了したものとみなされます。 `409 lease_lost` は、そうであったことを意味します。 `429 queue_full`、ウェイター32名。

**グラフ** (`GET /v1/orchestrator/graphs`) は、ディスパッチされたタスクの `graph` フィールドから計算された読み取り専用ビューです。 **回収** (`/v1/orchestrator/reclaim`) 完了したチェックアウトをスイープします。本体に `{"dry_run": false}` と記載されていない限り、POST は予行演習です。

## 12. 何かを拒否された場合の対処方法

- `error.code` (フラット形状の場合は `error`) で分岐します。メッセージは人が読むためのものです。
- `retry_after` は、それが容量の応答であることを意味します。その時間待ってから、同じリクエストを送信します。
- `409 stale_write`、`503 orchestrator_store_busy`: ストアがビジーでした。同じリクエストを再度実行しても安全です。
- `unknown` がどこであっても (所有権、生存性、ソース) は、デーモンがそれを読み取れなかったことを意味します。それは「存在しない」わけではないので、何も削除したり、無効になったと宣言したりする必要はありません。
- 想定していたルートが `404 not_found` または `501` を返した場合、そのルートはこのデーモンにはありません。その事実を伝え、代わりに Swift アプリのルートやプロバイダー標準のサブエージェントへ切り替えないでください。
