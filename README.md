# Nodoplex

Graph-based project management platform. All project entities -- visions, goals, features, requirements, tasks, and git refs -- are stored as nodes in a Neo4j graph, giving every stakeholder a single shared view of how work connects to company goals.

## Architecture

```
Vision → Goal → Feature → Requirement → Task → GitRef
         (BREAKS_DOWN_TO)              (IMPLEMENTED_BY)
```

- **Backend**: Go standard library `net/http` (Go 1.27+)
- **Graph DB**: Neo4j (stores all project entities and relationships)
- **Change log**: PostgreSQL (persists an append-only changelog for the changefeed)
- **API spec**: OpenAPI 3.0.3 (`api/openapi.yaml`), code generated via `oapi-codegen`

## Quick start

### Prerequisites

- Go 1.27+
- Docker and Docker Compose

### 1. Start databases

```bash
make docker-up
```

This starts Neo4j (bolt://localhost:7687, browser at http://localhost:7474) and PostgreSQL (localhost:5432).

The compose file uses Postgres 18, which can't open a `pg_data` volume created by Postgres 17 or earlier: the container exits and its log mentions `pg_upgrade`. To reset local data, run `docker compose down -v` (this also deletes the Neo4j data), or remove only the Postgres volume: find it with `docker volume ls` (it ends in `_pg_data`) and run `docker volume rm <name>`. To keep an existing changelog, export it with `pg_dump -Fc` from Postgres 17 and import it with `pg_restore` after the upgrade.

### 2. Build and run

```bash
cp .env.example .env   # adjust if needed
make run
```

The server starts on http://localhost:8080.

### 3. Verify

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

## API examples

### Create a vision

```bash
curl -s -X POST http://localhost:8080/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"vision","title":"Become the #1 PM tool","description":"Our north star"}' | jq
```

### Create a goal under the vision

```bash
curl -s -X POST http://localhost:8080/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"goal","title":"Launch MVP by Q3"}' | jq
```

### Link them

```bash
curl -s -X POST http://localhost:8080/edges \
  -H 'Content-Type: application/json' \
  -d '{"sourceId":"<vision-id>","targetId":"<goal-id>","type":"BREAKS_DOWN_TO"}' | jq
```

### Create a feature with priority

```bash
curl -s -X POST http://localhost:8080/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"feature","title":"SSO integration","priority":1}' | jq
```

### Create a task

```bash
curl -s -X POST http://localhost:8080/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"task","title":"Implement OIDC flow","status":"backlog"}' | jq
```

### Attach a git ref to a task

```bash
curl -s -X POST http://localhost:8080/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"gitref","repo":"d4nyll/nodoplex","refType":"branch","ref":"feat/oidc","url":"https://github.com/d4nyll/nodoplex/tree/feat/oidc"}' | jq

curl -s -X POST http://localhost:8080/edges \
  -H 'Content-Type: application/json' \
  -d '{"sourceId":"<task-id>","targetId":"<gitref-id>","type":"IMPLEMENTED_BY"}' | jq
```

### List features

```bash
curl -s 'http://localhost:8080/nodes?type=feature' | jq
```

### Get a node with its relationships

```bash
curl -s http://localhost:8080/nodes/<id> | jq
```

### Get the full subtree from a vision

```bash
curl -s http://localhost:8080/nodes/<vision-id>/subtree | jq
```

### Trace lineage from a task up to the root vision

```bash
curl -s http://localhost:8080/nodes/<task-id>/lineage | jq
```

### Subscribe to changes (SSE)

```bash
# All changes
curl -N http://localhost:8080/subscribe

# Only feature and task changes
curl -N 'http://localhost:8080/subscribe?type=feature&type=task'
```

### Poll changes since a changeId

```bash
curl -s 'http://localhost:8080/changes?type=feature&since=0&limit=50' | jq
```

## API documentation

The full OpenAPI spec is at [`api/openapi.yaml`](api/openapi.yaml). You can view it in any OpenAPI viewer (e.g. Swagger UI, Redocly).

## Development

```bash
# Regenerate code from OpenAPI spec
make generate

# Build
make build

# Run tests
make test

# Stop databases
make docker-down
```

## Project structure

```
nodoplex/
  api/
    openapi.yaml           # OpenAPI 3.0.3 spec (source of truth)
    oapi-codegen.yaml      # Code generation config
  cmd/server/
    main.go                # Entry point
  internal/
    api/
      generated.go         # Generated types + server interface
      handlers.go          # ServerInterface implementation
      convert.go           # Type conversion helpers
    changelog/
      store.go             # PostgreSQL changelog repository
      migrations.go        # Schema migration
    config/
      config.go            # Environment-based configuration
    db/
      neo4j.go             # Neo4j driver setup
      postgres.go          # PostgreSQL pool setup
    events/
      bus.go               # In-memory pub/sub event bus
    graph/
      models.go            # Domain models
      repository.go        # Neo4j Cypher queries
      service.go           # Business logic
```
