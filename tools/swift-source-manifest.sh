#!/bin/bash
# One deterministic Swift source inventory shared by the app build and test build.

clawdline_production_sources=(
  Sources/Activity.swift
  Sources/Ansi.swift
  Sources/Assistant.swift
  Sources/AssistantLogo.swift
  Sources/AssistantPlaceholder.swift
  Sources/AssistantQuota.swift
  Sources/ClaudeSkills.swift
  Sources/ClawdlineMessage.swift
  Sources/ClawdlineSessionMessage.swift
  Sources/CloudAccount.swift
  Sources/CloudAppBridge.swift
  Sources/CloudBridgeLifecycle.swift
  Sources/CloudCanonicalJSON.swift
  Sources/CloudClock.swift
  Sources/CloudCommandLedger.swift
  Sources/CloudEnvelope.swift
  Sources/CloudHandover.swift
  Sources/CloudKeys.swift
  Sources/CloudOutboundSpool.swift
  Sources/CloudPairing.swift
  Sources/CloudSettings.swift
  Sources/CloudTransport.swift
  Sources/CloudTransportFakes.swift
  Sources/Codex.swift
  Sources/CodexNaming.swift
  Sources/CodexSkills.swift
  Sources/Compat.swift
  Sources/Config.swift
  Sources/Controller.swift
  Sources/Coordinator.swift
  Sources/CoordinatorSuccession.swift
  Sources/Copy+Chinese.swift
  Sources/Copy+English.swift
  Sources/Copy+French.swift
  Sources/Copy+German.swift
  Sources/Copy+Hindi.swift
  Sources/Copy+Indonesian.swift
  Sources/Copy+Italian.swift
  Sources/Copy+Japanese.swift
  Sources/Copy+Korean.swift
  Sources/Copy+Portuguese.swift
  Sources/Copy+Russian.swift
  Sources/Copy+Spanish.swift
  Sources/Copy+Turkish.swift
  Sources/DeployWatch.swift
  Sources/DevStack.swift
  Sources/Drop.swift
  Sources/GitChanges.swift
  Sources/HookBridge.swift
  Sources/HotKey.swift
  Sources/ITerm.swift
  Sources/Log.swift
  Sources/Markdown.swift
  Sources/Mascot.swift
  Sources/NotchIsland.swift
  Sources/Onboarding.swift
  Sources/Orchestrator.swift
  Sources/OrchestratorPlanning.swift
  Sources/OwnedStorage.swift
  Sources/Panel.swift
  Sources/Paths.swift
  Sources/Planner.swift
  Sources/Project.swift
  Sources/ProjectArtifact.swift
  Sources/ProjectIcon.swift
  Sources/ProjectStatus.swift
  Sources/ReadingFreshness.swift
  Sources/RemoteAuth.swift
  Sources/RemoteIcon.swift
  Sources/RemoteQR.swift
  Sources/RemoteServer.swift
  Sources/RemoteTunnel.swift
  Sources/Scratch.swift
  Sources/SessionClosePolicy.swift
  Sources/SessionImageArtifact.swift
  Sources/SessionImagePreview.swift
  Sources/SessionInfo.swift
  Sources/ScreenFollow.swift
  Sources/ScreenTail.swift
  Sources/SessionRegistry.swift
  Sources/SessionState.swift
  Sources/SessionWatch.swift
  Sources/Settings.swift
  Sources/Shells.swift
  Sources/SmartNotification.swift
  Sources/StackLog.swift
  Sources/StartPoints.swift
  Sources/StateHook.swift
  Sources/Strings.swift
  Sources/Subagents.swift
  Sources/Subprocess.swift
  Sources/Targets.swift
  Sources/Tmux.swift
  Sources/Transcript.swift
  Sources/TranscriptReadCoordinator.swift
  Sources/TranscriptRevisionWatch.swift
  Sources/UsageLedger.swift
  Sources/Voice.swift
  Sources/WebPush.swift
  Sources/Whisper.swift
  Sources/WindowChrome.swift
  Sources/main.swift
)

