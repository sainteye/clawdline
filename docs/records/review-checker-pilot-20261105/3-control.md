# 原提示原始碼審查

判定：**changes_required（需先處理阻擋事項）**。

## 邊界與實際證據

- 唯一受審 checkout：`CASE_DIR`。
- `git rev-parse HEAD HEAD^{tree}`：`dd263f1cfdc4e6047490fedd734c968507223f23` / `43523a537ae12eda6098d3a5806d3ae6f794f871`，與任務一致；結束時再次核對相同。
- 比較基準：`01259ddd93a7a8adf2c2b9ee63167ce8e3d50663`；diff stat：45 files changed, 276 insertions(+), 430 deletions(-)。
- 已讀 changed source 與相關 unchanged paths，包括 `internal/adapters/cloud/pinned.go`、`internal/transport/cloud/pairing.go`、`internal/app/cloudops/cloudops.go`、`web/console/src/legacy/js/net/cloud-boot.js`、`cloud-client.js`、`docs/cloud-wire.md`、Cloud authority ADR 與具日期的實作紀錄。具日期的紀錄只用於辨認歷史邊界，不當成現行 production 證據。
- 實際執行：唯讀 git/rg/sed、`git diff --check`（exit 0）、`git status --short`（空白）。沒有 build/test、瀏覽、外部 API、派工或正式環境探查。沒有修改 repository。

## 發現

### F1 — 阻擋：既有、尚無 pin 的 Cloud 配對在升級後失去寫入能力

- 位置：`internal/transport/cloud/link.go:1119`、`:1121`；終端同類變更在 `:390`。
- 問題與後果：當既有 viewer 是 pin 檔引入之前完成配對、只有 roster 公鑰時，`pairedKey` 保持 nil；任何合法 32-byte roster key 都不會等於 nil，寫入授權因此在設定 `RosterAllowsSender` 前返回。`internal/app/cloudops/cloudops.go:485`–`:487` 會回 `403 unknown_sender`；終端則明確回未配對。這些已經能操作的舊 viewer 升級後必須重新配對，並非直接獲得 candidate 宣稱的可傳送行為。
- 證據：未改的 `link.go:1031`–`:1034` 仍明確承諾 roster fallback 是為了讓 pin 檔出現以前已配對的瀏覽器繼續運作；`internal/adapters/cloud/pinned.go:14`–`:16` 也保留該契約。`internal/transport/cloud/pairing.go:376` 是完成新 handover 才寫 pin 的路徑，本次沒有對舊 viewer 的遷移。candidate 甚至把 `terminal_test.go:420`–`:425` 的舊相容案例由允許改成拒絕，證明此行為確有改變，卻未解決升級連線。
- 具體修正：交付受信任的舊配對遷移／重新確認流程，在取消舊 fallback 前讓既有裝置取得可驗證 pin；保留升級前後相同行為的案例。如果這是刻意安全 cutover，需明列相容性中斷、恢復步驟與對應驗收，不能同時保留「舊瀏覽器不會停止運作」的承諾。不要單憑不受信任的 roster 自動建立 pin。
- 證據類型／信心：**source-inferred；高**。條件分支與回覆可從原始碼確定，未執行 runtime reproducer。

### F2 — 應修：Cloud 可寫宣告尚未接上服務端契約證據

- 位置：`web/console/src/cloud/copied.ts:31`–`:33`；新服務宣告在 `docs/cloud-terminal-wire.md:14`。
- 問題與後果：adapter 強制 `allowWrites: true`，但未改的 `cloud-boot.js:483`–`:497` 仍向同一 API 取得 token，再在 `:723`–`:726` 原樣交給 client；此舉不會改變 token 能力或 relay admission。未改的公開 wire 規格 `docs/cloud-wire.md:498` 仍寫 relay 的 ctl 發送與 ctlr 接收需要 `send_prompt`。若 consumer 仍照該契約，舊唯讀 viewer 即使 UI 開放，也會在到達機器前被拒絕。新的終端文件把 relay 宣告改成不需 send，這個 checkout 卻無對應 consumer 或 cutover 證據。
- 限定：這是**已確認的公開契約／證據缺口**，以及**有條件、合理的送出失敗風險**；不是已觀察到 production relay 拒絕。`docs/cloud.md:5` 明確說 private API／relay 不能由此 checkout 驗證，candidate commit 也承認 production 驗證待部署，不能將舊文件或前端旗標當成 live 結論。
- 具體修正：補上同一版本的 API／relay 契約與 consumer 接受證據，並明確記載 rollout 邊界；以舊唯讀且已 pin 的 viewer 驗證 ctl/ctlr 與終端 publish/subscribe 的兩端結果。在此之前把服務端變更標成待確認，且勿把前端 writable 當成端到端已可寫。保留具日期的歷史紀錄。
- 證據類型／信心：**source-inferred 契約缺口：高；實際部署是否拒絕：unverified**。

