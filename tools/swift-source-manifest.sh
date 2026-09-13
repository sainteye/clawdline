#!/bin/bash
# One deterministic Swift source inventory shared by the app build and test build.

clawdline_production_sources=(
  Sources/Activity.swift
  Sources/Ansi.swift
  Sources/Assistant.swift
  Sources/AssistantInstallation.swift
  Sources/AssistantLogo.swift
  Sources/AssistantPlaceholder.swift
  Sources/AssistantQuota.swift
  Sources/ClaudeSkills.swift
  Sources/ClawdlineMessage.swift
  Sources/ClawdlineSessionMessage.swift
  Sources/CloseabilityIndex.swift
  Sources/CloudAccount.swift
  Sources/CloudAppBridge.swift
  Sources/CloudBridgeLifecycle.swift
  Sources/CloudCanonicalJSON.swift
  Sources/CloudClock.swift
  Sources/CloudCommandLedger.swift
  Sources/CloudDurableStores.swift
  Sources/CloudEnvelope.swift
  Sources/CloudHandover.swift
  Sources/CloudKeys.swift
  Sources/CloudLocalRoute.swift
  Sources/CloudOutboundSpool.swift
  Sources/CloudPairing.swift
  Sources/CloudSettings.swift
  Sources/CloudStatus.swift
  Sources/CloudTransport.swift
  Sources/CloudTransportFakes.swift
  Sources/Codex.swift
  Sources/CodexNaming.swift
  Sources/StructuredModelProcess.swift
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
  Sources/DiagnosticReport.swift
  Sources/DevStack.swift
  Sources/Drop.swift
  Sources/GitChanges.swift
  Sources/HookBridge.swift
  Sources/HostPorts.swift
  Sources/HTTPReliability.swift
  Sources/HotKey.swift
  Sources/ITerm.swift
  Sources/LiveScreen.swift
  Sources/LocalBrowserReadiness.swift
  Sources/Log.swift
  Sources/MacHostAdapters.swift
  Sources/Markdown.swift
  Sources/Mascot.swift
  Sources/NotchIsland.swift
  Sources/Onboarding.swift
  Sources/Orchestrator.swift
  Sources/OrchestratorDraft.swift
  Sources/OrchestratorChildBrief.swift
  Sources/OrchestratorChildIdentity.swift
  Sources/OrchestratorTaskShape.swift
  Sources/OrchestratorHandoffSender.swift
  Sources/OrchestratorInventory.swift
  Sources/OrchestratorLandingQueue.swift
  Sources/OrchestratorLandingSweep.swift
  Sources/OrchestratorPlanning.swift
  Sources/OrchestratorPersistence.swift
  Sources/OrchestratorRegistry.swift
  Sources/OrchestratorResultFinalizer.swift
  Sources/OrchestratorRootAssignmentShape.swift
  Sources/OrchestratorSessionLanding.swift
  Sources/OrchestratorStore.swift
  Sources/OrchestratorEventPublisher.swift
  Sources/OwnedStorage.swift
  Sources/Panel.swift
  Sources/Paths.swift
  Sources/Planner.swift
  Sources/Project.swift
  Sources/ProjectArtifact.swift
  Sources/ProjectBoardProgramPlan.swift
  Sources/ProjectBoardStore.swift
  Sources/ProjectBoardNarrative.swift
  Sources/ProjectBoardIntegration.swift
  Sources/ProjectBoardReadCache.swift
  Sources/ProjectBoardRequestCoordinator.swift
  Sources/ProjectBoardHTTP.swift
  Sources/ProjectBoardWorkflow.swift
  Sources/ProjectBoardWorkflowHTTP.swift
  Sources/ProjectTimelineGitImporter.swift
  Sources/ProjectTimelineHTTP.swift
  Sources/ProjectTimelineIntegration.swift
  Sources/ProjectTimelineModel.swift
  Sources/ProjectTimelineReadCache.swift
  Sources/ProjectTimelineRequestCoordinator.swift
  Sources/ProjectTimelineStore.swift
  Sources/ProjectWorktreeHTTP.swift
  Sources/ProjectWorktreeLifecycle.swift
  Sources/ProjectIcon.swift
  Sources/ProjectRootPolicy.swift
  Sources/ProjectStatus.swift
  Sources/ProviderLifecyclePolicy.swift
  Sources/ReadingFreshness.swift
  Sources/QuestionSteps.swift
  Sources/RemoteAuth.swift
  Sources/RemoteIcon.swift
  Sources/RemotePage.swift
  Sources/RemoteQR.swift
  Sources/RemoteServer.swift
  Sources/RemoteServerSessionLanding.swift
  Sources/RemoteTunnel.swift
  Sources/Schedules.swift
  Sources/ScheduleService.swift
  Sources/ScheduleWebhook.swift
  Sources/Scratch.swift
  Sources/SessionClosePolicy.swift
  Sources/SessionImageArtifact.swift
  Sources/SessionImageMarker.swift
  Sources/SessionImagePreview.swift
  Sources/SessionInfo.swift
  Sources/SessionLaunchPolicy.swift
  Sources/SessionRegistry.swift
  Sources/SessionState.swift
  Sources/SessionWatch.swift
  Sources/Settings.swift
  Sources/Shells.swift
  Sources/SmartNotification.swift
  Sources/Snippets.swift
  Sources/StackLog.swift
  Sources/StartPoints.swift
  Sources/StateHook.swift
  Sources/Strings.swift
  Sources/Subagents.swift
  Sources/Subprocess.swift
  Sources/Targets.swift
  Sources/TerminalCommandScheduler.swift
  Sources/Tmux.swift
  Sources/Transcript.swift
  Sources/TranscriptReadCoordinator.swift
  Sources/TranscriptRevisionWatch.swift
  Sources/UpdateCheck.swift
  Sources/UsageFeatureAttribution.swift
  Sources/UsageFeatureClassifier.swift
  Sources/UsageLedger.swift
  Sources/VerificationRunLedger.swift
  Sources/VerificationLedgerRoute.swift
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
  Tests/CloudCommandRefusalTests.swift
  Tests/CloudCanonicalJSONTests.swift
  Tests/CloudClockTests.swift
  Tests/CloudCommandLedgerTests.swift
  Tests/CloudEnvelopeTests.swift
  Tests/CloudLifecycleTests.swift
  Tests/CloudOutboundSpoolTests.swift
  Tests/CloudPairingTests.swift
  Tests/CloudSettingsTests.swift
  Tests/CloudTestRunner.swift
  Tests/CloudTransparencyTests.swift
  Tests/CloudTransportTests.swift
  Tests/CodexSessionTests.swift
  Tests/ConversationTests.swift
  Tests/CoordinatorTests.swift
  Tests/DevStackTests.swift
  Tests/HookTests.swift
  Tests/HostPortsTests.swift
  Tests/LandingCurrencyTests.swift
  Tests/MarkdownTests.swift
  Tests/MascotTests.swift
  Tests/NotificationAddressTests.swift
  Tests/OrchestratorCompletionTests.swift
  Tests/OrchestratorCoordinationTests.swift
  Tests/OrchestratorDispatchTests.swift
  Tests/OrchestratorDraftTests.swift
  Tests/OrchestratorLandingQueueTests.swift
  Tests/OrchestratorLandingTests.swift
  Tests/OrchestratorLifecycleTests.swift
  Tests/OrchestratorRecoveryTests.swift
  Tests/OrchestratorPersistenceTests.swift
  Tests/OrchestratorRegistryTests.swift
  Tests/OrchestratorStoreTests.swift
  Tests/PeerMessageTests.swift
  Tests/PlannerTests.swift
  Tests/ProjectDocumentsTests.swift
  Tests/ProjectRunTests.swift
  Tests/ReadingFreshnessTests.swift
  Tests/RootAssignmentCoordinationTests.swift
  Tests/ScheduleResumeTests.swift
  Tests/ScheduledDispatchTests.swift
  Tests/ScheduleWebhookTests.swift
  Tests/SessionCloseAndQuotaTests.swift
  Tests/SessionCloseabilityTests.swift
  Tests/SessionImageMarkerTests.swift
  Tests/SessionLaunchTests.swift
  Tests/SessionRegistryTests.swift
  Tests/SessionWorkStateTests.swift
  Tests/SessionWatchTests.swift
  Tests/SnippetStoreTests.swift
  Tests/TestGroupManifest.swift
  Tests/TestHarness.swift
  Tests/TestIsolation.swift
  Tests/TestProcessProbes.swift
  Tests/TmuxCoexistenceTests.swift
  Tests/TranscriptTests.swift
  Tests/UsageLedgerTests.swift
  Tests/UsagePortfolioAndLifecycleTests.swift
  Tests/UsageProjectWorktreeTests.swift
  Tests/ProjectBoardTests.swift
  Tests/ProjectBoardIntegrationTests.swift
  Tests/ProjectBoardNarrativeTests.swift
  Tests/ProjectBoardWorkflowPresentationTests.swift
  Tests/ProjectBoardWorkflowTests.swift
  Tests/ProjectTimelineTests.swift
  Tests/VerificationLedgerTests.swift
  Tests/VerificationRunLedgerTests.swift
  Tests/W2ApplicationOwnershipTests.swift
  Tests/W2CommandAdmissionTests.swift
  Tests/ProjectWorktreeLifecycleTests.swift
  Tests/main.swift
)

clawdline_library_sources=()
for clawdline_source in "${clawdline_production_sources[@]}"; do
  if [ "$clawdline_source" != "Sources/main.swift" ]; then
    clawdline_library_sources+=("$clawdline_source")
  fi
done
unset clawdline_source

# Conservative compile-resource closure for the opt-in test artifact. Resources are also read
# at runtime, but hashing the entire tree prevents a newly compile-relevant file being omitted.
# The helper and manifest are inputs themselves: changing the recipe cannot reuse its old output.
clawdline_swift_test_target=arm64-apple-macos13.0
clawdline_swift_test_compile_resources=(
  tools/swift-source-manifest.sh
  tools/swift-test-artifact.sh
  test.sh
  Resources
)

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
