package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	mysqlerr "github.com/go-sql-driver/mysql"

	"simple-ipam/internal/ipam"
)

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type queryStore struct {
	exec sqlExecutor
}

// MySQLStore persists prefixes and IP allocations using database/sql.
type MySQLStore struct {
	db *sql.DB
	*queryStore
}

type txStore struct {
	*queryStore
}

func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{db: db, queryStore: &queryStore{exec: db}}
}

func (s *MySQLStore) InTx(ctx context.Context, fn func(ipam.Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	wrapped := &txStore{queryStore: &queryStore{exec: tx}}
	if err := fn(wrapped); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("rollback tx: %v (original error: %w)", rollbackErr, err)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *txStore) InTx(_ context.Context, fn func(ipam.Store) error) error {
	return fn(s)
}

func (s *queryStore) CreatePrefix(ctx context.Context, p ipam.Prefix) (int64, error) {
	res, err := s.exec.ExecContext(ctx, `
INSERT INTO prefixes (
parent_id, block_id, family, cidr, prefix_len, start_addr, end_addr,
start_ipv4, end_ipv4, start_ipv6_hi, start_ipv6_lo, end_ipv6_hi, end_ipv6_lo, status
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, nullableInt64(p.ParentID), nullableInt64(p.BlockID), p.Family, p.CIDR, p.PrefixLen, p.Start, p.End,
		nullableUint32(p.StartIPv4), nullableUint32(p.EndIPv4), nullableUint64(p.StartIPv6Hi), nullableUint64(p.StartIPv6Lo), nullableUint64(p.EndIPv6Hi), nullableUint64(p.EndIPv6Lo), p.Status)
	if err != nil {
		if isMySQLDuplicate(err) {
			return 0, fmt.Errorf("insert prefix: %w", ipam.ErrPrefixOverlap)
		}
		return 0, fmt.Errorf("insert prefix: %w", err)
	}
	return res.LastInsertId()
}

func (s *queryStore) FindOverlappingPrefix(ctx context.Context, p ipam.Prefix) (ipam.Prefix, error) {
	query, args := overlapPrefixQuery(p)
	var existing ipam.Prefix
	row := s.exec.QueryRowContext(ctx, query, args...)
	if err := row.Scan(&existing.ID, &existing.Family, &existing.CIDR, &existing.PrefixLen, &existing.Start, &existing.End, &existing.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ipam.Prefix{}, nil
		}
		return ipam.Prefix{}, fmt.Errorf("find overlapping prefix: %w", err)
	}
	return existing, nil
}

func (s *queryStore) ListPrefixes(ctx context.Context) ([]ipam.Prefix, error) {
	rows, err := s.exec.QueryContext(ctx, `
SELECT id, parent_id, block_id, family, cidr, prefix_len, start_addr, end_addr, status
FROM prefixes
WHERE parent_id IS NULL
ORDER BY id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list prefixes: %w", err)
	}
	defer rows.Close()

	var out []ipam.Prefix
	for rows.Next() {
		prefix, err := scanPrefix(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, prefix)
	}
	return out, rows.Err()
}

func (s *queryStore) GetPrefix(ctx context.Context, id int64) (ipam.Prefix, error) {
	row := s.exec.QueryRowContext(ctx, `
SELECT id, parent_id, block_id, family, cidr, prefix_len, start_addr, end_addr, status
FROM prefixes
WHERE id = ?
`, id)
	prefix, err := scanPrefix(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ipam.Prefix{}, fmt.Errorf("get prefix: %w", ipam.ErrNotFound)
		}
		return ipam.Prefix{}, fmt.Errorf("get prefix: %w", err)
	}
	return prefix, nil
}

func (s *queryStore) GetPrefixForUpdate(ctx context.Context, id int64) (ipam.Prefix, error) {
	row := s.exec.QueryRowContext(ctx, `
SELECT id, parent_id, block_id, family, cidr, prefix_len, start_addr, end_addr, status
FROM prefixes
WHERE id = ?
FOR UPDATE
`, id)
	prefix, err := scanPrefix(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ipam.Prefix{}, fmt.Errorf("lock prefix: %w", ipam.ErrNotFound)
		}
		return ipam.Prefix{}, fmt.Errorf("lock prefix: %w", err)
	}
	return prefix, nil
}

func (s *queryStore) ListChildPrefixes(ctx context.Context, parentID int64) ([]ipam.Prefix, error) {
	rows, err := s.exec.QueryContext(ctx, `
SELECT id, parent_id, block_id, family, cidr, prefix_len, start_addr, end_addr, status
FROM prefixes
WHERE parent_id = ?
ORDER BY family ASC, start_ipv4 ASC, start_ipv6_hi ASC, start_ipv6_lo ASC, id ASC
`, parentID)
	if err != nil {
		return nil, fmt.Errorf("list child prefixes: %w", err)
	}
	defer rows.Close()

	var out []ipam.Prefix
	for rows.Next() {
		prefix, err := scanPrefix(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, prefix)
	}
	return out, rows.Err()
}

