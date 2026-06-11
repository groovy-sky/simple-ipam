package ipam

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
)

type fakeStore struct {
	prefixes []Prefix
	ips      []IPAddress
	spaces   []Space
	blocks   []Block
}

func (f *fakeStore) InTx(_ context.Context, fn func(Store) error) error {
	return fn(f)
}

func (f *fakeStore) CreatePrefix(_ context.Context, p Prefix) (int64, error) {
	p.ID = int64(len(f.prefixes) + 1)
	f.prefixes = append(f.prefixes, p)
	return p.ID, nil
}

func (f *fakeStore) FindOverlappingPrefix(_ context.Context, p Prefix) (Prefix, error) {
	for _, existing := range f.prefixes {
		if existing.Family != p.Family || !sameOptionalInt64(existing.ParentID, p.ParentID) {
			continue
		}
		if rangesOverlap(existing.Start, existing.End, p.Start, p.End) {
			return existing, nil
		}
	}
	return Prefix{}, nil
}

func (f *fakeStore) ListPrefixes(context.Context) ([]Prefix, error) {
	var out []Prefix
	for _, prefix := range f.prefixes {
		if prefix.ParentID == nil {
			out = append(out, prefix)
		}
	}
	slices.Reverse(out)
	return out, nil
}

func (f *fakeStore) GetPrefix(_ context.Context, id int64) (Prefix, error) {
	for _, p := range f.prefixes {
		if p.ID == id {
			return p, nil
		}
	}
	return Prefix{}, ErrNotFound
}

func (f *fakeStore) GetPrefixForUpdate(ctx context.Context, id int64) (Prefix, error) {
	return f.GetPrefix(ctx, id)
}

func (f *fakeStore) ListChildPrefixes(_ context.Context, parentID int64) ([]Prefix, error) {
	var out []Prefix
	for _, prefix := range f.prefixes {
		if prefix.ParentID != nil && *prefix.ParentID == parentID {
			out = append(out, prefix)
		}
	}
	return out, nil
}

func (f *fakeStore) ListIPsInPrefix(_ context.Context, prefixID int64) ([]IPAddress, error) {
	var out []IPAddress
	for _, ip := range f.ips {
		if ip.PrefixID == prefixID {
			out = append(out, ip)
		}
	}
	slices.SortFunc(out, func(left, right IPAddress) int {
		return compareAddressText(left.Address, right.Address)
	})
	return out, nil
}

func (f *fakeStore) CreateIP(_ context.Context, ip IPAddress) (int64, error) {
	for _, existing := range f.ips {
		if existing.Family == ip.Family && existing.Address == ip.Address {
			return 0, ErrDuplicateIP
		}
	}
	ip.ID = int64(len(f.ips) + 1)
	f.ips = append(f.ips, ip)
	return ip.ID, nil
}

func (f *fakeStore) CreateSpace(_ context.Context, space Space) (int64, error) {
	space.ID = int64(len(f.spaces) + 1)
	f.spaces = append(f.spaces, space)
	return space.ID, nil
}

func (f *fakeStore) ListSpaces(context.Context) ([]Space, error) {
	return append([]Space(nil), f.spaces...), nil
}

func (f *fakeStore) GetSpace(_ context.Context, id int64) (Space, error) {
	for _, space := range f.spaces {
		if space.ID == id {
			return space, nil
		}
	}
	return Space{}, ErrNotFound
}

func (f *fakeStore) CreateBlock(_ context.Context, block Block) (int64, error) {
	block.ID = int64(len(f.blocks) + 1)
	f.blocks = append(f.blocks, block)
	return block.ID, nil
}

func (f *fakeStore) ListBlocks(_ context.Context, spaceID int64) ([]Block, error) {
	var out []Block
	for _, block := range f.blocks {
		if block.SpaceID == spaceID {
			out = append(out, block)
		}
	}
	return out, nil
}

