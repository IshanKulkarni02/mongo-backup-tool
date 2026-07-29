# Dev seed data

Sample schemas and rows for manually exercising the desktop app end to
end, and for the optional Postgres/MySQL integration suite
(`internal/engine/postgres`, `internal/engine/mysql` — build-tagged
`integration`, not run by plain `go test ./...`).

## Postgres / MySQL

```bash
# Start local instances (nothing else in this repo starts these for you)
docker compose -f docker-compose.test.yml up -d

# Load sample data
psql "postgres://mongobak:mongobak@localhost:55432/mongobak_test" -f scripts/dev-seed/postgres.sql
mysql -h 127.0.0.1 -P 53306 -u mongobak -pmongobak mongobak_test < scripts/dev-seed/mysql.sql

# Run the optional integration suite against them
go test -tags=integration ./internal/engine/postgres/... ./internal/engine/mysql/...

# Tear down (and wipe the data) when done
docker compose -f docker-compose.test.yml down -v
```

Both seed scripts create the same shape — `users` → `orders` →
`order_items`, all with foreign keys — so Tables' relationship inspector,
Schema Diff (diff `pg-dev` against a second connection after editing one
side), and the AI mock-data/codegen features all have something real to
work against. `postgres.sql` has a commented-out PostGIS example if you
swap the compose file's image for `postgis/postgis:16-3.4` and want to try
the automatic `ST_AsGeoJSON` wrapping in Tables.

## MongoDB

```bash
mongosh "mongodb://localhost:27017/mongobak_test" scripts/dev-seed/mongo.js
```

Seeds a `users` collection with a toy embedding field (for Vector Compare)
and a `punches` collection with a GeoJSON `location` field (for the Geo
Viewer) — right-click either cell in Browser to send it to the matching
tool directly.