func (s *queryStore) ListIPsInPrefix(ctx context.Context, prefixID int64) ([]ipam.IPAddress, error) {
	rows, err := s.exec.QueryContext(ctx, `
SELECT id, family, address, prefix_id, status
FROM ip_addresses
WHERE prefix_id = ?
ORDER BY family ASC, ip_ipv4 ASC, ip_ipv6_hi ASC, ip_ipv6_lo ASC, id ASC
`, prefixID)
	if err != nil {
		return nil, fmt.Errorf("list ips: %w", err)
	}
	defer rows.Close()

	var out []ipam.IPAddress
	for rows.Next() {
		var ip ipam.IPAddress
		if err := rows.Scan(&ip.ID, &ip.Family, &ip.Address, &ip.PrefixID, &ip.Status); err != nil {
			return nil, fmt.Errorf("scan ip: %w", err)
		}
		out = append(out, ip)
	}
	return out, rows.Err()
}

func (s *queryStore) CreateIP(ctx context.Context, ip ipam.IPAddress) (int64, error) {
	res, err := s.exec.ExecContext(ctx, `
INSERT INTO ip_addresses (family, address, prefix_id, ip_ipv4, ip_ipv6_hi, ip_ipv6_lo, status)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, ip.Family, ip.Address, ip.PrefixID, nullableUint32(ip.IPv4), nullableUint64(ip.IPv6Hi), nullableUint64(ip.IPv6Lo), ip.Status)
	if err != nil {
		if isMySQLDuplicate(err) {
			return 0, fmt.Errorf("insert ip: %w", ipam.ErrDuplicateIP)
		}
		return 0, fmt.Errorf("insert ip: %w", err)
	}
	return res.LastInsertId()
}

func (s *queryStore) CreateSpace(ctx context.Context, space ipam.Space) (int64, error) {
	res, err := s.exec.ExecContext(ctx, `
INSERT INTO spaces (name, description)
VALUES (?, ?)
`, space.Name, emptyStringToNil(space.Description))
	if err != nil {
		return 0, fmt.Errorf("insert space: %w", err)
	}
	return res.LastInsertId()
}

func (s *queryStore) ListSpaces(ctx context.Context) ([]ipam.Space, error) {
	rows, err := s.exec.QueryContext(ctx, `SELECT id, name, COALESCE(description, '') FROM spaces ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list spaces: %w", err)
	}
	defer rows.Close()

	var out []ipam.Space
	for rows.Next() {
		var space ipam.Space
		if err := rows.Scan(&space.ID, &space.Name, &space.Description); err != nil {
			return nil, fmt.Errorf("scan space: %w", err)
		}
		out = append(out, space)
	}
	return out, rows.Err()
}

func (s *queryStore) GetSpace(ctx context.Context, id int64) (ipam.Space, error) {
	var space ipam.Space
	err := s.exec.QueryRowContext(ctx, `SELECT id, name, COALESCE(description, '') FROM spaces WHERE id = ?`, id).
		Scan(&space.ID, &space.Name, &space.Description)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ipam.Space{}, fmt.Errorf("get space: %w", ipam.ErrNotFound)
		}
		return ipam.Space{}, fmt.Errorf("get space: %w", err)
	}
	return space, nil
}

