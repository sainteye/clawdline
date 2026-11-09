export interface VoiceDiagnostic {
  stage: "stopping" | "preparing" | "reading" | "recoverable"
  code: "stop_timeout" | "prepare_timeout" | "read_timeout" | "recording_limit" | "processing_failed" | "paused" | "recorder_error"
  bytes: number
  complete: boolean
}

let report: ((row: VoiceDiagnostic) => void) | null = null

export function attachVoiceDiagnostics(next: (row: VoiceDiagnostic) => void): void {
  report = next
}

/** Only typed metadata; neither the audio nor recognised words enter diagnostics. */
export function recordVoiceDiagnostic(row: VoiceDiagnostic): void {
  try { report?.(row) } catch { /* diagnostics cannot hold the microphone */ }
}
