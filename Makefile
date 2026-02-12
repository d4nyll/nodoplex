.PHONY: generate build run test docker-up docker-down docker-rebuild clean

generate:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml

build: generate
	go build -o bin/server ./cmd/server

run: build
	./bin/server

test:
	go test ./...

docker-up:
	docker compose up -d

docker-rebuild:
	docker compose up -d --build api

docker-down:
	docker compose down

clean:
	rm -rf bin/
