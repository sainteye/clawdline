# Clawdline 가이드

이 가이드는 **Clawdline Next**가 실행되는 컴퓨터의 Claude Code 또는 Codex Assistant 세션을 위한 것입니다. 이 데몬이 현재 제공하는 기능만 설명합니다. 아래의 모든 경로는 가이드를 출력한 빌드에 등록되어 있고, 누락된 경로가 있으면 테스트가 실패합니다. 저장된 사본을 믿기보다 `clawdline guide`로 다시 출력하세요. `clawdline guide zh-Hant`는 대만 번체 중국어 가이드를 출력하며 `zh-TW`도 별칭으로 사용할 수 있습니다. `clawdline guide`는 핵심 부분과 나머지 부분의 이름을 출력합니다. 해당 작업에 이르면 `clawdline guide ko dispatch`처럼 필요한 부분을 출력하거나 `clawdline guide ko all`로 전체를 확인하세요. 핵심을 포함해 어느 부분이든 출력 첫 줄은 `guide-version: <sha256>`입니다. 같은 명령에 `--since <hash>`를 붙이고 내용이 바뀌지 않았다면 `unchanged <hash>` 한 줄만 출력합니다. `clawdline guide ko refused <code>`는 거부 코드를 설명하는 부분을 출력하며, 해당 부분이 없으면 표준 출력 없이 종료 코드 1로 끝납니다.

이 가이드에서 **단계(step)**는 항목의 체크리스트 항목 하나, 작업의 **쓰기 범위(writes)**는 변경 가능한 경로, **담당(assignment)**은 항목의 소유자, **부분(part)**은 이 가이드의 이름 있는 구성 부분을 뜻합니다.

## 0. Swift 앱에서 Clawdline을 배운 경우, 먼저 읽으십시오.

Swift 앱은 2026-09-19에 퇴역했습니다. 실행이 중지되었고 로그인 시 자동 시작되지 않으며, 포트 7717에서 응답하는 것도 없습니다. 해당 앱의 디렉터리 `~/.config/clawdline`은 디스크에 남아 있지만, 이 데몬이 보유하지 않았던 기록을 위해 읽기 전용으로만 사용됩니다. 이 데몬은 기존 앱의 복사본이 아닙니다. 특히 다음 다섯 가지 차이를 확인하세요.

1. **작업 디렉터리는 `/tmp/.clawdline`가 아니라 `<state dir>/tasks`입니다.** `/tmp/.clawdline`는 Swift 브로커가 사용하던 곳입니다. 두 브로커가 같은 작업 ID 디렉터리에 쓰면 눈에 띄지 않는 충돌이 생깁니다. 어느 경로도 하드코딩하지 마세요. 인벤토리(§3)의 `task_root`를 읽고 그 아래에 `task.json`을 작성하세요.
2. **호출할 워크플로 래퍼나 워크플로 경로가 없습니다.** 메시지에는 더 이상 보드 분류가 실리지 않으며, 세션이 스스로 보드 카드를 열지 않습니다. `POST /v1/orchestrator/sessions/<terminal>/workflow`는 오래된 도우미가 작업 도중 실패하지 않도록 남아 있을 뿐입니다. 이 경로는 `workflow_retired`를 반환하고 아무것도 기록하지 않으므로 새 코드에서는 호출하지 마세요.
3. **보드 참여는 제안과 결정으로 이루어집니다**(§10). 세션이 제안하고 사람이 답합니다. `begin`이나 `deliver`할 대상은 없습니다.
4. **접속 위치가 다릅니다.** 포트는 7727(또는 `CLAWDLINE_NEXT_PORT`), 상태는 `~/.config/clawdline-next`(또는 `CLAWDLINE_NEXT_DIR`)에 있습니다. `~/.config/clawdline`을 읽지 마세요. 그 토큰은 이 데몬의 것이 아니며 `401 unauthorized`로 거부됩니다.
5. **Swift 앱에는 있었지만 이 데몬에는 없는 기능:** 영구 보고서 승격(`501 durable_report_promotion_unsupported`), 조정자 승계(`501 succession_unavailable`), 그리고 브리프 필드 `serialize`와 `attach_session`(각각 `bad_task`로 거부됨)입니다. `reasoning_effort`는 `codex` 작업에서만 `high` 또는 `xhigh`로 사용할 수 있습니다.

## 1. Root와 child 구분

첫 메시지에 *“You are a Clawdline CHILD agent for task …”*라고 쓰여 있었다면 당신은 **child**입니다. 지정된 `CHILD.md`의 지침을 따르세요. 작업을 다시 배정하거나 턴 완료 영수증을 보내지 않습니다. `clawdline task accept`로 브리프 수령을 확인하고 `clawdline task finish`로 완료를 보고합니다. 여기서 읽기를 멈추세요.

그 외에는 사람과 대화하는 일반 세션인 **root**입니다. 아래 내용은 root를 위한 것입니다.

첫 메시지에 *“You are an independently owned Clawdline Feature Root …”*라고 쓰여 있거나 보드 항목이 배정되었다면 다음으로 `clawdline guide ko feature-root`를 출력하세요. 항목을 읽는 단계부터 `done`까지의 일반적인 전체 흐름과 드문 상황에 필요한 부분을 안내합니다.

## 2. 데몬에 연결하기

**사용 가능한 경우 직접 만든 curl 대신 명령을 사용하세요.** 명령은 자체 프로세스에서 인증 정보를 읽으므로 명령줄, `ps`, 출력, 대화 기록에 노출되지 않습니다. 인증 정보 없이 직접 호출한 curl은 `401 unauthorized`("No valid credential came with this request …")를 반환합니다. 이는 권한 문제가 아니라 인증 정보 누락이므로 해당 명령을 실행하세요.

| 명령 | 기능 |
|---|---|
| `clawdline guide [lang]` | 이 가이드를 출력합니다. 데몬은 필요하지 않습니다 |
| `clawdline session report --summary "…"` | 완료한 턴을 기록합니다(§7) |
| `clawdline session close [--dry-run] [--terminal id]` | 완료된 세션을 점검하고 닫습니다. 강제 종료는 하지 않습니다(§2a) |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | 자신이 맡은 하위 작업을 child에게 배정합니다(§4) |
| `clawdline item show\|steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | 자신이 담당하는 보드 항목을 읽고 진행합니다(`clawdline guide ko feature-root`, §10) |
| `clawdline todo add\|list\|done` | 사람이 요청했을 때만 이 세션의 할 일 목록을 관리합니다(§10) |
| `clawdline heavy -- <command…>` | 이 컴퓨터의 단일 컴파일 슬롯에서 빌드나 테스트를 실행합니다(§11) |
| `clawdline send --to <terminal> "…"` | 다른 세션에 메시지를 전달합니다(§8) |
| `clawdline notify --title "…" --body "…"` | 사람에게 푸시 알림을 보냅니다(§9) |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | 세션 상단에 실행 가능한 메모 하나를 남깁니다(§9a) |
| `clawdline assistants` | 각 Assistant 계정의 잔여 사용량을 보여줍니다 |
| `clawdline landings` | 이 컴퓨터에서 아직 반영해야 할 모든 작업을 보여줍니다. `--work-id <item id>`는 한 보드 항목에 기록된 모든 반영 내역을 보여줍니다 |
| `clawdline leases [--json]` | 컴파일 슬롯과 반영 임대의 보유자 및 대기자를 보여줍니다 |
| `clawdline sessions [--json]` | 메시지 전달, 대기 또는 인계의 대상이 될 세션과 각 상태 및 작업을 보여줍니다 |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | 세션, child 작업 또는 보드 항목의 범주별 사용량을 보여줍니다. 기본값은 현재 세션입니다 |
| `clawdline cloud pair [--offer <code>]` | Cloud 브라우저 하나를 이 컴퓨터와 페어링합니다 |
| `clawdline task show [--json] <task id>` | child 작업 하나의 상태, 판정, 요약, 남은 일의 제목, 검증, 반영 내역과 체크아웃을 간략히 보여줍니다(§5) |
| `clawdline task wait <task id>… [--timeout 9m] [--any]` | 모든 child 또는 `--any`로 지정한 하나가 끝날 때까지 기다리고, 각각을 `task show`처럼 표시한 뒤 알림을 닫습니다. 종료 코드는 전체 성공 0, 하나라도 실패 1, 실패 없이 취소 5, 시간 초과 3, 작업 읽기 실패 4이며 우선순위는 4, 3, 1, 5입니다(§5) |
| `clawdline callback --title "…" [--timeout 30m] [--work-id <item>] -- <command…>` | 배포·확인이나 CI 대기처럼 오래 걸리는 명령을 데몬에 맡기고 즉시 돌아옵니다. 턴을 마치면 종료 시 child 완료와 같은 `<clawdline-notice>`가 입력됩니다(§5a, `clawdline guide ko callback`) |
| `clawdline task cancel <task id> --reason "…"` | 잘못 배정한 child를 취소합니다. 탭을 닫고 쓰기 범위와 슬롯을 해제하되, 커밋이 있는 브랜치는 보존합니다(§5) |
| `clawdline task ack <task id> <notice id>` | 완료 알림을 수동으로 닫습니다. 보통은 `task show`나 `task wait`가 닫으므로 드물게 사용합니다(§5) |
| `clawdline task accept <task dir>` | child가 지시서 수령을 확인합니다. root는 실행하지 않습니다 |
| `clawdline task finish <task dir>` | child가 완료를 보고합니다. root는 실행하지 않습니다 |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | 어느 컴퓨터에서나 Cloud 웹훅으로 일정을 시작하고 결과를 기다립니다. 종료 코드가 결과를 나타냅니다(「향후 작업 예약」). 데몬은 필요하지 않습니다 |

가이드 태그를 명시하지 않으면 CLI 언어는 명령 앞의 `--lang <tag>`, `CLAWDLINE_LANG`, 저장된 `product_language`, 영어 순서로 결정됩니다. 명시적인 `clawdline guide <tag>`는 이 선택보다 우선하며, 지원하지 않는 태그는 영어를 출력합니다. `clawdline guide -list`는 제공되는 아홉 개 태그를 나열합니다. 이 설정은 사람이 읽는 CLI 문구만 바꾸며 프로토콜 필드나 Agent의 언어는 바꾸지 않습니다.

위 오케스트레이션 명령(`webhook fire` 제외)은 성공하면 데몬의 JSON을 출력합니다. 거부되면 `refused, <status> <code>: <message>`와 오류에 실린 각 스칼라 값을 `key: value` 형태로 한 줄씩 출력하고, 마지막에 해결 방법을 출력한 뒤 종료 코드 1로 끝납니다. Cloud 명령은 별도의 사람이 읽기 쉬운 성공·오류 출력을 사용합니다. `--port`로 포트를 덮어쓸 수 있습니다.

`clawdline usage`는 토큰 사용 내역입니다(저장소의 `docs/token-ledger.md`). 토큰이 사용된 범주는 `board`, `protocol`, `rules`, `impl`, `delegate`, `harness`, `talk`, `compaction`, `other`입니다. 옵션이 없으면 `CLAUDE_CODE_SESSION_ID` 또는 `CODEX_THREAD_ID`가 가리키는 현재 세션을 읽습니다. 호출 수·최대 컨텍스트·비용을 담은 첫 줄, 비용별로 정렬된 범주별 비율·토큰·비용, 그리고 모든 누락 구간을 출력합니다. `--json`은 데몬의 응답을 출력합니다. `rules` 값은 상한입니다. 한 셸 명령에서 규칙 검사와 다른 일을 함께 실행했다면 그 명령 전체를 규칙 사용량으로 계산하기 때문입니다. 페어링된 기기나 오케스트레이터 토큰으로 읽는 경로는 `GET /v1/usage/sessions/<conversation>`, `GET /v1/usage/tasks/<task id>`, `GET /v1/usage/items/<item id>`입니다. 내역을 아직 읽지 않았거나 더는 읽을 수 없는 세션은 빈 합계 대신 `not_yet_read`, `transcript_missing`, `transcript_unreadable` 중 하나로 응답합니다. 알 수 없는 ID는 404 `unknown_session`, `unknown_task`, `unknown_item`으로 응답합니다. 현재 읽는 중인지는 `/v1/diagnostics`의 `usage`에서 확인합니다.

**오래 걸리는 명령 대기.** `clawdline heavy`, `clawdline dispatch`, 긴 테스트는 기다리는 동안 출력이 없고 완료되면 자체적으로 끝납니다. 몇 초마다 확인하지 말고 **한 번 길게 기다리세요**. 확인할 때마다 전체 컨텍스트를 다시 읽는 턴이 생깁니다. 토큰 검토에서 열 개 항목에 걸쳐 이런 턴 520회(7,220만 토큰)를 확인했으며, 대부분은 대기 중인 `heavy` 실행 때문이었습니다.

- **Claude Code:** 긴 `timeout`(최대 `600000` ms)을 지정한 Bash 호출 한 번을 사용하거나 `run_in_background`로 실행한 뒤 완료 알림을 기다립니다. `sleep`과 `tail`을 반복하지 않습니다.
- **Codex(codex-cli 0.157.1, 코드 모드):** `functions.exec` 셀 첫 줄에 `// @exec: {"yield_time_ms": 600000}`을 넣습니다. `exec_command`가 세션 ID를 반환하면 빈 `chars`와 `yield_time_ms: 300000`으로 `write_stdin`을 기다리고, 계속 실행 중이라면 같은 셀 안에서 반복합니다. 측정 결과 바깥 셀은 `600000` 설정에서 330초 동안 열려 있었고, 빈 `write_stdin`은 최대 300초 기다렸습니다. 바깥 셀이 양보하면 긴 `yield_time_ms`로 `wait`를 호출해 결과를 수집합니다.

child 대기나 빌드에는 셀 하나를 사용합니다. 명령만 교체하고 세션 폴링은 셀 안에 둡니다. 그래야 `exec_command`가 통상적인 30초 후 반환해도 Agent가 깨어나 같은 대기를 다시 요청하지 않습니다.

  ```js
  // @exec: {"yield_time_ms": 600000}
  let r = await tools.exec_command({cmd: "clawdline task wait --timeout 9m TASK_ID", yield_time_ms: 30000});
  while (r.session_id) {
    r = await tools.write_stdin({session_id: r.session_id, chars: "", yield_time_ms: 300000});
  }
  text(r.output);
  text(`exit ${r.exit_code}`);
  ```

빌드라면 명령을 `tools/heavy.sh …`로 교체하고 종료 코드를 확인합니다. 75는 컴파일 슬롯 또는 메모리 대기 시간이 만료되어 빌드를 실행하지 않았다는 뜻입니다.

`clawdline heavy`는 `--max-wait` 시간(기본 30분)까지만 기다리고, 그 안에 시작하지 못하면 명령을 실행하지 않은 채 75로 끝납니다. 도구가 허용하는 시간보다 더 오래 기다려야 한다면 백그라운드 실행을 사용합니다.

**오케스트레이터 경로에 curl 사용.** `<state dir>/orchestrator-token`을 읽어 `X-Clawdline-Orchestrator` 헤더로 보냅니다. 토큰은 명령 인수에 넣지 마세요. `DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"`를 지정하고 `-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")`를 사용합니다. `curl --fail-with-body`를 사용하세요. JSON 본문이 있는 POST에는 `-H 'Content-Type: application/json'`도 필요합니다. 없으면 `415 unsupported_media_type`으로 거부됩니다.

### Cloud 브라우저 페어링

페어링하면 이 컴퓨터를 읽을 수 있는 대상이 바뀝니다. 페어링된 브라우저는 즉시 읽을 수 있고, Cloud의 `commands` 설정이 켜져 있으면 이 컴퓨터를 조작할 수도 있습니다. 사람이 해당 브라우저의 페어링을 명시적으로 요청하거나 정확한 페어링 명령 또는 제안을 건넸을 때만 페어링 명령을 실행하세요. 페어링 자체는 `commands`를 켜지 않습니다. 그 설정은 별개입니다.

지원하는 방식은 두 가지입니다.

1. **브라우저가 제안을 표시합니다.** 브라우저가 제시한 줄을 이 컴퓨터에서 그대로 실행합니다.

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

   작은따옴표를 유지하세요. 제안은 내용을 알 수 없는 짧은 수명의 일회용 비밀입니다. 해독·수정·저장하거나 최종 답변에서 다시 적지 마세요. 만료되었거나 이미 사용되었다면 재시도하거나 바꾸지 말고 브라우저에서 새 제안을 받으세요.
2. **컴퓨터가 초대합니다.** `clawdline cloud pair`를 실행합니다. 일회용 `https://app.clawdline.com/#pair=…` 링크를 출력하고 기다립니다. 사람은 페어링할 브라우저에서 같은 Clawdline Cloud 계정에 로그인한 상태로 전체 링크를 엽니다. 링크도 제안처럼 취급하여 공개하거나 보관하지 마세요.

성공하면 세 줄을 출력합니다. `paired`는 브라우저 기기 ID, `browser`는 브라우저 지문, `machine`은 컴퓨터 지문입니다. 브라우저 지문은 브라우저 화면의 값과, 컴퓨터 지문은 이 컴퓨터에 표시된 값과 대조하세요. 하나라도 다르면 성공이 아닙니다. `paired`의 ID로 즉시 `clawdline cloud revoke <device-id>`를 실행하고 불일치를 보고하세요. `clawdline cloud devices`는 현재 브라우저 목록과 로컬 신뢰 상태를 보여주며, 페어링 후 읽기 전용 확인에도 사용합니다.

이 명령들은 실행 중인 로컬 데몬을 거칩니다. 실패하면 정확한 stderr를 보고하세요. 사람이 별도로 요청하지 않았다면 Cloud를 켜거나, 로그인하거나, `commands`를 활성화하거나, 키를 교체하거나, 받은 제안을 다른 것으로 바꾸지 마세요.

**위치와 주소.**

- 포트: `CLAWDLINE_NEXT_PORT`, 없으면 **7727**. 루프백 전용 주소는 `http://127.0.0.1:<port>`입니다.
- 상태 디렉터리: `CLAWDLINE_NEXT_DIR`, 없으면 `$XDG_CONFIG_HOME/clawdline-next`, 없으면 `~/.config/clawdline-next`입니다(Windows에서는 `%APPDATA%\clawdline-next`).
- `GET /v1/health`는 인증 정보 없이 `served_by: "clawdline-go"`로 응답합니다. 이를 통해 데몬이 실행되지 않는 경우와 요청이 거부된 경우를 구분하세요.

**인증 정보.** 세 종류가 있으며 세션은 첫 번째 것을 사용합니다.

| 인증 정보 | 위치 | 전송 방식 | 접근 범위 |
|---|---|---|---|
| 오케스트레이터 토큰 | `<state dir>/orchestrator-token` | `X-Clawdline-Orchestrator` 헤더 | `/v1/orchestrator/`, `/v1/work/`, `/v1/board` 아래의 모든 경로와 `GET /v1/places`, `POST /v1/artifacts/images` |
| 작업 비밀 | 배정할 때 root가 선택 | `X-Clawdline-Task-Secret` 헤더 | `/v1/orchestrator/tasks/<id>/` 아래 child 자신의 경로와 `POST /v1/orchestrator/proposals` |
| 기기 토큰 | `<state dir>/local-token` 또는 페어링된 기기의 토큰 | `Authorization: Bearer` | 콘솔 경로(`/v1/sessions/…`). 세션에는 필요하지 않습니다 |

