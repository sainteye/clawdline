# notesapi

A small notes API. Users authenticate with `Authorization: Bearer <token>` (seed users:
`tok-alice`, `tok-bob`, and the admin `tok-carol`).

| Route | What it does |
|---|---|
| `GET /me`, `PUT /me` | read or update your profile (name, email) |
| `POST /notes` | create a note |
| `POST /notes/import` | import notes from the old client |
| `GET/PUT/DELETE /notes/{id}` | read, edit, delete a note (owner, or an admin) |
| `GET /search?owner=<id>&q=<text>` | search your notes (admins may search any owner) |
| `GET /files?name=<path>` | read a shared document under `files/` |
| `GET /admin/stats` | counts, with the `X-Admin-Token` header |
| `/integrations` | create, read, export, delete, rotate, test and receive hooks |

    go run .        # listens on $ADDR (default 127.0.0.1:8082); ADMIN_TOKEN
    go test ./...

JSON request bodies are limited to 1 MiB. Authentication requires the literal `Bearer` scheme.
Audit records are one physical log line per event. Imported notes always belong to the importing
user and cannot replace an existing note. Shared documents must remain below `files/`, including
when the directory contains symlinks.

Integrations belong to their creator; only that owner or an admin may read, export, delete, rotate
or test one. Testing must reject loopback/private destinations, including redirects, and read at
most 1 MiB. Secrets never appear in logs. Hook signatures are exact and constant-time; a
non-empty `X-Hook-Nonce` is accepted once per integration.
