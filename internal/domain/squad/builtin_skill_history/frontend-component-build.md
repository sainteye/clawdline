# Frontend component build

Use this skill when creating or refactoring a reusable Console component. First inspect the existing component, tokens, state model, and nearby tests.

1. Name the component's required data, variants, events, and loading, empty, error, disabled, and success states. Keep props and state ownership explicit.
2. Use semantic HTML, accessible names, logical keyboard order, visible focus, and appropriate live announcements. Verify actual keyboard and screen-reader behavior; an automated scan alone is insufficient.
3. Reuse the Console's design language. Check mobile width, text enlargement, reduced motion, and both light and dark contexts that the product supports.
4. Test the behavior that matters to the user, including failure and recovery. Verify the served Cloud bundle when the change affects a Cloud-facing journey.

Do not add a new visual system or dependency merely because an example skill suggests one.
