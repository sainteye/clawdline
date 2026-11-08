# Performance optimization

Use this skill when a measured or reported slowness, a performance budget, or a suspected regression needs a fix; not to optimize code that has no evidence of a problem.

1. Measure first, with what the project already has: its benchmarks, profilers, timing logs, browser developer tools or existing monitoring. Do not install measurement tools or packages for this. Record the baseline: the exact command or procedure, the conditions, and the numbers.
2. Let the symptom choose what to measure: first load (payload size, server response time, blocking resources), sluggish interaction (long tasks on the main thread, re-renders, forced layout), slow endpoint (query plans, round trips), everything slow at once (shared pools, memory, CPU), intermittent slowness (contention, garbage collection, external dependencies).
3. Name the one bottleneck the measurement shows, then fix only it. Usual suspects: one query per row instead of one query; unbounded reads; an index added without reading the query plan before and after (an index that does not change the plan is reverted, and every index taxes writes); a connection pool raised instead of finding what holds connections; a cache in front of something already cheap, or a cache key missing an input the response depends on (tenant, viewer, locale, permissions), or a cache with no stated staleness window; images without dimensions or right sizing; heavy code loaded up front that could load on demand.
4. Re-measure the same way as the baseline, one change at a time, several runs. Keep a change only if the gain exceeds run-to-run variance and every test stays green. Within noise, worse, or a test changed to pass: revert. Never buy speed by dropping work the product needs, such as a validation or freshness.
5. Log every attempt, kept and reverted, with baseline, result and reason, where the project keeps such notes or in the report, so a failed idea is not tried again.
6. Guard the metric that justified the fix with the project's existing test, budget or monitor when it has one; propose one when it does not.

Do not load-test production or shared systems without authorization. Report the baseline, the bottleneck and its evidence, the change, the after-measurement with variance, and what was reverted.

Adapted from addyosmani/agent-skills' `performance-optimization` skill and its optimization patterns and performance checklist at commit 8bfcc1f9916f2eea55494d8c1fa7f4bde867dd9e (MIT; copyright 2025 Addy Osmani). See the pinned source and bundled license in the skill catalog.
