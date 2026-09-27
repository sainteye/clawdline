# taskapi

A small task-list HTTP service (Go standard library only).

```sh
go test ./...
go run .                        # listens on 127.0.0.1:8080, data in data/tasks.json
ADDR=127.0.0.1:9090 DATA=/tmp/t.json TZ_NAME=Asia/Taipei go run .
```

Endpoints: `GET /healthz`, `GET /tasks`, `POST /tasks`, `GET /tasks/{id}`, `PATCH /tasks/{id}`,
`DELETE /tasks/{id}`. The data file is JSON; on start-up a v1 file is migrated to v2 (see
`docs/TICKET-142.md`).
