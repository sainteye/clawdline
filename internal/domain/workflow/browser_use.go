package workflow

// BrowserUseInstructions is launch-only guidance for the machine browser
// setting. The daemon enforces these rules; the text never widens authority.
func BrowserUseInstructions() string {
	return `# Browser use through Clawdline

Browser use through Clawdline is enabled on this machine. Use ` + "`clawdline browser`" + ` (open <url>, read, click <label> or --x/--y, type <text>, key <name>, scroll, screenshot, release; ` + "`clawdline browser --help`" + `). It drives Clawdline's own Chromium, separate from the person's Chrome; every action is recorded for the person; the person may watch and take over from their Mac or phone.
Prefer ` + "`read`" + ` and labels from it over coordinates; take a screenshot only when you must see layout.
` + "`browser_person_controlling`" + `: the person has taken the tab over; wait, do not retry in a loop. ` + "`browser_stopped`" + `: the person stopped all browser use; do not retry. ` + "`browser_extension_missing`" + ` or ` + "`browser_disabled`" + `: continue another way and tell the person that Settings → Browser sets it up.
When a page needs the person (sign-in, CAPTCHA, payment or any confirmation with consequences), create a Clawdline note asking them to take over the Chrome tab (` + "`clawdline guide note`" + `), and wait. Never ask for a password in chat; for a secret use a Secret Handoff and ` + "`clawdline secret run <note> -- clawdline browser type --stdin`" + `.
Do not buy, send messages, publish, delete or accept terms in the browser unless the person asked for that exact action.
This setting does not authorize installing software, changing the person's Chrome or spending money.`
}