오케스트레이터 토큰을 `Bearer`로 보내면 기기 토큰과 비교되므로 거부됩니다. 토큰이 틀렸거나 없으면 `401 unauthorized`가 나옵니다. 토큰이 없거나 이 데몬의 것이 아니거나 기기가 페어링되지 않았다는 뜻입니다. `clawdline doctor`는 CLI가 읽는 디렉터리와 포트를 출력합니다.

**curl을 꼭 써야 할 때** 토큰을 명령줄에 노출하지 마세요.

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

- `--fail-with-body`가 없으면 거부 응답도 종료 코드 0으로 끝나 성공처럼 보입니다.
- **본문이 있는 모든 POST에는 `-H 'Content-Type: application/json'`이 필요합니다.** 없으면 `415 unsupported_media_type`으로 거부됩니다. `curl -d`만 쓰면 form 타입을 보냅니다.
- 경로에 더 작은 한도가 적혀 있지 않으면 본문 한도는 2 MiB입니다.
- `%47` 같은 tmux 터미널 ID는 경로에서 단일 세그먼트로 이스케이프하여 `%2547`로 보냅니다.

**거부 응답에는 두 형태가 있습니다.** 문장이 아닌 코드로 분기하세요.

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}` — 게이트와 브로커의 응답입니다. `retry_after` 같은 추가 필드는 `error` 안에 있습니다.
- `{"error":"<code>","detail":"…"}` — 없는 경로, 잘못된 메서드, 일부 읽기의 응답입니다.

이 데몬이 소유하지 않는 경로는 경로 이름을 포함한 `501 not_implemented`로 거부됩니다. 일반적인 컴퓨터에서는 이것이 최종 응답입니다. 누군가 `CLAWDLINE_NEXT_UPSTREAM_PORT`를 설정해 다른 데몬을 뒤에 둔 경우에만 전달됩니다. 그 데몬이 응답하지 않으면 응답하지 않은 주소와 함께 `502 upstream_unreachable`가 나옵니다. 두 응답 모두 이 데몬이 해당 기능을 제공한다는 뜻은 아닙니다. 2026-09-19 이전에는 기본 전달 대상이 포트 7717의 Swift 앱이었으므로 당시 기록에는 소유하지 않는 경로가 그 앱에 닿는다고 쓰였을 수 있지만, 지금은 그렇지 않습니다.

### 프로젝트의 Clawdline 표시 설정

사람이 현재 작업 중인 프로젝트를 Clawdline에서 알아보기 쉽게 해 달라고 요청했을 때 이 부분을 사용하세요. 목표는 단순히 파일을 만드는 것이 아닙니다. 프로젝트의 이름과 표시가 실제와 맞고, 오래 걸리는 작업이 진행 상태를 보고하며, Clawdline이 개발 서버를 시작하지 않아도 그 상태를 볼 수 있어야 합니다.

먼저 저장소의 지침, README, 배포·빌드 스크립트, 기존 프로세스 관리자 설정을 읽으세요. 프로젝트에서 이미 쓰는 명령을 유지하세요. Clawdline을 위해 배포 경로나 프로세스 관리자를 하나 더 만들지 마세요. 사람이 운영 변경을 요청하지 않았다면 어떤 것도 시작·중지·재시작·배포하지 마세요. 설정을 작성하는 일과 실제 배포는 별개의 작업입니다.

다음 네 가지를 확인하되 실제로 적용할 수 없는 항목만 건너뜁니다.

1. **프로젝트.** `clawdline project list`를 실행합니다. 이 체크아웃이 없다면 저장소 루트를 `clawdline project add <absolute-root>`로 추가하고 다시 목록을 확인합니다. 세션을 시작할 수 있는 위치를 기록할 뿐, 저장소를 변경하지 않습니다.
2. **이름과 아이콘.** 별도 설정이 없으면 Clawdline이 안정적인 아이콘을 만듭니다. 사람이 특정 이름이나 픽셀 표시를 원한다면 `~/.claude/project-icons.json`의 다른 항목은 모두 보존하고, 이 프로젝트에 적용되는 가장 긴 포함 경로만 수정하세요. 형식은 Clawdline 저장소의 `docs/project-status.md`에 있습니다. Projects 페이지에서 JSON을 직접 편집하지 않고 기존에 결정된 아이콘을 복사할 수도 있습니다. 이 전역 사용자 파일은 저장소의 일부가 아닙니다. 현재 요청에 수정 승인이 이미 포함되지 않았다면 변경 전 정확한 항목을 보여주세요.
3. **배포와 오래 걸리는 작업.** Clawdline은 상태 영수증만 읽으며 배포를 실행하지 않습니다. GitHub 저장소의 배포 영수증은 `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`이며 owner와 repo는 `origin`에서 가져옵니다. 실행을 이미 아는 생산자가 `state`(`running`, `ok`, `fail`, `none`), `label`, `url`, `started_at`, 측정된 `typical_seconds`를 원자적으로 기록합니다. 로컬 빌드·테스트·가져오기·배포 명령에는 도우미가 있다면 `clawdline-progress run --label <label> -- <command>`를 쓰고, 없으면 `docs/project-status.md`의 `run-<path>.json` 계약을 구현합니다. 소요 시간을 지어내지 마세요. 측정하기 전에는 생략합니다. 종료된 생산자가 영구적인 실행 중 상태를 남겨서도 안 됩니다.
4. **개발 서버.** 배포할 수 있는 가장 가까운 루트에 `.devstack.json`을 추가하거나 수정합니다. 현재 Go 데몬은 선언된 `processes`를 읽고 루프백 `port`를 검사하거나 `url`을 엽니다. 브라우저에서 `status`, `up`, `down`, `restart`, `logs` 명령을 실행하지는 **않습니다**. 실제 상태를 반영하는 최소한의 Tier 0 파일을 우선하세요.

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

   안정적인 포트나 URL이 없는 프로세스를 추측해서 넣지 마세요. 개발 환경 선언을 검증하면서 운영 환경을 탐색하거나 재시작하지 마세요.

변경한 각 층을 따로 검증하세요. `clawdline project list`에 체크아웃이 나와야 하고, 각 JSON 파일이 파싱되어야 하며, 변경한 스크립트 경로에 대한 저장소 자체 테스트가 통과해야 합니다. `GET /v1/devstacks`는 선언된 서버를 조용히 누락하지 않고 실행 중·중지·알 수 없음 중 하나로 보여야 합니다. 프로젝트의 세션에는 최신 진행·배포 영수증이 보여야 합니다. 읽을 수 없거나 형식이 잘못되었거나 오래된 값이 있다면 어느 것인지 밝히고 상태를 알 수 없음으로 두세요. 보이지 않는 것을 성공으로 보고하지 마세요. 마지막에 설정한 것, 적용하지 않기로 한 것, git 밖에서 바뀐 사용자 소유 파일을 나열하세요.

**통합: Claude와 Codex가 같은 규칙과 스킬 사용**(`/clawdline unify`). Codex는 `AGENTS.md`와 `.agents/skills/<name>/`를 읽습니다. Claude는 `CLAUDE.md`를 읽고, 이 파일이 없거나 `@AGENTS.md` 줄이 있을 때만 `AGENTS.md`도 읽으며, 스킬은 `.claude/skills/<name>/`에서 읽습니다. 프로젝트의 규칙이 `AGENTS.md`에 있고 `CLAUDE.md`가 없거나 이를 가져오며, 모든 스킬이 `.agents/skills/<name>/`에 있고 `.claude/skills/<name>`이 그 상대 링크이면 통합된 상태입니다. 사람이 통합을 요청하거나 `/clawdline unify`를 호출하면 다음과 같이 합니다.

1. 세션의 프로젝트(git 최상위 디렉터리)에서 `clawdline project unify`를 실행합니다. 프로젝트가 목록에 없으면 먼저 `clawdline project add`로 추가합니다. 이 명령은 아무것도 변경하지 않습니다. Claude와 Codex가 현재와 변경 후에 각각 무엇을 읽는지, 모든 스킬 행과 조치 문장, 모든 충돌을 사람의 언어로 보여주세요. Codex가 보지 못하는 `CLAUDE.md`의 줄도 포함합니다.
2. 이 대화에서 사람 자신의 메시지가 해당 계획을 승인한 뒤에만 `clawdline project unify --apply`를 실행합니다. 앞서 보여준 버전을 보내며, 그사이 디스크가 바뀌었다면 `plan_changed`로 응답하고 아무것도 적용하지 않습니다. 이때 계획을 다시 출력하고 다시 물으세요.
3. `clawdline project unify --check` 결과를 보여줍니다(종료 코드 0은 통합, 1은 어긋남, 3은 알 수 없음). 아무것도 커밋되지 않으므로 어떤 파일이 바뀌었는지 말해 사람이나 세션이 커밋할 수 있게 하세요.

두 디렉터리에서 내용이 다른 스킬, 다른 곳을 가리키는 링크, Codex가 보지 못하는 규칙 사이의 충돌은 사람이 결정할 사항입니다. 사람의 메시지에 명시되지 않았다면 `AGENTS.md`, `CLAUDE.md`, 스킬을 편집해 직접 해결하지 마세요. 경로는 계획 조회용 `GET /v1/projects/{place}/unify`와 `{"version"}` 및 `Idempotency-Key`를 사용하는 `POST /v1/projects/{place}/unify`입니다. 거부 코드는 `plan_changed`, `plan_unknown`(프로젝트 일부를 읽지 못해 아무것도 바뀌지 않음), `name_taken`(통합으로 생길 이름이 이미 존재하며 덮어쓰지 않음)입니다.

## 2a. Feature Root의 일반적인 작업 흐름

보드 항목 하나를 소유한 세션, 즉 일반적인 Feature Root가 순서대로 수행할 전체 과정입니다. 각 단계는 명령으로 실행합니다. 명령은 인증 정보를 직접 싣기 때문에 같은 경로에 직접 만든 curl을 보내면 거부됩니다. 드문 작업은 해당 `clawdline guide <part>`에 있으며, 마지막에 안내가 있습니다.

**1. 항목을 읽습니다.** `clawdline item show <item id>`는 종류, 단계, 인수 기준, 기록된 게이트, 인수 기준 버전(`acceptance vN`), Feature의 경우 사람이 정한 Needs independent review 스위치, 체크리스트 단계와 본문을 포함한 모든 문서를 출력합니다. 이를 작업의 기준 기록으로 삼으세요. `clawdline item show <item id> --doc <doc id>`는 문서 하나의 본문만 출력하므로 파일로 보낼 수 있습니다. `clawdline item steps <item id>`는 문서 본문을 뺀 같은 기록입니다. 항목을 쓸 때마다 `wrote …; item <id> is at version N`, 간단한 항목 요약과 `item show` 안내가 나옵니다. `item show`에서 인수 기준, 단계, 문서 전체를 읽으세요. `--expected-version`을 지정하지 않으면 쓰기는 항목의 현재 버전에 적용됩니다.

ASSIGNMENT.md에 **HANDOFF** 제목이 있다면 다른 세션이 구현을 시작한 뒤 완료하기 전에 떠난 항목을 인계받는 것입니다. 계획을 세우기 전에 지정된 인계 자료부터 읽으세요. 데몬은 이전 담당자에게 묻지 않고 자체 기록과 git에서 자료를 만들었습니다. 항목과 연결된 작업 및 결과, 아직 반영되지 않은 커밋, 각 worktree의 미커밋 변경을 보존한 패치와 sha256 및 원래 기반에 복원하는 `git apply` 명령, 이전 담당자의 마지막 메시지(또는 읽을 수 없는 이유), 단계와 열린 체크리스트가 들어 있습니다. 패치는 ASSIGNMENT.md 옆에 있고 worktree보다 오래 남습니다. 여기서 이어서 작업하세요. 처음부터 다시 시작하지 마세요.

**2. 이 항목을 위해 열린 세션의 이름을 정합니다.** 목표와 범위를 읽은 뒤 `clawdline item name <item id> "<task name>"`을 한 번 실행합니다. 항목이 아닌 세션의 이름을 바꿉니다.

**3. 구현하기 전.**

- Captured planning이 켜져 있고 인수 기준이 없다면 `clawdline item acceptance <item id> --body-file acceptance.md`로 관찰 가능한 기준을 적습니다.
- Needs independent review가 선택되었거나 Epic이라면 먼저 `clawdline guide ko epic`의 검토된 계획 절차를 따릅니다. 선택되지 않았다면 계획도 검토 child도 필요하지 않습니다.
- 여러 단계로 진행할 작업인데 체크리스트가 없다면 `clawdline item step-add <item id> "first" "second" …`로 한 번씩 검증할 수 있는 단계 두 개에서 여덟 개를 추가합니다. 단일 변경에는 단계가 없어도 됩니다.
- 그다음 `clawdline item phase <item id> implementing`을 실행합니다.

**4. 기본적으로 이 세션에서 작업합니다.** Feature를 직접 조사·구현·검증·반영하세요. 별도 세션이 유용한 구체적인 이유가 있을 때만 child를 배정합니다. 실제로 독립적인 병렬 작업, 서로 다른 도구나 권한, 필수 독립 검토가 그 예입니다. 배정 전에 이유를 말하세요. 일반적인 조사나 구현만으로는 배정할 이유가 되지 않습니다.

배정이 필요해도 종합·통합·반영은 이 세션에서 맡습니다.

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id`가 child를 항목에 연결하므로 반영 결과가 그 항목의 것으로 계산됩니다. child 하나가 여러 항목을 맡는다면 옵션을 반복합니다. 첫 항목이 child의 주 작업이 되고, 반영은 모두에 계산됩니다.
- 제목은 무엇이 달라질지를 말하는 한 줄이며 최대 60자입니다. 콜론(`:` 또는 `：`)은 관찰과 설명을 붙이므로 거부됩니다. 주어가 "the user"인 제목이나 코드 서식 식별자로 시작하는 제목도 거부됩니다. 각각 `bad_task`와 이유를 담은 `title: …`로 응답합니다.
- 지시서는 독립적으로 이해할 수 있어야 합니다. 확인한 사실마다 `file:line`이나 확인 명령을 넣어 child가 다시 찾지 않게 하세요.
- 조사 또는 Explore child의 지시서에는 질문의 답을 얻으면 끝나는 중단 조건과 턴 제한도 넣습니다.
- 읽기 전용 작업은 `--claims ""`를 사용합니다. 모든 플래그와 거부 코드는 `clawdline guide ko dispatch`에 있습니다.

**5. child가 완료되면** 입력창에 `<clawdline-notice>` 줄이 입력됩니다. `clawdline task show <task id>`를 실행하고 결과를 통합하세요. 읽으면 알림이 닫히므로 별도 ACK는 필요하지 않습니다. **배정 후에는 턴을 끝내세요.** 알림이 다시 깨워 줍니다. 열린 턴에서 계속 폴링하면 매번 전체 컨텍스트를 다시 읽습니다. 다른 할 일이 없고 반드시 기다려야 할 때만 `clawdline task wait <task id>…`를 실행하세요(기본 `--timeout 9m`, 첫 번째 완료를 기다리려면 `--any`). worktree child의 결과는 **브랜치를 대상에 병합하여** 통합합니다. **병합 자체가 몇 분 안에 반영을 기록합니다.** 별도 반영 기록을 게시하지 마세요. `clawdline landings`는 아직 해야 할 반영을 나열합니다. `--claims ""`로 배정한 child가 아무것도 쓰지 않았다면 브로커가 `nothing_to_land`를 기록합니다. 그 밖의 경우는 `clawdline task land <task id> <state>`를 사용합니다(`clawdline guide ko landing`).

**6. 완료 보고서.** 원인 파악에 상당한 조사가 필요했다면 작성합니다. 직접 관찰한 단순 수정에는 필요하지 않습니다. `done` 전에 추가하세요. 완료 후에는 담당자가 해제되어 `409 not_item_owner`가 나옵니다.

```sh
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

보고서를 문제를 제기한 사람이 읽을 수 있는 Markdown으로 쓰고 비공개 정보는 넣지 마세요.

**7. 항목을 마칩니다.** 각 단계가 검증되면 `clawdline item step-done <item id> <step id>`로 완료합니다. 직접 작업은 일회용 worktree에서 커밋하고 푸시하세요. child를 썼다면 그 브랜치를 병합하세요. 반영 후 다음 명령 하나로 항목을 `done`으로 옮길 수 있습니다. child의 기록된 반영에는 커밋·대상·원격 저장소가 들어 있습니다. 직접 작업이라면 직접 지정합니다.

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --commit <sha> --target main --remote origin \
  --deployment "what went live, where, which version"      # omit landing flags for a landed child
```

또는 단계를 하나씩 진행합니다.

```sh
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> deploying --no-landing-reason "why there is no code"    # work with no code
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

항목의 배포 정책에 따라 `done`에는 `--deployment` 또는 `--no-deployment-reason`이 필요합니다. `clawdline item steps <item id>`가 게이트 줄을 출력한다면, 그 줄이 안내하는 더 긴 경로를 따르세요.

**배포 대기.** 배포가 실행되거나 전파되는 동안 턴을 열어 두지 마세요. 배포와 검사를 콜백 하나로 시작하고 턴을 끝낸 뒤, 알림이 오면 항목을 마칩니다.

```sh
clawdline callback --title "The hosted console serves <sha>" --work-id <item id> --timeout 20m -- \
  sh -c './deploy.sh <sha> && tools/wait-hosted-console.sh <sha>'