func (f *fakeStore) FindOverlappingBlock(_ context.Context, block Block) (Block, error) {
	for _, existing := range f.blocks {
		if existing.SpaceID != block.SpaceID || existing.Family != block.Family {
			continue
		}
		if rangesOverlap(existing.Start, existing.End, block.Start, block.End) {
			return existing, nil
		}
	}
	return Block{}, nil
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

func TestCreatePrefixRejectsOverlappingIPv4(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	if _, err := service.CreatePrefix(context.Background(), "10.0.0.0/24"); err != nil {
		t.Fatalf("seed prefix error: %v", err)
	}

	_, err := service.CreatePrefix(context.Background(), "10.0.0.128/25")
	if !errors.Is(err, ErrPrefixOverlap) {
		t.Fatalf("expected ErrPrefixOverlap, got %v", err)
	}
}

func TestCreatePrefixAllowsAdjacentIPv4(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	if _, err := service.CreatePrefix(context.Background(), "10.0.0.0/25"); err != nil {
		t.Fatalf("seed prefix error: %v", err)
	}

	prefix, err := service.CreatePrefix(context.Background(), "10.0.0.128/25")
	if err != nil {
		t.Fatalf("expected adjacent prefix to be accepted, got %v", err)
	}
	if prefix.CIDR != "10.0.0.128/25" {
		t.Fatalf("unexpected prefix %s", prefix.CIDR)
	}
}

func TestCreatePrefixRejectsNestedPrefix(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	if _, err := service.CreatePrefix(context.Background(), "10.0.0.0/16"); err != nil {
		t.Fatalf("seed prefix error: %v", err)
	}

	_, err := service.CreatePrefix(context.Background(), "10.0.10.0/24")
	if !errors.Is(err, ErrPrefixOverlap) {
		t.Fatalf("expected ErrPrefixOverlap, got %v", err)
	}
}

func TestCreatePrefixRejectsOverlappingIPv6(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	if _, err := service.CreatePrefix(context.Background(), "2001:db8::/64"); err != nil {
		t.Fatalf("seed prefix error: %v", err)
	}

	_, err := service.CreatePrefix(context.Background(), "2001:db8::8000/65")
	if !errors.Is(err, ErrPrefixOverlap) {
		t.Fatalf("expected ErrPrefixOverlap, got %v", err)
	}
}

func TestAllocateNextIPSkipsOccupiedAddresses(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	prefix, err := service.CreatePrefix(context.Background(), "10.0.0.0/29")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}

	for _, address := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.4"} {
		if _, err := store.CreateIP(context.Background(), newIPAddress(prefix.ID, mustParseAddr(t, address), "allocated")); err != nil {
			t.Fatalf("seed IP %s error: %v", address, err)
		}
	}

	allocated, err := service.AllocateNextIP(context.Background(), prefix.ID)
	if err != nil {
		t.Fatalf("AllocateNextIP error: %v", err)
	}
	if allocated.Address != "10.0.0.3" {
		t.Fatalf("expected next free address 10.0.0.3, got %s", allocated.Address)
	}
}

func TestAllocateNextIPReturnsNoAvailableHosts(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	prefix, err := service.CreatePrefix(context.Background(), "10.0.0.0/30")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}
	for _, address := range []string{"10.0.0.1", "10.0.0.2"} {
		if _, err := store.CreateIP(context.Background(), newIPAddress(prefix.ID, mustParseAddr(t, address), "allocated")); err != nil {
			t.Fatalf("seed IP %s error: %v", address, err)
		}
	}

	_, err = service.AllocateNextIP(context.Background(), prefix.ID)
	if !errors.Is(err, ErrNoAvailableIP) {
		t.Fatalf("expected ErrNoAvailableIP, got %v", err)
	}
}

func TestAllocateSubnetSequentially(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	parent, err := service.CreatePrefix(context.Background(), "10.0.0.0/24")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}

	first, err := service.AllocateSubnet(context.Background(), parent.ID, 26)
	if err != nil {
		t.Fatalf("AllocateSubnet first error: %v", err)
	}
	second, err := service.AllocateSubnet(context.Background(), parent.ID, 26)
	if err != nil {
		t.Fatalf("AllocateSubnet second error: %v", err)
	}

	if first.CIDR != "10.0.0.0/26" {
		t.Fatalf("expected first /26 to be 10.0.0.0/26, got %s", first.CIDR)
	}
	if second.CIDR != "10.0.0.64/26" {
		t.Fatalf("expected second /26 to be 10.0.0.64/26, got %s", second.CIDR)
	}
	if second.ParentID == nil || *second.ParentID != parent.ID {
		t.Fatalf("expected allocated subnet parent id %d, got %+v", parent.ID, second.ParentID)
	}
}

