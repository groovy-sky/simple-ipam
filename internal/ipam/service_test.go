package ipam

import (
	"context"
	"testing"
)

type fakeStore struct {
	prefixes []Prefix
	ips      []IPAddress
}

func (f *fakeStore) CreatePrefix(_ context.Context, p Prefix) (int64, error) {
	p.ID = int64(len(f.prefixes) + 1)
	f.prefixes = append(f.prefixes, p)
	return p.ID, nil
}

func (f *fakeStore) ListPrefixes(context.Context) ([]Prefix, error) {
	return append([]Prefix(nil), f.prefixes...), nil
}

func (f *fakeStore) GetPrefix(_ context.Context, id int64) (Prefix, error) {
	for _, p := range f.prefixes {
		if p.ID == id {
			return p, nil
		}
	}
	return Prefix{}, context.DeadlineExceeded
}

func (f *fakeStore) ListIPsInPrefix(_ context.Context, prefixID int64) ([]IPAddress, error) {
	var out []IPAddress
	for _, ip := range f.ips {
		if ip.PrefixID == prefixID {
			out = append(out, ip)
		}
	}
	return out, nil
}

func (f *fakeStore) CreateIP(_ context.Context, ip IPAddress) (int64, error) {
	ip.ID = int64(len(f.ips) + 1)
	f.ips = append(f.ips, ip)
	return ip.ID, nil
}

func TestCreatePrefixCanonicalizesCIDR(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	prefix, err := service.CreatePrefix(context.Background(), "10.0.0.20/24")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}
	if prefix.CIDR != "10.0.0.0/24" {
		t.Fatalf("expected canonical prefix, got %s", prefix.CIDR)
	}
}

func TestAllocateNextIPSkipsOccupiedAddresses(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	prefix, err := service.CreatePrefix(context.Background(), "10.0.0.0/30")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}

	_, err = store.CreateIP(context.Background(), IPAddress{PrefixID: prefix.ID, Address: "10.0.0.1", Family: 4, Status: "allocated"})
	if err != nil {
		t.Fatalf("seed first IP error: %v", err)
	}

	allocated, err := service.AllocateNextIP(context.Background(), prefix.ID)
	if err != nil {
		t.Fatalf("AllocateNextIP error: %v", err)
	}
	if allocated.Address != "10.0.0.2" {
		t.Fatalf("expected next free address 10.0.0.2, got %s", allocated.Address)
	}
}
