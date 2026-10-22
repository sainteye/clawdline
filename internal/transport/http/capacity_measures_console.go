package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// consoleCapacityMeasures measures every row of the register's "console" area
// (capacity.AreaOf), one reading per row.
func (s *Server) consoleCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"console.assistant_facts_read_milliseconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console inventory read deadline; daemon cannot measure another device's pending read"}
		},
		// The connection-health page's bounds live in each open operator tab.
		capacity.ConsoleHealthUIRequestSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIBudgetSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIRefreshSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIParallelReads: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIReadsPerLoad: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIPageRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIEventPages: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIExportBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIIDCharacters: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIIncidentPages: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIProbeTargets: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIProbeHosts: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIProbeClaims: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIInvestigations: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIInvestigationRefs: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUITrendLanes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUITrendSignals: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIIncidentRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIIncidentExportBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		capacity.ConsoleHealthUIIncidentWindowSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console operator tab bound; daemon cannot measure another device's health page"}
		},
		// The browser connection-health producer's bounds live in each signed-in page that turned
		// it on; the page counts its own drops (ViewerHealthProducer.status), the daemon sees none.
		capacity.ConsoleViewerHealthBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthBatchRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthBufferRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthSendEverySeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthCoalesceSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthHourlyRequests: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthHourlyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthAttempts: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthBackoffSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthRetryAfterSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthRequestSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthElapsedSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		capacity.ConsoleViewerHealthCountRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser connection-health producer bound; the daemon cannot read another device's page"}
		},
		// Settings → Remote's owner-diagnostics block reads on its own cadence in each open window.
		capacity.ConsoleOwnerRetentionPollSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Settings window cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleOwnerRetentionRequestSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Settings window read deadline; daemon cannot measure another window's wait"}
		},
		"console.carrier_receive_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser encoded carrier queue; daemon cannot measure another device's held bytes"}
		},
		"console.carrier_receive_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser authentication queue; daemon cannot measure another device's pending envelopes"}
		},
		capacity.ConsoleExecutionRecoveryAttempts: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console opening recovery guard; daemon cannot measure another device's recovery attempts"}
		},
		// The console's transcript cadence lives in each open tab; the
		// daemon serves the reads and cannot count a tab's timers.
		capacity.ConsoleTranscriptSafetySeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleTranscriptFollowFast: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleTranscriptFollowLater: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleHealthLive: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleSessionNotesSafety: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleTranscriptFollowSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleTranscriptFillPages: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console opening history fill budget; daemon cannot measure another device’s reads"}
		},
		capacity.ConsoleTranscriptMergedRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab guard; daemon cannot measure a tab's merged page"}
		},
		capacity.ConsoleTranscriptBackoffSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser retries"}
		},
		capacity.ConsoleRelayTranscriptExpectReask: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console relay reader cadence; daemon cannot measure another device's reads"}
		},
		capacity.ConsoleRelayFeatureWait: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console relay reader wait for this machine's own word list; daemon cannot measure another device's reads"}
		},
		capacity.ConsoleStatusGapRetry: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console retry of a missing Cloud status row; daemon cannot measure another device's reads"}
		},
		capacity.ConsolePersonaRetry: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console retry of a transient role catalog failure; daemon cannot measure another device's reads"}
		},
		capacity.ConsoleStatusMarkerRetry: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console retry of a missing Cloud status inventory; daemon cannot measure another device's reads"}
		},
		capacity.ConsolePinnedListReadTimeout: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console pinned machine list read deadline; daemon cannot measure another device's wait"}
		},
		capacity.ConsoleNativePairingSaveSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "macOS shell's deadline for saving a pairing's permission; the daemon answers the save but does not see the shell's timer"}
		},
		capacity.ConsoleStatusMarkerRecoveryWait: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console retained inventory replay deadline; daemon cannot measure another device's wait"}
		},
		capacity.ConsoleOpenedRowRestate: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console relay reader cadence for the opened Session's own row; daemon cannot measure another device's reads"}
		},
		capacity.ConsoleOpenedRowQuiet: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab wait before the pane says the row has not come; daemon cannot measure live browser timers"}
		},
		capacity.ConsoleDirectHistoryHours: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser direct-attempt record in that browser's storage; daemon cannot read another device's storage"}
		},
		capacity.ConsoleDirectHistoryMachines: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-browser direct-attempt record in that browser's storage; daemon cannot read another device's storage"}
		},
		capacity.ConsoleMachineListPoll: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab cadence; daemon cannot measure live browser timers"}
		},
	}
}
