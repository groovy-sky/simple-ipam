# simple-ipam

A minimal Go-based IP address management (IPAM) service with a simple HTTP API and a MySQL-backed persistence layer.

## What this project does

The current service supports:

- Creating top-level prefixes (IPv4/IPv6) with overlap rejection
- Listing existing top-level prefixes
- Allocating the next free IP inside a prefix with transactional locking
- Creating specific IP addresses inside a prefix
- Listing assigned IPs for a prefix
- Allocating the next free child subnet inside a parent prefix
- Organizing address space with spaces and blocks

The implementation stays intentionally small and production-minded:

- `cmd/api` starts the HTTP service
- `internal/api` contains the REST handlers
- `internal/ipam` holds the core service logic
- `internal/store` contains the MySQL persistence adapter and schema

## Project layout

- `cmd/api/main.go` — application entry point
- `internal/api/handlers.go` — HTTP endpoints
- `internal/ipam/service.go` — prefix/IP/subnet allocation logic
- `internal/ipam/service_test.go` — unit tests for the core logic
- `internal/store/mysql_store.go` — DB access using `database/sql`
- `internal/store/schema.sql` — MySQL schema for prefixes, IPs, spaces, and blocks

## How it works

1. The API receives a prefix such as `10.0.0.0/24`.
2. The service canonicalizes the prefix and computes text and numeric bounds.
3. Prefixes and blocks are checked for overlap before they are written.
4. IP allocation locks the target prefix row, scans used addresses in ascending order, and stores the first free host address.
5. Child subnet allocation stores reserved subnets in `prefixes` with a nullable `parent_id` so later requests return the next free child block.

## Schema notes

`internal/store/schema.sql` now includes:

- Numeric prefix bounds for overlap checks:
  - IPv4: `start_ipv4`, `end_ipv4`
  - IPv6: `start_ipv6_hi`, `start_ipv6_lo`, `end_ipv6_hi`, `end_ipv6_lo`
- Numeric IP columns for ordered allocation:
  - IPv4: `ip_ipv4`
  - IPv6: `ip_ipv6_hi`, `ip_ipv6_lo`
- `prefixes.parent_id` for child subnet reservations
- `spaces` and `blocks` tables for the organizational hierarchy

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
  -d '{"cidr":"10.0.0.0/24"}'
```

### List prefixes

```sh
curl http://localhost:8080/prefixes
```

### Create a specific IP address

```sh
curl -X POST http://localhost:8080/ip-addresses \
  -H 'Content-Type: application/json' \
  -d '{"address":"10.0.0.10","prefix_id":1,"status":"reserved"}'
```

### Allocate the next IP in a prefix

```sh
curl -X POST http://localhost:8080/prefixes/1/allocate-ip
```

### Allocate the next child subnet in a prefix

```sh
curl -X POST http://localhost:8080/prefixes/1/allocate-subnet \
  -H 'Content-Type: application/json' \
  -d '{"size":26}'
```

### List IPs in a prefix

```sh
curl http://localhost:8080/prefixes/1/ips
```

### Create a space

```sh
curl -X POST http://localhost:8080/spaces \
  -H 'Content-Type: application/json' \
  -d '{"name":"engineering","description":"Shared engineering address space"}'
```

### List spaces

```sh
curl http://localhost:8080/spaces
```

### Create a block in a space

```sh
curl -X POST http://localhost:8080/spaces/1/blocks \
  -H 'Content-Type: application/json' \
  -d '{"cidr":"10.1.0.0/16"}'
```

### List blocks in a space

```sh
curl http://localhost:8080/spaces/1/blocks
```

## Development and testing

Run the checks:

```sh
go build ./...
go vet ./...
go test ./...
```
