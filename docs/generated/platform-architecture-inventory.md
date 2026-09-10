# Platform architecture inventory

Generated evidence; do not hand-edit. This is a historical fixed-tree baseline, not a freshness claim about HEAD.

- Observed tree: `75314e95a17d901362c0d998477205606b9add5d`
- Generator SHA-256: `658b69ce7afc78404356282fe9267255865826d5ab997735fca914b218fac5f6`
- Selected inventory SHA-256: `5ba0bce6789b1b42196c8661f0c111e76ad303e7dfd1213e91dd1abe7320dc30`
- Schema: `1`; evidence: `fixed_git_blobs_and_raw_lexical_candidates`

Reproduce from the repository root with this generator version:

```sh
python3 tools/platform-architecture-inventory.py --tree 75314e95a17d901362c0d998477205606b9add5d --format markdown
python3 tools/platform-architecture-inventory.py --tree 75314e95a17d901362c0d998477205606b9add5d --format json
```

## Scope and limits

- Reads only the named Git tree; excludes worktree/index/untracked overlays.
- Promisor-configured repositories are refused before object lookup, even when complete locally; all Git transports are disabled.
- Verifies the requested commit/root, every traversed tree and every selected blob by raw object type and hash before using their bytes.
- Imports, basename mentions and lock spellings are raw lexical candidates; comments, strings and inactive #if branches can match.
- Basename edges are not resolved symbols, dependencies, call graphs or compiler target edges.
- No compilation, runtime reachability, lock correctness, platform support, state ownership or release acceptance is inferred.
- Build references are bytes, not evaluated SwiftPM/shell; resources, web, history/churn and external repositories are outside this inventory.

A line is Python `str.splitlines()` length (including a final unterminated line).
The inventory digest covers sorted path/mode/blob/byte-count/content-SHA-256 records.
Missing build references, empty source partitions, nonregular sources, unreadable or corrupt traversed objects fail before output.
JSON carries every source row, import line, lock spelling and lexical edge; Markdown summarizes edges and lists every file.
Source-reading ownership map and adapter direction: [ADR](../adr/0001-platform-boundary-and-evidence.md).

## Fixed-tree totals

| Measurement | Count |
|---|---:|
| production_files | 145 |
| test_files | 67 |
| production_lines | 121552 |
| test_lines | 64659 |
| build_references | 5 |
| lexical_edges | 845 |

## Production import spellings

| Module spelling | Files |
|---|---:|
| `AVFoundation` | 2 |
| `AppKit` | 26 |
| `Carbon` | 1 |
| `CommonCrypto` | 1 |
| `CoreFoundation` | 2 |
| `CoreImage` | 1 |
| `CryptoKit` | 23 |
| `Darwin` | 6 |
| `Foundation` | 128 |
| `ImageIO` | 1 |
| `Network` | 3 |
| `SQLite3` | 2 |
| `Security` | 3 |
| `ServiceManagement` | 1 |
| `Speech` | 1 |
| `UniformTypeIdentifiers` | 2 |
| `os` | 2 |

## Largest lexical crossings (top 30)

Counts include comments/strings; do not compare these with a compiler graph or the existing architecture ratchet.

| From | Basename candidate target | Occurrences |
|---|---|---:|
| `Sources/Orchestrator.swift` | `Sources/RemoteAuth.swift` | 115 |
| `Sources/RemoteServer.swift` | `Sources/Orchestrator.swift` | 108 |
| `Sources/Settings.swift` | `Sources/Config.swift` | 98 |
| `Sources/OrchestratorStore.swift` | `Sources/Orchestrator.swift` | 92 |
| `Sources/Orchestrator.swift` | `Sources/OrchestratorDraft.swift` | 86 |
| `Sources/RemoteServer.swift` | `Sources/SessionWatch.swift` | 67 |
| `Sources/OrchestratorDraft.swift` | `Sources/Orchestrator.swift` | 65 |
| `Sources/UsageLedger.swift` | `Sources/Project.swift` | 54 |
| `Sources/Controller.swift` | `Sources/Config.swift` | 48 |
| `Sources/Coordinator.swift` | `Sources/Orchestrator.swift` | 40 |
| `Sources/UsageLedger.swift` | `Sources/Orchestrator.swift` | 40 |
| `Sources/Controller.swift` | `Sources/DevStack.swift` | 39 |
| `Sources/CodexNaming.swift` | `Sources/Codex.swift` | 38 |
| `Sources/Orchestrator.swift` | `Sources/RemoteServer.swift` | 38 |
| `Sources/Orchestrator.swift` | `Sources/SessionWatch.swift` | 38 |
| `Sources/RemoteServer.swift` | `Sources/RemoteAuth.swift` | 37 |
| `Sources/Orchestrator.swift` | `Sources/StartPoints.swift` | 35 |
| `Sources/OrchestratorLandingQueue.swift` | `Sources/Orchestrator.swift` | 34 |
| `Sources/Codex.swift` | `Sources/Transcript.swift` | 33 |
| `Sources/Orchestrator.swift` | `Sources/Targets.swift` | 33 |
| `Sources/Compat.swift` | `Sources/Codex.swift` | 32 |
| `Sources/Orchestrator.swift` | `Sources/Config.swift` | 32 |
| `Sources/CloudHandover.swift` | `Sources/CloudPairing.swift` | 29 |
| `Sources/Transcript.swift` | `Sources/Codex.swift` | 27 |
| `Sources/Settings.swift` | `Sources/Orchestrator.swift` | 26 |
| `Sources/OrchestratorInventory.swift` | `Sources/Orchestrator.swift` | 25 |
| `Sources/Targets.swift` | `Sources/ITerm.swift` | 25 |
| `Sources/Assistant.swift` | `Sources/Codex.swift` | 24 |
| `Sources/Orchestrator.swift` | `Sources/SessionState.swift` | 24 |
| `Sources/ProjectBoardStore.swift` | `Sources/Project.swift` | 24 |

