# Motion review

Use this skill when reviewing animation and transition code in a web interface against a high craft bar; not for designing new motion, non-motion code, or native mobile animation.

Default to flagging; approval is earned. A transition that runs but feels sluggish, starts from the wrong origin, fires too often or drops frames is a regression.

**Standards** (a violation is a finding)
1. Every animation has a purpose: spatial consistency, state, feedback, explanation, or avoiding a jarring change.
2. Match frequency. Keyboard-initiated and 100+ times a day actions get no animation; tens a day get little; occasional surfaces (modals, drawers, toasts) get standard motion; rare moments may delight.
3. Entering and exiting use `ease-out` or a strong custom curve such as `cubic-bezier(0.23, 1, 0.32, 1)`; on-screen movement uses ease-in-out such as `cubic-bezier(0.77, 0, 0.175, 1)`. `ease-in` on UI is a block.
4. UI motion stays under 300ms: press feedback 100–160ms, tooltips 125–200ms, dropdowns 150–250ms, modals and drawers 200–500ms.
5. Popovers, dropdowns and tooltips scale from their trigger; modals stay centred. Never start from `scale(0)`; start from 0.9–0.97 with opacity 0. Press feedback is about `scale(0.97)` on `:active`.
6. Rapid or gesture-driven motion (toasts, toggles, drags) is interruptible: transitions or springs that retarget, not keyframes that restart.
7. Animate only `transform` and `opacity`. Do not drive a child's transform through a CSS variable on its parent. In libraries whose `x`/`y`/`scale` shorthands run on the main thread, use a full transform string.
8. Honour `prefers-reduced-motion` with gentler motion (keep opacity and colour, drop movement), and gate hover motion behind `@media (hover: hover) and (pointer: fine)`.
9. Deliberate actions (press-and-hold, destructive confirm) are slower; the system's response snaps.
10. Motion matches the component's personality; when unsure, deleting it is often best.

**Flag on sight:** `transition: all`; `scale(0)` or a pure fade with no initial transform; `ease-in`; animation on a shortcut or command palette; UI duration over 300ms with no reason; centre origin on an anchored popover; keyframes on toasts or toggles; animated `width`, `height`, `margin`, `padding`, `top` or `left`; missing reduced-motion handling; ungated hover motion; symmetric timing on press-and-release; an all-at-once group entrance where a 30–80ms stagger belongs.

**Fix order:** delete; reduce; fix the easing; fix origin and physicality; make it interruptible; move it to transform and opacity; make timing asymmetric; polish (a slight blur under 20px to hide a crossfade, stagger, `@starting-style`, springs with bounce 0.1–0.3); then accessibility and cohesion.

**Output.** One table of before, after and why, each row citing `file:line`. Then remaining commentary by tier: feel-breaking regressions, missed simplifications, performance, interruptibility and timing, origin and cohesion, accessibility. Close with `Block` (any feel-breaking regression, animation on a frequent or keyboard action, `scale(0)` or `ease-in` on UI, or a layout-property animation with an easy fix) or `Approve`. When feel cannot be judged from code, say what to check slowed down or on a real device rather than guessing.

Adapted from emilkowalski/skills' `review-animations` skill and its standards note at commit a81dbca07f2217b7103e510c0f13f8d5b017afc9 (MIT; copyright 2026 Emil Kowalski). See the pinned source and bundled license in the skill catalog.
