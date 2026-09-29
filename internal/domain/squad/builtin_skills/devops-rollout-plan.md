# Deployment rollout plan

Use this skill when preparing a code or configuration rollout. Start from the repository's deployment procedure and current target state.

1. Identify the exact commit, target, dependencies, permissions, data changes, and reversible steps. Name the owner of each action.
2. Write preflight checks, ordered rollout commands, expected signals, time bounds, and a specific rollback procedure. Include what to do if a step only partially succeeds.
3. Verify the running service and the user-facing journey, not just process health. Use the current project's release identifier and deployment checks to prove which version users receive.
4. Record the actual landing and deployment evidence. Communicate only observed results.

Follow repository approval and release policy; this plan is not itself authorization to deploy.
