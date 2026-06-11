package ipam

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
)

// Prefix represents a stored prefix.
type Prefix struct {
	ID        int64
	Family    int
	CIDR      string
	PrefixLen int
	Start     string
	End       string
	Status    string
}

// IPAddress represents an allocated IP address.
type IPAddress struct {
	ID       int64
	Family   int
	Address  string
	PrefixID int64
	Status   string
}

// Store is the persistence contract used by the service.
type Store interface {
	CreatePrefix(ctx context.Context, p Prefix) (int64, error)
	ListPrefixes(ctx context.Context) ([]Prefix, error)
	GetPrefix(ctx context.Context, id int64) (Prefix, error)
	ListIPsInPrefix(ctx context.Context, prefixID int64) ([]IPAddress, error)
	CreateIP(ctx context.Context, ip IPAddress) (int64, error)
}

// Service contains the core IPAM workflow.
type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreatePrefix(ctx context.Context, cidr string) (Prefix, error) {
	parsed, err := netip.ParsePrefix(cidr)
	if err != nil {
		return Prefix{}, fmt.Errorf("parse prefix: %w", err)
	}

	canonical := parsed.Masked()
	start, end, err := prefixBounds(canonical)
	if err != nil {
		return Prefix{}, err
	}

	family := 4
	if canonical.Addr().Is6() {
		family = 6
	}

	p := Prefix{
		Family:    family,
		CIDR:      canonical.String(),
		PrefixLen: canonical.Bits(),
		Start:     start,
		End:       end,
		Status:    "active",
	}

	id, err := s.store.CreatePrefix(ctx, p)
	if err != nil {
		return Prefix{}, err
	}
	p.ID = id
	return p, nil
}

func (s *Service) ListPrefixes(ctx context.Context) ([]Prefix, error) {
	return s.store.ListPrefixes(ctx)
}

func (s *Service) AllocateNextIP(ctx context.Context, prefixID int64) (IPAddress, error) {
	prefix, err := s.store.GetPrefix(ctx, prefixID)
	if err != nil {
		return IPAddress{}, err
	}

	parsed, err := netip.ParsePrefix(prefix.CIDR)
	if err != nil {
		return IPAddress{}, fmt.Errorf("parse stored prefix: %w", err)
	}

	allIPs, err := s.store.ListIPsInPrefix(ctx, prefixID)
	if err != nil {
		return IPAddress{}, err
	}

	occupied := map[string]struct{}{}
	for _, ip := range allIPs {
		occupied[ip.Address] = struct{}{}
	}

	start, end := usableRange(parsed)
	candidate := start
	for ; compareAddr(candidate, end) <= 0; candidate = nextAddr(candidate, 1) {
		candidateText := candidate.String()
		if _, exists := occupied[candidateText]; exists {
			continue
		}
		allocated := IPAddress{
			Family:   prefix.Family,
			Address:  candidateText,
			PrefixID: prefixID,
			Status:   "allocated",
		}
		_, err := s.store.CreateIP(ctx, allocated)
		if err != nil {
			return IPAddress{}, err
		}
		return allocated, nil
	}

	return IPAddress{}, errors.New("no available host addresses in prefix")
}

func (s *Service) ListIPs(ctx context.Context, prefixID int64) ([]IPAddress, error) {
	return s.store.ListIPsInPrefix(ctx, prefixID)
}

func prefixBounds(prefix netip.Prefix) (string, string, error) {
	start := prefix.Masked().Addr()
	end := lastAddress(prefix)
	return start.String(), end.String(), nil
}

func usableRange(prefix netip.Prefix) (netip.Addr, netip.Addr) {
	start := prefix.Masked().Addr()
	hostBits := addrBits(prefix.Addr()) - prefix.Bits()
	if hostBits == 0 {
		return start, start
	}
	if hostBits == 1 {
		return start, nextAddr(start, 1)
	}
	return nextAddr(start, 1), prevAddr(lastAddress(prefix), 1)
}

func lastAddress(prefix netip.Prefix) netip.Addr {
	addrBits := addrBits(prefix.Addr())
	start := addrToBigInt(prefix.Masked().Addr())
	maxHosts := new(big.Int).Lsh(big.NewInt(1), uint(addrBits-prefix.Bits()))
	end := new(big.Int).Add(start, new(big.Int).Sub(maxHosts, big.NewInt(1)))
	buf := end.FillBytes(make([]byte, addrBytes(prefix.Addr())))
	if prefix.Addr().Is4() {
		return netip.AddrFrom4([4]byte(buf))
	}
	return netip.AddrFrom16([16]byte(buf))
}

func compareAddr(left, right netip.Addr) int {
	return addrToBigInt(left).Cmp(addrToBigInt(right))
}

func nextAddr(addr netip.Addr, step int) netip.Addr {
	value := new(big.Int).Set(addrToBigInt(addr))
	value.Add(value, big.NewInt(int64(step)))
	buf := value.FillBytes(make([]byte, addrBytes(addr)))
	if addr.Is4() {
		return netip.AddrFrom4([4]byte(buf))
	}
	return netip.AddrFrom16([16]byte(buf))
}

func prevAddr(addr netip.Addr, step int) netip.Addr {
	value := new(big.Int).Set(addrToBigInt(addr))
	value.Sub(value, big.NewInt(int64(step)))
	buf := value.FillBytes(make([]byte, addrBytes(addr)))
	if addr.Is4() {
		return netip.AddrFrom4([4]byte(buf))
	}
	return netip.AddrFrom16([16]byte(buf))
}

func addrToBigInt(addr netip.Addr) *big.Int {
	if addr.Is4() {
		b := [4]byte(addr.As4())
		return new(big.Int).SetBytes(b[:])
	}
	b := [16]byte(addr.As16())
	return new(big.Int).SetBytes(b[:])
}

func addrBits(addr netip.Addr) int {
	if addr.Is4() {
		return 32
	}
	return 128
}

func addrBytes(addr netip.Addr) int {
	if addr.Is4() {
		return 4
	}
	return 16
}
