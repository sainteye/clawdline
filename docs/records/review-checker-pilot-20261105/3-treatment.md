# 歷史來源審查

判定：proceed_with_findings。完成來源審查；這不是 Cloud 執行期或正式上線的核准。

## 凍結範圍與方法

- BASE：01259ddd93a7a8adf2c2b9ee63167ce8e3d50663。
- HEAD：dd263f1cfdc4e6047490fedd734c968507223f23。
- tree：43523a537ae12eda6098d3a5806d3ae6f794f871。
- 已實際執行 git rev-parse、git diff --stat 與來源讀取；身分完全一致。45 檔，276 additions、430 deletions。
- CROSS-SURFACE：觸發 A（API／server、UI、Cloud wire、CLI、持久化載入、文件／英繁翻譯）及 B（配對與權限預設改變、移除 caps route 與 remote_write gate）。背景終端重新驗證與串流亦為必要相依。
- 僅讀指定 clone；未讀兄弟實驗、先前報告、ground truth 或主 checkout，未改 repository、未 build/test、未瀏覽或呼叫外部 API。
- 方法偏差：一次 rg 輸出以 head -200 截取，超出 brief 的 git／rg／sed 工具白名單；沒有修改檔案。後續以 rg／sed 重讀必要證據，不把截斷輸出當成完整搜尋證明。

## 已確認發現

### F1 — should-fix／non_blocking：公開權限契約仍互相矛盾

位置：docs/cloud.md:33、docs/cloud-terminal-wire.md:14；對照未改的 docs/cloud-wire.md:498、docs/terminal-direct-path.md:63。

新 overview 宣稱每個配對登入 viewer 都能傳送，新 terminal wire 也移除 relay 的 send grant 條件；但 overview 仍連向的公開 wire 規格明列 relay 發 ctl 與收 ctlr 必須有 send_prompt，direct-path 說明仍要求 send_prompt。讀者或後續實作者無法從同一候選樹判定應遵循哪個跨服務契約，亦可能把 relay 拒絕誤判成 daemon 問題。

修正：釐清 Cloud 服務實際採用哪個版本後，同步維護現行 wire 與 direct-path 說明；若某段僅是歷史規格，明確界定其時間與替代規格，保留歷史來源引用。信心：高（文件矛盾已讀到）；這項發現不等於已證實正式 relay 拒絕。

## INTENDED? author must confirm

### Q1 — Cloud token／relay 是否同步改約？

證據：web/console/src/cloud/copied.ts:31-33 強制 allowWrites=true；internal/transport/cloud/link.go:1119-1131 不再依 Caps 決定效果；docs/cloud-wire.md:498 仍記錄 relay 需要 send_prompt。diff 沒有 Cloud API token 發行或 relay enforcement 的實作，clone 根目錄亦不含該服務。

具體後果（若外部服務仍依所記載契約）：舊唯讀 token 即使在 UI 可送出且機器會放行，ctl／terminal 流量仍可能在 relay 被拒絕，無法達成「paired device can send」。

請作者提供配套 Cloud revision／契約及缺少 send_prompt 的已配對 viewer 實測；或者補齊服務端遷移。此為整合缺口與 plausible issue，尚非已確認執行期缺陷，不能用目前來源證據斷言已部署 relay 的行為。

### Q2 — 舊 roster-only 配對是否刻意失去操作權？

證據：internal/transport/cloud/link.go:390-391、1119-1122 新增必須有 pin 的拒絕；同檔 :1031-1034 的未改 publicKeyFor 說明仍承諾保留 pin 檔出現以前的 roster fallback；terminal_test.go 把 roster-only 成功測試改為拒絕。

具體後果（來源推導，高信心）：先前靠 roster fallback 運作、沒有本機 pin 的舊 viewer 仍可驗簽／讀取，但不能再送 effectful command，也不能使用終端，必須重新配對；未看到把這類既有配對轉為 pin 的遷移。