### F3 — 應修：移除無害相容旗標會使既有開啟命令失敗

- 位置：`cmd/clawdline/auth.go:111`–`:113`。
- 問題與後果：base 明確接受 `open --send` 作為不改行為的相容旗標；candidate 移除註冊，仍使用 `flag.ExitOnError`。既有 `clawdline open --send`／`--send --print` 腳本會在建立 browser credential 前被未知旗標擋下，與取消「額外授權」無關。
- 證據：base 的 `cmd/clawdline/auth.go` diff 刪除 `fs.Bool("send", true, "compatibility flag; new browsers can already send")`；`docs/user/install.md` diff 同時刪除舊腳本仍接受 `--send` 的承諾。
- 具體修正：保留 deprecated `--send` no-op 解析，不重新引入權限分支；驗收舊、新兩種 open 命令都可走到同一 browser 建立行為。
- 證據類型／信心：**source-inferred；高**。沒有執行 CLI。

## 已查證正確的部分與接受主張

| 主張 | 結果 | 證據與限制 |
|---|---|---|
| 指定 commit/tree 身分 | passed | 前後兩次 rev-parse 一致 |
| 舊本機已核准非 local 憑證讀取時升級 Send 並保存 | passed，僅 source | `authority.go:144`–`:160`；Save 失敗會拒絕建立 authority，未把失敗當成功 |
| 新本機配對／舊 AddDevice caller 取得 Read+Send | passed，僅 source | `authority.go:459`、`:481`–`:487`；SetCapabilities 也固定保留 Send，但仍拒絕 Admin |
| schema、Go 與 TypeScript 的移除同步 | passed，僅 artifact inspection | auth/settings schema 與兩份 generated diff 一致；未執行 generator |
| 前端不再因舊 capability bits 關閉 allowWrites | passed，僅 source | 新 PairedCloudClient override，且 newCloudSession 使用該 client；不是 relay 可寫證明 |
| 本機撤銷與 Cloud commands 開關仍有拒絕路徑 | passed，僅 source | auth Revoke 移除 row；link 本機拒絕清單先行；allowCommands 每次讀檔，讀失敗回 false；終端保留 roster unavailable 與 denied 區別 |
| 既有 Cloud 配對連線不受破壞 | failed，source-inferred | F1 的 roster-only 舊裝置 |
| 正式兩機／手機 relay 可傳送 | unverified | 本任務禁止 network/runtime；private consumer 不在允許範圍 |
| commit 所述完整 public checks 通過 | unverified | 本輪沒有執行；commit 文字不是獨立測試收據 |

新 `TestOldPairedDeviceCanSendAfterRestart` 的磁碟 Send、單次保存及重啟斷言，從 base 無遷移路徑推論可捕捉遺漏；HTTP 與終端舊唯讀 allow 斷言亦會與 base 行為不同。這是測試敏感度的原始碼判斷，沒有實際 red/green run。Cloud mock roster 的測試不能證明真實 relay 接受缺少 send_prompt 的 token。

簡化檢查：新增 adapter 只覆寫既有 client option，沒有另寫傳輸或加 dependency；本輪沒有證據支持另開重構。

## 意圖問題與範圍外事項

- 意圖問題：pin 前的既有配對是否刻意退役？若是，需把它明列為 cutover 及恢復流程；此審查沒有替作者推定已獲准中斷。
- 範圍外：歷史文件所描述的舊 app、舊 Cloud 實作不是本輪要修的 live 元件，沒有以歷史實測推定目前 production 狀態。

## 角色與技能來源

- 實際角色：`clawdline.persona.code-reviewer`。
- 角色版本：`sha256:1d280ec7e3fd4459cd460a7011e197fbfbf7cf4e9a988f49bb91998a1a63aead`。
- 來源：本任務 launch 的 `personas/code-reviewer.md`；snapshot `OMITTED`。
- 使用技能：`clawdline.skill.two-axis-code-review`，版本 `sha256:1221272ac73f36a5323171093d7c8534cfebe90c82a93a0c8721ef5b41bb398d`；已讀取並分別對照 request 與 project rules。
