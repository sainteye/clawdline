package http

import (
	"fmt"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// cloudCapacityMeasures measures every row of the register's "cloud" area
// (capacity.AreaOf), one reading per row.
func (s *Server) cloudCapacityMeasures() map[string]func() capacity.Reading {
	out := map[string]func() capacity.Reading{
		"cloud.viewer_rekey_pending": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "browser policy only; disabled by default; no daemon queue; live browser usage is not measured"}
		},
		"cloud.viewer_rekey_cooldown": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "browser interval policy only; disabled by default; live browser activity is not measured"}
		},
		"cloud.rekey_pending": func() capacity.Reading {
			line, ok := cloudLines.Load(s.cfg.Dir)
			if !ok {
				return capacity.Reading{Known: true, Note: "Cloud key runtime is inactive"}
			}
			if reader, ok := line.(interface{ RekeyPendingReading() capacity.Reading }); ok {
				return reader.RekeyPendingReading()
			}
			return capacity.Unmeasured("Cloud link does not expose raw key exchange usage")
		},
		"cloud.rekey_interval": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "global raw-offer admission interval; excess requests are refused"}
		},
		"cloud.envelope_key_id": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-envelope key identifier parse guard; no retained buffer"}
		},
		"cloud.rekey_lifetime": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "signed offer expiry; runtime remains disabled by default"}
		},
		"cloud.rekey_body": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-message signed raw-control parse guard"}
		},
		"cloud.health_investigation_bundle_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-leased bundle read byte guard; no retained network buffer"}
		},
		"cloud.health_investigation_report_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-task typed report byte guard; oversized reports are refused"}
		},
		"cloud.health_investigation_report_refs": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-task evidence reference guard; no shared queue"}
		},
		"cloud.health_investigation_read_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "one read per claim with a fixed deadline and no automatic retry"}
		},

		capacity.CloudTaskListReread: func() capacity.Reading {
			line, ok := cloudLines.Load(s.cfg.Dir)
			if !ok {
				return capacity.Reading{Known: true,
					Note: "the Cloud line is off: this machine's own task list is not read for Cloud at all"}
			}
			reader, ok := line.(interface {
				TaskListReading() (capacity.Reading, bool)
			})
			if !ok {
				return capacity.Unmeasured("this Cloud line does not report its task-list reads")
			}
			reading, built := reader.TaskListReading()
			if !built {
				return capacity.Reading{Known: true,
					Note: "the Cloud line was never built: the task list has not been read"}
			}
			return reading
		},
		capacity.CloudSessionRowVolatile: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-row Cloud re-send window; a held change goes out on the next pass after it"}
		},
		capacity.CloudRelayQueue: func() capacity.Reading {
			line, ok := cloudLines.Load(s.cfg.Dir)
			if !ok {
				return capacity.Reading{Known: true, Note: "the Cloud line is off: there is no queue"}
			}
			q, ok := line.(interface {
				RelayQueue() (waiting, depth int, counters capacity.Counters, ok bool)
			})
			if !ok {
				return capacity.Unmeasured("this Cloud line does not report its queue")
			}
			waiting, depth, counters, ok := q.RelayQueue()
			if !ok {
				return capacity.Reading{Known: true, Note: "the Cloud line was never built: there is no queue"}
			}
			r := capacity.Reading{Known: true, Used: int64(waiting), Counters: counters}
			if limit := CapacityLimit(capacity.CloudRelayQueue); int64(depth) != limit {
				r.Note = fmt.Sprintf("the queue holds %d, not the register's %d", depth, limit)
			}
			return r
		},
		capacity.CloudReadQueue: func() capacity.Reading {
			line, ok := cloudLines.Load(s.cfg.Dir)
			if !ok {
				return capacity.Reading{Known: true, Note: "the Cloud line is off: there is no read queue"}
			}
			reader, ok := line.(interface {
				ReadLaneReadings() (active, waiting capacity.Reading)
			})
			if !ok {
				return capacity.Unmeasured("this Cloud line does not report its read queue")
			}
			_, waiting := reader.ReadLaneReadings()
			return waiting
		},
		capacity.CloudLaneRefusals: func() capacity.Reading {
			line, ok := cloudLines.Load(s.cfg.Dir)
			if !ok {
				return capacity.Reading{Known: true, Note: "the Cloud line is off: there is no busy reply queue"}
			}
			reader, ok := line.(interface{ LaneRefusalReading() capacity.Reading })
			if !ok {
				return capacity.Unmeasured("this Cloud line does not report its busy reply queue")
			}
			return reader.LaneRefusalReading()
		},
		capacity.CloudPeerPairs:  func() capacity.Reading { return s.peerStoreCapacity(capacity.CloudPeerPairs) },
		capacity.CloudPeerGrants: func() capacity.Reading { return s.peerStoreCapacity(capacity.CloudPeerGrants) },
		capacity.CloudPeerInbox:  func() capacity.Reading { return s.peerStoreCapacity(capacity.CloudPeerInbox) },
		capacity.CloudPeerOutbox: func() capacity.Reading { return s.peerStoreCapacity(capacity.CloudPeerOutbox) },
		"cloud.peer_discovery_reply": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "one reply per pending machine inventory request"}
		},
		capacity.CloudPeerInboxPage: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "one item per pinned content read"}
		},
		capacity.CloudPeerBody: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-message body guard; no retained buffer"}
		},
		capacity.CloudViewerEventsBatch: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-batch guard on a page's diagnostic events; no retained buffer"}
		},
		capacity.CloudViewerEventsRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-batch log guard; no retained buffer"}
		},
		capacity.CloudViewerEventsLine: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-row log line guard; no retained buffer"}
		},
		capacity.CloudSessionRowID: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read guard on the id a sessions.row names; no retained buffer"}
		},
		capacity.CloudSessionRowAsks: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "asks live in the Cloud link's publisher and drain each pass; not measured here"}
		},
		capacity.CloudViewerEventsSeen: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "batch ids live in the Cloud link and reset with it; not measured here"}
		},
		capacity.CloudPeerFrame: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-frame ingress guard; no retained buffer"}
		},
		capacity.CloudPeerIngress: func() capacity.Reading {
			line, ok := cloudLines.Load(s.cfg.Dir)
			if !ok {
				return capacity.Reading{Known: true, Note: "the Cloud peer line is off"}
			}
			reader, ok := line.(interface{ PeerIngressUsage() (int, bool) })
			if !ok {
				return capacity.Unmeasured("this Cloud line does not report peer ingress")
			}
			waiting, active := reader.PeerIngressUsage()
			if !active {
				return capacity.Reading{Known: true, Note: "the Cloud peer line has no socket"}
			}
			return capacity.Reading{Known: true, Used: int64(waiting)}
		},
		capacity.CloudReloadStopSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-reload clock; a timed-out replacement is refused"}
		},
		capacity.CloudTerminalRosterRefresh:        func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalRosterRefresh) },
		capacity.CloudTerminalRosterDeadline:       func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalRosterDeadline) },
		capacity.CloudRosterShortReadAttempts:      func() capacity.Reading { return s.terminalCapacity(capacity.CloudRosterShortReadAttempts) },
		capacity.CloudTerminalRosterRetry:          func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalRosterRetry) },
		capacity.CloudTerminalUnverifiedRetire:     func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalUnverifiedRetire) },
		capacity.CloudTerminalConnections:          func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalConnections) },
		capacity.CloudTerminalViewerConnections:    func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalViewerConnections) },
		capacity.CloudTerminalRequestBytes:         func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalRequestBytes) },
		capacity.CloudTerminalHistoryReceipt:       func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalHistoryReceipt) },
		capacity.CloudTerminalHistoryLine:          func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalHistoryLine) },
		capacity.CloudTerminalHistoryCapture:       func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalHistoryCapture) },
		capacity.CloudTerminalReceipts:             func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalReceipts) },
		capacity.CloudTerminalKeySeconds:           func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalKeySeconds) },
		capacity.CloudTerminalIngress:              func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalIngress) },
		capacity.CloudTerminalListIngress:          func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalListIngress) },
		capacity.CloudTerminalRefusals:             func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalRefusals) },
		capacity.CloudTerminalRevocationRetire:     func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalRevocationRetire) },
		capacity.CloudTerminalFrameHeartbeat:       func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalFrameHeartbeat) },
		capacity.CloudTerminalDirectPeers:          func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectPeers) },
		capacity.CloudTerminalDirectOffers:         func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectOffers) },
		capacity.CloudTerminalDirectSDP:            func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectSDP) },
		capacity.CloudTerminalDirectCandidates:     func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectCandidates) },
		capacity.CloudTerminalDirectNegotiate:      func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectNegotiate) },
		capacity.CloudTerminalDirectGather:         func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectGather) },
		capacity.CloudTerminalDirectMessage:        func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectMessage) },
		capacity.CloudTerminalDirectChunk:          func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectChunk) },
		capacity.CloudTerminalDirectAck:            func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectAck) },
		capacity.CloudTerminalDirectProbe:          func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectProbe) },
		capacity.CloudTerminalDirectProbeUnsettled: func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalDirectProbeUnsettled) },
		capacity.CloudTerminalSweep:                func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalSweep) },
		capacity.CloudTerminalReceiptBusyRetries:   func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalReceiptBusyRetries) },
		capacity.CloudTerminalReceiptBusyRetry:     func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalReceiptBusyRetry) },
		capacity.CloudTerminalReceiptSeconds:       func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalReceiptSeconds) },
		capacity.CloudTerminalEarlyFrames: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab guard; daemon cannot measure live browser use"}
		},
		capacity.CloudTerminalObservationRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console page diagnostic guard; daemon cannot measure live browser use"}
		},
		capacity.CloudTerminalListRetry: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console terminal list guard; daemon cannot measure live browser retries"}
		},
		capacity.CloudHeaderReadDiagnosticRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab policy; daemon cannot measure another device's sessionStorage"}
		},
		capacity.CloudHeaderReadDiagnosticAge: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Console tab policy; daemon cannot measure another device's sessionStorage"}
		},
		capacity.CloudTerminalUnconfirmed: func() capacity.Reading { return s.terminalCapacity(capacity.CloudTerminalUnconfirmed) },
		// C4: the Cloud line's outbound spool, both of its bounds.
		capacity.CloudSpool: func() capacity.Reading {
			rows, _ := s.spoolReadings()
			return rows
		},
		capacity.CloudSpoolBytes: func() capacity.Reading {
			_, bytes := s.spoolReadings()
			return bytes
		},
		// The stuck channel (2026-09-21): the per-channel bound that was
		// refusing answers unregistered, and the receipts that were being
		// charged as though they were queue.
		capacity.CloudSpoolChannelBytes: func() capacity.Reading {
			channelBytes, _ := s.spoolChannelReadings()
			return channelBytes
		},
		capacity.CloudSpoolReceipts: func() capacity.Reading {
			_, receipts := s.spoolChannelReadings()
			return receipts
		},
		capacity.CloudSpoolRefusals: func() capacity.Reading { return s.spoolRefusalReading() },
	}
	for name, m := range s.connectionHealthCapacityMeasures() {
		out[name] = m
	}
	return out
}
