package store

import (
	"context"
	"database/sql"
	"fmt"

	"simple-ipam/internal/ipam"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore persists prefixes and IP allocations using database/sql.
type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{db: db}
}

func (s *MySQLStore) CreatePrefix(ctx context.Context, p ipam.Prefix) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO prefixes (family, cidr, prefix_len, start_addr, end_addr, status)
		VALUES (?, ?, ?, ?, ?, ?)
	`, p.Family, p.CIDR, p.PrefixLen, p.Start, p.End, p.Status)
	if err != nil {
		return 0, fmt.Errorf("insert prefix: %w", err)
	}
	return res.LastInsertId()
}

func (s *MySQLStore) ListPrefixes(ctx context.Context) ([]ipam.Prefix, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, family, cidr, prefix_len, start_addr, end_addr, status FROM prefixes ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list prefixes: %w", err)
	}
	defer rows.Close()

	var out []ipam.Prefix
	for rows.Next() {
		var p ipam.Prefix
		if err := rows.Scan(&p.ID, &p.Family, &p.CIDR, &p.PrefixLen, &p.Start, &p.End, &p.Status); err != nil {
			return nil, fmt.Errorf("scan prefix: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetPrefix(ctx context.Context, id int64) (ipam.Prefix, error) {
	var p ipam.Prefix
	err := s.db.QueryRowContext(ctx, `SELECT id, family, cidr, prefix_len, start_addr, end_addr, status FROM prefixes WHERE id = ?`, id).
		Scan(&p.ID, &p.Family, &p.CIDR, &p.PrefixLen, &p.Start, &p.End, &p.Status)
	if err != nil {
		return ipam.Prefix{}, fmt.Errorf("get prefix: %w", err)
	}
	return p, nil
}

func (s *MySQLStore) ListIPsInPrefix(ctx context.Context, prefixID int64) ([]ipam.IPAddress, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, family, address, prefix_id, status FROM ip_addresses WHERE prefix_id = ? ORDER BY id ASC`, prefixID)
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

func (s *MySQLStore) CreateIP(ctx context.Context, ip ipam.IPAddress) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ip_addresses (family, address, prefix_id, status)
		VALUES (?, ?, ?, ?)
	`, ip.Family, ip.Address, ip.PrefixID, ip.Status)
	if err != nil {
		return 0, fmt.Errorf("insert ip: %w", err)
	}
	return res.LastInsertId()
}
