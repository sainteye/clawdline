# Project instruction and skill files

The gear on each Project row opens that Project's settings. Its first section inventories disk candidates for Codex instructions, Claude Code instructions, and Skills. It shows each filename, its path relative to the Project or the machine's home directory, its source, and whether it can be read or edited. The inventory does not claim that a running assistant has loaded a file: overrides, plugins, and the time a Session started can change the effective instructions.

The Project scope runs from the nearest repository root through the selected place. It includes `AGENTS.md`, `CLAUDE.md`, `CLAUDE.local.md`, `.claude/CLAUDE.md`, `.claude/CLAUDE.local.md`, and direct `SKILL.md` children under `.agents/skills` or `.claude/skills` at each layer. The home scope includes `.codex/AGENTS.md`, `.claude/CLAUDE.md`, `.claude/CLAUDE.local.md`, and direct skill children under `.agents/skills`, `.codex/skills`, or `.claude/skills`. Missing common instruction files remain listed as missing. Home files are read-only in this interface because changing one may affect every Project.

`GET /v1/projects/{place}/files` lists candidates; `GET /v1/projects/{place}/files/{file}` returns UTF-8 content and its SHA-256 version. `PUT` to the same file route takes `expected_version` and `content`, plus `Idempotency-Key`. The daemon resolves the place from its current Project catalog. It accepts only a file ID from the bounded inventory, never a browser-supplied path. Regular files are opened beneath bound directory handles; symlink or directory swaps are refused. A save requires the version the editor read, writes a temporary file in the same directory, syncs it, and renames it. Concurrent edits through this API are serialized; a change made elsewhere returns `file_changed` and leaves the browser's draft intact. The interface does not create, delete, or rename files.

The paired Cloud console carries three distinct words for the inventory read, content read, and save. The Cloud Project ID is resolved to its owning machine before a request is sent. Reads require the paired device's read access; save also requires the remote write gate and carries the editor's idempotency key. A lost save answer is treated as uncertain: the browser reads the file again and confirms matching content before it says the save succeeded. The Cloud relay carries encrypted request and answer content; it does not use a public document URL for these files.

The editor guards its draft when a person closes the modal, changes page, selects another file, or reloads the inventory. On a phone the groups and content stack in one column. Closing the modal returns keyboard focus to the Project gear that opened it.

## Project file tree

The same Project gear has a Files view beside Settings and instructions. It opens the selected
Project's own directory, not a repository ancestor or the machine's home directory. Folders are
loaded only when expanded; a file's content is requested only when selected. This view is read-only.
The existing instruction and Skill editor remains in Settings and instructions.

`GET /v1/projects/{place}/tree?directory=<relative>` lists one directory;
`GET /v1/projects/{place}/tree/file?path=<relative>` reads one regular UTF-8 text file. The server
resolves `{place}` from its current Project catalog. It rejects absolute, noncanonical, overlong,
deep, and `.git` paths. Each directory component and the final file are opened beneath bound
directory handles, with symlink changes rejected. Symlinks and nonregular entries can be named in
the tree but cannot be followed or previewed. The directory read stops after 1,024 entries and
says it is truncated; text above 128 KiB or containing NUL or invalid UTF-8 is refused by name.

The paired Cloud console carries separate `project-tree-list` and `project-tree-read` words. Both
are reads on the Project's owning machine, available to a paired device with read access even when
the remote write switch is off. Selecting a file sends its text in the encrypted relay answer;
folder expansion sends only that folder's immediate names and metadata. No tree write word exists.
On a phone, the folder list and preview stack vertically, and the preview offers a return control
that focuses the selected file. The native folder disclosure and file buttons remain keyboard
reachable without relying on pointer-only actions.