# … the notice: clawdline task show <callback id>, then
clawdline item finish <item id> … --deployment "what went live, where, which version (callback <callback id>)"
```

**8. 턴을 보고합니다.** `clawdline session report --summary "…"`를 실행합니다(§7).

**9. 담당 세션은 열어 둡니다.** 보드 항목을 완료하거나 취소하면 담당 배정만 해제됩니다. 그 항목을 소유했던 세션은 종료되지 않습니다. `session report` 후에도 후속 작업에 사용할 수 있게 세션을 두세요. 항목이 `done` 또는 `cancelled`가 되었다는 이유만으로 `clawdline session close`를 실행하지 마세요. 사람이 나중에 명시적으로 세션 종료를 요청할 수 있습니다. Agent가 배정한 child의 탭은 해당 작업 종료 후 브로커가 `clawdline guide child`의 규칙에 따라 별도로 닫습니다.

**거부된 경우.** `version_conflict`라면 같은 명령을 다시 실행하세요. 명령이 버전을 다시 읽습니다. `steps_incomplete`는 아직 열린 단계가 있다는 뜻입니다. 그 밖의 코드는 §12와 해당 작업의 부분을 차례로 읽으세요.

**드문 작업별 안내:** `clawdline guide ko board`는 제안·결정·할 일·완료 항목 다시 열기·사람을 기다리기·게이트와 단계별 거부, `clawdline guide ko epic`은 계획·계획 검토·Epic의 하위 항목·persona, `clawdline guide ko landing`은 수동 반영·인계(오래 작업한 Root의 이정표 인계 포함)·Root 배정, `clawdline guide ko running`은 멈춘 child·남은 일·재시작을 다룹니다.

## 3. 배정하기 전에 기존 작업 확인

다른 세션이 이미 같은 일을 하고 있을 수 있습니다. 병합되지 않은 브랜치에 완료된 결과가 있다면 공유 작업 트리의 `git status`에는 보이지 않습니다. 먼저 기존 상태를 읽으세요.

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- 응답에는 `generation`, `task_root`, 그리고 `live`, `unlanded`, `droppable`, `unreadable` 네 목록이 있습니다. 각 행에는 데몬이 받아들일 `do`가 들어 있습니다. `claims`를 지정하면 활성 행마다 겹치는 쓰기 범위(`overlaps`)가 표시됩니다.
- **배정에는 `generation`이 필요합니다**(§4). 행의 봉인된 필드에서 얻은 16자리 16진수이며, 행이 시작·종료하거나 쓰기 범위가 바뀌면 값이 바뀝니다.
- **`task_root`는 `task.json`을 둘 위치입니다.** 이 데몬 자체의 필드입니다. Swift 브로커는 `/tmp/.clawdline`을 고정 경로로 썼으므로 해당 필드가 없었습니다.
- `project`가 Git 저장소 안의 절대 경로가 아니면 `400 bad_request`가 나옵니다.

다음도 한 번 읽어 볼 가치가 있습니다.

- `GET /v1/orchestrator/inflight?project=…` — 저장소에서 아직 끝나지 않은 모든 작업, 담당자와 선언된 쓰기 범위를 보여줍니다.
- `clawdline assistants` — Assistant별 `availability`(`ok`, `low`, `exhausted`, `unknown`), `windows`, `stale`, `resets_at`을 보여줍니다. 이를 읽고 배정 대상을 고르세요. 할당량 때문에 배정 자체를 거부하지는 않습니다.

**배정이 필요한 작업인가요?** 독립적인 부분으로 나뉘는 일은 병렬로 처리하면 빨라집니다. 각 단계가 앞 단계에 의존하는 연쇄 작업은 인계 때마다 흐름이 끊겨 더 느려집니다. 진단, 지시서를 쓰는 것보다 작은 일, 누군가 즉시 결과를 기다리는 일은 현재 세션에서 진행하세요. 이 컴퓨터의 규칙은 `<state dir>/dispatch-policy.md`와 사람의 `dispatch-policy.local.md`에 있으며, 모든 child가 지시서에서 받습니다.

## 4. 담당 child에게 작업 배정

담당 child는 root 아래의 범위가 정해진 작업입니다. **종합·통합·반영은 root가 맡습니다.**

**다음 네 단계를 하나의 명령으로 수행할 수 있습니다.** 지시서는 표준 입력이나 파일로 줍니다.

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--persona <id>] [--work-id uuid …] [--task-id uuid] [--label "…"] [--project-dir D] < brief.md     # or --instructions-file brief.md
```

명령은 ID와 비밀을 만들고 inventory에서 `generation`과 `task_root`를 읽어 `task.json`을 쓴 뒤 작업을 게시합니다. `stale_inventory`가 한 번 나오면 inventory를 다시 읽고 한 번 재전송합니다. 성공하면 `dispatched <id> <state> [worktree <path>]`를 출력하고, 이어 데몬의 경고 및 쓰기 범위가 겹치는 각 활성 작업을 한 줄씩 출력합니다. `--json`은 대신 데몬 응답을 출력합니다. 거부되면 stderr에 `refused, <status> <code>: <message>`를 출력하고 추가 필드와 해결 방법을 각 한 줄씩 출력한 뒤 종료 코드 1로 끝납니다. 이 부분 끝의 표에 코드별 의미가 있습니다. root는 `CLAUDE_CODE_SESSION_ID`나 `CODEX_THREAD_ID`가 가리키는 현재 대화이며, 없으면 `--conversation`으로 지정합니다. `--assistant`가 없으면 child는 root와 같은 Assistant를 사용합니다. `--project-dir`이 없으면 프로젝트는 현재 디렉터리의 git 최상위입니다. `--claims ""`는 쓰지 않는 child를 선언합니다. 데몬이 worktree와 child 탭을 여는 동안 명령은 아무것도 출력하지 않습니다. child가 만들어졌을 때 응답하는 요청 하나이므로 한 번 길게 기다리세요(§2 「오래 걸리는 명령 대기」). 비밀은 argv, `task.json`, 출력에 나타나지 않습니다. 토큰도 다른 얇은 명령처럼 내부에서 읽습니다.

재시도가 필요할 수 있는 검토 작업은 첫 호출 전에 소문자 UUID를 정하고 모든 시도에 `--task-id`로 전달하세요. 명령은 원래 배정 의도의 비공개 사본을 `task.json` 옆에 보관하므로 데몬이 지시서를 다시 쓴 뒤에도 같은 요청을 재전송할 수 있습니다. 브로커는 원래 작업 ID와 `(replayed)`를 돌려줍니다. 지시서가 바뀌면 로컬에서 거부됩니다. 명령이 시간 초과되거나 출력이 유실되었다면 실패로 간주하기 전에 `GET /v1/orchestrator/tasks/<id>`를 확인하세요. 작업이 없다면 같은 ID와 지시서로 재시도할 수 있습니다. 명시적인 거부는 작업을 만들지 않았으므로 원인을 고친 뒤 재시도할 수 있습니다.

`--persona <id>`는 내장 persona로 child를 시작합니다(`task.json`의 `persona`). 현재 빌드에 없는 ID는 로컬에서 거부됩니다. `plan_review`를 포함해 어느 종류에도 기본 persona가 없습니다. 원하면 직접 `code-reviewer`를 지정하세요. `GET /v1/personas`는 이 빌드의 ID 목록을 반환합니다. persona의 의미는 §10의 Epic 부분(`clawdline guide ko epic`)에 있습니다.

바이너리 없이 호출할 때 명령이 대신 수행하는 단계는 다음과 같습니다.

