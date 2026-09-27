# notesapi

A small notes API. Users authenticate with `Authorization: Bearer <token>` (seed users:
`tok-alice`, `tok-bob`, and the admin `tok-carol`).

| Route | What it does |
|---|---|
| `GET /me`, `PUT /me` | read or update your profile (name, email) |
| `POST /notes` | create a note |
| `GET/PUT/DELETE /notes/{id}` | read, edit, delete a note (owner, or an admin) |
| `GET /files?name=<path>` | read a shared document under `files/` |
| `GET /admin/stats` | counts, with the `X-Admin-Token` header |

    ADMIN_TOKEN=<token> go run .   # listens on $ADDR (default 127.0.0.1:8082); ADMIN_TOKEN is required
    go test ./...
