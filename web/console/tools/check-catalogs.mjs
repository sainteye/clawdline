// Normal development gate: en/zh-Hant stay complete; secondary languages may lack post-v1 keys.
import { spawnSync } from "node:child_process"
import path from "node:path"
import { fileURLToPath } from "node:url"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const reference = path.join(root, "public/catalogs/en.json")
const validator = path.join(root, "tools/validate-catalog.mjs")
const tags = ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"]
let failed = false
for (const tag of tags) {
  const result = spawnSync(process.execPath, [validator,
    "--reference", reference, "--target", path.join(root, `public/catalogs/${tag}.json`), "--tag", tag,
  ], { encoding: "utf8" })
  if (result.stdout) process.stdout.write(result.stdout)
  if (result.stderr) process.stderr.write(result.stderr)
  if (result.status !== 0) failed = true
}
if (failed) process.exitCode = 1
