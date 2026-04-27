# ghreplica Conventions To Reuse

This project should follow the useful Go, Echo, GORM, database, and runtime conventions from `../ghreplica`.

## Project Shape

- `cmd/<binary>/main.go` should stay thin and only handle CLI or process entrypoint wiring.
- `internal/app` should assemble runtime dependencies and own process lifecycle.
- `internal/config` should load environment variables into one typed `Config`.
- `internal/httpapi` should own the Echo server, route registration, request parsing, and response shaping.
- `internal/database` should own GORM models, explicit table names, database opening, schema application, and pool config.
- Domain-specific work should live in focused packages, for example:
  - `internal/slack`
  - `internal/e2b`
  - `internal/workers`
  - `internal/templates`
  - `internal/sandboxes`

## Database

- Use GORM models as explicit structs.
- Every persistent model must declare an explicit `TableName()` method.
- Keep table-name declarations together in `internal/database/table_names.go`.
- For e2b-agents, apply the schema from GORM models instead of keeping SQL migration files.
- The `migrate up` command should call the GORM schema application path directly.
- Use local surrogate IDs where useful, but preserve external IDs exactly.
- For this project, E2B sandbox IDs should be treated as agent IDs.
- E2B template IDs should be treated as agent image IDs.
- Core query fields should be first-class columns.
- Long-tail source data can live in `JSONB` columns, but JSONB should not replace core indexed fields.

## API

- Wrap Echo in a `Server` struct.
- Inject dependencies through an `Options` struct.
- Register routes centrally in a `registerRoutes()` method.
- Keep handlers thin:
  - parse params and query strings
  - validate inputs
  - call a database or service dependency
  - return JSON or simple error responses
- Use path-based API versioning such as `/v1/...`.
- Include standard health endpoints:
  - `GET /healthz`
  - `GET /readyz`
  - optionally `GET /metrics`
- Use simple JSON error shapes such as:

```json
{"message":"Not Found"}
```

- Use `page` and `per_page` for paginated list endpoints when pagination is needed.

## Runtime Assembly

- Build a central runtime struct in `internal/app`, similar to `ServeRuntime`.
- Runtime assembly should open database handles, create clients, construct services, create the HTTP server, and start workers.
- Runtime dependencies should have explicit cleanup through `Close()` methods.
- Use context cancellation for server shutdown and worker shutdown.
- Keep HTTP-serving work and background-worker work separate in the runtime model.
- If needed, use separate database handles or pool settings for different workload classes, for example:
  - control/API
  - queue/jobs
  - Slack webhook ingestion
  - E2B sandbox reconciliation

## Background Work

- Keep request paths small.
- Persist important inbound events first, then process asynchronously.
- Model background jobs with typed argument structs.
- Use durable jobs for work that must survive process restarts.
- Use bounded retries with jitter for external-service work.
- Split concepts clearly:
  - accept inbound event
  - persist raw delivery
  - enqueue job
  - process delivery
  - update canonical state or projections

## Service Boundaries

- Use small interfaces between packages instead of passing concrete mega-services everywhere.
- HTTP handlers should depend on narrow service interfaces.
- Webhook/event acceptors should depend on narrow processor or dispatcher interfaces.
- Keep Slack-specific concepts out of core E2B sandbox identity.
- Slack workspaces and channels are gateway state, not the center of the product model.

## Config

- Load environment variables into one typed `Config`.
- Trim string values.
- Parse durations from strings like `30s`, `15m`, or `1h`.
- Provide conservative defaults for local development.
- Validate config before starting runtime.
- Validation should check:
  - required database settings
  - required Slack settings
  - required E2B API key settings
  - filesystem paths
  - incompatible options
  - connection pool budgets, if multiple pools are used

## Testing

- Put tests near the package under test.
- Use SQLite in-memory databases for fast database tests when possible.
- Provide a test schema helper instead of using runtime migrations in every unit test.
- Add a test that verifies every schema model declares `TableName()`.
- Add HTTP tests around route behavior and response shapes.
- Add service tests around Slack event handling, E2B sandbox reconciliation, and job processing.

## Implementation Direction For e2b-agents

The likely initial package layout should be:

```text
cmd/e2b-agents/
internal/app/
internal/config/
internal/database/
internal/httpapi/
internal/slack/
internal/e2b/
internal/workers/
docs/
```

The first implementation should keep the useful discipline from `ghreplica`: explicit tables, explicit runtime wiring, thin HTTP handlers, typed config, GORM-managed schema application, and narrow interfaces between Slack, E2B, database, and worker code.
