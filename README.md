# Nodoplex

Nodoplex is a server that records the work on a product as a directed acyclic graph. Each item of work is a Node, and each relationship between 2 Nodes is a directed Edge. For each product, Nodoplex also keeps a Changefeed: a log of the changes to the graph.

The specifications in [`specs/`](specs/) define the behavior:

- [`00_mental-model.md`](specs/00_mental-model.md): the concepts
- [`01_base-specification.md`](specs/01_base-specification.md): the rules, the steps of each operation, and the error codes
- [`02_extended-specification.md`](specs/02_extended-specification.md): HTTP, JSON, server-sent events, storage, and configuration

The API contract is [`api/openapi.yaml`](api/openapi.yaml) (OpenAPI 3.0.3).

## Architecture

```
Product <- Vision <- Goal <- Feature <- Requirement <- Task <- SubTask
```

- **Backend**: Go standard library `net/http`
- **Neo4j**: the Nodes and the Edges, the latest Change ID of each Changefeed, and an outbox of Change Entries
- **PostgreSQL**: the Change Entries. Each write records its Change Entry in the outbox, in the same Neo4j transaction as its changes. The server then copies the Change Entry to PostgreSQL, in Change ID order, and wakes each open subscription of the Product.

## Quick start

Prerequisites: Go 1.27+, Docker with Docker Compose.

```bash
docker compose up -d --build
```

This starts the API (http://localhost:8080), Neo4j (bolt://localhost:7687, browser at http://localhost:7474), and PostgreSQL (localhost:5432). The API tries to start again every 2 s until Neo4j and PostgreSQL accept connections.

To run the server outside Docker, against the databases of the stack:

```bash
docker compose stop api
make build
set -a; . ./.env.example; set +a
./bin/server
```

## API examples

Each request must give the user ID in the header `Nodoplex-User-Id`. Each write to a Node, or to an Edge, sends the `version` of the Node (for an Edge, of its Tail Node) that the client last read.

```bash
H='Nodoplex-User-Id: alice'

# Create a Product. The slug is 3 or more labels, e.g. com.example.language-app
curl -s -X PUT -H "$H" localhost:8080/products/com.example.language-app \
  -d '{"fields":{"title":"A language-learning app"}}'

# Create a Vision under the Product (each Node except a Product names its Parents)
curl -s -X POST -H "$H" localhost:8080/products/com.example.language-app/nodes \
  -d '{"type":"vision","parents":[{"id":"<product-id>"}],"fields":{"title":"Make learning fun"}}'

# Task B depends on Task A
curl -s -X POST -H "$H" localhost:8080/products/com.example.language-app/edges \
  -d '{"kind":"dependency","tailId":"<task-b-id>","headId":"<task-a-id>","version":<task-b-version>}'

# Set the status of a leaf. Nodoplex derives the status of each other Node
curl -s -X PATCH -H "$H" localhost:8080/products/com.example.language-app/nodes/<task-id> \
  -d '{"version":<version>,"fields":{"status":"in_progress"}}'

# The Tasks that are not_started and not blocked, 50 at a time
curl -s -H "$H" 'localhost:8080/products/com.example.language-app/nodes?type=task&status=not_started&blocked=false'

# The next available leaf
curl -s -H "$H" localhost:8080/products/com.example.language-app/next-work

# Archive a Node
curl -s -X POST -H "$H" localhost:8080/products/com.example.language-app/nodes/<id>/archive -d '{"version":<version>}'
```

## Changefeed

```bash
# The Change Entries after Change ID 0, at most 100
curl -s -H "$H" 'localhost:8080/products/com.example.language-app/poll?startChangeId=0&limit=100'

# Only the Change Entries after which a Task is done
curl -s -G -H "$H" localhost:8080/products/com.example.language-app/poll \
  --data-urlencode startChangeId=0 --data-urlencode limit=100 \
  --data-urlencode 'filterGroups=[{"type":["task"],"status":{"check":"after","values":["done"]}}]'

# Server-sent events: each new Change Entry, as it happens
curl -N -H "$H" localhost:8080/products/com.example.language-app/subscribe

# Each Change Entry after Change ID 42, then each new one
curl -N -H "$H" -H 'Last-Event-ID: 42' localhost:8080/products/com.example.language-app/subscribe
```

The `id` of each event is the Change ID of its Change Entry. A browser `EventSource` can't set headers, so it can give the user ID in the query parameter `userId`.

## Health checks

Each health check needs no user ID, and sends status 200 and no body, or status 503 and the code `unavailable`:

- `GET /livez`: 200 whenever the server can answer an HTTP request
- `GET /startupz`: 200 after the server has connected to Neo4j and PostgreSQL, and has created its constraints, indexes, and tables
- `GET /readyz`: 200 if the server has started, and a query to Neo4j and a query to PostgreSQL both succeed within 2 s each

## Configuration

| Environment variable                    | Default      | Meaning                                                                                           |
| --------------------------------------- | ------------ | ------------------------------------------------------------------------------------------------- |
| `PORT`                                  | `8080`       | the HTTP port                                                                                     |
| `NEO4J_URI`                             | none         | e.g. `bolt://localhost:7687`                                                                      |
| `NEO4J_USER`, `NEO4J_PASSWORD`          | none         | the Neo4j credentials                                                                             |
| `POSTGRES_URL`                          | none         | e.g. `postgres://nodoplex:changeme@localhost:5432/nodoplex`                                       |
| `NODOPLEX_EXPOSE_CHANGEFEED`            | `true`       | `false` refuses poll and subscribe                                                                |
| `NODOPLEX_CHANGE_ENTRY_COUNT_TARGET`    | `1000000`    | above this number of Change Entries of all Products, a background job deletes the oldest ones     |
| `NODOPLEX_CHANGE_ENTRY_SIZE_TARGET`     | `1073741824` | above this total size, in bytes, a background job deletes the oldest Change Entries              |
| `NODOPLEX_DELETE_OLD_CHANGE_ENTRIES`    | `true`       | `false` keeps every Change Entry                                                                  |

The server doesn't start if a variable has a value that isn't valid, or if a variable with no default isn't set.

Only one server process may use a pair of databases at a time, because the server serializes the writes of each Product with locks in its own process.

## Development

```bash
make generate   # write internal/api/generated.go from api/openapi.yaml
go build ./...
go test ./...   # unit tests
```

The integration tests run against Neo4j and PostgreSQL. Each test uses its own PostgreSQL schema and its own Products. Stop the API of the stack first, because its outbox copier would copy the Change Entries of the tests:

```bash
docker compose up -d neo4j postgres
docker compose stop api
NODOPLEX_INTEGRATION=1 go test ./... -count=1
```

The [`bruno/`](bruno/) directory holds a [Bruno](https://www.usebruno.com/) collection of the operations.

## Project structure

```
cmd/server/         entry point
internal/
  api/              HTTP: routes, request checks, operations, server-sent events
  changefeed/       filter groups of the Changefeed, and the signals to subscriptions
  config/           configuration from the environment
  db/               Neo4j and PostgreSQL connections
  graph/            the part of the graph of a Product that an operation reads, in memory:
                    rules, operation steps, Change Entries
    graphtest/      generated graphs and operations for the tests that compare partial
                    graphs with whole graphs
  store/            Neo4j and PostgreSQL storage, the outbox, retention
```
