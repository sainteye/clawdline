import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const build = readFileSync(new URL('../build.sh', import.meta.url), 'utf8');
const release = readFileSync(new URL('../tools/release.sh', import.meta.url), 'utf8');
const entitlements = readFileSync(
  new URL('../Resources/Clawdline.entitlements', import.meta.url), 'utf8');

assert.match(build, /CFBundleIdentifier<\/key><string>com\.tsunamiworks\.clawdline<\/string>/,
  'the shipped bundle uses the company identifier');
assert.doesNotMatch(build, /dev\.sainteye\.clawdline/,
  'the build no longer emits the personal bundle identifier');

assert.match(entitlements, /com\.apple\.security\.automation\.apple-events/,
  'the hardened app retains iTerm automation access');
assert.match(entitlements, /com\.apple\.security\.device\.audio-input/,
  'the hardened app retains microphone access');

const buildOnly = build.indexOf('CLAWDLINE_BUILD_ONLY');
const runningProbe = build.indexOf('pgrep -x Clawdline');
assert.ok(buildOnly >= 0 && buildOnly < runningProbe,
  'a package-only build exits before inspecting or restarting the live app');

assert.match(release, /CLAWDLINE_BUILD_ONLY=1/,
  'release packaging cannot restart the installed app');
assert.match(release, /Developer ID Application: TsunamiWorks Co\., Ltd\. \(\$TEAM_ID\)/,
  'releases derive the company Developer ID identity from the fixed Team ID');
assert.match(build, /for attempt in 1 2 3/,
  'Developer ID signing retries Apple timestamp service failures');

const verify = release.indexOf('codesign --verify');
const submit = release.indexOf('notarytool submit');
const staple = release.indexOf('stapler staple');
const publish = release.indexOf('git tag -a');
assert.ok(verify >= 0 && verify < submit && submit < staple && staple < publish,
  'signing, notarization, and stapling all finish before publishing');

console.log('release signing contract: 10 checks passed');
