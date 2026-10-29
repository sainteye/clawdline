# See Sessions in Cloud from a browser or phone

Use this walkthrough after [your first local Session](first-session.md) is visible. Keep Clawdline running on the machine you want to reach. You will sign in to your account, connect that machine, then pair each browser or installed web app you want to use.

## 1. Sign in and connect the machine

On macOS, open Clawdline's **Cloud** tab and do the steps it still shows; skip any that are already done. These steps need an app version that includes them; updating the website does not update the app.

1. Choose **Continue with GitHub**, sign in in your default browser, allow it to open Clawdline, and return to the Cloud tab.
2. Choose **Connect this machine**, approve the native confirmation, approve the machine in your default browser with the same GitHub account, and return to Clawdline.
3. Choose **Pair this app**, then **Pair directly with this app's machine**. Approve the native confirmation, compare the fingerprints, and choose **They match — done**.

**Done when:** this machine's Sessions appear in the Cloud tab. For a machine without the app, use [the terminal steps](remote-access.md#connect-the-machine).

## 2. Pair a new browser on this computer

1. In the browser and profile you will keep using, open [app.clawdline.com](https://app.clawdline.com/) and sign in to the same GitHub account.
2. Open **Pair** and choose **Show the pairing code**. You do not need to select a machine first.
3. Choose **Open Clawdline to pair**, allow the browser to open the app, check the machine it names, and choose **Pair with this machine**.
4. Return to the same browser page, compare the fingerprints, and choose **They match — done**.

**Done when:** the app says **Browser paired** and the browser shows the machine's Sessions. If the app does not open, use [the code or command fallback](remote-access.md#pair-with-a-code-or-command).

## 3. Pair your phone

Set up the phone first, in the place you will keep using. Safari, the Home Screen app and each Chrome profile keep separate keys, so pairing in one and switching later means pairing again.

1. **Prepare the phone.**
   - **iPhone or iPad:** in Safari, open app.clawdline.com and choose **Share → Add to Home Screen**. Open Clawdline from the Home Screen icon and sign in there.
   - **Android:** choose a Chrome profile, or install Clawdline from Chrome's menu. Sign in there.
2. **Show the QR on the computer.** In the app's **Cloud** tab, find **Pair a browser or phone** (once Sessions appear: **Settings → Remote → Pair a browser or phone**). Choose **Show a pairing QR code** and approve the native confirmation. Click the QR to enlarge it.
3. **Scan from the phone,** in that same app or profile: choose **Pair → Scan the machine's QR**, allow the camera, compare the fingerprints, and choose **They match — done**.

**Done when:** the phone shows the machine's Sessions and the computer says **Browser paired**. For an expired QR, a denied camera or a black preview, see [phone recovery](remote-access.md#pair-an-iphone-ipad-or-android-phone).

## Read, act, and reconnect

Pairing supplies a browser's keys. Sending also requires this machine's Cloud command switch and the browser's local grant to allow operation; see [Let paired devices act](remote-access.md#let-paired-devices-act).

If the machine is offline, Cloud may retain an older view. Restore its connection before treating the view as fresh. If the machine is visible but this browser still asks to pair, check the GitHub account, browser profile, and whether you opened Safari or the Home Screen app.

## Use Sessions from another local machine

The CLI uses a separate Cloud viewer identity. On the machine where you will read, run `clawdline cloud viewer login`, approve the displayed device and capabilities in your Cloud account, then run `clawdline cloud viewer machines`. For each target machine, compare its fingerprint on that machine before running `clawdline cloud viewer pair --machine MACHINE_ID --fingerprint FINGERPRINT`. Run the printed one-time `clawdline cloud pair --offer` command on the target machine. `clawdline cloud viewer sessions` lists paired machines and their Session status. To use a browser, pair that browser separately; the hosted Console shows machines in its existing Session list.

An actionable Session has three values: machine ID, Session ID, and execution generation. Select the current row in the hosted Console or pass all three to `clawdline cloud viewer read --machine MACHINE_ID --session SESSION_ID --generation GENERATION`. The first read shows the newest transcript page. If it returns `next_before`, use `clawdline cloud viewer read --machine MACHINE_ID --session SESSION_ID --generation GENERATION --before NEXT_BEFORE` to fetch one older page. For native transcripts, also pass `--revision SIGNATURE` from the held page that supplied the cursor (inside `transcript.signature` in the CLI reply). If the cursor is stale, read the newest page again and use its new cursor and signature. Repeat with the new cursor until none remains. Each page checks the same current execution and permission again. `send`, `answer`, `interrupt`, and `end` use the same target. A CLI action prints its request ID before sending. If its outcome is uncertain, query `clawdline cloud viewer receipt --machine MACHINE_ID --session SESSION_ID --generation GENERATION --action ACTION --request REQUEST_ID` before acting again. An offline or stale machine does not queue an action.

[Pairing, permissions, and revocation](remote-access.md) · [Cloud trust boundary](../cloud.md) · [Agent contract for Cloud actions](../agent-contract.md)
