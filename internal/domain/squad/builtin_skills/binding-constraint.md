# Binding constraint

Use this skill when throughput or latency is too low and the work passes through stages (requests, jobs, builds, queues, review steps). A flow-limited system has one binding constraint; improving anything else is wasted.

1. Define the unit of work, the stages it passes through, and the end-to-end metric that matters.
2. Measure before changing anything: a baseline on the same machine and workload, with per-stage utilization, queue or wait time, and rate. Measured rates beat opinions.
3. Identify the constraint from that evidence: the stage that is near full use, has the longest queue, or whose rate caps the system.
4. Exploit it first without major spend: remove idle time, unnecessary or repeated work, and rework at that stage. Estimate the gain before acting.
5. Make the other stages serve the constraint rather than maximizing their own use.
6. Elevate the constraint (more capacity, parallelism) only if exploiting it is not enough, choosing the cheapest adequate option.
7. Measure again on the same setup. The constraint often moves; start again from step 3 rather than keep tuning the old one.

Report the baseline, the constraint and its evidence, the change, and the after-measurement. Do not run load against production or shared systems without authorization, and do not optimize several stages "just in case".

Adapted from tjboudreaux/cc-thinking-skills at commit 7b8fece345dfaa11773be7152ccd194589cb5437 (MIT; copyright 2025 TJ Boudreaux). See the pinned source and bundled license in the skill catalog.
