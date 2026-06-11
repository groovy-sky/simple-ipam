# IPAM MVP Plan (Go + MySQL, No VRF)

## 1) Goal
Build a minimal, production-minded IPAM service with:
- Prefix management (IPv4 + IPv6)
- IP address allocation within prefixes
- Overlap prevention for prefixes
- Simple REST API

## 2) Tech Stack
- Go
- MySQL
- DB access: database/sql + sqlc
- Validation/parsing: net/netip


## 3) Architecture
- `cmd/api`: service bootstrap
- `internal/api`: HTTP handlers + request/response DTOs
- `internal/ipam`: domain/service logic
- `internal/store`: repository and SQLC-generated queries
- `internal/util`: IP/CIDR conversion helpers

Service rules:
- Canonicalize all prefixes/IPs before write
- Store both canonical text and numeric bounds
- Use transactions + row locking for allocation

## 4) Data Model
### Table: `prefixes`
Fields:
- id
- family (4/6)
- cidr (canonical text)
- prefix_len
- start/end bounds:
  - IPv4: `start_ipv4`, `end_ipv4` (INT UNSIGNED)
  - IPv6: `start_ipv6_hi`, `start_ipv6_lo`, `end_ipv6_hi`, `end_ipv6_lo` (BIGINT UNSIGNED)
- status
- created_at

Indexes/constraints:
- Exact uniqueness by family + network start + prefix_len
- Range indexes for overlap checks

### Table: `ip_addresses`
Fields:
- id
- family (4/6)
- address (canonical text)
- prefix_id (FK)
- numeric IP fields:
  - IPv4: `ip_ipv4`
  - IPv6: `ip_ipv6_hi`, `ip_ipv6_lo`
- status
- created_at

Indexes/constraints:
- Unique IP per family globally
- FK index by prefix_id

## 5) API Endpoints
- `POST /prefixes`
  - Create prefix after overlap validation
- `GET /prefixes`
  - List prefixes (pagination optional in MVP)
- `POST /ip-addresses`
  - Create IP inside prefix
- `POST /prefixes/{id}/allocate-ip`
  - Allocate next available IP inside prefix
- `GET /prefixes/{id}/ips`
  - List assigned IPs in prefix

## 6) Validation & Core Logic
### Prefix creation
1. Parse CIDR via `netip.ParsePrefix`
2. Canonicalize network address
3. Compute numeric bounds
4. In transaction, reject overlap for same family
5. Insert prefix

### IP creation
1. Parse IP via `netip.ParseAddr`
2. Resolve target prefix
3. Ensure IP is inside prefix bounds
4. Enforce uniqueness
5. Insert IP

### Next-available allocation
1. Begin transaction
2. Lock prefix row (`SELECT ... FOR UPDATE`)
3. Read used IPs in ascending order
4. Find first gap in usable host range
5. Insert allocated IP
6. Commit

Edge cases handled explicitly:
- /31, /32 (IPv4)
- /127, /128 (IPv6)
- Prefix with no usable host addresses

## 7) SQL Patterns
### IPv4 overlap detection
Condition for overlap:
- `existing.start <= new.end AND existing.end >= new.start`

### Containment check
- `ip >= prefix.start AND ip <= prefix.end`

IPv6 uses hi/lo lexicographic comparison via SQL predicates (or filtered in Go for MVP simplicity).
