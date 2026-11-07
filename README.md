# Nodoplex

Graph-based project management platform. All project entities -- visions, goals, features, requirements, tasks, and outputs -- are stored as nodes in a Neo4j graph, giving every stakeholder a single shared view of how work connects to company goals.

## Architecture

```
Vision → Goal → Feature → Requirement → Task → Output
         (BREAKS_DOWN_TO)              (IMPLEMENTED_BY)
```

A node can also depend on another node of the same type through a `DEPENDS_ON` edge, for example requirement B depends on requirement A.

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

Every example works in the namespace `acme`. See [Namespaces](#namespaces).

### Create a vision

```bash
curl -s -X POST http://localhost:8080/namespaces/acme/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"vision","title":"Become the #1 PM tool","description":"Our north star"}' | jq
```

### Create a goal under the vision

```bash
curl -s -X POST http://localhost:8080/namespaces/acme/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"goal","title":"Launch MVP by Q3"}' | jq
```

### Link them

```bash
curl -s -X POST http://localhost:8080/namespaces/acme/edges \
  -H 'Content-Type: application/json' \
  -d '{"sourceId":"<vision-id>","targetId":"<goal-id>","type":"BREAKS_DOWN_TO"}' | jq
```

### Create a feature with priority

```bash
curl -s -X POST http://localhost:8080/namespaces/acme/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"feature","title":"SSO integration","priority":1}' | jq
```

### Create a task

```bash
curl -s -X POST http://localhost:8080/namespaces/acme/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"task","title":"Implement OIDC flow","status":"backlog"}' | jq
```

### Attach an output to a task

```bash
curl -s -X POST http://localhost:8080/namespaces/acme/nodes \
  -H 'Content-Type: application/json' \
  -d '{"type":"output","outputType":"git","description":"Branch with the OIDC flow","payload":{"repository":"https://github.com/d4nyll/nodoplex.git","baseRef":"main","currentRef":"feat/oidc"}}' | jq

curl -s -X POST http://localhost:8080/namespaces/acme/edges \
  -H 'Content-Type: application/json' \
  -d '{"sourceId":"<task-id>","targetId":"<output-id>","type":"IMPLEMENTED_BY"}' | jq
```

### Record that one requirement depends on another

```bash
curl -s -X POST http://localhost:8080/namespaces/acme/edges \
  -H 'Content-Type: application/json' \
  -d '{"sourceId":"<requirement-b-id>","targetId":"<requirement-a-id>","type":"DEPENDS_ON"}' | jq

# What requirement B depends on, and what depends on requirement A
curl -s http://localhost:8080/namespaces/acme/nodes/<requirement-b-id>/dependencies | jq
curl -s http://localhost:8080/namespaces/acme/nodes/<requirement-a-id>/dependents | jq
```

### List features

```bash
curl -s 'http://localhost:8080/namespaces/acme/nodes?type=feature' | jq
```

### Get a node with its relationships

```bash
curl -s http://localhost:8080/namespaces/acme/nodes/<id> | jq
```

### Get the full subtree from a vision

```bash
curl -s http://localhost:8080/namespaces/acme/nodes/<vision-id>/subtree | jq
```

### Trace lineage from a task up to the root vision

```bash
curl -s http://localhost:8080/namespaces/acme/nodes/<task-id>/lineage | jq
```

### Subscribe to changes (SSE)

```bash
# All changes in the namespace
curl -N http://localhost:8080/namespaces/acme/subscribe

# Only feature and task changes
curl -N 'http://localhost:8080/namespaces/acme/subscribe?type=feature&type=task'
```

### Poll changes since a changeId

```bash
curl -s 'http://localhost:8080/namespaces/acme/changes?type=feature&since=0&limit=50' | jq
```

## API documentation

The full OpenAPI spec is at [`api/openapi.yaml`](api/openapi.yaml). You can view it in any OpenAPI viewer (e.g. Swagger UI, Redocly).

### Namespaces

Every node belongs to exactly one namespace. A namespace is an opaque string that the caller picks, and Nodoplex attaches no meaning to it. It can be 1 to 256 characters long and may contain ASCII letters, digits, hyphens (`-`), underscores (`_`), colons (`:`) and periods (`.`), but can't start or end with a period. A node gets the namespace of the request that creates it, and the namespace never changes.

A request carries its namespace as the first path segment: every API path starts with `/namespaces/{namespace}/`, for example `GET /namespaces/acme/nodes/{id}`. Only `/health` sits outside a namespace. Nodoplex uses a path prefix rather than a header for three reasons:

- The OpenAPI spec declares the namespace as a required path parameter on every operation, so the generated server passes it to every handler as an argument. A handler that ignores it doesn't compile.
- Browsers can't set custom headers on an `EventSource`, so a header couldn't carry the namespace for `/subscribe`.
- The namespace shows up in every URL, so access logs and proxies see which namespace a request touched.

Every operation sees only its own namespace:

- Reads, lists, traversals, edge creation and deletion return 404 for a node in another namespace, with the same body as for a node that doesn't exist.
- An edge can only connect two nodes in the request's namespace, so no edge crosses namespaces.
- `/changes` and `/subscribe` only return changes made in the request's namespace. Change ids come from one sequence shared by all namespaces, so the ids a namespace sees have gaps where other namespaces made changes.
- A request to an unprefixed path such as `/nodes`, or with an empty namespace such as `/namespaces//nodes`, gets a 400. So does a namespace that is too long, contains any other character (for example `/`, `@`, a space or a non-ASCII letter, encoded or not), or starts or ends with a period.

Nodes carry a `:Node` label and a `namespace` property in Neo4j. Lookups by id seek the unique constraint on `(:Node).id` and lists seek the index on `(:Node).namespace`; `NODOPLEX_INTEGRATION=1 go test ./internal/graph -run Integration` checks the query plans.

### Dependencies

A `DEPENDS_ON` edge from node B to node A means B depends on A. `POST /namespaces/{namespace}/edges` with `"type":"DEPENDS_ON"` creates one:

- Both nodes must be in the request's namespace (404 otherwise) and have the same type (400 otherwise).
- A node can't depend on itself (400).
- An edge that would close a cycle of `DEPENDS_ON` edges gets a 409, whether the cycle is direct (A depends on B, B on A) or runs through other nodes.
- Creating an edge that already exists returns 201 and leaves the single existing edge in place, so a client can retry safely.

Concurrent requests can't form a cycle between them: the server checks for a cycle and creates the edge in one transaction that holds a per-namespace lock, so dependency creations in one namespace run one at a time.

`GET /namespaces/{namespace}/nodes/{id}/dependencies` lists the nodes a node depends on, and `GET /namespaces/{namespace}/nodes/{id}/dependents` lists the nodes that depend on it. `DELETE /namespaces/{namespace}/edges` removes a `DEPENDS_ON` edge like any other, and deleting a node removes its `DEPENDS_ON` edges. Children, parents, subtree, and lineage follow `BREAKS_DOWN_TO` and `IMPLEMENTED_BY` only, so `DEPENDS_ON` edges never appear in them.

The changelog, `/changes` and `/subscribe` record node changes only. Creating or deleting an edge of any type writes no changelog entry and sends no event.

### Outputs

An Output node records something a task produced, such as a Git branch, a document or a deployment. An `IMPLEMENTED_BY` edge from the task attaches it, and a task is the only node type that can be its parent. An Output has these fields:

- `outputType`: a non-empty string that names the kind of output, for example `git`. Nodoplex doesn't interpret it. The node type stays in `type`, which is `output`.
- `description`: text that says what the output is.
- `payload`: any JSON object. Nodoplex stores it and returns it unchanged, without interpreting or validating it. `PATCH` replaces the whole payload.

An output with `outputType` `git` uses this payload shape (`GitOutputPayload` in the OpenAPI spec):

```json
{
  "repository": "https://github.com/d4nyll/nodoplex.git",
  "baseRef": "main",
  "currentRef": "feat/oidc"
}
```

- `repository`: the Git remote URL.
- `baseRef`: the Git ref the work started from.
- `currentRef`: the Git ref that holds the work, for example a branch name or a commit SHA.

### Assigning a namespace to existing nodes

Nodes created before namespaces existed have no namespace. The server never returns them, and logs a warning with their count at startup. To move all of them, and their changelog entries, into one namespace, run the server once with `-assign-namespace` against the same databases:

```bash
go run ./cmd/server -assign-namespace=acme
# assign-namespace: moved 4 nodes and 5 changelog entries into namespace "acme"

# or, with Docker Compose
docker compose run --rm api -assign-namespace=acme
```

The command exits after the migration. It only touches nodes and changelog entries that have no namespace, so running it again changes nothing.

## Development

```bash
# Regenerate code from OpenAPI spec
make generate

# Build
make build

# Run tests
make test

# Run the integration tests too, against the databases from `make docker-up`
NODOPLEX_INTEGRATION=1 go test ./...

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
