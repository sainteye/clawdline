# Monitoring and alerting

Use this skill when defining or repairing service signals, alerts, and on-call response. Map the real request path and identify where work can queue, fail, retry, or stall.

1. Separate availability, correctness, latency, and error signals. For Clawdline delivery, measure accepted, executed, delivered, observed, and acknowledged states independently.
2. Establish baselines from actual daemon and Cloud behavior before proposing thresholds or service objectives. State gaps when measurements are unavailable.
3. Define an alert's owner, trigger, expected action, and recovery signal. Avoid pages that have no actionable response.
4. Test a failed dependency, stalled queue, retry storm, stale snapshot, and recovery. Confirm dashboards and alerts describe the same state as the user sees.

Example thresholds from external guides are starting hypotheses, not product limits or evidence.