func TestAllocateSubnetReturnsExhausted(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	parent, err := service.CreatePrefix(context.Background(), "10.0.0.0/30")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}

	for i := 0; i < 4; i++ {
		if _, err := service.AllocateSubnet(context.Background(), parent.ID, 32); err != nil {
			t.Fatalf("AllocateSubnet #%d error: %v", i+1, err)
		}
	}

	_, err = service.AllocateSubnet(context.Background(), parent.ID, 32)
	if !errors.Is(err, ErrNoAvailableSubnet) {
		t.Fatalf("expected ErrNoAvailableSubnet, got %v", err)
	}
}

func TestCreateIPAddressInsidePrefixAccepted(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	prefix, err := service.CreatePrefix(context.Background(), "10.0.0.0/24")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}

	created, err := service.CreateIP(context.Background(), "10.0.0.10", prefix.ID, "reserved")
	if err != nil {
		t.Fatalf("CreateIP error: %v", err)
	}
	if created.Address != "10.0.0.10" || created.Status != "reserved" {
		t.Fatalf("unexpected created ip %+v", created)
	}
}

func TestCreateIPAddressOutsidePrefixRejected(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	prefix, err := service.CreatePrefix(context.Background(), "10.0.0.0/24")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}

	_, err = service.CreateIP(context.Background(), "10.0.1.10", prefix.ID, "")
	if !errors.Is(err, ErrAddressOutOfRange) {
		t.Fatalf("expected ErrAddressOutOfRange, got %v", err)
	}
}

func TestCreateIPAddressDuplicateRejected(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	prefix, err := service.CreatePrefix(context.Background(), "10.0.0.0/24")
	if err != nil {
		t.Fatalf("CreatePrefix error: %v", err)
	}
	if _, err := service.CreateIP(context.Background(), "10.0.0.10", prefix.ID, ""); err != nil {
		t.Fatalf("seed CreateIP error: %v", err)
	}

	_, err = service.CreateIP(context.Background(), "10.0.0.10", prefix.ID, "")
	if !errors.Is(err, ErrDuplicateIP) {
		t.Fatalf("expected ErrDuplicateIP, got %v", err)
	}
}

func TestCreateSpaceAndBlock(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	space, err := service.CreateSpace(context.Background(), "engineering", "shared space")
	if err != nil {
		t.Fatalf("CreateSpace error: %v", err)
	}
	block, err := service.CreateBlock(context.Background(), space.ID, "10.1.0.0/16")
	if err != nil {
		t.Fatalf("CreateBlock error: %v", err)
	}
	if block.SpaceID != space.ID || block.CIDR != "10.1.0.0/16" {
		t.Fatalf("unexpected block %+v", block)
	}
}

func TestCreateBlockRejectsOverlapWithinSpace(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	space, err := service.CreateSpace(context.Background(), "engineering", "")
	if err != nil {
		t.Fatalf("CreateSpace error: %v", err)
	}
	if _, err := service.CreateBlock(context.Background(), space.ID, "10.1.0.0/16"); err != nil {
		t.Fatalf("seed block error: %v", err)
	}

	_, err = service.CreateBlock(context.Background(), space.ID, "10.1.128.0/17")
	if !errors.Is(err, ErrBlockOverlap) {
		t.Fatalf("expected ErrBlockOverlap, got %v", err)
	}
}

func mustParseAddr(t *testing.T, value string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatalf("ParseAddr(%q) error: %v", value, err)
	}
	return addr
}

func compareAddressText(left, right string) int {
	leftAddr, leftErr := netip.ParseAddr(left)
	rightAddr, rightErr := netip.ParseAddr(right)
	if leftErr != nil || rightErr != nil {
		return 0
	}
	return compareAddr(leftAddr, rightAddr)
}

func rangesOverlap(startA, endA, startB, endB string) bool {
	leftStart, err := netip.ParseAddr(startA)
	if err != nil {
		return false
	}
	leftEnd, err := netip.ParseAddr(endA)
	if err != nil {
		return false
	}
	rightStart, err := netip.ParseAddr(startB)
	if err != nil {
		return false
	}
	rightEnd, err := netip.ParseAddr(endB)
	if err != nil {
		return false
	}
	return compareAddr(leftStart, rightEnd) <= 0 && compareAddr(leftEnd, rightStart) >= 0
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