## Complete selected file inventory

P = production, T = tests, B = build reference. Locks/held doors are raw production spellings, not held-lock correctness.

| File | Kind | Lines | Git blob | Imports | Locks / held doors |
|---|---|---:|---|---|---:|
| `Package.swift` | B | 24 | `7595f58f255b01b4d37574de5f741e0a1dcb3ecb` | PackageDescription | 0 / 0 |
| `Sources/Activity.swift` | P | 135 | `2e1382d4239e2d7ea87b2c3f049f88891aad9e0a` | Foundation | 0 / 0 |
| `Sources/Ansi.swift` | P | 182 | `55910baa6dc3ddecc35982d8360bfdf48bafa610` | AppKit | 0 / 0 |
| `Sources/Assistant.swift` | P | 501 | `29b6e618a55815e7d70a991bad978d3880289a4d` | Foundation | 0 / 0 |
| `Sources/AssistantLogo.swift` | P | 54 | `7c622e4fe0bd20671078fd4d32d9d29dbbedb4bd` | AppKit | 0 / 0 |
| `Sources/AssistantPlaceholder.swift` | P | 17 | `683110a559505cec5fa0d4559c2f5a84aa9828c7` | Foundation | 0 / 0 |
| `Sources/AssistantQuota.swift` | P | 483 | `6ee0f897048d945ea5a282f6146e2d3077ddcf78` | Foundation | 13 / 0 |
| `Sources/ClaudeSkills.swift` | P | 279 | `4afdf99a453d9e263902d1f48b8f57763e1bb22a` | Foundation | 0 / 0 |
| `Sources/ClawdlineMessage.swift` | P | 367 | `738103d8871da4bddb5840e9e64185577f857508` | Foundation | 0 / 0 |
| `Sources/ClawdlineSessionMessage.swift` | P | 106 | `856359dbdc66b727f9438d57e8de93cf1381f5d9` | Foundation | 0 / 0 |
| `Sources/CloseabilityIndex.swift` | P | 229 | `53073e8ade7088d08418f11ba374c161550c7b05` | Foundation | 0 / 0 |
| `Sources/CloudAccount.swift` | P | 1506 | `f3e26588905db36f21943bb32f83e91c1a4e2120` | Foundation, os | 7 / 0 |
| `Sources/CloudAppBridge.swift` | P | 1721 | `7c004fe64f23669e656924d5fe5d6a405f797c0b` | Foundation | 9 / 0 |
| `Sources/CloudBridgeLifecycle.swift` | P | 559 | `c7a431484626a7c759367e9dc7530a438fd02e56` | Foundation | 0 / 0 |
| `Sources/CloudCanonicalJSON.swift` | P | 544 | `b742f6f7b01f0ce35bd1ee4658e0ac0d98b40754` | Foundation | 0 / 0 |
| `Sources/CloudClock.swift` | P | 191 | `beec9e1679875409a6e96c8911506c8ec8f9e10f` | Foundation | 0 / 0 |
| `Sources/CloudCommandLedger.swift` | P | 720 | `281a04a6da972a8510050f2db2268c2df23c61a9` | Foundation | 5 / 0 |
| `Sources/CloudEnvelope.swift` | P | 375 | `6917d00660a21cb1dd6ebcbd9c00e0b275430b08` | CryptoKit, Foundation | 0 / 0 |
| `Sources/CloudHandover.swift` | P | 842 | `a5b079a8c87edb6661a2ca27ab0bb3b167cfd554` | CryptoKit, Foundation | 9 / 0 |
| `Sources/CloudKeys.swift` | P | 667 | `a683babda7c8b856d62ccf5fe8a182201ad65289` | CryptoKit, Foundation, Security, os | 3 / 0 |
| `Sources/CloudLocalRoute.swift` | P | 262 | `0b721454c84c32db020ac5985e505e4596dab5aa` | Foundation | 0 / 0 |
| `Sources/CloudOutboundSpool.swift` | P | 873 | `a462cd188df91ac0092bfea0e0ea04e262fe8f06` | Foundation | 0 / 0 |
| `Sources/CloudPairing.swift` | P | 695 | `f379052276b1ce03cf5a36f9e2327ab242c36002` | CryptoKit, Foundation | 0 / 0 |
| `Sources/CloudSettings.swift` | P | 580 | `2f41742cca8adc255eee06c7c2474a2739803724` | Foundation | 0 / 0 |
| `Sources/CloudTransport.swift` | P | 1024 | `ab4287ecfdd3993559434ce2715bdca9e14346fd` | Foundation | 7 / 0 |
| `Sources/CloudTransportFakes.swift` | P | 1046 | `3ff3573f0fa9788afdac57efbed27694775fd7f6` | CryptoKit, Foundation, Network | 43 / 0 |
| `Sources/Codex.swift` | P | 807 | `0dd9f98fc187d3c4803693b7614882bb1a281d67` | Foundation | 12 / 0 |
| `Sources/CodexNaming.swift` | P | 1387 | `9371d36c3a3be496bf009ce156f17d662a43bd73` | Darwin, Foundation | 53 / 0 |
| `Sources/CodexSkills.swift` | P | 87 | `7abc0b505f9b11688f3a41e539ddbcc2259892a3` | Foundation | 0 / 0 |
| `Sources/Compat.swift` | P | 540 | `d29a95ab105ef85ba50af72d9b7fc4c17ea5c9be` | Foundation | 6 / 0 |
| `Sources/Config.swift` | P | 1230 | `70c64aa104a943db479d367eb623c25892fe392e` | AppKit | 31 / 0 |
| `Sources/Controller.swift` | P | 4131 | `f937d5ca0ab65cb4ecd5bf66bddb4dc5a8de1cbc` | AppKit | 0 / 0 |
| `Sources/Coordinator.swift` | P | 1874 | `d36f8ff6b440aa0aacad993fc69549fbfc301e88` | Darwin, Foundation | 55 / 0 |
| `Sources/CoordinatorSuccession.swift` | P | 519 | `67ce768372644ba2da81842750cf767242b32972` | Foundation | 0 / 0 |
| `Sources/Copy+Chinese.swift` | P | 2161 | `07aef9ecb2af501f1b248b5f8fb7eed312f28b3e` | Foundation | 0 / 0 |
| `Sources/Copy+English.swift` | P | 1085 | `40c67b78658c03637334d277164a43097f057815` | Foundation | 0 / 0 |
| `Sources/Copy+French.swift` | P | 1081 | `c7f20f9312496d4d8ad8735f1b442a79ff8d781d` | Foundation | 0 / 0 |
| `Sources/Copy+German.swift` | P | 1081 | `5740ef3b417fa00626f9ba474c36537ebeca4d47` | Foundation | 0 / 0 |
| `Sources/Copy+Hindi.swift` | P | 1085 | `5b17f92b9b3cb90dab6969d778147134578c4406` | Foundation | 0 / 0 |
| `Sources/Copy+Indonesian.swift` | P | 1081 | `9b0bac48f2cb15a05860a2a4a73225c04cc03b7b` | Foundation | 0 / 0 |
| `Sources/Copy+Italian.swift` | P | 1081 | `5cce91e686617eb5100d1160638eee8d4d7a3797` | Foundation | 0 / 0 |
| `Sources/Copy+Japanese.swift` | P | 1079 | `a849de06d0ea5df719d2b47d47026350b948f216` | Foundation | 0 / 0 |
| `Sources/Copy+Korean.swift` | P | 1079 | `1f78c79f6cdb1ff2f5ddb05a86433fc06608965e` | Foundation | 0 / 0 |
| `Sources/Copy+Portuguese.swift` | P | 1084 | `05c7dd339b6977f982d1ccd06833e1a185871fbb` | Foundation | 0 / 0 |
| `Sources/Copy+Russian.swift` | P | 1081 | `63a88d7b039b65b9b31a242092fefd018592fb83` | Foundation | 0 / 0 |
| `Sources/Copy+Spanish.swift` | P | 1081 | `4cf87f98f31b06a80f0bfb877ec98badc796befc` | Foundation | 0 / 0 |
| `Sources/Copy+Turkish.swift` | P | 1081 | `a00c33aa3625636fe4376f48c313d536431a6775` | Foundation | 0 / 0 |
| `Sources/DeployWatch.swift` | P | 116 | `985168584a0bc2b992738794d9a361c05233e2fd` | Foundation | 0 / 0 |
| `Sources/DevStack.swift` | P | 539 | `be5da72e798149bf45bad987c68f90666f871d02` | Foundation | 5 / 0 |
| `Sources/DiagnosticReport.swift` | P | 192 | `8ed9a37175b52d0a2da7e801485bc0523145a0c6` | Foundation | 0 / 0 |
| `Sources/Drop.swift` | P | 205 | `5b5787ee3e3137b8c1f6740896bf795d8bce2a90` | AppKit, UniformTypeIdentifiers | 0 / 0 |
| `Sources/GitChanges.swift` | P | 224 | `c2256ef7eda5007606d9a13d99bc9ed35a4397be` | Foundation | 0 / 0 |
| `Sources/HookBridge.swift` | P | 602 | `645d2cc404f0c15a84a4687aa24404dd62cd9488` | Foundation | 0 / 0 |
| `Sources/HotKey.swift` | P | 138 | `84a11243d6d0b227d2e629b4a29e13bacf08e1cc` | AppKit, Carbon | 0 / 0 |
| `Sources/ITerm.swift` | P | 1599 | `2f6b3bf7ecb8fee257d14944cab3c63705f7f220` | Foundation | 55 / 0 |
| `Sources/LiveScreen.swift` | P | 722 | `05d47c7a10e04e76abc7e95d9a5818d491673d30` | Darwin, Foundation | 11 / 0 |
| `Sources/LocalBrowserReadiness.swift` | P | 61 | `9feac5ddc8665fdca14fb5650359f1c9c6f4627a` | Foundation, Network | 0 / 0 |
| `Sources/Log.swift` | P | 33 | `e311054bf635ce94aedc79b536520aa5fb674b9a` | Foundation | 3 / 0 |
| `Sources/Markdown.swift` | P | 490 | `954ba034f2c64b2b9545038edc3444c1e8823f19` | AppKit | 0 / 0 |
| `Sources/Mascot.swift` | P | 533 | `045947800a778a9f3437583e88d6f36047c957e8` | AppKit | 0 / 0 |
| `Sources/NotchIsland.swift` | P | 1145 | `36a036d6fe73e771b09c38af0bf3cb301dbc8aee` | AppKit, Foundation | 0 / 0 |
| `Sources/Onboarding.swift` | P | 1465 | `b05145f867cd55e2826ebb9901a59edaa0d514e8` | AppKit, Foundation | 0 / 0 |
| `Sources/Orchestrator.swift` | P | 10750 | `04c5288fa2146d7478240e15a1b8538fac7af9d3` | AppKit, CryptoKit, Foundation, Security | 398 / 9 |
| `Sources/OrchestratorChildBrief.swift` | P | 443 | `58597f68fef2ad5cb7defd86a9921931be6cc050` | Foundation | 0 / 0 |
| `Sources/OrchestratorChildIdentity.swift` | P | 334 | `6810663a140ea2062e419fbeb9085ec06802b33c` | Foundation | 8 / 0 |
| `Sources/OrchestratorDraft.swift` | P | 1361 | `e8fc2e8fb95e0bee8eea90efba0034bd3f863bef` | CryptoKit, Foundation | 0 / 0 |
| `Sources/OrchestratorHandoffSender.swift` | P | 360 | `e9b7e80affd1e069ec79bef3a98abe81e9937f66` | Foundation | 0 / 0 |
| `Sources/OrchestratorInventory.swift` | P | 447 | `44a2b0e59c6b9016d1d5a2b5738835e543ff37df` | CryptoKit, Foundation | 2 / 0 |
| `Sources/OrchestratorLandingQueue.swift` | P | 768 | `702735a91919454c498ce7e6fa40f71a37b24777` | Foundation | 15 / 0 |
| `Sources/OrchestratorLandingSweep.swift` | P | 920 | `d2641c26e68964781f324871aba29451bbe1a484` | Foundation | 5 / 0 |
| `Sources/OrchestratorPlanning.swift` | P | 737 | `bee8c085f022c52550bbe5e66fc302bc37f040c0` | Foundation | 8 / 3 |
| `Sources/OrchestratorRegistry.swift` | P | 224 | `5b60422b90f35acb5dd64a516ee0916f2484224a` | Foundation | 6 / 1 |
| `Sources/OrchestratorRootAssignmentShape.swift` | P | 205 | `7afe6710a8eb964d641f547d07abd8f3c08fe003` | Foundation | 0 / 0 |
| `Sources/OrchestratorSessionLanding.swift` | P | 312 | `122ef99bec3c0a6fe184a96b1ec1ba63688f50f5` | Foundation | 13 / 0 |
| `Sources/OrchestratorStore.swift` | P | 927 | `b9609dfae46bee47673e151ef46d157a1b0429cc` | Foundation | 0 / 0 |
| `Sources/OrchestratorTaskShape.swift` | P | 637 | `816c584ce26c1042f2c436ed398b3ed4bafd11f7` | Foundation | 0 / 0 |
| `Sources/OwnedStorage.swift` | P | 422 | `9e8e36b3634dec878bf8d78155e748c33bca6aaa` | Foundation | 7 / 0 |
| `Sources/Panel.swift` | P | 1292 | `c1effca4dea68203e6472041c41ab44f75c4d768` | AppKit | 0 / 0 |
| `Sources/Paths.swift` | P | 44 | `1ac82e31d4496f1a61816ff0d9aa61ea11571483` | Foundation | 0 / 0 |
| `Sources/Planner.swift` | P | 612 | `a4b2d3db2f0e5a977bde4fea32e465f392e901a3` | Foundation | 0 / 0 |
| `Sources/Project.swift` | P | 100 | `5be70034e076d110a6bb5c0b804012f8f33354b5` | Foundation | 0 / 0 |
| `Sources/ProjectArtifact.swift` | P | 476 | `dc6b0696a925b7da31ae3d203f2426c6ff41f966` | Foundation | 0 / 0 |
| `Sources/ProjectBoardHTTP.swift` | P | 243 | `7d19a680a1d5c0f3b0b24e1f139c27be82f7d56c` | CryptoKit, Foundation | 0 / 0 |
| `Sources/ProjectBoardIntegration.swift` | P | 701 | `db5e8f0e7c6251997ab12be67af306958f95bac3` | CryptoKit, Foundation | 16 / 0 |
| `Sources/ProjectBoardNarrative.swift` | P | 375 | `f5fafcefe7f8fc5789778d4293a54e60a50f11e4` | Foundation | 0 / 0 |
| `Sources/ProjectBoardReadCache.swift` | P | 336 | `d669d30493675591b64c01fa0ac9444c11959394` | Foundation | 17 / 0 |
| `Sources/ProjectBoardRequestCoordinator.swift` | P | 146 | `74f237a24031b1ff4ad8b82c587797b4f26b4126` | Foundation | 0 / 0 |
| `Sources/ProjectBoardStore.swift` | P | 4701 | `a56efe9cfb7a02e1a0126976f1f7ebf19a8f6621` | CryptoKit, Foundation | 32 / 0 |
| `Sources/ProjectBoardWorkflow.swift` | P | 1367 | `56cd1796abe53bce558a7ea14d868a517c92c2ae` | CoreFoundation, CryptoKit, Foundation | 46 / 0 |
| `Sources/ProjectBoardWorkflowHTTP.swift` | P | 337 | `1445c18f9718949c85cf3ff3016ad003219a42a1` | Foundation | 0 / 0 |
| `Sources/ProjectIcon.swift` | P | 292 | `7976355cf68b8ec9d22669481af96df81d2cb094` | AppKit | 0 / 0 |
| `Sources/ProjectStatus.swift` | P | 351 | `f6a26dae0ca30a44ab31de493d58021775128cb3` | Foundation | 0 / 0 |
| `Sources/ProjectTimelineGitImporter.swift` | P | 91 | `5758c47affb1cfaef9a70d81d893821e5c64dc69` | CryptoKit, Foundation | 0 / 0 |
| `Sources/ProjectTimelineHTTP.swift` | P | 121 | `03b1055f5b6e07cbcd42324f64633e3c35fc1c71` | Foundation | 0 / 0 |
| `Sources/ProjectTimelineIntegration.swift` | P | 131 | `0b33771c678918ecc82d5723eddb72c254aedd6d` | CryptoKit, Foundation | 0 / 0 |
| `Sources/ProjectTimelineModel.swift` | P | 254 | `6216413bcbaaa0125002cdef9577887cd92e034d` | Foundation | 0 / 0 |
| `Sources/ProjectTimelineReadCache.swift` | P | 186 | `5d8b6137202a1be2419efada7bce896ba8041119` | Foundation | 12 / 0 |
| `Sources/ProjectTimelineRequestCoordinator.swift` | P | 51 | `af367f46def5aae964f53cfd13418299c83df856` | Foundation | 0 / 0 |
| `Sources/ProjectTimelineStore.swift` | P | 460 | `063719871a03141df7ee1bb06e4cdc359f7fb781` | CryptoKit, Foundation | 11 / 0 |
| `Sources/QuestionSteps.swift` | P | 176 | `bd747ff18da6932f1482812fbb505c3621015fd7` | Foundation | 0 / 0 |
| `Sources/ReadingFreshness.swift` | P | 503 | `b8883c21097f6e75ec9e852cf27d6068786bdeaa` | Foundation | 7 / 0 |
| `Sources/RemoteAuth.swift` | P | 538 | `486569e184cc49264a239719276d31b52cec0550` | CommonCrypto, CryptoKit, Foundation | 41 / 0 |
| `Sources/RemoteIcon.swift` | P | 391 | `bd26d447362e135414ecb73bb8e542c9940c898e` | AppKit, Foundation | 18 / 0 |
| `Sources/RemotePage.swift` | P | 1196 | `59be6a553ed61263d0b269b7dc845d2bbb180645` | Foundation | 0 / 0 |
| `Sources/RemoteQR.swift` | P | 44 | `29e93a5ca151f23c9df598b93ea23ba6bed9dab7` | AppKit, CoreImage, Foundation | 0 / 0 |
| `Sources/RemoteServer.swift` | P | 5831 | `36ed559a9d6bf0bfd9d15b9181c0a5573c9626f3` | AppKit, Foundation, Network | 16 / 0 |
| `Sources/RemoteServerSessionLanding.swift` | P | 68 | `d3c9f9e3d36ea8727d59b801c751c7bed742cefa` | Foundation | 0 / 0 |
| `Sources/RemoteTunnel.swift` | P | 743 | `46f0a7bb33711a0e515522ee1a18510d3db878ab` | Foundation | 0 / 0 |
| `Sources/ScheduleWebhook.swift` | P | 1180 | `539533c4c0ac2fcba8bbbeeb5939ad454c8bbeaf` | CryptoKit, Darwin, Foundation, Security | 21 / 0 |
| `Sources/Schedules.swift` | P | 425 | `268c4dd7e253867f2141a42d7777d05dba1d9e77` | Foundation | 0 / 0 |
| `Sources/Scratch.swift` | P | 54 | `03e779069ce3616d3bb0dcc1370d5972f3807ceb` | Foundation | 0 / 0 |
| `Sources/SessionClosePolicy.swift` | P | 118 | `db337fbcf6f003e7480f34719c050927222bd1f3` | Foundation | 0 / 0 |
| `Sources/SessionImageArtifact.swift` | P | 579 | `4b571d4ff418e185130a3708fe0602243f9bd4b8` | AppKit, Foundation, ImageIO | 11 / 0 |
| `Sources/SessionImageMarker.swift` | P | 208 | `f045e006af67179d81774c448aead70ce5a8778d` | Foundation | 0 / 0 |
| `Sources/SessionImagePreview.swift` | P | 211 | `8e46fd5116724cadd2981885824e02a56782ff5f` | AppKit | 0 / 0 |
| `Sources/SessionInfo.swift` | P | 950 | `f92ba7dc9901f881845c50d95cde2848467eedc0` | Foundation | 6 / 0 |
| `Sources/SessionRegistry.swift` | P | 503 | `49b64fabd4eb09fd04dc7e24dca91ddbed4b4d75` | Foundation | 0 / 0 |
| `Sources/SessionState.swift` | P | 845 | `ed86f2d4e475598166f355110e079144a43fc1a4` | Foundation | 0 / 0 |
| `Sources/SessionWatch.swift` | P | 1244 | `6a2ba01a4ab725606b68de9ea12862d31ce2bfc3` | AppKit, Foundation | 5 / 0 |
| `Sources/Settings.swift` | P | 3736 | `36268ad13699bbc5387284625c06d4d5ad31728a` | AppKit, CryptoKit, UniformTypeIdentifiers | 0 / 0 |
| `Sources/Shells.swift` | P | 478 | `a161707ab7128c3c0ea439d2438ed4acbb52e20a` | Foundation | 12 / 0 |
| `Sources/SmartNotification.swift` | P | 457 | `d8f86e5ddbcf19110510de3f7df6a6a5191fe3b9` | Foundation | 16 / 0 |
| `Sources/Snippets.swift` | P | 616 | `88767e9abca5badea6eb13493080730690409b5c` | Foundation | 15 / 0 |
| `Sources/StackLog.swift` | P | 212 | `cf6197def47eb6c46a1030a085506122e08acb65` | AppKit | 0 / 0 |
| `Sources/StartPoints.swift` | P | 1274 | `bb4614171e69f326c1ae618207ee74a8461cc5d9` | AppKit, CryptoKit, Foundation | 11 / 0 |
| `Sources/StateHook.swift` | P | 513 | `f002ae8c1768a69281011f3e74f70045b9908b2c` | Foundation | 0 / 0 |
| `Sources/Strings.swift` | P | 2071 | `00bd43a378524d97762a6800e69408ae6083168d` | Foundation | 0 / 0 |
| `Sources/StructuredModelProcess.swift` | P | 83 | `9b72d6893f1c27e083f080a9f89ffdc4dffae3ce` | Darwin, Foundation | 0 / 0 |
| `Sources/Subagents.swift` | P | 427 | `9d5b88daaaf7a9f21a89b33cda09e06f8cb2c704` | Foundation | 22 / 0 |
| `Sources/Subprocess.swift` | P | 45 | `fcc873260bd2ea0908e714377837b7f8f2715d59` | Foundation | 0 / 0 |
| `Sources/Targets.swift` | P | 1104 | `2eeaf4833a15a2897b54132b571c4b7a53424470` | AppKit, Foundation | 16 / 0 |
| `Sources/Tmux.swift` | P | 1079 | `46f9d24401333fe351540f308572cd957aaad3ab` | Foundation | 0 / 0 |
| `Sources/Transcript.swift` | P | 2104 | `6739c64c4e1e84a808c35b412e6ef880151857ed` | AppKit, CoreFoundation | 19 / 0 |
| `Sources/TranscriptReadCoordinator.swift` | P | 122 | `8c8e5f555c5d08d59f04255b14847c82aa1e1116` | Foundation | 0 / 0 |
| `Sources/TranscriptRevisionWatch.swift` | P | 180 | `50e310a83512073d0ba40ead54a3db257caf4558` | Darwin, Foundation | 0 / 0 |
| `Sources/UpdateCheck.swift` | P | 521 | `b7479976327762350170d9dfff4afcadf713e509` | Foundation | 9 / 0 |
| `Sources/UsageFeatureAttribution.swift` | P | 479 | `d0cb427a2f0ca9fb2a468d26374590fc48c5a18a` | Foundation | 9 / 0 |
| `Sources/UsageFeatureClassifier.swift` | P | 442 | `c414c713b8c252456b6edd6874567daa1a479cfa` | CryptoKit, Foundation | 0 / 0 |
| `Sources/UsageLedger.swift` | P | 5941 | `4f4efef8648bc0e074b4ebb8184a4dd7371167d5` | CryptoKit, Foundation, SQLite3 | 16 / 0 |
| `Sources/VerificationLedgerRoute.swift` | P | 560 | `2703f6e2b6ba077093d651eb86a35f6c628fd6c6` | Foundation | 0 / 0 |
| `Sources/VerificationRunLedger.swift` | P | 877 | `748babd123cea628299f70e880d82754b99fb9c4` | CryptoKit, Foundation, SQLite3 | 0 / 0 |
| `Sources/Voice.swift` | P | 633 | `21c72bee512220d9162d08576bf5bcbb9b323fa2` | AVFoundation, Speech | 7 / 0 |
| `Sources/WebPush.swift` | P | 826 | `7c4c3412f3bac28466ef347e0e99c220912c8c69` | CryptoKit, Foundation | 30 / 0 |
| `Sources/Whisper.swift` | P | 501 | `a3a920bde39ff0e0b3a65dc722c3581e564da091` | AVFoundation, Foundation | 3 / 0 |
| `Sources/WindowChrome.swift` | P | 616 | `a986f9a8d5707cdf673d7f43fa0e241b2a109c86` | AppKit | 0 / 0 |
| `Sources/main.swift` | P | 597 | `5a7551d5519034826d39ef649dbca3254d6dfb7d` | AppKit, ServiceManagement | 0 / 0 |
| `Tests/BackgroundAndStorageTests.swift` | T | 1544 | `919ed31479b3af0369508389b5428ff12d77e6c3` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/CloudAccountTests.swift` | T | 1791 | `1339673e43e2ae18e4162b862d6d7f7565ff51ec` | Darwin, Foundation | 0 / 0 |
| `Tests/CloudAppBridgeTests.swift` | T | 2000 | `2df150893cd51de396a834a95fa876679c1c4eb4` | AppKit, Foundation | 0 / 0 |
| `Tests/CloudCanonicalJSONTests.swift` | T | 302 | `933904153de87af792f01cca96ba3de7fd2f4b94` | Foundation | 0 / 0 |
| `Tests/CloudClockTests.swift` | T | 263 | `d8b0c6c591ff4315d6d8c129b2a2c195ba4d315d` | Foundation | 0 / 0 |
| `Tests/CloudCommandLedgerTests.swift` | T | 1024 | `521ddecae031964a252b85fa03532409e38ee371` | Foundation | 0 / 0 |
| `Tests/CloudEnvelopeTests.swift` | T | 180 | `894b0e29bd43c38bbf88f7354ee8363ccf68facc` | CryptoKit, Foundation, Security | 0 / 0 |
| `Tests/CloudLifecycleTests.swift` | T | 1426 | `120787e11e80a152503606667e0402385b053994` | CryptoKit, Foundation | 0 / 0 |
| `Tests/CloudOutboundSpoolTests.swift` | T | 1292 | `f20387d19db4f5838335e76cabd8f97e5b341fb4` | Foundation | 0 / 0 |
| `Tests/CloudPairingTests.swift` | T | 745 | `0340c6922731cb45555d6fe8da1148827cb9b346` | CryptoKit, Foundation | 0 / 0 |
| `Tests/CloudSettingsTests.swift` | T | 884 | `b40bc572302de3b2b794e3be0ff7b8fd52d67483` | Foundation | 0 / 0 |
| `Tests/CloudTestRunner.swift` | T | 255 | `045278c9e46cc18448443eef68636082131c65e5` | Foundation | 0 / 0 |
| `Tests/CloudTransportTests.swift` | T | 1212 | `438edeac3ce0ec819ff7d5ce21cefd91521d06a7` | Foundation | 0 / 0 |
| `Tests/CodexSessionTests.swift` | T | 1802 | `035a612776c5b2b3f319a39c480b9e73e9212533` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/ConversationTests.swift` | T | 479 | `89d14ecac98448affeab0bfa8c1660578ba1bf73` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/CoordinatorTests.swift` | T | 2000 | `6dc69075e2ef8c2d56e5d2a65458220ff1ef2908` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/DevStackTests.swift` | T | 481 | `857fb84abba41dbbd306d9f65f936a6fd94cdbd1` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/HookTests.swift` | T | 1453 | `6febad8d7c602f7ca2d1e64f19530778221ce4d4` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/LandingCurrencyTests.swift` | T | 1096 | `6da143a3503cd6dcbcf6d48c5fb3122fbdb5d4f0` | Foundation | 0 / 0 |
| `Tests/MarkdownTests.swift` | T | 1990 | `f84d0af2bf26e2c00b8b02f9eaf14b938b74ecf5` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/MascotTests.swift` | T | 841 | `207da21e56106737080670c6be0bb56e9c28cbc2` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/NotificationAddressTests.swift` | T | 148 | `6d97a13e55bc45921cdeca162a3358dd0bd85590` | AppKit, Foundation | 0 / 0 |
| `Tests/OrchestratorCompletionTests.swift` | T | 1112 | `73a064f19f182222ec6273164f2891e91e156406` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/OrchestratorCoordinationTests.swift` | T | 1976 | `4500fccae2dbb86325fb56dbc101ab86ae578ce8` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/OrchestratorDispatchTests.swift` | T | 1887 | `24222391bcb90ea45c490181eedee1d471dc5af5` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/OrchestratorDraftTests.swift` | T | 654 | `3cddc63388a36bc02ccb75c700727e9d3ba4cc81` | Foundation | 0 / 0 |
| `Tests/OrchestratorLandingQueueTests.swift` | T | 904 | `bbf346b6fb5390895d164e80f2f5e357ecccb6a5` | AppKit, Foundation | 0 / 0 |
| `Tests/OrchestratorLandingTests.swift` | T | 1961 | `7bf87824ff201dcabec5037c35b8ccb428c5e357` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/OrchestratorLifecycleTests.swift` | T | 1186 | `fdf0f3007846a0eb807a9f1d3234e013eac7c633` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/OrchestratorRecoveryTests.swift` | T | 1548 | `abc0db05c224fcacf6b20f5ac0e999ed42b1bc3d` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/OrchestratorRegistryTests.swift` | T | 273 | `bc05b8eb4b5912e71aa4eea161d7d3a5cfca998d` | Foundation | 0 / 0 |
| `Tests/OrchestratorStoreTests.swift` | T | 901 | `e83640ccfde99f008f41d8b94b8dff96449155c5` | Foundation | 0 / 0 |
| `Tests/PeerMessageTests.swift` | T | 414 | `a6bbd9e556aef3ab0fff083232c48ab411539891` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/PlannerTests.swift` | T | 1961 | `c66409e50fe3ad0fb135d09642a7afb8cd7aae39` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/ProjectBoardIntegrationTests.swift` | T | 1148 | `cd04e6b93923fa04986ed734c488c6d2387fd6fc` | Foundation | 0 / 0 |
| `Tests/ProjectBoardNarrativeTests.swift` | T | 338 | `794576fed1e04b9ba911727cdd0dea2e9bd23e20` | Foundation | 0 / 0 |
| `Tests/ProjectBoardTests.swift` | T | 1981 | `8a040280f989d3dc36ea9470237728a75b947251` | Foundation | 0 / 0 |
| `Tests/ProjectBoardWorkflowPresentationTests.swift` | T | 57 | `9ac4b8311d907b9ca3935dd4a6a3157678c21a57` | AppKit, Foundation | 0 / 0 |
| `Tests/ProjectBoardWorkflowTests.swift` | T | 1187 | `fb94bc6bf11e320940e33f7e9b171e2a5a9b73b7` | Foundation | 0 / 0 |
| `Tests/ProjectDocumentsTests.swift` | T | 288 | `5c0f62aaa47b1eaba50cb5f67d234bea6cf38b6b` | Foundation | 0 / 0 |
| `Tests/ProjectRunTests.swift` | T | 243 | `4ff542aa309b463e5cf3a7d1cedf5144228a46d2` | Foundation | 0 / 0 |
| `Tests/ProjectTimelineTests.swift` | T | 428 | `e18af82cf151d4fcddea3d6003e2ee03eb92a178` | Foundation | 0 / 0 |
| `Tests/ReadingFreshnessTests.swift` | T | 355 | `641effee1e3ccbeec4b686dabaa030b851f8ffd4` | Foundation | 0 / 0 |
| `Tests/RootAssignmentCoordinationTests.swift` | T | 421 | `b80a93a92c44ce41cf2dcd4eace1870bf81fc418` | Foundation | 0 / 0 |
| `Tests/ScheduleResumeTests.swift` | T | 181 | `1ee7a8f215a29946e66c943f76e694773bf38a37` | Foundation | 0 / 0 |
| `Tests/ScheduleWebhookTests.swift` | T | 703 | `94f0f0961b9a0ead33135aae93f049a4291aaa54` | CryptoKit, Foundation | 0 / 0 |
| `Tests/ScheduledDispatchTests.swift` | T | 1999 | `74cad11e71b371f85ee521f37c7a97f356d5ba62` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/SessionCloseAndQuotaTests.swift` | T | 1707 | `dcc166f1a8eaffb358d5bf63a51c66aaee8f1e7f` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/SessionCloseabilityTests.swift` | T | 1003 | `a8529771b2a9b877e9453762872050957875661c` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/SessionImageMarkerTests.swift` | T | 468 | `16a3a423e469b0f5afab7bf52194e8e0b48c66a4` | AppKit, Foundation | 0 / 0 |
| `Tests/SessionLaunchTests.swift` | T | 1691 | `05a14470d81e462597b925c1a9e20fe8cdf31864` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/SessionRegistryTests.swift` | T | 742 | `54f9e1dbbb7602945f109460bd0be406e68748b4` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/SessionWatchTests.swift` | T | 1445 | `8c2dffa9c287a7f4fda6f0b92a46e065ffd96caa` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/SessionWorkStateTests.swift` | T | 119 | `2dde0cac23fa09d81de2313dfa478942c6690ffc` | Foundation | 0 / 0 |
| `Tests/SnippetStoreTests.swift` | T | 428 | `14011d22ad0eb85319d7bfd7ec0cd97b2572a14f` | Foundation | 0 / 0 |
| `Tests/TestGroupManifest.swift` | T | 641 | `3131fe27930ecb3f007211df4a6726ebdc0373a4` | Foundation | 0 / 0 |
| `Tests/TestHarness.swift` | T | 69 | `f0827c80532b5d5220a75e296be48c9d40642f09` | AppKit, Foundation | 0 / 0 |
| `Tests/TestIsolation.swift` | T | 154 | `4ac05776fd3e92c56dc6f7c5cae81b4048ed9934` | Foundation | 0 / 0 |
| `Tests/TestProcessProbes.swift` | T | 77 | `9c6ece8378c7957dab7171ff1cf66462ed35003e` | Foundation | 0 / 0 |
| `Tests/TmuxCoexistenceTests.swift` | T | 1252 | `2eae4d06a0a7a4407ed48c2001b4e44fd593a5f3` | Foundation | 0 / 0 |
| `Tests/TranscriptTests.swift` | T | 2000 | `17ca1a0ede9a4d08b67129bd656de01c13ddcd54` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/UsageLedgerTests.swift` | T | 1994 | `b09f1fbfeec50a3c4685d72aea0d246daf37f0bf` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/UsagePortfolioAndLifecycleTests.swift` | T | 2000 | `cc6389d1e3a3fc9a15d11f12879794dcde2a473b` | AppKit, Carbon, Foundation, SQLite3 | 0 / 0 |
| `Tests/UsageProjectWorktreeTests.swift` | T | 662 | `a56e1f587ea15ea5d9c7e87b6a8daf4ddf8a3dfb` | Foundation | 0 / 0 |
| `Tests/VerificationLedgerTests.swift` | T | 412 | `be153eba497e190552d2769a88cf37ff87d9199f` | Foundation | 0 / 0 |
| `Tests/VerificationRunLedgerTests.swift` | T | 423 | `a45c4f4a745f033df0b99f965ce4206bc552be1d` | Foundation, SQLite3 | 0 / 0 |
| `Tests/main.swift` | T | 58 | `a77763c46a11511c465d10fe7514b026494a8a9e` | Foundation | 0 / 0 |
| `build.sh` | B | 1872 | `87cd3362088f379fac238f93ac90265674819779` | — | 0 / 0 |
| `test.sh` | B | 2328 | `8e75e05079c50c2f8095f65f0d25d9bb595d821a` | — | 0 / 0 |
| `tools/check-architecture-boundaries.sh` | B | 945 | `de2c6466653188728fb062e65a2d677f5c0fc8c4` | — | 0 / 0 |
| `tools/swift-source-manifest.sh` | B | 269 | `fe02cb1bd0ff96a72cfb0f1b5bd181de6eb20ed2` | — | 0 / 0 |