clawdline_test_sources=(
  Tests/BackgroundAndStorageTests.swift
  Tests/CloudAccountTests.swift
  Tests/CloudAppBridgeTests.swift
  Tests/CloudCanonicalJSONTests.swift
  Tests/CloudClockTests.swift
  Tests/CloudCommandLedgerTests.swift
  Tests/CloudEnvelopeTests.swift
  Tests/CloudLifecycleTests.swift
  Tests/CloudOutboundSpoolTests.swift
  Tests/CloudPairingTests.swift
  Tests/CloudSettingsTests.swift
  Tests/CloudTestRunner.swift
  Tests/CloudTransportTests.swift
  Tests/CodexSessionTests.swift
  Tests/ConversationTests.swift
  Tests/CoordinatorTests.swift
  Tests/DevStackTests.swift
  Tests/HookTests.swift
  Tests/MarkdownTests.swift
  Tests/MascotTests.swift
  Tests/OrchestratorCompletionTests.swift
  Tests/OrchestratorCoordinationTests.swift
  Tests/OrchestratorDispatchTests.swift
  Tests/OrchestratorLandingTests.swift
  Tests/OrchestratorLifecycleTests.swift
  Tests/OrchestratorRecoveryTests.swift
  Tests/PeerMessageTests.swift
  Tests/PlannerTests.swift
  Tests/ReadingFreshnessTests.swift
  Tests/RootAssignmentCoordinationTests.swift
  Tests/ScheduleResumeTests.swift
  Tests/ScheduledDispatchTests.swift
  Tests/SessionCloseAndQuotaTests.swift
  Tests/SessionCloseabilityTests.swift
  Tests/SessionLaunchTests.swift
  Tests/ScreenTailTests.swift
  Tests/SessionRegistryTests.swift
  Tests/SessionWatchTests.swift
  Tests/TestGroupManifest.swift
  Tests/TestHarness.swift
  Tests/TestIsolation.swift
  Tests/TestProcessProbes.swift
  Tests/TranscriptTests.swift
  Tests/UsageLedgerTests.swift
  Tests/UsagePortfolioAndLifecycleTests.swift
  Tests/main.swift
)

clawdline_library_sources=()
for clawdline_source in "${clawdline_production_sources[@]}"; do
  if [ "$clawdline_source" != "Sources/main.swift" ]; then
    clawdline_library_sources+=("$clawdline_source")
  fi
done
unset clawdline_source

verify_swift_source_manifest() {
  local mode="${1:-full}"
  local manifest_tmp_root="${TMPDIR:-/tmp}"
  local expected_production actual_production expected_tests actual_tests
  case "$mode" in
    production|full) ;;
    *)
      echo "unknown Swift source manifest verification mode: $mode" >&2
      return 2
      ;;
  esac

  expected_production=$(mktemp "$manifest_tmp_root/clawdline-expected-production.XXXXXX")
  actual_production=$(mktemp "$manifest_tmp_root/clawdline-actual-production.XXXXXX")
  printf '%s\n' "${clawdline_production_sources[@]}" | LC_ALL=C sort > "$expected_production"
  find Sources -type f -name '*.swift' -print | LC_ALL=C sort > "$actual_production"
  if ! diff -u "$expected_production" "$actual_production"; then
    echo "Swift production source manifest differs from Sources/ on disk" >&2
    rm -f "$expected_production" "$actual_production"
    return 1
  fi

  rm -f "$expected_production" "$actual_production"
  if [ "$mode" = "production" ]; then
    echo "Swift source manifest: ${#clawdline_production_sources[@]} production files"
    return 0
  fi

  expected_tests=$(mktemp "$manifest_tmp_root/clawdline-expected-tests.XXXXXX")
  actual_tests=$(mktemp "$manifest_tmp_root/clawdline-actual-tests.XXXXXX")
  printf '%s\n' "${clawdline_test_sources[@]}" | LC_ALL=C sort > "$expected_tests"
  find Tests -type f -name '*.swift' -print | LC_ALL=C sort > "$actual_tests"
  if ! diff -u "$expected_tests" "$actual_tests"; then
    echo "Swift test source manifest differs from Tests/ on disk" >&2
    rm -f "$expected_tests" "$actual_tests"
    return 1
  fi
  rm -f "$expected_tests" "$actual_tests"
  echo "Swift source manifest: ${#clawdline_production_sources[@]} production, ${#clawdline_test_sources[@]} test files"
}