請作者確認這個相容性收窄是否屬本次範圍。若是，需修正 fallback 說明並交代重新配對路徑；若不是，需安全的遷移或相容方案。意圖未決，沒有當作 confirmed defect。

## 已核對正確的來源連接

- authority.New 先 checkState，再為 Approved、非 Local 的舊裝置補 Read+Send，Save 失敗回錯誤，成功後才填入 authority map（internal/domain/auth/authority.go:141-166）。
- addDevice 為非 Local 裝置補 Read+Send；SetCapabilities 不再建立唯讀裝置。HTTP gate 使用實際 authority verdict，terminalAccess 仍逐次檢查 Holds 與 Send；升級後的配對資格可沿用既有機制。
- 本機 caps route 移除與 schema／Go／TypeScript 型別移除相互對應；remote_write 同步從 settables、schema、UI 與 gate 移除。
- Cloud effect admission 仍檢查未撤銷 roster、pin／roster 公鑰一致與動態 allowCommands；terminalAuthority 仍有拒絕與未驗證的分流。未發現因刪除 caps 判斷直接跳過上述檢查。
- generic Cloud Router 仍加 ViaCloud，HTTP terminal 路由仍拒絕；專用 Cloud terminal ingress 走同一 terminal service。這條不受此次改約而新增的旁路未成立。
- 本機終端保留 auth.Changes 背景 revalidate；修改的測試改以 Revoke 驗證失去資格，沒有把撤銷模型刪除。
- newCloudSession 注入 PairedCloudClient；legacy boot 提供 durable nextSequence，base client 的 writable constructor 仍要求該 provider。
- 改動的主要權限文案同時更新英文與繁體中文 catalog；其他語言依該 clone 規則不要求逐次全更。
- 新 TestOldPairedDeviceCanSendAfterRestart 直接建構 Read-only store，檢查記憶體、持久化、只存一次與撤銷；從 base 的 New 不補 Send 可推導其首個 assertion 會失敗。這是來源推導的 red 能力，沒有實際執行。
- 聚焦簡化檢查：小型 client subclass 是現有注入 seam 上的局部改動；沒有證據支持在本次強制改成另一套抽象。

## 驗收宣稱與證據狀態

1. 「舊本機配對載入後可傳送」：來源層 passed；執行期 unverified（本輪禁止測試）。
2. 「舊 Cloud capability 不再在 console／daemon 本身阻擋」：來源層 passed，以上兩端已讀；端到端 unverified，Q1 的外部 token／relay 端缺證。
3. 「機器保留 pin 與 command switch」：來源層 passed；舊配對相容範圍有 Q2，實機時序 unverified。
4. 「full public check passed」：unverified。只有候選 commit message 的自述，沒有執行紀錄可獨立核實；本輪也禁止重跑。
5. 「不破壞必要連接」：文件一致性 failed（F1）；Cloud 外部連接、兩機／手機 relay、真實 browser／平台／時序 unverified。commit message 本身亦將 production 驗證留待部署。

未發現其他可確認的行為缺陷；這不構成全系統無缺陷證明。沒有把 retired-app-record 的舊 --send／remote_write 敘述列為新問題。CLI 移除相容 --send 可由 diff 確定，但文件同步刪除其支援承諾，未另擴張成阻擋本次核心宣稱的缺陷。

## 角色與技能來源

實際角色：clawdline.persona.code-reviewer，版本 sha256:6324291052765bd9000aca77475a041422bf82bfe8837a33282fd300b1695edf；snapshot OMITTED，來源為本任務 launch 的 personas/code-reviewer.md。

已讀並套用 clawdline.skill.two-axis-code-review，版本 sha256:1221272ac73f36a5323171093d7c8534cfebe90c82a93a0c8721ef5b41bb398d；read receipt 6d456f322c0c63b8ed254a3c655a0b6a272a648125755dfe239db994adfb75a6，applied receipt 另見命令輸出。

Fixed but not yet released (awaiting review)：Nothing。本任務沒有修正程式或建立／移除 worktree（移除 0）。

