# Verification before completion

Use this skill when you are about to say that work is complete, fixed, passing, or ready, or to accept someone else's report that it is. A claim needs evidence produced for this claim, now.

1. Identify the command, test, request, or file read that would prove the claim.
2. Run it in full, fresh; an earlier run, a partial run, or "it should pass" is not evidence.
3. Read the whole output: exit code, failure count, skipped items, warnings that change the meaning.
4. If the output confirms the claim, state the claim with that evidence. If it does not, state the actual status with the evidence.
5. Match the evidence to the claim: a fixed bug needs the original symptom re-tested; a regression test needs to be seen failing without the fix and passing with it; a delegated task needs its actual diff or artifact checked, not its success message; "requirements met" needs each requirement checked, not only green tests.

Words such as "should", "probably", and "seems to" in a status mean the check has not been done. Report what was not verified and why instead of rounding it up to done.

Adapted from obra/superpowers at commit 8ca22dba9a94f28898bbce59f2537ff4d87c747d (MIT; copyright 2025 Jesse Vincent). See the pinned source and bundled license in the skill catalog.