**1. ID와 비밀을 선택합니다.**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 characters, lowercase
SECRET=$(openssl rand -hex 32)          # 64 lowercase hex
```

비밀은 POST 본문으로 데몬에 전달되고, 데몬이 child 탭에 입력하는 한 줄로 child에게 전달됩니다. `task.json`이나 배정 응답에는 없으며 다시 필요하지 않습니다. 단, respawn 응답에는 복사된 작업의 새로운 비밀이 들어 있습니다.

**2. inventory를 읽어**(§3) `generation`과 `task_root`를 얻습니다.

**3. `<task_root>/<TASK_ID>/task.json`을 씁니다.** 데몬은 요청 본문이 아닌 이 파일에서 지시서를 읽습니다. 접수 시 검증하고 승인된 내용으로 `task.json`을 다시 쓴 뒤 같은 기록에서 child의 `CHILD.md`를 만듭니다. 제목·지침·쓰기 범위·산출물·종류·시간 제한이 포함됩니다. 따라서 child는 검증된 지시서를 읽으며 `task.json`을 직접 읽지 않습니다.

| 필드 | 규칙 |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | 동일한 ID |
| `assistant` | `claude` 또는 `codex` |
| `project_dir` | 존재하는 디렉터리의 절대 경로 |
| `title` | 화면에 표시되는 한 줄, 최대 60자이며 달라질 결과를 설명합니다. 콜론(`:` 또는 `：`), 주어로 쓰인 "the user", 코드 서식 식별자로 시작하는 제목은 `bad_task`와 `title: …`로 거부됩니다 |
| `instructions` | 필수, 최대 16 KiB. child에게 다른 맥락이 없으므로 독립적으로 이해되어야 합니다. 확인한 사실마다 `file:line`이나 확인 명령을 넣고, 조사 child에는 중단 조건과 턴 제한도 줍니다 |
| `claims` | **필수**. child가 쓸 수 있는 상대 경로 최대 32개입니다. `[]`는 아무것도 쓰지 않으며 `claims_missing` 경고가 나옵니다 |
| `isolation` | 기본값 `none`, 또는 자체 브랜치의 별도 체크아웃을 위한 `worktree` |
| `permission_mode` | `ask`, `edits`, `full` 중 하나 |
| `timeout_minutes` | 1–240, 기본값 30 |
| `kind`, `deliverables`, `model` | 선택 항목. `model`은 `[a-z0-9._-]` 형식, 최대 64자입니다 |
| `work_id` | 연결할 보드 항목의 선택적 UUID |
| `persona` | 선택적 내장 persona ID(`GET /v1/personas`). 기본값은 없음 |
| `auto_compact_window` | Claude 전용 선택 항목. child가 압축을 시작하는 컨텍스트 크기(토큰 50000–1000000) 또는 압축 없음인 `null`입니다. 생략하면 컴퓨터의 `claude_auto_compact_window`를 따르며, 사람이 설정하지 않았다면 꺼져 있습니다. 일상 지시서가 아니라 실행 비교를 위한 값입니다. 압축은 세부 사항을 잃을 수 있습니다 |
| `root` | **필수**. `{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "project_dir": "<the same absolute repository path as project_dir>", "label": "…"}`. 역할 범위가 지정된 root는 데몬이 프로젝트 범위를 확인하도록 `root.project_dir`가 필요합니다 |

**배정 전에 child에게 필요한 모든 도구와 작업 환경·시작 모드를 맞추세요.** 지시서에 필수 도구를 적고 root가 선택한 환경에서 실제로 제공되는지 확인합니다. Codex CLI child는 권한 플래그만으로 ChatGPT 데스크톱 앱의 내장 `@Browser`를 얻지 못합니다. UI·접근성·반응형 레이아웃 검토라면 Browser/Computer Use가 실제 있는 환경에 맡기거나, 동등한 인수 검증이 가능한 Playwright/Chrome CDP 같은 로컬 브라우저 도구를 지정하고 설치되어 있음을 입증하세요. 그 환경에서 앱·origin·GUI 접근 요청이 생길 수 있다면 `--permission-mode ask`로 배정합니다. Codex의 `full`은 비대화형 셸 실행(`--ask-for-approval never`)을 뜻하며 모든 도구를 뜻하지 않습니다. 만들어지지 않은 요청은 Auto-review도 검토할 수 없습니다. child는 시작할 때 명령 이름만 확인하지 말고 각 필수 도구를 실제로 사용해 봅니다. 사용할 수 없으면 즉시 정확한 부족 사항을 보고하고, root는 접근을 복원하거나 다시 배정합니다. root가 맞지 않는 작업 환경을 골랐다는 이유로 도구가 필요한 인수 검증을 미검증 상태로 마치지 마세요.

**`root.session_id`는 터미널 ID가 아닌 대화 ID입니다.** Claude Code는 이를 `CLAUDE_CODE_SESSION_ID`, Codex는 `CODEX_THREAD_ID`로 내보냅니다. 데몬이 child를 root 아래에 묶고 완료를 알리는 데 사용합니다. 이 탭을 가리키는지 확인하려면 `GET /v1/orchestrator/whoami?conversation_id=<id>`의 `terminal_id`를 읽으세요.

**4. 오케스트레이터 토큰으로 배정합니다.**

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

비밀이 argv에 남지 않도록 본문을 표준 입력으로 보내세요(`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`). 응답은 `{ok, task, warnings?}`입니다. `warnings`에서 `claims_overlap`, `claims_missing`, `claims_ignored_for_worktree`, `dirty_worktree_base`, `work_not_placed`를 확인하세요. 마지막 코드는 지정한 항목을 아직 보드에 올리지 못했으며 보드 정리 작업이 다음 주기에 처리한다는 뜻입니다. 같은 ID를 다시 게시하면 저장된 작업과 `replayed: true`를 반환하므로 재시도는 안전합니다.

탭을 열지 못해도 `task.state: "spawn_failed"`와 함께 HTTP 200으로 응답합니다. 오케스트레이터 토큰을 사용한 `POST /v1/orchestrator/tasks/<id>/respawn`은 새 비밀을 가진 복사 작업을 엽니다. 원본당 최대 두 번입니다.

잘못된 지시서나 범위로 배정했거나 같은 일을 중복 배정했나요? 슬롯과 쓰기 범위를 점유한 채 완료 또는 시간 초과를 기다리지 마세요. `clawdline task cancel <id> --reason "…"`로 바로 중지합니다(§5).

**자주 만나는 거부 코드**(검사 순서):

| 상태 | 코드 | 조치 |
|---|---|---|
| 409 | `task_unreadable` | 해당 ID의 작업이 저장되어 있지만 읽을 수 없습니다. 같은 ID로 다시 보내지 마세요 |
| 422 | `bad_task` | 메시지에 문제가 된 필드가 나옵니다. "No readable task.json under …"도 포함됩니다. `task_root`를 확인하세요 |
| 422 | `claims_required` | `claims`를 추가하세요 |
| 422 | `root_session_required`, `root_assistant_required` | `root.session_id`와 `root.assistant`를 추가하세요 |
| 403 | `session_scope_mismatch` | `root.project_dir`가 있으며 root 세션의 프로젝트와 역할 스냅샷에 맞는지 확인하세요. 지시서 또는 CLI를 고치세요. 사람에게 프로젝트 설정 변경을 요구하지 마세요 |
| 422 | `detached_route_required` | `root.poll_only`를 보냈습니다. 이는 분리된 자동화입니다(§6) |
| **409** | **`stale_inventory`** | `generation`이 없거나 오래되었습니다. 오류 안의 현재 inventory 전체를 읽고 다시 판단한 뒤 그 `generation`으로 재전송하세요 |
| 422 | `work_not_found`, `work_other_project`, `work_closed` | 지정한 `work_id`가 항목이 아니거나 다른 프로젝트 소속이거나 닫혀 있습니다 |
| 422 | `also_work_not_found` | `also_work_ids` 중 보드 항목이 아닌 ID가 있습니다. `work_id` 검사 후 같은 방식으로 검사합니다 |
| 503 | `store_unavailable` | 지정한 항목 확인에 필요한 보드를 읽지 못했습니다. 아무것도 시작되지 않았으므로 다시 보내세요 |
| 409 | `graph_*` | 작업 그래프 접수 규칙입니다(`graph` 필드) |
| 409 | `no_child_capability` | 이 플랫폼에서 child를 열 수 없습니다. `missing`에 이유가 나옵니다 |
| 429 | `squad_launch_capacity` | persona 시작 작업이 너무 많이 세션을 기다리고 있습니다. 나중에 재시도하세요 |
| 429 | `rate_limited` | 10분 안에 너무 많이 배정했습니다 |
| 422 / 409 | `root_unresolved`, `conversation_ambiguous` | 대화 ID에 맞는 활성 세션이 없거나 둘 이상입니다. ID를 고치고 분리 모드로 바꾸지 마세요 |
| 403 | `session_actor_required` | 역할을 가진 root는 자기 세션에서 그 세션의 squad 기능으로 배정해야 합니다 |
| 403 | `session_scope_mismatch` | 여기서는 root 세션의 프로젝트가 역할 스냅샷과 맞지 않는 경우도 포함합니다 |
| 503 | `squad_policy_unavailable` | 역할 배정 설정을 읽지 못했습니다. 아무것도 시작되지 않았습니다 |
| 409 | `persona_disabled_for_auto_assignment` | 대상 프로젝트에서 해당 persona의 자동 배정이 꺼져 있습니다 |
| 429 | `over_capacity` | root의 child 슬롯(기본 5개) 또는 컴퓨터 전체 슬롯이 찼습니다. `retry_after`를 확인하세요 |
| 409 | `workspace_busy` | 다른 root의 쓰기 범위와 겹칩니다. 오류에 차단 중인 작업이 나옵니다 |
| 409 | `worktree_unavailable` | 별도 체크아웃을 만들지 못했습니다 |
| 429 | `terminal_busy` | 터미널 쓰기 레인이 모두 사용 중입니다. `retry_after: 5`를 확인하세요 |

## 5. 실행 중과 완료 후

child는 `clawdline task accept`로 지시서 수령을 확인합니다(`/accepted`를 게시하거나 `accepted.json`을 남깁니다). 계획이 바뀌면 진행 메모 하나(`/progress`)를 보내고, 알림을 최대 다섯 번(`/notify`) 보낼 수 있습니다. 완료할 때 `result.json`을 쓰고 `clawdline task finish`를 실행합니다. root는 이 경로들을 호출하지 않습니다.

- `clawdline task show <id>`는 작업 하나와 상태를 보여줍니다(`GET /v1/orchestrator/tasks/<id>`). `GET /v1/orchestrator/tasks`는 목록을 보여줍니다(`?state=`, 최대 500개를 지정하는 `?limit=`).
- **완료되면 데몬이 입력창에 `<clawdline-notice>` 줄을 입력합니다.** `body`는 작업, 종료 방식, 해당 결과에만 속한 사실(정지, 해제된 쓰기 범위, 브랜치, 남은 일의 개수)과 실행할 명령 `clawdline task show <id>`를 담은 짧은 문장 하나입니다. 그 명령은 작업을 출력한 뒤 알림을 닫습니다. JSON(버전 3)에는 `task`, `state`, `notice_id`가 있고, `outstanding`, `leftovers`, `claims_released`는 내용이 있을 때만 있습니다. 결과 자체는 `task show`가 출력합니다. 입력하지 못한 줄은 5초에서 300초 간격으로 재시도합니다. 화면에 표시되었지만 작업을 아직 읽지 않았다면 전체 줄을 다시 입력하지 않습니다. 같은 명령을 알려 주는 짧은 `task_reminder` 줄을 2분에서 30분 간격으로 최대 여덟 번 입력한 뒤 중단합니다. 메뉴가 열려 있는 동안에는 입력하지 않으며, 메뉴 때문에 여덟 번의 횟수가 소모되지도 않습니다. 줄은 최대 12시간 대기하다 메뉴가 닫히면 입력됩니다. `task show`는 다음을 보냅니다.

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  `clawdline task show <id>`와 `clawdline task wait <id>…`는 종료된 작업을 출력한 뒤 이를 전송합니다. `clawdline task ack <id> <notice_id>`는 수동으로 전송하고 한 줄을 출력합니다. 두 번째 ACK는 `changed: false`로 응답합니다. 미확인 알림은 `GET /v1/orchestrator/completions`에 나열되고, `POST /v1/orchestrator/completions/reconcile`로 재활성화할 수 있습니다. 전송을 포기한 알림도 세션이 다시 유휴 상태가 되면 한 번 더 입력됩니다.
- **알림 줄을 못 보더라도 완료 사실을 알 수 있습니다.** 매 턴 경계에 읽는 자신의 `GET /v1/work/v2/agent/session-todos/<conversation id>`에는 `unacknowledged_completions`가 있습니다. 완료되었지만 아직 확인하지 않은 child마다 `task_id`, `title`, `state`, `kind`, `result_path`, `notice_id`, `ack_path`를 포함하며, 알림이 대기 중인지 포기되었는지도 보여줍니다. `clawdline session report`는 영수증 뒤에 이를 출력합니다. 각각 `clawdline task show <id>`로 읽고 통합하세요. 읽기가 ACK이므로 두 목록에서 모두 사라집니다. `task show`는 요약 전체와 생략한 항목 수를 출력합니다. `--json`은 기호와 산출물을 포함한 데몬 응답 전체입니다. 이것으로 부족할 때만 `result.json` 자체를 읽으세요.
- **남은 일이 적힌 결과는** child가 하지 않은 일을 알려 줄 뿐 그 자체로 아무것도 바꾸지 않습니다. `task show`가 제목을 나열합니다. 하나를 사람에게 묻고 싶다면 `POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}`를 보냅니다. 사람은 추적, 나중에(Backlog), 거절 중 하나로 답합니다. 답하기 전에는 보드에 아무것도 올라가지 않습니다.
- **지시서를 받은 직후 멈춘 child는 재촉한 뒤 보고됩니다.** 지시서가 입력되었지만 수령 확인은 하지 않았고 화면이 5분 동안 유휴 상태(프롬프트 표시, 빈 입력창, 메뉴와 작업 줄 없음)라면 데몬이 `CHILD.md`를 가리키는 한 줄을 입력합니다. 비밀은 입력하지 않습니다. 그 뒤 5분 더 확인 없이 유휴 상태면 작업은 정지 판정과 함께 `spawn_failed`로 끝나고 `task_finished` 대신 `"kind": "task_stalled"` 알림이 옵니다. `POST /v1/orchestrator/tasks/<id>/respawn`으로 재시작하거나 다시 배정한 뒤 알림을 ACK하세요. 작업 중이거나 메뉴를 표시 중이거나 이미 확인한 child에게는 이 줄을 입력하지 않습니다.
- **잘못 배정한 child는 취소합니다.** 지시서·범위가 틀렸거나 중복이라면 `clawdline task cancel <id> --reason "wrong brief"`를 실행합니다(`POST /v1/orchestrator/tasks/<id>/cancel`, `{"reason":"…"}`). 이유는 필수이며 최대 500바이트입니다. 작업은 그 이유를 판정으로 남기고 `cancelled`로 끝납니다. 탭이 닫히고 쓰기 범위와 child 슬롯이 해제되며, 취소 이유를 담은 알림 하나가 옵니다. **커밋은 버리지 않습니다.** 커밋한 child의 브랜치와 체크아웃은 남고, 반영은 브랜치의 커밋 수를 적은 메모와 함께 대기합니다. `task show`와 `clawdline landings`에서 확인하세요. 필요한 것을 병합하거나 `clawdline task land <id> abandoned`로 기록합니다. 배정한 root 세션이나 콘솔의 사람만 취소할 수 있습니다. 다른 주체는 `403 not_task_root`로 거부됩니다. 역할로 열린 세션은 자체 기능을 보내야 하며(`session_actor_required`) CLI 명령이 이를 처리합니다. 이미 끝난 작업은 `state`를 포함한 `409 task_already_terminal`로 응답합니다. 같은 취소를 다시 실행하면 `replayed: true`와 같은 성공 응답이 나옵니다. 기다리던 작업이 취소되면 `clawdline task wait`의 종료 코드는 5입니다. 그 밖에는 완료·실패·시간 초과로 작업이 끝납니다.
- **child가 완료해도 코드가 반영된 것은 아닙니다.** 통합하기 전까지 결과는 공유 작업 트리나 child 브랜치에 남습니다.

## 5a. 오래 걸리는 명령을 기다릴 때 사용하는 callback

배포, CI 실행, 릴리스 전파, 긴 빌드처럼 답이 나중에 나오는 작업은 턴 안에서 기다리거나 이후 턴에서 반복 조회하지 마세요. 데몬에 맡깁니다.

```sh
clawdline callback --title "CI is green on <sha>" --timeout 45m -- gh run watch <run id> --exit-status
```

명령은 `callback <id> briefed`를 출력하고 돌아옵니다. **턴을 끝내세요.** 명령이 끝나면 데몬이 `callback <first 8 of id> finished: success (exit 0 after 6m) — run clawdline task show <id>` 같은 본문의 `<clawdline-notice>`를 입력합니다. `task show`는 종료 코드와 출력 마지막 줄 등 결과를 보여주고 child의 경우처럼 알림을 닫습니다.

- callback은 탭이 없는 자신의 작업입니다. `clawdline task cancel <id> --reason "…"`는 명령의 전체 프로세스 그룹을 중지합니다. `--timeout`(1분–4시간, 기본 30분)이 지나면 중지되고 `timeout`으로 확정됩니다. 실행 중인 callback은 실행 중인 child처럼 세션 종료를 막습니다.
- 명령은 셸을 거치지 않고 주어진 단어 그대로 실행됩니다. 셸이 필요하면 `sh -c '…'`를 사용하세요. 현재 디렉터리에서 실행되며(`--dir`로 변경 가능), 환경에서는 PATH, HOME, locale, USER, SHELL, TMPDIR, TERM만 넘깁니다. 인증 정보는 전달되지 않습니다. 필요한 명령은 자체 파일에서 읽어야 합니다.
- 출력은 작업 디렉터리의 `output.log`에 기록되고 종료 후 7일간 보존됩니다.
- 한 번만 실행됩니다. 그사이 데몬이 재시작하면 명령을 다시 감시합니다. 데몬이 보지 않는 동안 종료되어 종료 상태를 남기지 않은 명령은 결과 불명인 `failure`로 확정하며 **다시 실행하지 않습니다.** 안전하다면 직접 새로 시작하세요.
- CLI가 데몬에 닿지 못해 시작 여부가 불확실하다면 출력된 ID를 `--task-id <the id it printed>`에 넣어 재시도하세요. 같은 ID가 두 번 시작되지는 않습니다.
- callback은 child 슬롯을 차지하지 않습니다. 세션당 최대 8개, 컴퓨터당 최대 16개가 실행됩니다. 초과하면 `retry_after`와 함께 `429 callback_capacity`로 거부됩니다. dispatch에서 스스로 `kind callback`이라고 선언할 수 없습니다(`bad_task`). Windows는 `501 no_callback_capability`로 거부합니다.

## 6. 반영과 그 밖의 세 가지 작업 유형

**child 브랜치를 병합했다면 더 할 일이 없습니다.** 브로커가 아래 방식으로 `landed`를 직접 기록합니다. 쓰기 범위를 선언하지 않은 child(`--claims ""`)가 스스로 완료했고 브랜치나 체크아웃에 아무것도 남기지 않았다면 브로커가 알림을 입력하기 전에 `nothing_to_land`를 기록합니다. 수동 반영 기록은 이 두 경우에 속하지 않는 cherry-pick, `incorporated` 결과, `nothing_to_land`, `abandoned`를 위한 것입니다. 오케스트레이터 토큰으로 명령 하나를 보냅니다.

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

스크립트는 `X-Clawdline-Orchestrator` 헤더로 다음 경로를 호출할 수 있습니다.

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- 허용되는 키는 위에 나온 것과, 허용하지만 사용하지 않는 `delivery`뿐입니다. 그 밖의 키는 거부됩니다. `pending`과 `abandoned`에는 작업 비밀이나 오케스트레이터 토큰을 사용할 수 있습니다. `landed`, `incorporated`, `nothing_to_land`에는 오케스트레이터 토큰만 사용할 수 있습니다.
- `landed`에는 `target`과 `commit`이 필요합니다. `incorporated`에는 이 결과를 검증된 반영으로 함께 실어 간 다른 작업을 나타내는 `carrier_task`도 필요합니다. 데몬은 **둘 다 Git에서 검증합니다.** 실패하면 `409 unverified_landing`과 다음 `reason` 중 하나가 나옵니다: `commit_unresolved`, `target_unresolved`, `not_on_target`, `base_unknown`, `predates_dispatch`, `delivery_unknown`, `nothing_delivered`, `not_the_delivery`; `incorporated`의 경우 `carrier_required`, `carrier_is_delivery`, `carrier_unresolved`, `carrier_not_landed`, `carrier_repository_mismatch`, `carrier_target_mismatch`, `carrier_commit_mismatch`, `delivery_is_ancestor`도 있습니다.
- 작업이 실제로 저장소에 썼다면 `nothing_to_land`는 `409 wrote_to_repository`로 거부됩니다.
- 확정된 반영은 바꿀 수 없습니다. `409 invalid_transition` 또는 값이 다를 경우 `409 landing_conflict`가 나옵니다.
- **병합은 자체적으로 기록됩니다.** 완료된 작업의 브랜치를 대상에 병합하면 브로커가 같은 Git 검증을 거쳐 몇 분 안에 대상의 HEAD를 커밋으로 사용해 `landed`를 기록합니다. 기록된 대상이 없다면 기본 체크아웃의 브랜치가 그 결과를 담은 유일한 브랜치일 때만 대상으로 정합니다. cherry-pick, `incorporated`, `nothing_to_land`는 직접 기록해야 합니다.
- **완료 알림은 작업 종료 시점의 브랜치 상태를 말하고 각각 필요한 명령을 안내합니다.** 브랜치에 커밋이 없다면 그 브랜치로는 반영을 증명할 수 없으므로 현재 상태에서 `landed` 기록은 불가능합니다. 체크아웃이 남아 있는 동안 그 체크아웃의 해당 브랜치에서 커밋하거나 `clawdline task land <id> abandoned`로 기록하세요. 커밋이 있다면 대상에 병합하세요. 병합이 반영을 기록합니다. 브랜치를 읽지 못했다면 먼저 브랜치를 확인하고 기록하세요. 공유 체크아웃에 썼다면 해당 변경을 대상에 싣는 커밋으로 `clawdline task land <id> landed`를 기록하거나 `abandoned`로 기록하세요. 아무것도 쓰지 않았고 브로커가 `nothing_to_land`를 기록했다면 ACK만 남습니다.

`clawdline landings`(`GET /v1/orchestrator/landings`)는 이 컴퓨터에서 대기 중인 모든 반영과 각 `ownership.status`를 보여줍니다. `unknown`은 "주인이 없음"이 아니라 근거를 읽지 못했다는 뜻입니다. `503 landings_incomplete`는 일부 행을 읽지 못했다는 뜻이며, 불완전한 짧은 목록으로 대신 응답하지 않습니다.

`clawdline landings --work-id <item id>`(`GET /v1/orchestrator/landings?work_id=<item id>`)는 이와 달리 보드 항목 하나에 **기록된 모든** 반영을 보여줍니다: `{"work_id", "landings": [...], "at"}`. 각 행에는 `id`와 `source`가 있습니다. `task`는 연결된 child의 `landed`나 `incorporated` 기록이며 ID는 작업 ID입니다. `root`는 `item phase deploying --commit`이 쓴 기록입니다. `phase_event`는 예전 데몬이 항목 이력에 남긴 사본입니다. 연결된 작업 기록을 읽지 못하면 누락하지 않고 `state: "unknown"`인 행으로 표시합니다. 항목 읽기에도 같은 `landings` 행이 있습니다. 없는 항목은 `404 work_not_found`입니다.

**두 root가 같은 체크아웃에 반영할 때는** 먼저 반영 임대를 받습니다(§11).

다른 세 가지 작업 유형에는 각각 별도 경로가 있습니다. 구분은 세부 구현이 아니라 작업의 경계입니다.

| 종류 | 경로 | 의미 |
|---|---|---|
| **인계** | `POST /v1/orchestrator/handoffs` | 기존 작업과 전체 상태를 새 세션에 넘깁니다 |
| **Root 배정** | `POST /v1/orchestrator/root-assignments` | 새 기능을 독립적으로 소유할 새 Root를 만듭니다 |
| **분리된 자동화** | `POST /v1/orchestrator/detached-tasks` | 보고 대상 없이 무인으로 실행합니다 |

**인계.** 먼저 `<state dir>/handoffs/<handoff_id>/handoff.md`를 씁니다(목록 경로가 `package_root`를 알려 줍니다). 세 가지 제목을 넣으세요. **REFERENCES**는 받는 쪽이 읽어야 할 모든 자료, **VERIFICATION**은 계속하기 전에 그 자료에서 답해야 할 질문, **OPEN THREADS**는 이어서 할 일을 담습니다. 그다음 정해진 형태의 본문으로 게시합니다.

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

받는 세션에는 파일을 읽고 참조 자료를 확인하며 검증 질문에 답한 뒤 작업을 이어가라고 전달됩니다. 인계가 시작되면 같은 프로젝트에서 보내는 세션이 담당하는 열린 보드 항목을 포착합니다. 받는 세션의 대화 ID가 생기고 첫 대화 기록이 관찰되면, 그 항목들의 활성 배정과 담당자가 하나의 트랜잭션으로 이동합니다. 인계가 실패하면 담당권은 보내는 세션에 남습니다. 이미 닫혔거나 다른 사람에게 옮겨졌거나 별도로 배정 중인 항목은 그대로 둡니다. 검증 게이트가 있는 항목이 verifying 또는 merging 단계였다면 새 담당자가 다시 검증하도록 implementing으로 돌아갑니다. 인계 줄이 입력되면 `handoff_receipt` 알림 하나를 받지만, 그 알림만으로 보드 이전이 완료되었다고 증명되지는 않습니다. 거부 코드는 `bad_task`(`handoff.md`가 없거나 비어 있는 경우 포함), `sender_not_found`, `sender_ambiguous`, `rate_limited`, `terminal_busy`입니다. 컴퓨터 coordinator 역할을 맡고 있다면 `succession_required`로 거부됩니다. 이 데몬에는 승계 기능이 없으므로(`501`) 그 세션은 인계할 수 없습니다.

**이정표 인계.** 오래 작업한 Root가 이정표에 이르면 `clawdline handoff --summary summary.md`로 넘겨 이후 작업이 매번 이전의 모든 내용을 다시 읽지 않게 할 수 있습니다. 요약에는 정확히 다섯 개의 `## ` 제목, 즉 Goal, Verified decisions, Blockers, Evidence, Next step가 있어야 합니다. Evidence에는 내용 자체가 아니라 열어 볼 경로·커밋·ID 또는 `clawdline` 명령을 넣습니다. 전체는 최대 6 KiB이며 인증 정보와 대화 내용은 없어야 합니다. `--check`는 아무것도 열지 않고 모든 문제를 나열합니다. 데몬은 같은 문제를 `bad_milestone_summary`로 거부합니다. 데몬은 옆에 `obligations.md`도 씁니다. 여기에는 새 세션으로 이동하는 보드 항목(사람이 아직 답하지 않은 결정 포함)과 여전히 원래 세션 소유인 실행 중 child·미확인 알림·미처리 반영이 들어 있습니다. 인계 후에는 남은 알림 확인과 반영을 계속하고, `clawdline session close`가 `safe`라고 응답하면 닫으세요. 인계 여부는 스스로 선택합니다. `clawdline usage --compare-handoff`는 이 컴퓨터에서 비용을 줄였는지 알려 주며 강제하지 않습니다.

**Root 배정.** `Idempotency-Key` 헤더는 `request_id`와 같아야 합니다.

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

각 assignment 필드는 1–8192바이트, 전체는 최대 32 KiB입니다. 데몬이 직접 지시서를 쓰고 세션을 엽니다. **부모·비밀·시간 제한·결과·반영 기록이 없습니다.** 누구에게도 보고하지 않으므로 완료해도 알림을 받을 사람이 없습니다. 거부 코드는 `bad_root_assignment`, `idempotency_mismatch`, `request_conflict`, `rate_limited`입니다. child·분리 작업·인계로 Root 배정을 가장하지 마세요.

**분리된 자동화.** dispatch(§4)처럼 `task_root` 아래에 `task.json`을 쓰고 `{"task_id", "secret", "inventory_generation"}`를 보냅니다. 다만 지시서의 root는 `{"session_id": null, "poll_only": true}`여야 하며, 아니면 `detached_task_required`로 거부됩니다. 아무에게도 알리지 않으므로 `GET /v1/orchestrator/tasks/<id>`를 폴링하고 `result.json`을 읽으세요. Root나 Feature 담당자가 되지는 않습니다.

### 향후 작업 예약

Clawdline Next가 예약 작업을 직접 관리합니다. 퇴역한 앱, `cron`, 분리 작업의 연쇄를 쓰지 마세요. `GET /v1/orchestrator/schedules`로 목록을, `GET /v1/orchestrator/schedules/<id>`로 개별 내용을 모두 읽습니다. 쓰기에 필요한 `place_id`는 `GET /v1/places`에서 얻습니다.

일회성 일정(`on`)은 오케스트레이터 토큰으로 바로 만들 수 있습니다. 반복 일정(`days`)은 지속적인 지시이므로 이 세션에 보낸 사람의 명시적 요청이 필요합니다.

1. `GET /v1/orchestrator/sessions/<conversation>/run`을 읽습니다. 사람이 Clawdline을 통해 이 세션에 메시지를 보낼 때 발급된 최근 run입니다.
2. 일반적인 일정 본문에 대화와 run을 포함하고 `Idempotency-Key`를 지정해 `POST /v1/orchestrator/schedules`로 보냅니다.

```json
{"title":"Morning sweep","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"Inspect the overnight failures and report actionable findings.",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

사람이 그 변경을 명시적으로 요청했다면 같은 증명으로 `PATCH /v1/orchestrator/schedules/<id>`(일정 본문 전체 전송)와 `DELETE /v1/orchestrator/schedules/<id>`(두 증명 필드를 JSON 본문으로 전송)도 승인됩니다. `POST /v1/orchestrator/schedules/<id>/run`은 즉시 한 번 실행합니다. 성공을 보고하기 전에 만든 일정 또는 바꾼 일정을 다시 읽으세요.

`via`가 없다면 오케스트레이터 토큰은 일회성 `on` 일정에만 사용할 수 있습니다. 지어낸 run, 만료된 run, 다른 세션의 run은 각각 `run_unknown`, `run_expired`, `run_other_session`으로 거부됩니다. 증명 형식이 잘못되면 `invalid_user_authorization`입니다. 사람이 터미널에 직접 입력했다면 run이 없으므로 Clawdline을 통해 지시를 다시 보내 달라고 요청하세요. 한 run을 사람의 메시지에 없던 작업까지 허가하는 포괄적 권한으로 재사용하지 마세요. 이 증명은 전달 기록을 감사할 수 있게 할 뿐, 컴퓨터 전체의 오케스트레이터 토큰을 세션별 인증 정보로 바꾸지는 않습니다.

시간이 없는 일정은 `at`, `days`, `on` 대신 `"trigger_only": true`를 보냅니다. 시계로는 실행되지 않으며, `enabled` 상태일 때 `…/run` 또는 웹훅으로만 실행됩니다. 반복 일정과 같은 지속적인 지시이므로 같은 증명이 필요합니다.

**다른 컴퓨터에서 작업을 시작하고 결과 확인.** 컴퓨터 간 직접 채널은 없습니다. 대상 컴퓨터에서 trigger-only 일정을 만들고 Cloud 웹훅을 연결한 뒤, 호출하는 쪽이 읽을 수 있는 곳에 URL을 보관합니다. URL은 인증 정보이므로 본인만 읽을 수 있는 파일이나 `CLAWDLINE_WEBHOOK_URL`에 두고 명령줄 인수에는 넣지 마세요. 호출하는 컴퓨터에서는 다음을 실행합니다.

```sh
clawdline webhook fire --url-file <path>
```

이 명령은 `{"deliver_within_seconds": 60}`을 보냅니다(`--deliver-within`으로 조정). 따라서 대상 컴퓨터가 꺼져 있다면 나중에 실행되지 않습니다. 결과가 나거나 `--timeout`(60분)이 지날 때까지 전달 상태를 따라가며, 변경 사항은 stderr에, 최종 한 줄은 stdout에 출력합니다. 종료 코드는 성공 `0`, 도착했지만 성공하지 못한 종료(실패·시간 초과·취소·시작 실패) `1`, 컴퓨터에 도달하지 않음(만료·취소·연결 불가) `2`, 거부(`dispatch_refused`와 코드, URL 사용 불가, 속도 제한) `3`, 대기를 중단했으며 마지막 상태만 알 수 있음 `4`입니다. `--no-wait`는 `202` 후 전달 ID만 출력하고 돌아옵니다. 종료 코드와 마지막 줄을 그대로 보고하세요. 2는 실행된 뒤 실패했다는 뜻이 아니라 아무것도 실행되지 않았다는 뜻입니다.

검증 대기 중인 항목(사이드바의 驗收, `docs/verifications.md`)을 위해 데이터를 읽는 예약 작업은 읽은 결과를 해당 기록의 메모로 씁니다. 작업에는 오케스트레이터 토큰을 쥐여 주지 말고 자체 작업 비밀을 사용하게 하세요.

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<readout>"}'
```

