# Build stage
FROM golang:1.27 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

# Runtime stage
FROM gcr.io/distroless/static-debian13

COPY --from=builder /server /server

EXPOSE 8080

ENTRYPOINT ["/server"]
