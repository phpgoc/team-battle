# Team Battle

[简体中文](README.md) · **English** · [한국어](README.ko.md)

A single-binary tournament manager for round-one 1v1 matches, KOF, ace matches, live viewing, history entry, and player records.

## Use

### Start

```sh
go run . --db test.sqlite
```

The repository `test.sqlite` is a demo database. The administrator and demo players use `test1234`. Use a new database path for production.

### Workflow

1. A super administrator creates a tournament with its name, start time, and manager.
2. Add 6–12 players and choose fixed teams or a random draw.
3. Set round-one, KOF, and ace rules, complete the draw, and start the tournament.
4. The manager records each winner. KOF keeps the winner and replaces the loser. After one round win per side, draw the ace match.
5. Confirm the tournament after all matches finish. Only a super administrator can delete or restore a closed tournament.

Anyone can watch a public tournament without signing in. Live viewing opens after six players are entered and scores update through SSE. The home page filters history by tournament name, date, and player.

Super administrators manage accounts, tournaments, site settings, and backgrounds. Tournament managers handle their roster, lineups, results, and completion. Signed-in users can edit their profile and view records.

## Development

Go 1.22+ is required. The frontend is embedded with `go:embed`, with no Node service or external CDN. SQLite uses a pure-Go driver and supports `CGO_ENABLED=0`.

```sh
gofmt -w cmd internal main.go
go test ./...
go build ./...
```

End-to-end tests require Node.js 20+ and npm:

```sh
cd e2e
npm ci
npx playwright install chromium
npm test
```

Application code and tests are in `internal/app`, migrations are in `internal/migrations`, the production entry point is `cmd/team-battle`, and frontend assets are in `internal/app/web`. The demo database is `test.sqlite`; temporary data and test artifacts belong in `runtime`.


## Deploy

Build and run:

```sh
CGO_ENABLED=0 go build -trimpath -o team-battle ./cmd/team-battle
./team-battle --db ./runtime/team-battle.sqlite --host 127.0.0.1 --port 9527
```

The first start creates the database directory, schema, and `admin` account with password `test1234`. Migrations run before HTTP starts. Ctrl+C, SIGTERM, SIGHUP, and SIGQUIT wait for active requests before closing the database.

| Flag | Default | Description |
| --- | --- | --- |
| `--db` | `./runtime/team-battle.sqlite` | SQLite file |
| `--host` | `localhost` | HTTP listen host |
| `--port` | `9527` | HTTP listen port |
| `--http-max-reads` | `100` | Concurrent regular HTTP reads |
| `--http-max-writes` | `10` | Concurrent HTTP writes, including queued requests |
| `--db-max-connections` | `4` | Shared SQLite connection pool limit |
| `--sse-max-connections` | `500` | Shared SSE connection limit |
| `--language` | `zh` | Operation log language: `zh`, `en`, or `ko` |
| `--log` | quiet | `stdout` writes to standard output. Other values are appended as file paths |

Regular HTTP requests over their limit receive a 503 response. SQLite in WAL mode supports concurrent reads and a single writer. SSE has its own limit and releases database connections after setup.

The program serves HTTP. Put it behind Nginx for HTTPS.

Log examples:

```sh
./team-battle --log stdout
./team-battle --log ./runtime/team-battle.log
```

```nginx
client_max_body_size 50m;
location / {
    proxy_pass http://127.0.0.1:9527;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 3600s;
}
```

Stop the service before upgrading. Back up the database and its sibling `uploads/` directory, replace the binary, and start it again. The program applies forward migrations automatically. A failed migration stops startup. Restore the complete backup to roll back.
