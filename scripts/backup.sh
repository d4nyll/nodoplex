#!/bin/sh
# Backs up both databases of the running stack to ~/nodoplex-backups/<date-time>/,
# without stopping anything. Run it from the folder that holds docker-compose.yaml.
#
# To restore a backup into a stack with empty databases:
#   1. docker compose up -d neo4j postgres     (not the api, so that the databases stay empty)
#   2. docker compose exec -T neo4j cypher-shell -u neo4j -p changeme < <backup>/neo4j.cypher
#   3. docker compose exec -T postgres pg_restore -U nodoplex -d nodoplex < <backup>/postgres.dump
#   4. docker compose up -d api
set -eu

dir="$HOME/nodoplex-backups/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$dir"

docker compose exec -T postgres pg_dump -U nodoplex -Fc nodoplex > "$dir/postgres.dump"

# APOC writes every Node, Edge, index, and constraint as Cypher statements
curl -sf -u neo4j:changeme -H 'Content-Type: application/json' http://localhost:7474/db/neo4j/tx/commit \
  -d '{"statements":[{"statement":"CALL apoc.export.cypher.all(null, {stream: true, format: \"plain\"}) YIELD cypherStatements RETURN cypherStatements"}]}' \
  | jq -je 'if .errors != [] then error(.errors | tostring) else .results[0].data[].row[0] end' > "$dir/neo4j.cypher"

echo "Backed up to $dir"
