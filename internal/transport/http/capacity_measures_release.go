package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// releaseCapacityMeasures measures every row of the register's "release" area
// (capacity.AreaOf), one reading per row.
func (s *Server) releaseCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.ReleaseMetadataConcurrency: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-verification guard: one manifest and one signature read; no retained queue"}
		},
		capacity.ReleaseManifestBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read guard on one release manifest; no retained buffer"}
		},
		capacity.ReleaseSignatureBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read guard on one manifest signature list; no retained buffer"}
		},
		capacity.ReleaseCheckIntervalSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseCheckJitterSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseFetchTimeoutSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseDownloadTimeoutSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseArtifactBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a per-read guard of the release updater; no retained buffer"}
		},
		capacity.ReleaseListBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a per-read guard of the release updater; no retained buffer"}
		},
		capacity.ReleaseArchiveEntries: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a count the release updater keeps in its state directory"}
		},
		capacity.ReleaseUnpackedBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a per-read guard of the release updater; no retained buffer"}
		},
		capacity.ReleaseSmokeTimeoutSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseHealthWaitSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleasePendingDeadlineSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseBootAttempts: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a count the release updater keeps in its state directory"}
		},
		capacity.ReleaseSupervisorRuns: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a count the release updater keeps in its state directory"}
		},
		capacity.ReleaseLockStaleSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseAppSwapPollSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseAppRelistSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseAutoApplyRetrySeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
		capacity.ReleaseBackupsKept: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a count the release updater keeps in its state directory"}
		},
		capacity.ReleasePreviousKept: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a count the release updater keeps in its state directory"}
		},
		capacity.ReleaseFailedVersions: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a count the release updater keeps in its state directory"}
		},
		capacity.ReleaseStateFileBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a per-read guard of the release updater; no retained buffer"}
		},
		capacity.ReleaseApplyFollowSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a timing ceiling of the release updater; no retained buffer"}
		},
	}
}
