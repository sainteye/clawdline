# ADR 0002 — Cloud authority 與執行端信任

狀態：W0-A 交付候選；**不是 wire-authority cutover receipt**。
日期：2026-09-10。固定 source commit/tree 與讀取區間同 [ADR 0001](0001-platform-boundary-and-evidence.md)。
授權來源：CLA-296 Plan v4（2026-09-10），「Target architecture」、「Cross-repository contract
publication」及 Ubuntu profile。沒有查詢 live Cloud，也沒有修改 deployment 或既有安全 policy。

## 決策

1. Cloud API 是 account、device、entitlement、schedule 等 **control plane**；Relay 傳送
   ciphertext 與必要的 routing／authentication metadata，屬 **encrypted data plane**。
   API 不串在每一個加密 envelope 的 payload 路徑。
2. 公開 Clawdline repository 的 PWA 是 E2EE console。private Cloud repository 的
   marketing／connect UI 是控制平面 UI；兩者不可混稱同一執行端。
3. Mac 或 Ubuntu executor 是解密與執行的 endpoint，持有 plaintext workspace、process
   memory 與所需 credentials。移到 GCE 不會讓執行端變成「Cloud 看不到」的 ciphertext relay。
4. GCE 是新的 execution plane，沿用既有 AWS／Cloudflare control-plane 分工。GCE executor
   IaC 與權限／state 必須獨立，不能複製 Cloud API database 或把部署 state 混在一起。

來源界線：本 repo 的 [docs/cloud.md](../cloud.md):3–19 已敘明 API／relay／PWA、host key
與 ciphertext；[CloudEnvelope.swift](../../Sources/CloudEnvelope.swift):174–235 在 host seal/open；
[CloudTransport.swift](../../Sources/CloudTransport.swift):875–896 解密後交 inbound command。
因此「executor 管理者與能取得其記憶體／disk 的雲端基礎設施在 endpoint 信任範圍內」是從
執行位置導出的 threat-model 假設，並非對 GCP 供應商權限的實測聲明。W0-A 不聲稱 confidential
computing、硬體隔離或 host compromise 後仍有 E2EE 保護。

## Authority 保持與 W0-E 的交付接口

[docs/cloud.md](../cloud.md):9–11 指向 private `clawdline-cloud/docs/PROTOCOL.md` 作為現有
normative prose authority。W0-A 保持這個地位，沒有重新定義 nonce、channel、command auth 或
revocation policy；程式與 prose 發生差異時，需記錄相同 field／fixture 的兩端證據，不能默默
把其中一方當新規格。

| 階段 | Authority 與必要證据 |
|---|---|
| 此次 W0-A | 現有 private PROTOCOL authority 不變；public repo docs 是 host 實作說明。沒有宣稱 private consumer／production 已同步 |
| W0-E candidate | `Contracts/Cloud/v1/` 放 language-neutral schema、closed vocabulary、valid/invalid byte fixtures、單一 manifest、semantic version、compatibility floor、source digest、changelog；尚不自動取代舊 authority |
| Private mirror | 保存 candidate 的相同 bytes，另記 public source commit／digest；consumer 不依賴使用者持有 private checkout，也不得各自維護第二份 fixtures |
| Cutover | public contract-owner 與 private consumer review 均通過，Swift／API／Relay／PWA 對同一 bundle digest 的驗證到齊後，另立 recorded cutover ADR，同時更新兩 repo authority references |
| Rollout | additive readers/validators → public/private mirrors CI → Relay/API readers → Mac producer → Ubuntu producer → observed compatibility window → 提高 client floor。production 不可先 emit 尚無已部署 reader 接受的版本 |

移除欄位或變更 crypto canonicalization 需要 protocol major；price、DB models 不加入 wire
bundle。舊 reader 相容與 rollback 不能由「兩份 Markdown 看起來相同」證明。W0-D 提供 Cloud
consumer/CI/release evidence，W0-E 才能比較相同版本、同一 fixture、同一 digest。

## 目前 crypto／networking 的證據層級

- 固定 source 使用 CryptoKit；[CloudEnvelope.swift](../../Sources/CloudEnvelope.swift):49、139–147、174–235 有 12-byte AES-GCM nonce、簽章輸入與 seal/open；[CloudKeys.swift](../../Sources/CloudKeys.swift):3–6、456–483 有 Ed25519／content key 與 Keychain storage seam。這些是 current bytes，不能由此改寫未檢查的 private authority。
- [CloudTransport.swift](../../Sources/CloudTransport.swift):207–249 有 socket／URLSession seam，`:875–896` 使用 pinned sender key 與 sequence gate；[CloudAppBridge.swift](../../Sources/CloudAppBridge.swift):199–238 仍路由回 RemoteServer。
- Linux crypto 實作套件、network stack、secret provisioning 細節及 Foundation 行為留待 W0-F 的真實 compiler 證據與 W0-E 的 shared vectors；W3 再選型。這個 ADR 不批准換演算法、nonce 長度或簽章 bytes。
- ledger/spool 已存在不等於 production durability 已接線。persist-before-effect、duplicate-effect、reconnect/resume 與 revocation freshness 的承諾須由相應後續節點的 runtime receipt 支持。

更完整的資產、權限與攻擊邊界見 [trust boundary](../platform-trust-boundary.md)；Linux 實作及
接受條件見 [capability matrix](../platform-capability-matrix.md)。刪除本 ADR 候選可回退本次
文件變更；authority 和 deployment 從未切換，因此沒有需操作的 runtime rollback。
