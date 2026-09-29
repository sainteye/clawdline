# Frontend component build

Use this skill when creating or refactoring a reusable frontend component. First inspect the existing component, design tokens, state model, and nearby tests.

1. Name the component's required data, variants, events, and loading, empty, error, disabled, and success states. Keep props and state ownership explicit.
2. Use semantic HTML, accessible names, logical keyboard order, visible focus, and appropriate live announcements. Verify actual keyboard and screen-reader behavior; an automated scan alone is insufficient.
3. Reuse the current product's design language. Check mobile width, text enlargement, reduced motion, and the themes the product supports.
4. Test the behavior that matters to the user, including failure and recovery. When the project deploys a bundled interface, verify the served version as its release procedure requires.

Do not add a new visual system or dependency merely because an example skill suggests one.