메모는 이 작업을 시작한 일정과 기록의 `schedule_id`가 같을 때만 추가됩니다. 다르면 `schedule_mismatch`입니다. 서명은 `task:<task id>`이며 일정이 시작하지 않은 작업은 `not_scheduled`로 거부됩니다. 기록 ID는 `clawdline verify list`로 찾으세요.

## 7. 완료한 자신의 턴 보고

턴의 작업이 실제로 완료되고 해당하는 경우 검증과 커밋까지 마쳤을 때, 최종 답변 직전의 마지막 행동으로 다음을 실행합니다.

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

세션 행에 **전달됨, 승인 대기 중** 표시 하나가 붙습니다. 이는 반영보다 약한 표시이며 검토를 요청하지 않습니다. 데몬이 세션을 유휴 상태로 읽을 때, 그리고 해당 터미널이 같은 대화를 유지할 때만 표시됩니다. 작업 중·대기 중·화면을 읽을 수 없는 상태가 우선합니다.

- **완료한 턴에만 사용합니다.** 부분 작업, 진단, 차단 사항, 사람에게 돌려보내는 질문에는 사용하지 않습니다. child는 보내지 않습니다(`409 child_session`).
- 명령은 `CLAUDE_CODE_SESSION_ID` 또는 `CODEX_THREAD_ID`에서 대화 ID를 찾고(없으면 `--conversation`), `GET /v1/orchestrator/whoami`에서 터미널을 확인한 뒤 `POST /v1/orchestrator/sessions/<terminal>/complete`에 `{"summary"}`를 보냅니다.
- 요약은 1–500자입니다. 호출마다 새 영수증이 생기며 가장 최근 것이 유효합니다.
- 응답에는 `open_todos`도 있습니다. 이 세션에 전달되었거나 읽혔지만 완료되지 않은 직접 할 일을 오래된 순으로 최대 20개 보여줍니다(더 많으면 `open_todos_truncated`). 명령은 영수증 뒤에 stderr로 각 ID와 내용을 한 줄씩 출력합니다. 완료한 것은 `clawdline todo done <id>`로 표시하세요. 할 일이 남아 있어도 영수증은 기록되며 종료 코드는 바뀌지 않습니다. `open_todos_unknown: true`는 읽지 못했다는 뜻이지 열린 할 일이 없다는 뜻이 아닙니다.
- 거부 코드는 `conversation_id_malformed`(소문자 UUID가 아님), `conversation_not_found`, `conversation_ambiguous`, `registry_stale`, `session_not_found`, `session_unbound`, `child_session`입니다. 거부 사실을 정확히 보고하세요. 채팅 문장만으로는 영수증이 되지 않습니다.

**사람에게 상태 보고서를 남깁니다.** git 프로젝트의 파일을 바꾼 턴이라면 사람이 작업 상태를 보고 이 턴에서 추가·수정한 모든 파일을 읽을 수 있는 페이지 하나를 제공하세요. 로컬 HTML 파일이며 업로드하지 않고 외부 콘텐츠도 불러오지 않습니다.

```sh
clawdline report --repo <project> --status status.md [--notes notes.txt] [--lang zh-TW] [--open] <commit>…
```

- **이 턴에서 만든 커밋만 오래된 순으로** 지정합니다. 각 커밋을 따로 읽으므로 사이에 있는 다른 세션의 커밋은 포함되지 않습니다. 범위는 전달하지 마세요.
- `status.md`에는 선택적인 `# Title`과 그 아래 선택적인 한 줄을 두고, 카드마다 `## ` 제목 하나를 둡니다. 제목은 ✅, 🟡, ❌ 중 하나로 시작하고 본문은 짧은 Markdown으로 적습니다.
- `--notes`는 파일 위에 표시할 `path: sentence`를 한 줄씩 받습니다. 반복 가능한 `--pin`은 파일을 앞에 놓으며 이 턴에 `CLAUDE.md`나 `AGENTS.md`를 건드렸다면 자동으로 앞에 놓습니다. `--exclude`는 경로를 제외하고 그 사실을 표시합니다. `--at`은 보여 줄 파일 내용의 리비전이며 기본값은 `HEAD`입니다.
- 보고서는 저장소 밖인 `<state dir>/reports/<date>-<id>/report.html`에 보관됩니다. 주소 두 개를 출력합니다. 첫 번째 **`file://` 주소**는 터미널에서 열 수 있고, 두 번째 **`http://127.0.0.1:<port>/reports/<id>`**는 이 컴퓨터의 데몬이 제공합니다. 최종 답변에는 둘 다 넣으세요. 콘솔은 `file://`를 열 수 없는 텍스트로, `http://`를 링크로 보여줍니다. 이 주소는 이 컴퓨터의 콘솔에 로그인한 브라우저에서만 열립니다. 휴대전화나 Cloud 열람자는 `report_not_over_cloud`, `report_local_only`로 거부됩니다.
- `--out`은 다른 파일이나 디렉터리에 쓰고 `file://` 주소만 출력합니다. stderr에는 생략하거나 줄인 내용이 표시됩니다. `--open`은 이 컴퓨터 브라우저에서 파일도 엽니다.

## 8. 다른 세션과 대화

**대상을 찾습니다.** `GET /v1/orchestrator/sessions`는 주소록입니다. 각 세션의 `id`(터미널 ID), `label`, `assistant`, `cwd`, `state`, `work_state`, 활성 child라면 `taskId`를 보여줍니다.

**메시지를 보냅니다.**

```sh
clawdline send --to <terminal id> "text"        # or text on stdin
```

이는 `Idempotency-Key`와 `{from_session, to_session, text}`를 담은 `POST /v1/orchestrator/messages`입니다. 데몬은 발신자를 표시하는 `<clawdline-message>` 봉투 안에 넣어 받는 세션의 입력창에 입력합니다.

- `to_session`은 **터미널 ID**입니다. 메시지는 대화 ID가 아니라 지정한 탭을 따라갑니다. `from_session`은 발신자의 터미널 또는 대화 ID이며 명령이 채웁니다.
- 텍스트만 가능하며 최대 100,000자입니다. `images` 필드는 없습니다. 보내더라도 조용히 무시됩니다.
- `ok`는 입력창에 바이트가 도달했다는 뜻일 뿐 누군가 읽었다는 뜻은 아닙니다.
- 전송에는 수십 초 걸릴 수 있습니다. 데몬이 입력 전 컴퓨터의 모든 세션을 읽기 때문입니다(2026-09-19 한 Mac에서 전달당 약 30초 측정). 명령은 최대 2분 기다립니다. 중간에 끊지 마세요. 입력은 되었지만 기록되지 않을 수 있고, 같은 키로 재시도하면 `409 request_in_progress`가 나올 수 있습니다.
- 명령은 먼저 `Idempotency-Key`를 출력합니다. 도중에 실패했다면 `--key <that key>`로 다시 실행하세요. 같은 키와 본문은 한 번만 입력됩니다. 같은 키에 다른 본문을 쓰면 `409 idempotency_key_reused`입니다.
- 거부 코드는 `source_not_found`, `target_not_found`, `same_session`, `target_busy`(받는 쪽에 메뉴가 열려 있어 아무것도 입력되지 않음), `terminal_busy`, `delivery_failed`입니다.

**사람에게 그림을 보여줍니다.** 로컬 경로만 붙이지 마세요. 휴대전화에서는 열 수 없습니다.

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- 오케스트레이터 토큰만 사용할 수 있습니다. 로컬 파일 1–6개이며, 각각 일반 파일이고 최대 12 MiB, 한 변 최대 12,000 px여야 합니다. PNG·JPEG·GIF는 직접 읽고 다른 형식은 macOS의 `sips`를 거칩니다.
- 응답의 `artifacts` 각각에는 `<clawdline-image id="…">` 같은 `marker`가 있습니다. **답변에 그 marker를 넣으세요.** 콘솔이 그 위치에 그림을 표시합니다. 그림은 24시간 보관됩니다.

## 9. 사람에게 알리기

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

`POST /v1/orchestrator/notify`입니다. 사람이 기다리는 일에만 사용하세요. 푸시는 드물기 때문에 가치가 있습니다. `--session <terminal>`을 지정하면 알림을 탭했을 때 해당 세션이 열립니다.

- `409 agent_notify_disabled`: 사람이 Agent 알림을 껐습니다. 잘못한 것이 아니므로 재시도하지 마세요.
- `409 not_subscribed`: 푸시 구독 기기가 없습니다.
- `429 rate_limited`: child의 알림과 공유하는 컴퓨터 전체 한도인 시간당 30개를 넘었습니다.
- `502 push_failed`: 푸시 서비스가 거부했습니다. 오류에 `sent`와 `failed`가 있습니다.

## 9a. 사람의 조치가 필요한 메모 남기기

오래 작업하는 Agent가 사람이 읽거나 행동하거나 결정해야 할 구체적인 한 가지를 전할 때, 일반 채팅 메시지가 흐름에 묻힐 수 있다면 메모를 사용합니다. 메모는 대상 세션의 접힌 주의 영역에 남고, 사람이 처리됨으로 옮길 때까지 빨간 점으로 표시됩니다. 그동안 독립적인 작업을 계속해도 됩니다. 메모는 진행 기록·개인 알림·푸시 알림·보드 결정의 승인이 아닙니다. 같은 요청의 중복 메모를 만들지 마세요.

**채팅에서 선택을 요청하기 전에** 실제 질문, 결정에 필요한 장단점, 완전한 답변 제안 2–4개가 들어간 `answer` 메모 하나를 만듭니다. 버튼을 탭하면 답안이 대화 메시지로 전송되므로 각 `draft`는 자체적으로 뜻이 분명해야 합니다. 생성 후에는 채팅에서 짧게 가리키면 됩니다. 메모를 만들었거나 사람이 처리됨으로 표시했다는 사실을 답변으로 간주하지 마세요. 버튼을 탭하거나 직접 입력해 보낸 대화 메시지를 기다린 뒤 선택에 따라 행동하세요. 메모 생성이 실패하면 그 사실을 말하고 채팅에서 직접 질문하세요. Agent가 처리할 수 있는 일상적인 선택에는 쓰지 말고 사람의 판단이 필요한 결정에 사용합니다.

JSON 본문 파일로 만듭니다. `--target`이 없으면 CLI가 `whoami`로 활성 Root의 터미널 ID를 찾습니다. 다른 세션을 대상으로 할 때는 주소록(`clawdline guide ko send`)의 활성 **터미널 ID**를 `--target`에 지정하세요. `--from`은 기본적으로 환경의 활성 Root 대화 ID입니다. CLI가 인증 정보를 명령줄에 두지 않고 읽으며 발신·대상 ID를 넣고 데몬의 영구 메모 ID를 출력합니다. 결과가 불확실하면 출력된 `--key`를 재사용하세요.

```json
{"kind":"answer","title":"Choose a date","summary":"One release date needs your choice.","action":"Choose a date when you have a moment.","reason":"Only you can choose it.","options":[{"label":"Tuesday","draft":"Tuesday works for me."},{"label":"Wednesday","draft":"Wednesday works for me."}]}
```

```sh
clawdline note create --body-file note.json
# For another Session: clawdline note create --target <terminal-id> --body-file note.json
```

`kind`는 `read`, `answer`, `action`, `report` 중 하나이며 `title`, `summary`, `action`, `reason`은 필수입니다. `answer`에는 선택지 2–4개를 넣을 수 있습니다. 각 `draft`는 버튼에 표시되는 제안 답변입니다. 사람이 탭하면 콘솔이 곧바로 메모 대상 세션에 대화 메시지로 보내고, 이어 메모 ID·제목·행동이 적힌 맥락 줄을 보냅니다. 받는 세션은 어느 요청에 대한 답인지 알 수 있지만, 맥락 줄은 버튼에 보이지 않습니다. 전송이 성공한 뒤에만 메모가 최근 처리됨으로 이동합니다. 실패하면 메모는 대기 상태로 남고 주의 영역에 답변이 전송되지 않았다고 표시됩니다. `detail`에는 더 긴 내용을 넣을 수 있습니다. `document_url`은 실제로 읽을 수 있는 Cloud 문서를 가리킬 수 있으며, 게시 전에 문서 경로와 파일을 확인해야 합니다. 메모 상태가 아니라 도착한 대화 메시지에 따라 행동하세요. 사람이 수동으로 처리됨 표시만 한 메모는 아무것도 보내지 않았습니다. 답변이 실제 차단 사항이라면 사용자 대기 상태를 기록하고 기존 주의 알림을 한 번 보내세요. 메모가 보인다는 것만으로 푸시가 전송되거나 Agent가 깨어나지는 않습니다.

## 10. 보드

보드에는 보드 항목, Backlog, 각 세션의 자체 할 일 목록이라는 세 구조가 있으며, **무엇을 올릴지는 사람이 결정합니다.** 세션은 Clawdline을 통해 보내진 사람 자신의 메시지가 명시적으로 지시할 때만 보드 항목을 만듭니다. 그 외에는 제안합니다. 스스로 카드를 만들지 않습니다. 예외는 Epic 담당자입니다. 검토된 Epic 계획을 마친 뒤 Epic을 Feature와 Issue 항목으로 나누고 세션에 배정할 수 있습니다(`clawdline guide ko epic`).

**보드 항목과 함께 말한 TODO / 待辦 / 土度는 그 항목의 단계입니다.** `--step`으로 항목에 넣으세요. `clawdline todo add`로 중복 기록하지 마세요. 그 명령은 보드 항목 없이 이 세션만의 할 일로 추적해 달라고 사람이 요청한 목록에만 씁니다.

**사람이 보드 항목을 만들라고 했을 때.** Clawdline을 통해 보내져 run이 있는 메시지가 명시적으로 요청한 경우에만 직접 만듭니다.

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin] [--assign-self]
```

예를 들어 사람이 *"릴리스 노트를 정리할 보드 항목을 만들어. TODO: 초안 작성, 링크 확인, 게시"*라고 썼다면 다음 명령 하나만 실행합니다.

```
echo "Clean up the release notes before the next release." | \
  clawdline item add --project <place id> --kind feature --title "Clean up the release notes" \
  --step "Draft the notes" --step "Check the links" --step "Publish"
