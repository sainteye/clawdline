# Copied, not written

Every `.css` file beside this one is a byte-for-byte copy of the Swift app's
console stylesheet, taken from `~/code/clawdline/Resources/web/app/css`.

They are copied rather than rewritten because the instruction was to replicate
the existing screen strictly, and a stylesheet re-typed from a screenshot is an
approximation that will diverge on the first detail nobody looked at. Reusing
the original makes the replication exact by construction: the React components
emit the same DOM and the same class names, so the same rules apply to them.

`MANIFEST.json` records the SHA-256 prefix of each source file at the moment it
was copied, and `tools/check-legacy-css.sh` compares them. That guard exists
because a copy that cannot notice its source changed is the defect this project
keeps finding in other places — two things that were once the same, drifting
with nothing to say so.

When the guard goes red, the answer is to look at what changed over there and
copy it again on purpose. It is never to update the manifest.
