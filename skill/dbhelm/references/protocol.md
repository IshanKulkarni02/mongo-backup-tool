# DBHelm agent protocol

`dbhelm agent ...` talks to a local broker (`dbhelm serve`) over loopback HTTP
with a bearer token. The token and port are in `.dbhelm/run/broker.json`; the
CLI reads it for you. You should never need to call the broker directly.

## Envelope

```json
{ "ok": true,  "data": {}, "meta": { "rows": 3, "truncated": false, "ms": 12, "requestId": "req_..." } }
{ "ok": false, "error": { "code": "DENIED", "message": "...", "hint": "..." }, "data": {}, "meta": {} }
```

`data` may accompany an error (a pending request carries `requestId` and
`status`).

## Methods (agent door)

| Verb | Method | Notes |
|---|---|---|
| `status` | `ping` | `operatorAttached` tells you whether a person can approve changes |
| `connections` | `connections` | name, engine, capabilities, access (`read`/`write`). Never a URI |
| `databases` | `databases` | Postgres lists schemas |
| `schema` | `schema` | tables/collections with approximate counts |
| `describe` | `describe` | SQL: columns, keys, indexes. Documents: indexes and one sample |
| `query` | `query` | SQL, or `find` (filter/sort/skip/limit), or `pipeline` |
| `explain` | `explain` | plain `EXPLAIN` only; `ANALYZE` is refused |
| `snapshot list` | `snapshot.list` | newest first |
| `snapshot create` | `snapshot.create` | allowed with read access |
| `snapshot diff` | `snapshot.diff` | counts per table/collection, or one page (at most 100) of changed ids; leave `--to` empty to compare with the live database (MongoDB only) |
| `write` | `write` | request; needs `write` access |
| `snapshot restore` | `snapshot.restore` | request; needs `write` access; replaces current data |
| `request ID` | `request.get` | `--wait N` seconds |

## Limits

- Rows: 100 by default, at most 1000 (`--max-rows`); documents/pipeline results at most 199.
- Cells and documents are cut at 4 KB and marked `[cell truncated]`.
- Wall clock: 15 s per read.
- Server-side JavaScript in filters and pipelines (`$where`, `$function`,
  `$accumulator`) and `$out`/`$merge` are refused.

## Request life cycle

`pending` -> `running` -> `done` | `failed`, or `pending` -> `denied` |
`expired` (30 minutes without a decision). An approved request keeps running
even if you stopped waiting; fetch its result with `request ID`.