func (s *queryStore) CreateBlock(ctx context.Context, block ipam.Block) (int64, error) {
	res, err := s.exec.ExecContext(ctx, `
INSERT INTO blocks (
space_id, family, cidr, prefix_len, start_addr, end_addr,
start_ipv4, end_ipv4, start_ipv6_hi, start_ipv6_lo, end_ipv6_hi, end_ipv6_lo, status
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, block.SpaceID, block.Family, block.CIDR, block.PrefixLen, block.Start, block.End,
		nullableUint32(block.StartIPv4), nullableUint32(block.EndIPv4), nullableUint64(block.StartIPv6Hi), nullableUint64(block.StartIPv6Lo), nullableUint64(block.EndIPv6Hi), nullableUint64(block.EndIPv6Lo), block.Status)
	if err != nil {
		if isMySQLDuplicate(err) {
			return 0, fmt.Errorf("insert block: %w", ipam.ErrBlockOverlap)
		}
		return 0, fmt.Errorf("insert block: %w", err)
	}
	return res.LastInsertId()
}

func (s *queryStore) ListBlocks(ctx context.Context, spaceID int64) ([]ipam.Block, error) {
	rows, err := s.exec.QueryContext(ctx, `
SELECT id, space_id, family, cidr, prefix_len, start_addr, end_addr, status
FROM blocks
WHERE space_id = ?
ORDER BY id DESC
`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("list blocks: %w", err)
	}
	defer rows.Close()

	var out []ipam.Block
	for rows.Next() {
		var block ipam.Block
		if err := rows.Scan(&block.ID, &block.SpaceID, &block.Family, &block.CIDR, &block.PrefixLen, &block.Start, &block.End, &block.Status); err != nil {
			return nil, fmt.Errorf("scan block: %w", err)
		}
		out = append(out, block)
	}
	return out, rows.Err()
}

func (s *queryStore) FindOverlappingBlock(ctx context.Context, block ipam.Block) (ipam.Block, error) {
	query, args := overlapBlockQuery(block)
	var existing ipam.Block
	row := s.exec.QueryRowContext(ctx, query, args...)
	if err := row.Scan(&existing.ID, &existing.SpaceID, &existing.Family, &existing.CIDR, &existing.PrefixLen, &existing.Start, &existing.End, &existing.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ipam.Block{}, nil
		}
		return ipam.Block{}, fmt.Errorf("find overlapping block: %w", err)
	}
	return existing, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPrefix(s scanner) (ipam.Prefix, error) {
	var (
		prefix   ipam.Prefix
		parentID sql.NullInt64
		blockID  sql.NullInt64
	)
	if err := s.Scan(&prefix.ID, &parentID, &blockID, &prefix.Family, &prefix.CIDR, &prefix.PrefixLen, &prefix.Start, &prefix.End, &prefix.Status); err != nil {
		return ipam.Prefix{}, err
	}
	if parentID.Valid {
		value := parentID.Int64
		prefix.ParentID = &value
	}
	if blockID.Valid {
		value := blockID.Int64
		prefix.BlockID = &value
	}
	return prefix, nil
}

func overlapPrefixQuery(p ipam.Prefix) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
SELECT id, family, cidr, prefix_len, start_addr, end_addr, status
FROM prefixes
WHERE family = ?
`)
	args := []any{p.Family}
	if p.ParentID == nil {
		builder.WriteString(` AND parent_id IS NULL`)
	} else {
		builder.WriteString(` AND parent_id = ?`)
		args = append(args, *p.ParentID)
	}
	builder.WriteString(overlapWhereClause(p.Family, p.StartIPv4, p.EndIPv4, p.StartIPv6Hi, p.StartIPv6Lo, p.EndIPv6Hi, p.EndIPv6Lo))
	builder.WriteString(` ORDER BY id ASC LIMIT 1`)
	return builder.String(), argsWithRange(args, p.StartIPv4, p.EndIPv4, p.StartIPv6Hi, p.StartIPv6Lo, p.EndIPv6Hi, p.EndIPv6Lo)
}

func overlapBlockQuery(block ipam.Block) (string, []any) {
	query := `
SELECT id, space_id, family, cidr, prefix_len, start_addr, end_addr, status
FROM blocks
WHERE space_id = ? AND family = ?
` + overlapWhereClause(block.Family, block.StartIPv4, block.EndIPv4, block.StartIPv6Hi, block.StartIPv6Lo, block.EndIPv6Hi, block.EndIPv6Lo) + `
ORDER BY id ASC LIMIT 1
`
	args := []any{block.SpaceID, block.Family}
	return query, argsWithRange(args, block.StartIPv4, block.EndIPv4, block.StartIPv6Hi, block.StartIPv6Lo, block.EndIPv6Hi, block.EndIPv6Lo)
}

func overlapWhereClause(family int, startIPv4, endIPv4 *uint32, startIPv6Hi, startIPv6Lo, endIPv6Hi, endIPv6Lo *uint64) string {
	if family == 4 {
		return ` AND start_ipv4 <= ? AND end_ipv4 >= ?`
	}
	return ` AND (start_ipv6_hi < ? OR (start_ipv6_hi = ? AND start_ipv6_lo <= ?))
AND (end_ipv6_hi > ? OR (end_ipv6_hi = ? AND end_ipv6_lo >= ?))`
}

func argsWithRange(args []any, startIPv4, endIPv4 *uint32, startIPv6Hi, startIPv6Lo, endIPv6Hi, endIPv6Lo *uint64) []any {
	if startIPv4 != nil && endIPv4 != nil {
		// Match the placeholder order in `start_ipv4 <= ? AND end_ipv4 >= ?`.
		return append(args, uint64(*endIPv4), uint64(*startIPv4))
	}
	// Match the placeholder order in the IPv6 lexicographic overlap predicate.
	return append(args, *endIPv6Hi, *endIPv6Hi, *endIPv6Lo, *startIPv6Hi, *startIPv6Hi, *startIPv6Lo)
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableUint32(v *uint32) any {
	if v == nil {
		return nil
	}
	return uint64(*v)
}

func nullableUint64(v *uint64) any {
	if v == nil {
		return nil
	}
	return *v
}

func emptyStringToNil(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func isMySQLDuplicate(err error) bool {
	var mysqlError *mysqlerr.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
