# Systematic debugging

Use this skill when a test fails, a build breaks, or software behaves unexpectedly, before you propose or make a fix. A fix chosen before the cause is known treats a symptom.

1. Investigate the root cause first. Read the whole error and stack trace, including line numbers and codes. Reproduce the failure with exact steps; if it does not reproduce reliably, gather more data instead of guessing. Check what changed recently: the diff, commits, dependencies, configuration and environment.
2. Where several components are involved, add temporary diagnostics at each boundary to show what enters and leaves it, run once, and find the layer where the data first goes wrong. When the error surfaces deep in a call chain, trace the bad value backwards, caller by caller, to where it originates, and fix it there.
3. Compare against what works: a similar working path in the same codebase, or the reference you are following, read completely. List every difference before assuming one cannot matter.
4. State one hypothesis ("X is the cause because Y") and test it with the smallest change that can confirm or refute it, one variable at a time. If it is refuted, form a new hypothesis; do not stack fixes.
5. Then fix the cause: first a reproduction that fails, in the project's own test style where one exists; then one change; then the reproduction and the related checks again. Remove the temporary diagnostics.
6. If three fixes have failed, or each fix reveals a new problem elsewhere, stop and question the design with the person instead of trying a fourth.

Say "I don't understand X yet" rather than guess. If the investigation shows the cause is truly external or timing-dependent, record what was checked, handle it explicitly, and leave a way to observe it next time. Do not run or install third-party scripts to bisect a failure; write the smallest search the project's own tools allow.

Adapted from obra/superpowers' `systematic-debugging` skill and its root-cause tracing note at commit 8ca22dba9a94f28898bbce59f2537ff4d87c747d (MIT; copyright 2025 Jesse Vincent). See the pinned source and bundled license in the skill catalog.
