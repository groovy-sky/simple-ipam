# simple-ipam

A minimal Go-based IP address management (IPAM) MVP with a simple HTTP API and a MySQL-backed persistence layer.

## What this project does

The current MVP supports:

- Creating prefixes (IPv4/IPv6)
- Listing existing prefixes
- Allocating the next free IP inside a prefix
- Listing assigned IPs for a prefix

The implementation is intentionally small and production-minded:

- `cmd/api` starts the HTTP service
- `internal/api` contains the REST handlers
- `internal/ipam` holds the core service logic
- `internal/store` contains the MySQL persistence adapter and schema

## Project layout

- `cmd/api/main.go` — application entry point
- `internal/api/handlers.go` — HTTP endpoints
- `internal/ipam/service.go` — prefix/IP allocation logic
- `internal/ipam/service_test.go` — unit tests for the core logic
- `internal/store/mysql_store.go` — DB access using `database/sql`
- `internal/store/schema.sql` — MySQL schema for the MVP

## How it works

1. The API receives a prefix such as `10.0.0.0/24`.
2. The service canonicalizes the prefix and computes its range.
3. Prefixes are stored in MySQL through the repository layer.
4. When you allocate the next IP in a prefix, the service scans for the first unused host address and stores it.

## Prerequisites

- Go 1.24+
- MySQL server running locally (or any reachable MySQL instance)

## Run the service

1. Create or point to a MySQL database.
2. Apply the schema from `internal/store/schema.sql`.
3. Set the DSN environment variable if needed:

   ```sh
   export IPAM_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/ipam?parseTime=true'
   ```

4. Start the API:

   ```sh
   go run ./cmd/api
   ```

5. The server listens on port `8080`.

## API examples

### Create a prefix

```sh
curl -X POST http://localhost:8080/prefixes \
  -H 'Content-Type: application/json' \
  -d '{"cidr":"10.0.0.0/30"}'
```

### List prefixes

```sh
curl http://localhost:8080/prefixes
```

### Allocate the next IP in a prefix

```sh
curl -X POST http://localhost:8080/prefixes/1/allocate-ip
```

### List IPs in a prefix

```sh
curl http://localhost:8080/prefixes/1/ips
```

## Development and testing

Run the tests:

```sh
go test ./...
```