```

- `item add`는 `--run`으로 지정하지 않았다면 이 대화의 최신 run(`GET /v1/orchestrator/sessions/<conversation>/run`)을 읽습니다. 요청 전에 Idempotency-Key를 출력하며 `--key`로 같은 쓰기를 재시도할 수 있습니다. 만든 항목과 각 단계 ID를 출력합니다. 요청 경로는 `POST /v1/work/v2/agent/items`, 본문은 `{"session_id", "via": {"run"}, "project_id", "kind", "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`입니다. 응답은 `201`과 `{"item", "assigned", "assignment_state"}`입니다.
- Feature·Issue·Epic은 기본적으로 사람의 보드에서 **미배정** 상태로 만들어집니다. 단계는 `--step`에 준 순서대로, 없으면 설명의 최상위 Markdown 목록이 두 줄 이상일 때 거기서 가져옵니다. 사람은 종종 나중을 위한 작업 기록을 요청합니다. 만들었다고 해서 이 세션의 일이 되는 것은 아닙니다. `assignment_state`는 `not_requested`입니다.
- 사람이 **이 세션이 지금 작업하라고 요청한 경우에만** `--assign-self`(`"assign": {"mode": "self"}`)를 추가합니다. 같은 쓰기에서 항목은 이 세션에 배정되어 `assigned`가 됩니다. 직접 요청했으므로 터미널에 별도 입력은 없습니다. 단계별로 작업하고 각 단계가 검증되면 완료하세요(`clawdline item steps <item id>`, `clawdline item step-done <item id> <step id>`). 작업 중 필요한 단계가 더 생기면 `clawdline item step-add`를 사용합니다. 다른 배정 항목처럼 `clawdline item phase`로 단계를 진행합니다. 이렇게 맡은 Epic은 구현 전에 아래 Epic 절차를 따릅니다. 나중에 미배정으로 만든 항목을 맡으라는 요청이 오면 아래의 `clawdline item claim`을 사용합니다. Refactor는 외부 동작을 바꾸지 않고 내부 구조를 바꾸는 실행 가능한 작업입니다. 담당자가 있고 단계, Feature의 진행 단계·게이트·검토 스위치를 따릅니다. Plan은 `--assign-self`가 있든 없든 미배정 Planning 상태로 만들어지고 단계가 없습니다(`planning_has_no_steps`).
- 등록된 Clawdfather는 실행 가능한 프로젝트 작업의 예외입니다. 프로젝트 코드를 소유하거나 편집하지 않습니다. 사람이 새 항목을 명시적으로 요청했다면 `clawdline item add --project <place id> --kind feature --title "…" --assign-new` 또는 `--assign-terminal <id>`로 항목을 먼저 만들고 프로젝트 세션에 위임할 수 있습니다. `assignment_state`는 프로젝트 담당자가 기록된 `assigned`, 새 세션의 첫 대화가 필요한 `awaiting_user`, 배정 실패인 `failed`와 `assignment_error`, 배정을 요청하지 않은 `not_requested` 중 하나를 알려 줍니다. 마지막 두 경우에는 사람이 보드에서 배정할 수 있습니다. 중단된 위임을 재생했을 때 `pending`과 함께 원래 항목이 표시되면 다른 배정을 시도하기 전에 보드를 확인하세요. 명시적인 메시지가 없다면 제안하고 수락을 기다립니다.
- 카드에는 사람의 문구를 인용하며 "HH:MM에 보낸 메시지를 바탕으로 세션이 생성"되었다고 표시됩니다.
- 아무것도 쓰지 않는 거부 코드: `run_unknown`(run을 지정하지 않았거나 발급되지 않음), `run_expired`(하루 넘음), `run_other_session`(다른 세션으로 보낸 메시지), `session_not_found`, `child_session`(child는 `result.json`으로 보고), `project_not_found`, `project_mismatch`(일반적인 실행 항목은 이 세션의 프로젝트에 있어야 함), `coordinator_required`(활성 역할이 없는 컴퓨터 세션), `machine_delegation_required`(일반 세션이 컴퓨터 전용 복합 위임을 요청), `invalid_assignment`(잘못된 `assign` 또는 Clawdfather의 `self` 요청), `too_many_steps`(128개 초과), `run_items_exhausted`(메시지 하나로 최대 다섯 항목).
- **run이 없으면** 사람이 터미널에 직접 입력한 것입니다. `item add`가 `no_run` 또는 `run_unknown`으로 응답하면 아래의 제안으로 전환하고 보드의 Agent proposals에서 수락해 달라고 말하세요.

스스로 보드 항목을 만들거나 추측한 작업을 계획하려고 여러 개 만들지 마세요.

**사람이 가리킨 보드 항목을 맡습니다.** Clawdline을 통한 메시지에서 이미 있는 특정 항목을 맡으라고 했다면, 예를 들어 *"릴리스 노트 항목을 맡아"*, *"claim <item id>"*라면 명령 하나로 맡습니다.

```
clawdline item claim <item id>
```

- `item claim`은 `--run`을 지정하지 않았다면 이 대화의 최신 run과 항목 버전을 읽고, 요청 전에 Idempotency-Key를 출력합니다(`--key`로 같은 쓰기 재시도). 끝나면 항목을 출력합니다. `POST /v1/work/v2/agent/items/<id>/claim`에 `{"expected_version", "session_id", "via": {"run"}}`를 보내며, 메시지가 보내진 **현재 세션 자신**에게 배정합니다. 다른 세션이나 터미널은 지정하지 않습니다.
- 사람이 보드에서 직접 배정한 것과 동일하게 항목의 담당자가 이 세션이 되고 `assigned`로 이동합니다. 단계가 없고 설명에 목록이 있다면 이를 단계로 채웁니다. 터미널에는 별도 입력이 없습니다. 아래의 일반 배정 항목처럼 작업하세요.
- 카드에는 사람의 문구를 인용하며 "HH:MM의 메시지를 바탕으로 세션이 맡음"이라고 표시됩니다.
- 아무것도 쓰지 않는 거부 코드: `item add`와 같은 `run_unknown`, `run_expired`, `run_other_session`, `session_not_found`, `child_session`; 그리고 `work_not_found`, `project_mismatch`(다른 프로젝트), `item_assigned`(이미 세션 담당자가 있거나 새 세션을 여는 중이며 세션 간 이동은 사람만 할 수 있음), `item_terminal`(완료 또는 취소), `planning_not_assignable`(Plan은 Planning에 남지만 Epic·Refactor는 맡을 수 있음), `version_conflict`(변경되었으므로 재실행), `run_claims_exhausted`(메시지 하나당 최대 다섯 번).
- **run이 없어서** `no_run` 또는 `run_unknown`이 나오면 사람이 배정하도록 항목을 그대로 두세요.

**사람이 요청한 새 세션에 항목을 배정합니다.** Clawdline 메시지에서 특정 미배정 Feature나 Issue를 새 세션에 넘기라고 요청했다면, 예를 들어 *"<item id>의 보안 세션을 열어"*라면 다음 명령 하나를 사용합니다.

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- Epic의 하위 항목이 아니라면 `item assign`은 `item claim`처럼 `--run`이 없을 때 이 대화의 최신 run을 읽습니다. 요청은 `POST /v1/work/v2/agent/items/<id>/assign`에 `{"expected_version", "session_id", "mode": "new_session", "assistant"?, "model"?, "persona"?, "via": {"run"}}`를 보내며, 사람이 "New Session"을 고를 때와 같은 새 세션을 엽니다.
- 카드에는 사람의 말을 인용한 "HH:MM의 메시지를 바탕으로 세션이 배정" 표시와 새 세션의 persona가 보입니다.
- 아무것도 쓰지 않는 거부 코드는 `item claim`과 같은 `run_unknown`, `run_expired`, `run_other_session`, `session_not_found`, `child_session`, `project_mismatch`, `item_assigned`, `item_terminal`, `version_conflict`, `run_claims_exhausted`입니다. `item claim`과 `item assign`은 한 메시지의 다섯 번 한도를 공유합니다. 그 밖에 `kind_person_assigns`(Feature나 Issue만 가능), `new_session_only`(직접 맡으려면 claim 사용), `unknown_persona`, `persona_disabled_for_auto_assignment`(대상 프로젝트에서 자동 배정 역할 꺼짐)가 있습니다.

사람의 메시지가 가리킨 항목 외에는 자발적으로 맡거나 배정하지 마세요. 사람 전용 경로 `POST /v1/work/v2/items/<id>/assign`도 사용하지 마세요. 세션은 `session_cannot_create_item`으로 거부됩니다. Epic 담당자는 `clawdline item assign`으로 자신의 하위 항목도 배정합니다(`clawdline guide ko epic`).

**보드 항목을 위해 열린 새 세션의 이름을 정합니다.** 목표와 범위를 읽은 뒤 실제 작업을 설명하는 짧은 이름으로 `clawdline item name <item id> "<task name>"`을 실행하세요. 보드 제목을 바꾸거나 다른 모델 턴을 시작하지 않고 세션 이름을 한 번 바꿉니다. 활성 새 세션 담당자만 실행할 수 있습니다. 같은 이름을 재전송하면 안전하고 다른 이름은 거부됩니다. 사람은 여전히 세션 제목을 수동으로 정할 수 있습니다. 기존 세션에 준 항목은 세션 이름을 바꾸지 않습니다.

**보드 항목을 제안합니다.** 보드의 **Agent proposals** 대기열에는 다음 경로 하나로 넣습니다.

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id`는 `GET /v1/places`의 행에 있는 `id`입니다.
- 출처 하나가 필수이며 이 세션의 것이어야 합니다. 이 세션이 맡은 보드 항목 또는 자체 할 일입니다(`proposal_source_required`, `proposal_source_invalid`). 사람이 요청한 일에서 나온 제안은 해당 할 일을 출처로 인용합니다. 즉, 사람이 요청하면 아래의 `clawdline todo add`로 기록한 뒤 그 ID로 제안합니다.
- 사람이 곧바로 이해할 수 있는 말로 작성하세요. `title`은 눈에 띄는 결과, `description`은 바뀌는 내용, `reason`은 지금 할 가치가 있는 이유, `suggested_acceptance`는 완료 후 관찰할 수 있는 것을 말합니다. 네 필드 모두 필수입니다. 설명의 중심을 뜻을 모를 약어·내부 ID·코드 경로·구현 용어로 채우지 마세요. 보드에는 먼저 제목·출처·이유가 보이고 **Explain / 詳細說明**을 열면 변경 내용과 사람이 볼 결과가 보입니다.
- **단계가 있는 항목을 제안하려면** `description`에 최상위 Markdown 목록을 두 줄 이상 씁니다. 사람이 수락하고 배정하면 각 줄이 `steps`가 됩니다.
- `201`은 대기 중인 제안을 뜻합니다. 사람은 보드의 Agent proposals에서 수락·수정·거절합니다. 그 전에는 보드 항목이 되지 않습니다. 거부 코드는 `invalid_proposal`, `proposal_too_large`, `project_not_found`, `proposals_full`입니다.

**이전 제안 경로.** `POST /v1/orchestrator/proposals`와 대화에서 물은 뒤 사용하는 `…/<id>/asked`도 제공됩니다. child는 작업 비밀과 `task_id`로 남은 일을 여기 기록하고, root의 작업 제안도 여기서 지금 물을지 보류할지의 `instructions`를 얻습니다. 이 경로의 행은 구형 "확인 필요" 영역에 나타나며 **v2 보드의 Agent proposals에는 나타나지 않습니다.** 따라서 보드에서 사람에게 항목을 제안하는 경로가 아닙니다.

**사람에게 결정을 묻습니다.**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "work_id": "<the Board item this is about>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

항목은 열려 있고 이 세션 담당이어야 합니다. 보드가 항목 카드를 질문의 맥락으로 사용하기 때문입니다(`decision_source_required`, `decision_source_not_found`, `decision_source_invalid`, `decision_source_closed`). 선택지는 2–4개이며 `default`는 그중 하나여야 합니다. 아무도 답하지 않을 때 적용되며, 기한은 `due_in_minutes`로 60–10080분을 지정하지 않으면 7일입니다. `blocking` 결정만 푸시됩니다. 답변은 `GET /v1/orchestrator/decisions/<id>`로 읽습니다.

**사람이 답하고 세션은 그 말을 전달할 뿐입니다.** 제안·결정·보드 항목의 답변은 `/v1/work/…` 아래에서 처리됩니다. 세션이 그곳에 쓰려면 사람의 말을 실어 온 run을 `"via": {"run": "<id>"}`로 지정해야 하며, 없으면 `403 session_cannot_decide`로 거부됩니다. 최신 run은 `GET /v1/orchestrator/sessions/<conversation>/run`에서 읽습니다. 지어낸 run, 만료된 run, 다른 세션의 run, 질문보다 오래된 run은 각각 이름을 붙여 거부됩니다. 사람이 터미널에 직접 입력했다면 run이 없으므로 Clawdline이나 콘솔을 통해 답하도록 요청하세요.

**아직 수거할 child.** `GET /v1/orchestrator/sessions/<conversation id>/todos`는 터미널 ID가 아닌 대화 ID를 받습니다(틀리면 `409 session_id_is_terminal`). 브로커가 작업 사실에서 열고 닫으므로 따로 쓸 것은 없습니다.

매 턴 경계에서 유휴 상태라고 선언하기 전에 `GET /v1/work/v2/agent/session-todos/<conversation id>`도 읽으세요. `assigned_items`는 사람이 이 세션에 배정한 보드 항목, `recent_items`는 최근 완료한 항목, `direct_todos`는 간단한 요청, `unacknowledged_completions`는 아직 ACK하지 않은 완료 child입니다(§5). 이 조회 덕분에 작업 도중 들어온 배정이 현재 턴을 중단시키지 않고 대기합니다. 현재 턴을 마친 다음 배정 항목을 다음 담당 작업으로 받고 `clawdline item show <id>`로 문서 본문까지 포함한 전체 기록을 읽으세요.

**사람이 보낸 할 일.** 마지막 줄이 `(Clawdline to-do <id>. When it is done: clawdline todo done <id>)`인 메시지는 사람이 Clawdline에서 이 세션으로 보낸 `direct_todos` 중 하나입니다. 그 위의 문구가 요청입니다. 작업을 끝내고 검증했다면 턴 보고 **전에** 해당 ID로 `clawdline todo done <id>`를 실행하세요. 그렇지 않으면 작업은 끝나도 사람 목록에는 열린 행으로 남습니다. 끝내지 못한 것은 열린 채로 둡니다. `clawdline session report`는 이 세션에 보냈지만 아직 완료 표시하지 않은 모든 할 일을 stderr에 나열합니다(§7).

**사람이 요청한 경우 세션 자체의 할 일.** 사람이 이 세션의 작업을 Clawdline 할 일로 기록하라고 명시적으로 요청했거나 여러 항목의 목록을 주며 거기서 추적하라고 했을 때만 자체 목록에 씁니다.

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add`는 `POST /v1/work/v2/agent/session-todos/<conversation id>`에 `{"todos": [{"text": "…"}, …]}`를 보내며 먼저 출력한 Idempotency-Key로 `--key` 재시도를 지원합니다. 호출 하나에는 96 KiB 본문 안에 각각 최대 8 KiB인 행 1–20개를 담고, 모두 추가하거나 하나도 추가하지 않습니다. 이 세션의 열린 할 일이 500개를 넘게 되는 목록은 전부 `direct_todos_full`로 거부됩니다. 응답은 입력 순서의 행을 담은 `201`입니다. 대화는 이 데몬이 아는 활성 세션이어야 합니다(`conversation_id_malformed`, `session_not_found`). Clawdline child는 `child_session`으로 거부되며 `result.json`으로 계속 보고합니다.

자발적으로 목록을 만들거나 추측한 일을 계획하려고 사용하지 마세요. 각 행은 검증된 뒤에만 `clawdline todo done <id>`로 완료합니다. 사람에게는 세션이 추가한 행으로 보이며 사람만 전송하거나 삭제할 수 있습니다. 이는 보드 항목이 아니며 보드에 표시되지 않습니다. 할 일은 현재 세션의 작업 목록이고 보드 항목은 사람이 보드에서 추적하려는 일입니다. 보드 추적을 요청했다면 위의 `clawdline item add`를 쓰고 목록은 항목의 `--step`에만 넣으세요.

사람의 말에서 방금 완료한 항목이 여전히 미완료임이 분명하다면 직접 보드를 바로잡으세요. Recently Done에 그대로 두거나 대체 항목을 만들거나 사람에게 다시 열어 달라고 요청하지 마세요. 현재 버전을 다시 읽고 다음을 사용합니다.

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "The concrete behavior or acceptance claim that remains unfinished"}
```

방금 완료한 항목이라는 참조가 명확할 때만 사용하세요. 이 경로는 같은 세션이 최종 배정에서 해제된 `done` 항목만 받습니다. 사람의 취소를 되돌리거나 다른 세션의 완료를 가져올 수 없습니다. 이전 근거를 보존하고 `implementing`에서 새 주기를 시작하며 이 세션을 담당자로 복원하고 이유를 변경 불가능한 항목 이력에 기록합니다. 이유는 최대 8 KiB입니다. 모호한 후속 메시지는 보드 항목을 변경할 권한이 아닙니다.

담당 Agent가 사람의 행동이나 선택을 기다려야 한다면 결정을 열고 답변을 기다립니다. 먼저 이 항목에 대해 `POST /v1/orchestrator/decisions`로 결정을 만드세요. 이 항목의 `work_id`, 2–4개 선택지, `default`, 기한을 넣습니다. 행동 요청이라면 `{"id": "done", "label": "I've done it"}`와 `{"id": "cannot", "label": "I can't"}` 같은 선택지를 둘 수 있습니다. 그다음 컴퓨터 인증 경로에서 항목에 결정을 연결합니다.

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

결정은 존재해야 하고(`decision_not_found`), 이 세션의 것이어야 하며(`decision_other_session`), 이 항목에 관한 것이어야 하고(`decision_other_item`), 아직 열려 있어야 합니다(`decision_not_open`). 다른 `condition`에 `decision_id`를 넣으면 `decision_requires_waiting_user`입니다. 결정 없이 `waiting_user`를 지정하면 `waiting_user_requires_decision`으로 거부됩니다. 사람은 보드 카드나 "Waiting on you"에서 답합니다. 답변이 들어오거나 기한에 기본 선택지가 적용되면 데몬이 같은 쓰기에서 항목의 `waiting_user`를 지우고 답변을 기록하며, 세션이 유휴 상태일 때 선택된 ID와 라벨을 입력합니다. 직접 대기를 중지하려면 같은 경로에서 `condition`을 빈 문자열 또는 다른 조건으로 바꿉니다. 그러면 결정은 `withdrawn`이 되어 "Waiting on you"에서 사라집니다. 항목이 해제·재배정·취소·완료되어도 동일합니다. 철회된 결정의 답변은 `decision_withdrawn`으로 거부됩니다.

**기록된 계획 및 검증 게이트.** `planning_gate`는 기본적으로 켜지고 `verify_gate`는 꺼집니다. `clawdline setting get|set planning_gate|verify_gate`는 `on/off` 또는 `true/false`를 받습니다. 실행 주기의 첫 성공적인 배정 시 두 값을 고정합니다. 재배정이나 이후의 전역 설정 변경은 그 주기에 영향을 주지 않습니다. 계획 게이트가 켜진 Epic 또는 Feature는 구현 전 인수 기준이 필요합니다. Epic에는 계획과 독립 검토도 필요합니다. Feature는 사람이 Needs independent review 스위치를 켰을 때만 이 두 가지가 필요합니다(아래). Issue에는 계획 게이트가 없습니다. 계획 설정이 꺼져 있으면 Epic의 강제 계획도 생략됩니다. 두 게이트가 켜지면 계획 뒤에 독립 검증을 합니다. 계획만 켜지면 일반적인 병합 검증이 유지됩니다. 검증만 켜지면 계획은 생략하되 정확한 후보를 검증합니다. 둘 다 꺼지면 일반 생명주기를 따릅니다. 사람이 보드에서 인수 기준을 직접 채울 필요는 없습니다. 게이트가 있는 항목에 기준이 없다면 배정 후 해당 전환 전 `clawdline item acceptance <item id> --body-file <file>`로 관찰 가능한 기준을 적으세요. 담당 세션은 빈 계약을 한 번 채울 수 있습니다. 사람이 Clawdline을 통해 이 담당 Root에게 항목의 인수 기준 개정을 명시적으로 지시하면, 전체 교체 Markdown을 파일에 쓰고 `clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>`을 실행합니다. 항목 버전은 `clawdline item show <id>`의 `item version N` 줄에서 읽습니다(`item steps`는 인수 기준 버전만 출력합니다). 메시지 run은 `GET /v1/orchestrator/sessions/<conversation>/run`에서 읽습니다. 보존된 메시지 발췌에 인수 기준 변경 요청이 명시되어 있어야 합니다. 금지·논의·단순 질문은 승인이 아닙니다. 이 Root가 열린 항목 하나만 맡고 있다면 대화 맥락으로 항목을 가리킬 수 있지만, 그렇지 않다면 ID나 제목으로 특정해야 합니다. run은 현재 인수 기준 버전보다 새로워야 합니다. 명시적인 거부는 아무것도 바꾸지 않습니다. 응답이 불확실하면 같은 `--key`, `--run`, `--expected-version`, 파일 바이트로 재시도하세요. 사람도 직접 편집할 수 있습니다. 병합 전 기준을 바꾸면 기존 PASS와 override는 무효가 되고, 병합이 시작되면 기준은 잠깁니다.

기록된 검증 게이트가 켜져 있다면 커밋된 후보가 있는 깨끗한 등록 worktree에서 `clawdline item phase <id> verifying`를 실행하세요. CLI가 현재 브랜치와 전체 HEAD를 보내고 데몬은 프로젝트·주기 기반·트리·인수 기준 digest를 검사합니다. 분리된 읽기 전용 Codex 검사자는 Issue에는 `code-reviewer`, Epic에는 `reality-checker`, 참조 사진이나 디자인 문서가 있는 Feature에는 `evidence-collector`(없으면 `reality-checker`)를 사용합니다. 판정 유형은 `PASS`, `FAIL`, `NEEDS_WORK`입니다. 검증되지 않은 문장은 이유를 설명하며 병합을 허가하지 않습니다. 결과가 없거나 형식이 잘못되면 기술적 실패로 처리하고 제한된 재시도 한 번 뒤 상위로 올립니다. Epic의 최종 전체 흐름 검증은 모든 하위 항목이 종료되고 영향을 받은 구성요소가 실행 가능한 후보 하나로 통합된 뒤에 합니다. 먼저 집중적인 하위 테스트와 통합 smoke check를 하세요. 모의 UI·분리된 브랜치·미완성 API를 상대로 최종 브라우저 또는 다중 계정 전체 흐름 검증을 배정하지 마세요. 배정 전에 선택한 작업 환경이 권한 있는 브라우저나 동등한 로컬 자동화로 대상 URL을 실제로 열 수 있고, 필요한 테스트 계정·fixture·origin 권한을 갖췄음을 입증하세요. 지시서에 해당 접근 경로를 적으세요. `--permission-mode full`만으로 브라우저 접근이 생기지는 않습니다. 도구 사전 점검이 실패하면 같은 차단 사항에 다른 검사자를 보내기 전에 해결하세요. 실패한 사전 점검은 전체 흐름 검증 시도가 아닙니다. Epic당 포괄적인 전체 흐름 검증 한 차례를 계획하고, 하위 항목이나 수정마다 반복하지 마세요. 결함을 고친 뒤에는 영향받은 시나리오만 다시 실행합니다. 인수 범위나 통합 경계가 실질적으로 바뀔 때만 전체 검증을 반복하고 이유를 기록합니다. `verifying → merging`에는 정확한 후보와 기준에 대한 유효한 PASS 또는 이유를 명시한 override가 필요합니다. 검증했다는 문장만으로 허가할 수 없습니다. 연속 세 번 FAIL이면 활성 상위 Epic 담당자에게 올리고, 그 담당자가 없으면 사람에게 올립니다. 기술 실패는 별도로 올립니다. 지정된 상위 담당자만 `POST /v1/work/v2/agent/items/<id>/gate-decision`을 사용합니다. 사람은 `POST /v1/work/v2/items/<id>/gate-decision`을 사용합니다. Agent가 사람 경로를 사용하지 마세요. 보드는 AI·사람·기술 override를 검사자의 PASS와 구분해 표시합니다. 상세 기록 보관 한도에 이르면 사람이 먼저 `GET /v1/work/v2/items/<id>/gate-export`를 다운로드하고 manifest digest를 검증한 다음, 그 digest와 항목 버전으로 `POST /v1/work/v2/items/<id>/gate-purge`를 확인합니다. 삭제되는 것은 자격을 갖춘 종료 상세 기록뿐이며 집계·최신 사실·감사 기록은 남습니다.

**진행 단계 변경.** 담당 세션이 자신의 항목을 실행 단계에 따라 직접 옮깁니다. 다른 주체나 턴 영수증, 조건 해제는 단계를 옮기지 않습니다. `…/edit`의 필드도 아닙니다(`phase_not_editable`). 각 단계가 나타내는 작업이 실제로 이뤄졌을 때 전환하세요.

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

명령은 항목 버전을 읽고 Idempotency-Key를 먼저 출력하며(`--key`는 같은 쓰기를 재시도), 항목을 출력합니다. 호출하는 경로는 다음과 같습니다.

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "no_landing_reason"?: "…", "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- 순서는 한 단계씩 `assigned → implementing → verifying → merging → deploying → done`입니다. `verifying`에서 `implementing`으로 돌아갈 수 있고, `merging`에서 `implementing`이나 `verifying`로 돌아갈 수 있습니다. 기록된 검증 게이트가 꺼진 항목은 반영 증거가 있으면 `implementing`에서 바로 `deploying`으로 갈 수도 있고, 이때 `verification`은 선택 사항입니다. 게이트가 켜진 항목은 `verification_gate_on`으로 이 건너뛰기를 거부하며 전체 순서를 따릅니다. 다른 단계는 건너뛸 수 없고 `done`은 `deploying`에서만 도달합니다.
- `merging`에는 `verification`이 필요합니다. `deploying`에는 이 항목에 연결된 브로커 child의 반영 또는 `landing`이 필요합니다. 후자의 커밋은 데몬이 프로젝트의 로컬 `target` 브랜치와 `refs/remotes/<remote>/<target>` 양쪽에서 찾아야 하므로 먼저 푸시하세요. 다른 저장소에 반영된 작업이라면(백엔드 항목의 변경이 프런트엔드 커밋에 실린 경우) `landing.project`(`--landing-project`)에 `GET /v1/places`에서 얻은 그 프로젝트 ID를 지정합니다. 커밋도 그곳에서 찾습니다. 프로젝트 디렉터리 안의 중첩 저장소(`cloud/`)는 별도 프로젝트입니다. 증거는 브로커의 **Root 반영 기록**으로 한 번 기록되며 같은 항목·저장소·커밋·대상을 다시 적으면 동일한 기록입니다. 항목 이력의 `landing_id`와 `clawdline landings --work-id <item id>`에서 확인할 수 있습니다. `deploying` 직전 데몬이 연결 child의 브랜치가 병합됐는지 Git에 바로 묻기 때문에 몇 초 전 병합도 다음 브로커 조회를 기다리지 않고 인정됩니다. 코드가 없는 작업은 반영 대신 `no_landing_reason`(`--no-landing-reason`)을 사용합니다. `landing`과 함께 쓰면 `invalid_landing_evidence`, 연결 child의 반영이 아직 남아 있으면 작업 ID를 포함한 `landing_owed`, 이미 반영한 child가 있으면 `landing_recorded`로 거부됩니다. `done`에는 `deployment` 또는 `no_deployment_reason`이 필요합니다. 항목의 `deployment_policy`에서 `required`는 `deployment`만, `not_required`는 `no_deployment_reason`만, `agent_decides`는 둘 중 하나를 허용합니다. 먼저 모든 체크리스트 단계를 완료해야 합니다.
- `done`은 담당 배정을 해제하고 세션의 Recently Done 행으로 옮깁니다. 필요한 완료 보고서는 그 전에 추가하세요.
- 거부 코드는 `invalid_transition`(다음 단계가 아니거나 증거 부족), `steps_incomplete`, `not_item_owner`, `item_unassigned`, `item_terminal`(사람이 다시 열어야 함), `evidence_unknown`, `direct_landing_not_applicable`, `invalid_landing_evidence`, `landing_project_not_found`, `landing_commit_unresolved`, `landing_target_unresolved`, `landing_not_on_target`, `landing_remote_unresolved`, `landing_not_published`, `landing_owed`, `landing_recorded`, `landings_full`(항목당 Root 반영 64개), `version_conflict`입니다. 버전 충돌이면 다시 읽고 보내세요. 반영 부족 거부에는 브로커가 방금 확인한 상태(미병합 브랜치, 읽을 수 없는 저장소 등)가 끝에 나옵니다.

**명령 하나로 완료하기.** 작업이 반영된 뒤 `clawdline item finish <item id>`는 `implementing`, `verifying`, `merging`, `deploying` 중 현재 단계에서 `done`까지 하나의 트랜잭션으로 진행합니다.

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?}, "no_landing_reason"?: "…",
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- 각 단계는 `item phase`와 동일한 게이트를 지나며 자체 `item.phase_changed`를 기록합니다. 하나라도 거부되면 전체가 거부되고 아무것도 기록되지 않습니다.
- 반영 정보는 새로 입력하지 않고 읽어 옵니다. 게이트 항목의 커밋은 허가된 후보이고, 그 외에는 연결 child가 반영한 커밋입니다. 대상은 반영이 가리키는 브랜치이고 원격은 그 브랜치가 추적하는 곳입니다. 직접 지정한 필드가 우선하며 `item phase deploying`과 같이 Git으로 증명합니다. 기록된 검증 게이트가 있다면 여전히 PASS가 필요합니다. 게이트 항목을 `implementing`에서 곧바로 완료하려 하면 `verification_candidate_required`로 거부됩니다. 후보 worktree에서 `item phase`로 `verifying`에 들어가 PASS를 기다린 뒤 완료하세요.
- 방금 병합한 연결 child의 반영은 완료 명령 자체가 기록합니다. 데몬이 반영 기록을 읽기 전에 Git을 확인하므로 병합 직후 `landing_required`가 나온다면 브랜치가 대상에 올라 있지 않은 것입니다. 거부 응답이 브로커가 확인한 상태를 알려 줍니다.
- 코드가 없는 작업은 `item phase`와 같은 규칙으로 `clawdline item finish <item id> --no-landing-reason "…" --no-deployment-reason "…"`를 사용합니다.
- 이미 `done`인 항목은 현 상태로 응답하고 아무것도 쓰지 않으므로 같은 반영을 두 번 봐도 다시 이동하지 않습니다.
- 추가 거부 코드: `verification_required`, `landing_required`, `deployment_required`(해당 단계의 필수 정보 부족), `landing_target_unknown`, `landing_remote_unknown`, `landing_remote_unreadable`, `landing_ambiguous`(플래그로 특정), `landing_owed`, `landing_recorded`, `finish_not_started`(아직 `implementing` 전).

배정된 항목에는 `steps`가 있을 수 있습니다. 성공적인 배정에서 설명의 최상위 Markdown 목록이 두 줄 이상이면 단계가 만들어질 수 있고, `clawdline item add`로 만든 항목에는 `--step` 행이 있습니다. 각 단계는 해당 항목의 체크리스트이지 다른 보드 항목이 아닙니다. 검증한 단계는 `clawdline item step-done <item id> <step id>`로 완료합니다. 이 명령은 `{"session_id"}`를 담아 `POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete`를 보냅니다. Agent 경로에서 `expected_version`은 선택 사항입니다. 생략하면 현재 버전에 쓰고, 지정하면(`--expected-version`) 비교하여 오래된 버전에는 `version_conflict`로 응답합니다. 열린 단계가 있으면 `done` 전환은 `steps_incomplete`로 거부됩니다. 부모 항목의 단계가 진행되었다는 이유만으로 Clawdline이 체크리스트를 자동 완료하지 않습니다.

**자신이 맡은 항목을 단계로 나누기.** 단계가 없는 항목을 맡았고 작업이 여러 변경이나 시스템 부분으로 이루어져 따로 검증할 수 있다면 구현 전에 순서 있는 구체적인 단계 2–8개로 나누세요. 간단한 단일 변경에는 단계가 **없어야** 합니다. 목록을 채우려고 하나만 만들지 마세요. 작업이 예상보다 커졌다면 그때 단계를 추가합니다.

```
clawdline item step-add <item id> "Wire the route" "Cover it with a test" "Say it in the guide"
```

제목은 인수로 주거나 표준 입력의 비어 있지 않은 줄마다 하나씩 줍니다. 명령은 각 제목 전에 항목을 다시 읽고, 각 쓰기 전에 Idempotency-Key를 출력하며, `item show` 안내를 담은 짧은 영수증을 출력합니다. 제목 하나당 담당자 전용 요청 하나입니다.

```
POST /v1/work/v2/agent/items/<id>/steps     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

