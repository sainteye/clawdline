# Scoped control icon replacements

The 2026-11-18 screenshot review approved replacing font glyphs in the
illustrated controls with the console's existing line-icon style. This is a
bounded visual change, not a migration of every source-search match.

| Review group | Changed control |
| --- | --- |
| TR-01 | Message-board pinned summary and member disclosures |
| TR-03 | The pictured creation quote and Session-assignment picker |
| TR-04 | Instruction-file folders and the project file explorer |
| TR-05, TR-06, TR-07, TR-13 | Shared Project/choice pickers, Playbook scope and view management |
| TR-08 | The pictured Bots settings disclosure and work-result link |
| TR-09 | Skill-card expansion |
| TR-12 | The question target picker and alternative-question disclosure |
| TR-17 | The pictured slow-outcome badge, error-rate card and trend chip |
| TR-20, TR-25 | Machine warning and waiting-card expansion |

React controls and the waiting card's DOM markup share the existing Bots
select's chevron geometry. Existing click targets, accessible names and state
owners remain in place. The slow badge uses a clock, warning/error indicators
use an alert icon, and trend chips use directional SVG arrows. Trend copy keeps
its numbers and placeholders; the decorative direction moves out of the
translation string in all shipped catalogs.

The excluded background-work strip, technical-detail disclosures, Chrome
status links, Session to-do header, video card, conversation tools and Bots
trace remain unchanged. Existing SVG controls are retained. Unpictured source
matches, including additional navigation and diagnostic branches, are outside
this change. No glyph-prevention scanner or new commit guard is introduced.

Visual evidence uses the actual components, production styles and explicitly
synthetic data. It is distinct from deployment and verification of the hosted
console or a running daemon installation.