단계는 위치순이므로 `"position"`은 마지막 단계의 다음 위치입니다. 검증한 뒤 `clawdline item step-done`으로 각각 완료합니다. 보드 항목과 할 일에 금지된 자발적 생성에 해당하지 않습니다. 항목은 이미 이 세션의 것이고, 단계는 맡은 일의 진행 과정을 사람에게 보여줍니다.

Issue나 사고의 근본 원인을 찾거나 그럴듯한 대안과 실제 수정을 구분하는 데 상당한 조사가 필요했다면 `done` 전에 사람이 읽을 수 있는 완료 보고서를 추가하세요. 직접 관찰할 수 있는 단순 수정에는 필요하지 않습니다. 발생 상황·근본 원인·변경 사항·검증 방식·남은 범위를 파일에 적고 다음을 실행합니다.

```
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

명령은 `POST /v1/work/v2/agent/items/<id>/documents`를 대신 보냅니다(필드는 §10의 Epic 부분, `clawdline guide ko epic`). 명령이 읽는 인증 정보 없이 직접 만든 curl을 보내면 `401 unauthorized`입니다. 본문은 최대 64 KiB의 Markdown입니다. 원시 디버그 로그가 아니라 문제를 제기한 사람이 이해할 보고서를 쓰고 비공개 정보는 제외하세요. 활성 담당자는 항목이 종료되기 전에 추가해야 하며 버전 충돌 시 다시 읽어야 합니다. 완료 보고서는 작성자가 있는 서술 기록일 뿐 검증·반영·배포 증거를 대신하지 않습니다. 있으면 닫힌 보드 항목에 남고 세션의 Recently Done 행에서 바로 열립니다.

`/v1/board`는 퇴역한 Swift 앱의 오래된 카드를 읽기 전용으로 보여줍니다. 반영은 브로커가 판정하는 사실이므로 항목을 수동으로 반영 완료 표시할 수 없습니다(`422 landing_is_broker_fact`).

### Epic과 Feature: 구현 전에 사람이 정한 검토 스위치 따르기

실행 주기에 계획 게이트가 켜진 Epic에는 계획과 독립 검토가 필요합니다. Feature에는 사람이 정하는 **Needs independent review** 스위치가 있습니다(항목의 `review_required`; `clawdline item steps <id>`가 출력). 보드에서 사람만 설정할 수 있습니다. Agent는 설정할 수 없으며 Feature의 위험도를 스스로 판정해 대신 켜서도 안 됩니다. 데몬은 `implementing` 진입 요청 때 이 스위치를 읽습니다.

- **선택되지 않음**(기본값): Feature의 짧은 인수 기준을 쓰고 구현한 뒤 집중 테스트를 합니다. 검토용 계획을 쓰거나 `plan_review` child를 배정하거나 위험 평가를 기록하지 마세요.
- **선택됨**, 또는 계획 게이트가 켜진 모든 Epic: 검토된 계획 절차를 따릅니다.

선택되지 않은 Feature에도 검토가 필요하다고 생각한다면 사람에게 의견을 말하고 스위치를 켤지 결정하게 하세요. Agent가 데몬에 별도로 검토를 요청하는 방법은 없습니다.

1. 신중하게 계획하여 항목에 문서로 씁니다.
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. 계획에서 빠졌거나 잘못되었거나 위험한 부분을 비판적으로 검토할 읽기 전용 child를 배정합니다.
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. child가 끝나기를 기다립니다. `--work-id`로 배정한 검토 child가 성공하면 자체적으로 항목에 `plan_review` 문서 영수증을 기록합니다. `GET /v1/work/v2/items/<id>`의 `.documents`를 확인하세요. 영수증이 없을 때만, 예를 들어 child에 `--work-id`를 주지 않았을 때만 수동으로 기록합니다.
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
   같은 작업으로 다시 실행해도 이미 있는 문서를 응답하므로 안전합니다. 계획이 검토에 따라 어떻게 바뀌었는지 사람에게 전할 짧은 요약은 두 번째 검토가 아니라 별도의 `other` 문서입니다. 검토 뒤 Feature 계획이 바뀌었다면 이전 검토의 위험 경계 안에 머무는 변경에 한해서만 수정 계획 뒤에 `Review boundary assessment` 제목의 `other` 문서와 JSON `{"new_risk_boundary":false,"reason":"..."}`을 씁니다. 새 경계이거나 불확실하면 그 부분에 집중한 새 검토가 필요합니다. Epic에는 기존의 검토 최대 두 번 한도가 여전히 적용됩니다.
4. `clawdline item step-add <item id> …`로 작업을 단계로 나눕니다.
5. 그다음에만 `clawdline item phase <item id> implementing`을 실행합니다.

검증은 순서대로 계획하세요. 각 구현 child가 자기 코드를 집중 테스트로 확인하고, Epic 담당자는 영향받은 구성요소를 통합해 필요한 최소한의 구성요소 간 smoke check를 합니다. 통합된 후보가 동작한 뒤에야 실제 전체 흐름 검증과 해당 독립 UX·제품 검토를 배정합니다. 배정 전 검사자가 사용할 브라우저 경로·대상 URL·테스트 계정·fixture·권한을 확인하세요. 브라우저가 없다는 사실을 발견하거나 우회하려고 읽기 전용 검사 작업을 반복 배정하지 마세요. 먼저 접근을 고치거나 동등한 로컬 브라우저 도구를 선택하세요. 실패한 사전 점검은 전체 흐름 검증 시도가 아닙니다. 안정된 후보에 대해 Epic당 포괄적인 전체 흐름 검증 한 차례를 계획하고 child나 수정마다 반복하지 마세요. 좁은 수정 후에는 영향받은 경로만 재검증합니다. 인수 범위나 통합 경계가 실질적으로 바뀔 때만 전체 검증을 반복하고 이유를 기록합니다.

`clawdline item doc`은 항목의 버전과 마지막 문서 위치를 읽고 Idempotency-Key를 출력합니다(`--key`로 같은 쓰기 재시도). 이어 항목을 출력합니다. 본문은 `--body-file` 또는 표준 입력에서 읽습니다. 요청은 `POST /v1/work/v2/agent/items/<id>/documents`에 `{"expected_version", "session_id", "role", "title", "body", "reference", "position"}`를 보내며 역할은 `spec`, `design`, `test`, `deploy`, `completion_report`, `other`, `plan`, `plan_review`입니다.

문서를 수정하려면 같은 `--role`과 `--title`로 다시 씁니다. 데몬이 본문·reference·position을 교체하되 ID는 유지하고 버전을 하나 올려 `document.revised`를 기록합니다. CLI는 `added … at v1` 또는 `revised … to vN`이라고 출력하며 `clawdline item show`는 각 문서의 `vN`을 보여줍니다. 같은 텍스트를 다시 보내면 바뀌지 않고 현재 문서로 응답하므로 재시도가 안전합니다. 이전 본문은 보존되지 않습니다. 두 버전을 모두 남기려면 다른 제목을 사용하세요.

- `plan`, `plan_review`, 검토 경계 평가는 제자리에서 수정하지 않습니다. 쓸 때마다 새 문서를 만듭니다. 계획 게이트가 순서대로 읽고 검토가 읽은 계획을 특정하기 때문입니다.
- 항목당 문서는 최대 32개입니다. `completion_report`는 그 수에 포함되지 않으므로 항목이 가득 차도 추가할 수 있습니다. 항목당 완료 보고서는 하나이며 다른 제목으로 다시 써도 기존 것을 수정하고 새 제목을 적용합니다.
- 33번째 문서는 `documents_full`로 거부되고 아무것도 쓰지 않습니다. 메시지에는 기존 문서를 대신 수정하는 `clawdline item doc` 명령이 나옵니다.
- `plan`과 `plan_review`는 Epic 또는 Feature에만 쓸 수 있습니다(다른 종류는 `document_role_not_applicable`).
- `plan_review`의 `reference`는 계획을 검토한 Clawdline child의 작업 ID입니다. 데몬은 해당 작업이 존재하고(`plan_review_task_unknown`), 항목 담당 세션이 배정했으며(`plan_review_task_not_owned`), 항목 라인이 지정되어 있다면 이 항목의 라인이고(`plan_review_task_other_item`), 종류가 `plan_review`이며(`plan_review_task_wrong_kind`), `success`로 끝났고(`plan_review_task_unfinished`), 최신 계획보다 먼저 배정되지 않았을 때만(`plan_review_task_stale`) 받습니다. 앞에 계획이 없으면 `epic_plan_required`입니다. 검토 child의 자동 문서도 같은 검사를 통과하며 같은 작업으로 반복 쓰면 멱등입니다.
- 계획 게이트가 켜진 Epic은 검토된 계획 없이는 `clawdline item phase <item id> implementing`이 `epic_plan_required` 또는 `epic_plan_review_required`로 거부됩니다. 사람이 Needs independent review를 선택한 Feature도 `feature_plan_required` 또는 `feature_plan_review_required`로 거부됩니다. 선택되지 않은 Feature에는 인수 기준만 필요합니다. 선택된 Feature의 계획을 수정했다면 위험 경계가 그대로라는 증거 또는 새 검토도 필요합니다. 계획 게이트가 꺼진 Epic은 바로 implementing에 들어갈 수 있습니다.
- 게이트는 최신 `plan_review` 작업의 영수증을 읽습니다. 각 발견의 `severity`는 `blocking` 또는 `non_blocking`입니다. 구형 서식의 `important`, `minor`는 차단하지 않는 것으로 간주합니다. 발견이 없으면 판정은 `safe_to_land`, 모두 `non_blocking`이면 `proceed_with_findings`, 하나라도 `blocking`이면 `changes_required`입니다. 최신 검토에 차단 발견이 있으면 `item phase implementing`과 아직 담당 중인 항목에 `--work-id`로 배정하는 작업 중 `--kind plan_review`가 아닌 것은 `epic_plan_review_blocking` 또는 `feature_plan_review_blocking`으로 거부됩니다. 응답은 차단 발견과 다음 명령, 즉 계획 수정 후 새 검토 배정을 나열합니다. 차단하지 않는 발견만 있거나 구형 영수증에 severity가 없다면 진행할 수 있습니다. Epic 검토는 최대 두 번입니다. 두 번째도 차단한다면 발견에 답하도록 계획을 수정하고 세 번째 검토 없이 그 수정 계획으로 implementing에 들어갑니다. 세 번째를 배정하지 마세요.

**Epic을 하위 항목으로 나누고 배정합니다.** 이는 "사람의 메시지가 지시한 경우에만 보드 항목을 만든다"와 "사람만 항목을 배정한다"의 유일한 예외입니다. 사람이 Epic을 배정한 것이 나누는 권한입니다. 검토된 계획에 따라 Epic이 `implementing`에 들어간 뒤 다른 세션이 맡는 편이 나은 부분은 하위 `feature` 또는 `issue` 항목으로 만들고 배정합니다.

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | description on stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- 터미널 ID는 세션 주소록 `GET /v1/orchestrator/sessions`에 있습니다(`clawdline guide ko send`). 받는 세션은 Epic의 프로젝트에서 작업해야 합니다. 자신에게 배정할 수도 있습니다. `--assign-new`는 Epic을 명시한 Root Assignment로 새 세션을 엽니다. `--assign` 옵션이 없으면 하위 항목은 사람이 배정할 때까지 미배정으로 남습니다.
- `item child`는 Epic 버전을 읽고 요청 전 Idempotency-Key를 출력합니다(`--key`로 같은 쓰기 재시도). 이어 하위 항목을 출력합니다. 요청은 `POST /v1/work/v2/agent/items/<epic id>/children`에 `{"expected_version", "session_id", "kind", "title", "description", "steps"?, "deployment_policy"?, "assign"?: {"mode": "existing_session", "terminal_id"} | {"mode": "new_session", "assistant"?, "model"?, "persona"?}}`를 보내며 응답은 `201`과 `{"item", "assigned", "assignment_error"?: {"code", "message"}}`입니다. 하위 항목은 Epic의 프로젝트에 있고 Epic을 가리키는 `parent_id`를 지니며 카드에는 Epic 담당 세션이 만들었다고 표시됩니다. 단계는 `--step` 행을 쓰고, 없다면 배정 시 설명의 목록에서 가져옵니다.
- 하위 항목을 먼저 만들고 다음에 배정합니다. 배정 실패 시 하위 항목은 **미배정으로 남고**, 응답에 `session_unavailable`, `project_mismatch`, `assignment_failed` 같은 `assignment_error` 코드가 있으며 명령은 종료 코드 1로 끝납니다. `item assign`으로 다시 배정하거나 사람이 맡도록 남겨 두세요.
- `item assign`은 `POST /v1/work/v2/agent/items/<child id>/assign`에 `{"expected_version", "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}`를 보냅니다. 자신이 맡은 Epic의 열린 하위 항목을 다른 세션에 옮기며, 사람이 선택해 배정한 것과 같습니다.
- 아무것도 쓰지 않는 거부 코드: `not_epic_owner`(Epic 담당자가 아님), `parent_not_epic`(부모가 Epic이 아님), `epic_not_planned`(Epic이 아직 implementing 전), `item_terminal`(Epic 종료), `child_kind_not_allowed`(Feature·Issue만 가능), `epic_children_full`(열린 것과 닫힌 것 합쳐 최대 32개), `not_epic_child`(Epic의 하위 항목이 아닌 것의 `item assign`; 사람의 메시지가 요청한 경우를 빼면 사람만 배정), `invalid_assignment`, `version_conflict`, `persona_not_applicable`(422, 기존 세션에 persona 지정), `unknown_persona`(400, 목록에 없는 ID).
- **persona**는 새 세션을 열 때 시스템 프롬프트에 추가되는 역할 문구이며 대화 내내 작업 방식을 정합니다. 새 세션(`--assign-new`, `--new`, `dispatch`)에만 적용됩니다. 기존 세션은 시작 때의 persona를 유지합니다. 기본값은 없습니다. persona는 `CLAUDE.md`·`AGENTS.md`, 지시서, `CHILD.md`, 이 프로토콜보다 우선하지 않습니다. `GET /v1/personas`에 ID가 나옵니다. 각 persona의 `teams`는 소속 팀 전체이며 여러 팀에 속할 수 있습니다.
  - `architect` — Epic 계획.
  - `backend` — 데몬·API·저장소 기능.
  - `frontend` — 콘솔·휴대전화 레이아웃 기능.
  - `minimal-change` — 유지 가능한 최소 Issue 수정.
  - `code-reviewer` — 코드 검토와 `plan_review` child.
  - `reality-checker` — 검증, "동작한다"는 말보다 근거 우선.
  - `security` — 권한·페어링·Cloud 관련 작업.
  - `technical-writer` — 문서와 가이드.
  - 마케팅용 블로그·사이트·문서 저장소: `seo`(페이지와 메타데이터), `content-writer`(파일에 쓰는 글), `ai-search`(AI 답변 엔진이 인용할 수 있는 페이지), `social-media`, `instagram`, `email`(뉴스레터), `growth`(측정된 실험), `pr`(발표문).
  - 제품·품질·운영: `product-manager`, `sprint-prioritizer`, `feedback-synthesizer`, `trend-researcher`, `ux-researcher`; `test-automation`, `accessibility`, `performance`, `api-tester`, `evidence-collector`(수집된 근거를 바탕으로 주장별 PASS·FAIL 판정); `sre`, `devops`, `incident-commander`, `finops`, `secrets`.
  - 디자인·사업: `ui-designer`(프로젝트 디자인 시스템의 화면), `ux-architect`(흐름과 레이아웃 구조), `brand-guardian`(브랜드 일관성), `ui-finish-gate`(출시 전 시각 검토), `image-prompt`(이미지 생성 프롬프트), `pricing`, `customer-success`, `support`(답변 초안), `analytics`(실제 데이터 기반 답변), `devrel`(실행되는 예제), `privacy`(개인정보 점검, 법률 자문 아님).
  - `zero-review-lead` — 기존 기능이나 절차를 처음부터 재검토하는 Epic 담당자. 역할별 관점을 계획하고 동일한 사실 자료 묶음으로 읽기 전용 검토 child를 배정해 그 근거를 목표 설계로 종합합니다. 스킬은 `zero-based-review`입니다.
- **사람이 경험하는 부분이 바뀌는 Epic에는 독립 UX·제품 검토를 추가합니다.** 계획에서 사람에게 보이는 인터페이스·사용자 여정·제품 정책이 바뀌는지 분류하세요. 바뀐다면 병합 전에 읽기 전용 전문가 child를 적어도 하나 배정합니다. 레이아웃·상호작용·전체 제품 흐름에는 기본적으로 `ux-architect`를 사용합니다.

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

  지시서에는 통합된 후보를 명시하고 데스크톱 및 지원하는 최소 휴대전화 크기의 근거, 키보드와 화면 읽기 도구 동작, 막다른 흐름, 제품 적합성, 심각도, 구체적 권고를 요청합니다. 근거가 없다면 반드시 **미검증이라고 표시하고 이유를 말해야** 합니다. 위험의 중심이 레이아웃이 아닌 정책·범위라면 `product-manager`를 대신 사용하고, 별도의 출시 전 시각 점검이 중요하면 `ui-finish-gate`를 추가합니다. 차단 발견을 모두 해결하고 작업 ID·판정·처리를 Epic의 검증 근거나 완료 보고서에 기록하세요. 사람에게 보이는 영향이 없다면 계획에 이유를 적고 형식적인 검토를 추가하지 마세요. 검토한 범위도 기록합니다. 보통 통합된 Epic에 대해 관련 전문가마다 한 번만 배정합니다. UX·브랜드·보안 등 역할을 체크리스트처럼 습관적으로 모두 보내지 마세요. 작은 문구·간격·테스트 수정이나 기존 검토 범위 안의 발견은 직접 집중 검증으로 마무리하세요. 이후 변경이 사용자 여정·제품 정책·브랜드 방향·보안 경계 또는 기록된 범위 밖의 다른 위험을 실질적으로 바꿨을 때만 해당 경계를 명시해 관련 전문가만 다시 배정합니다. 이 검토는 기록된 계획 게이트나 검증 게이트의 정확한 후보에 대한 검사자 PASS를 대신하지 않습니다.
- **하위 항목을 병합한 뒤에도 Epic 책임은 남습니다.** 즉시 그 하위 항목과 `clawdline item steps <child id>`를 다시 읽고 모든 단계가 완료됐는지 확인하세요. 병합만으로 하위 항목이 닫히지 않으며 `merging`은 머무를 상태가 아닙니다. 하위 항목 담당 세션은 남은 단계를 완료하고, 정확한 커밋이 로컬 대상과 `origin/main` 양쪽에서 확인되는 반영 영수증을 기록한 뒤, 배포 정책에 맞는 배포 근거나 배포 불필요 이유와 함께 `deploying → done`으로 옮겨야 합니다. 자신이 담당자라면 직접 하고, 다른 세션이 담당자라면 그 담당자에게 후속 요청하거나 허가된 하위 항목 재배정 경로를 쓰세요. 담당자인 척하지 마세요(`not_item_owner`). 브로커 완료 알림이 있다면 ACK한 뒤 worktree 잔여물을 분류하고 반영 내용과 동일함이 입증된 것 또는 작업 임시 산출물만 제거하세요. 미반영·혼합·불명확한 바이트는 다음 담당자에게 남깁니다. 모든 하위 항목이 `done` 또는 `cancelled`가 되기 전에는 부모 Epic을 완료했다고 선언하지 마세요. `epic_children_open`이 남은 수를 알려 줍니다. Epic 하위 항목 외의 보드 항목은 만들지 마세요.
- **하위 항목 완료가 독립 Feature Root 세션을 닫지는 않습니다.** Epic이 `--assign-new`로 연 Root마다 `clawdline session close --dry-run --terminal <id>`를 사용해 `closeability`를 읽으세요. 자신이 연 Root에 한해서만 `closing as epic_owner`라고 응답합니다. 라벨이나 터미널 위치로 소유권을 추정하지 말고 `clawdline session report`를 종료로 간주하지 마세요. 하위 항목이 `done`이 된 뒤 Root 담당자에게 자신의 작업·반영·알림·할 일·worktree를 감사하고 종료 보고를 완료하도록 요청하세요. 증명은 현재 데몬이 지원하는 경로에서만 얻습니다. 퇴역한 Swift 종료 경로는 해당하지 않습니다. 그 경로나 보호된 종료가 불가능하다면 제품 차단 사항과 다음 담당자를 기록하고 세션을 유지하세요. 신원과 작업을 검증했고 `closeability.state=safe`일 때만 `clawdline session close --terminal <id>`로 끝냅니다. 이 명령은 종료 전에 inventory를 다시 읽고, 이미 없어졌다면 두 번째 실행은 `session_not_found`입니다. `clawdline close <terminal id>`로 보호 장치를 우회하지 마세요. `blocked`에는 지정된 해결 담당자에게 후속 요청합니다. `unknown`(`terminal_unreadable` 포함)이면 세션을 보존하고 빠진 근거와 다음 담당자를 기록하세요. 강제 종료하거나 보관 처리하거나 정리됐다고 주장하지 마세요. Epic 조정이 끝났다고 말하기 전에 각 Root의 종료 결과나 지정된 차단 사항을 나열하세요. 보드의 `done`은 이 inventory를 대신하지 않습니다.

## 11. 조정

**컴퓨터 coordinator(「Clawdfather」).** 프로젝트 바깥의 데몬 소유 컴퓨터 작업 공간에서 세션을 보고하고 지원되는 컴퓨터 작업을 관리합니다. Clawdline을 포함한 프로젝트 소스 코드는 편집하지 않습니다. 사람이 엔지니어링 작업을 명시적으로 요청하면 먼저 `clawdline item add --project … --assign-new`로 프로젝트 보드 항목을 만들고 프로젝트 세션에 위임합니다. 그런 요청이 없다면 사람이 수락할 항목을 제안합니다. 배정된 프로젝트 담당자가 child 배정·검증·반영을 맡습니다. 작업 공간은 조직상의 경계이지 파일시스템 샌드박스가 아닙니다. 새 역할 연결은 그 작업 공간에서 해야 하며 기존 연결은 계속 읽을 수 있습니다. 콘솔의 별도 Clawdfather 동작에서 세션을 연 뒤 대화 ID를 등록하세요. 제품 경계는 `docs/clawdfather-role.md`에 있습니다. 새 세션 안에서 `clawdline coordinator bind`를 실행해 등록하거나 오프라인임이 입증된 이전 담당자로부터 다시 연결합니다. 명령은 자기 대화 ID를 읽고 온라인 또는 읽을 수 없는 담당자는 교체하지 않습니다. `GET /v1/orchestrator/coordinator`로 역할을 확인합니다. `/coordinator/bearings`는 활성 작업·대기 중인 반영·열린 대기·배달 실패 기록·보유 임대·`unknown`을 한눈에 보여줍니다. `{"session_id": "<conversation id>"}`를 담은 `POST …/coordinator/register`가 역할을 맡고, 기존 세션이 오프라인이면 `expected_coordinator_id`, `expected_generation`을 담은 `POST …/coordinator/rebind`가 역할을 이동합니다. 승계는 `501 succession_unavailable`로 응답합니다.

**파일 대기.** 대기는 "이 경로의 담당자가 끝나면 알려 줘"라는 뜻입니다. 기록과 메시지일 뿐 잠금이나 파일 감시가 아닙니다.

- `POST /v1/orchestrator/waits` — `{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}`를 받습니다. 세션 ID는 대화 ID입니다. 담당자의 입력창에 한 번 알립니다.
- 담당자가 `{"owner_session_id", "commit"?, "note"?}`를 담은 `POST /v1/orchestrator/waits/<id>/release`로 끝내면 각 대기자에게 알립니다. 시간이 지났다는 이유로 자동 해제되지는 않습니다.
- 대기자는 `{"waiter_session_id"}`를 담은 `POST …/waits/<id>/cancel`로 빠질 수 있습니다.
- `409 owner_busy`와 `502 request_delivery_failed`는 **대기는 기록되었지만** 담당자에게 아직 전달되지 않았다는 뜻입니다. `502 release_incomplete`는 아직 통지받지 못한 대기자를 나열합니다. 해제를 다시 보내세요.

**임대.** 자원은 두 가지입니다. `heavy_compile`은 컴퓨터 전체의 단일 컴파일 슬롯이고, `landing`은 체크아웃마다 하나입니다.

**빌드나 테스트 묶음은 직접 실행하지 말고 `clawdline heavy -- <command>`를 사용하세요.** 이 명령은 `heavy_compile`을 대기하고 컴퓨터에 메모리가 충분해지면(전체의 4분의 1, 최대 1 GB 사용 가능, 메모리 정체 10% 이하) 명령을 낮은 우선순위로 실행합니다. Linux에서는 메모리가 부족할 때 커널이 가장 먼저 종료할 대상으로도 지정합니다. 실행 중 임대를 갱신하고 끝나면 해제하며 원래 명령의 종료 코드를 유지합니다. 데몬이 없거나 알 수 없는 거부 때문에 빌드를 포기하지는 않습니다. 이때는 stderr에 설명 한 줄을 쓰고 명령을 그대로 실행합니다. 슬롯과 메모리를 얻기 전에 `--max-wait`(기본 30분)이 지나면 대기열에서 빠지고 명령은 실행하지 않은 채 **75**로 끝납니다. 이 코드를 명령 자체의 실패로 오해하지 마세요. 나중에 다시 실행합니다. 대기 시작과 종료에만 각각 한 줄을 출력하며 중간에는 아무것도 출력하지 않습니다. §2처럼 한 번 길게 기다리세요. `heavy` 안에서 호출한 `heavy`는 바로 실행됩니다. `--min-available 1500M`은 더 많은 메모리를 요구하고 `--no-slot`은 메모리만 검사합니다. 저장소에 `tools/heavy.sh <command>`가 있다면 바이너리를 대신 찾아 줍니다.

- `POST /v1/orchestrator/leases` — `{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`를 받습니다. `granted` 또는 `position`과 `retry_after_seconds`가 포함된 `queued`로 응답합니다. 대기 중이면 같은 `request_id`로 다시 요청합니다.
- `POST …/leases/renew | release | cancel`에는 `{"request_id", "resource", "checkout"}`를 보냅니다. 보유자가 `/renew`로 갱신하며, 원래 `request_id`로 `POST /v1/orchestrator/leases`를 다시 보내도 갱신됩니다.
- 60초 안에 갱신하지 않으면 임대를 잃은 것으로 봅니다. `409 lease_lost`가 이를 뜻합니다. 대기자가 32명이면 `429 queue_full`입니다.

**그래프**(`GET /v1/orchestrator/graphs`)는 배정된 작업의 `graph` 필드에서 계산한 읽기 전용 보기입니다. **회수**(`/v1/orchestrator/reclaim`)는 완료된 체크아웃을 정리합니다. POST는 본문에 `{"dry_run": false}`가 없는 한 예행연습입니다.

## 12. 요청이 거부되었을 때

- `error.code`(평면 형태라면 `error`)로 분기하세요. 메시지는 사람이 읽기 위한 것입니다.
- `retry_after`는 용량 제한 응답입니다. 지정된 시간만큼 기다린 뒤 같은 요청을 보내세요.
- `409 stale_write`, `503 orchestrator_store_busy`는 저장소가 바빴다는 뜻입니다. 같은 요청을 다시 보내도 안전합니다.
- 담당권·생존 상태·출처 등 어디에서든 `unknown`은 데몬이 읽지 못했다는 뜻입니다. "없음"이 아니므로 이를 근거로 삭제하거나 종료를 선언하지 마세요.
- 예상한 경로가 `404 not_found` 또는 `501`로 응답한다면 이 데몬에는 없는 기능입니다. 그 사실을 말하고 퇴역한 Swift 앱 경로나 대체 provider-native subagent로 우회하지 마세요.
