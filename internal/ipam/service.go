package ipam

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrInvalidPrefix     = errors.New("invalid prefix")
	ErrInvalidIPAddress  = errors.New("invalid ip address")
	ErrInvalidSubnetSize = errors.New("invalid subnet size")
	ErrPrefixOverlap     = errors.New("prefix overlaps with existing prefix")
	ErrBlockOverlap      = errors.New("block overlaps with existing block")
	ErrDuplicateIP       = errors.New("ip address already exists")
	ErrAddressOutOfRange = errors.New("address is outside prefix")
	ErrFamilyMismatch    = errors.New("address family does not match prefix")
	ErrNoAvailableIP     = errors.New("no available host addresses in prefix")
	ErrNoAvailableSubnet = errors.New("no available subnet of requested size in prefix")
)

// Prefix represents a stored prefix.
type Prefix struct {
	ID          int64
	ParentID    *int64
	BlockID     *int64
	Family      int
	CIDR        string
	PrefixLen   int
	Start       string
	End         string
	StartIPv4   *uint32
	EndIPv4     *uint32
	StartIPv6Hi *uint64
	StartIPv6Lo *uint64
	EndIPv6Hi   *uint64
	EndIPv6Lo   *uint64
	Status      string
}

// IPAddress represents an allocated IP address.
type IPAddress struct {
	ID       int64
	Family   int
	Address  string
	PrefixID int64
	IPv4     *uint32
	IPv6Hi   *uint64
	IPv6Lo   *uint64
	Status   string
}

// Space groups blocks under an organization or business unit.
type Space struct {
	ID          int64
	Name        string
	Description string
}

// Block represents a CIDR aggregate within a space.
type Block struct {
	ID          int64
	SpaceID     int64
	Family      int
	CIDR        string
	PrefixLen   int
	Start       string
	End         string
	StartIPv4   *uint32
	EndIPv4     *uint32
	StartIPv6Hi *uint64
	StartIPv6Lo *uint64
	EndIPv6Hi   *uint64
	EndIPv6Lo   *uint64
	Status      string
}

// Store is the persistence contract used by the service.
type Store interface {
	InTx(ctx context.Context, fn func(Store) error) error
	CreatePrefix(ctx context.Context, p Prefix) (int64, error)
	FindOverlappingPrefix(ctx context.Context, p Prefix) (Prefix, error)
	ListPrefixes(ctx context.Context) ([]Prefix, error)
	GetPrefix(ctx context.Context, id int64) (Prefix, error)
	GetPrefixForUpdate(ctx context.Context, id int64) (Prefix, error)
	ListChildPrefixes(ctx context.Context, parentID int64) ([]Prefix, error)
	ListIPsInPrefix(ctx context.Context, prefixID int64) ([]IPAddress, error)
	CreateIP(ctx context.Context, ip IPAddress) (int64, error)
	CreateSpace(ctx context.Context, space Space) (int64, error)
	ListSpaces(ctx context.Context) ([]Space, error)
	GetSpace(ctx context.Context, id int64) (Space, error)
	CreateBlock(ctx context.Context, block Block) (int64, error)
	ListBlocks(ctx context.Context, spaceID int64) ([]Block, error)
	FindOverlappingBlock(ctx context.Context, block Block) (Block, error)
}

// Service contains the core IPAM workflow.
type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreatePrefix(ctx context.Context, cidr string) (Prefix, error) {
	prefix, err := buildPrefix(cidr, nil, nil)
	if err != nil {
		return Prefix{}, err
	}

	err = s.store.InTx(ctx, func(tx Store) error {
		overlap, err := tx.FindOverlappingPrefix(ctx, prefix)
		if err != nil {
			return err
		}
		if overlap.ID != 0 {
			return ErrPrefixOverlap
		}

		id, err := tx.CreatePrefix(ctx, prefix)
		if err != nil {
			return err
		}
		prefix.ID = id
		return nil
	})
	if err != nil {
		return Prefix{}, err
	}
	return prefix, nil
}

func (s *Service) ListPrefixes(ctx context.Context) ([]Prefix, error) {
	return s.store.ListPrefixes(ctx)
}

func (s *Service) AllocateNextIP(ctx context.Context, prefixID int64) (IPAddress, error) {
	var allocated IPAddress

	err := s.store.InTx(ctx, func(tx Store) error {
		prefix, err := tx.GetPrefixForUpdate(ctx, prefixID)
		if err != nil {
			return err
		}

		parsed, err := netip.ParsePrefix(prefix.CIDR)
		if err != nil {
			return fmt.Errorf("parse stored prefix: %w", err)
		}

		allIPs, err := tx.ListIPsInPrefix(ctx, prefixID)
		if err != nil {
			return err
		}

		start, end := usableRange(parsed)
		candidate, ok := firstAvailableIP(start, end, allIPs)
		if !ok {
			return ErrNoAvailableIP
		}

		allocated = newIPAddress(prefixID, candidate, "allocated")
		allocated.Family = prefix.Family
		id, err := tx.CreateIP(ctx, allocated)
		if err != nil {
			return err
		}
		allocated.ID = id
		return nil
	})
	if err != nil {
		return IPAddress{}, err
	}

	return allocated, nil
}

func (s *Service) ListIPs(ctx context.Context, prefixID int64) ([]IPAddress, error) {
	return s.store.ListIPsInPrefix(ctx, prefixID)
}

func (s *Service) CreateIP(ctx context.Context, address string, prefixID int64, status string) (IPAddress, error) {
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return IPAddress{}, fmt.Errorf("%w: %v", ErrInvalidIPAddress, err)
	}
	if status == "" {
		status = "allocated"
	}

	prefix, err := s.store.GetPrefix(ctx, prefixID)
	if err != nil {
		return IPAddress{}, err
	}

	prefixAddr, err := netip.ParseAddr(prefix.Start)
	if err != nil {
		return IPAddress{}, fmt.Errorf("parse stored prefix start: %w", err)
	}
	prefixEnd, err := netip.ParseAddr(prefix.End)
	if err != nil {
		return IPAddress{}, fmt.Errorf("parse stored prefix end: %w", err)
	}

	if prefixAddr.Is4() != addr.Is4() {
		return IPAddress{}, ErrFamilyMismatch
	}
	if compareAddr(addr, prefixAddr) < 0 || compareAddr(addr, prefixEnd) > 0 {
		return IPAddress{}, ErrAddressOutOfRange
	}

	created := newIPAddress(prefixID, addr, status)
	created.Family = prefix.Family
	id, err := s.store.CreateIP(ctx, created)
	if err != nil {
		return IPAddress{}, err
	}
	created.ID = id
	return created, nil
}

func (s *Service) AllocateSubnet(ctx context.Context, parentID int64, size int) (Prefix, error) {
	var child Prefix

	err := s.store.InTx(ctx, func(tx Store) error {
		parent, err := tx.GetPrefixForUpdate(ctx, parentID)
		if err != nil {
			return err
		}

		parentPrefix, err := netip.ParsePrefix(parent.CIDR)
		if err != nil {
			return fmt.Errorf("parse stored prefix: %w", err)
		}
		if size < parentPrefix.Bits() || size > addrBits(parentPrefix.Addr()) {
			return ErrInvalidSubnetSize
		}

		children, err := tx.ListChildPrefixes(ctx, parentID)
		if err != nil {
			return err
		}

		candidate, err := nextAvailableChildPrefix(parentPrefix, children, size)
		if err != nil {
			return err
		}

		child, err = buildPrefix(candidate.String(), int64Ptr(parentID), parent.BlockID)
		if err != nil {
			return err
		}

		id, err := tx.CreatePrefix(ctx, child)
		if err != nil {
			return err
		}
		child.ID = id
		return nil
	})
	if err != nil {
		return Prefix{}, err
	}

	return child, nil
}

func (s *Service) CreateSpace(ctx context.Context, name, description string) (Space, error) {
	space := Space{Name: name, Description: description}
	id, err := s.store.CreateSpace(ctx, space)
	if err != nil {
		return Space{}, err
	}
	space.ID = id
	return space, nil
}

func (s *Service) ListSpaces(ctx context.Context) ([]Space, error) {
	return s.store.ListSpaces(ctx)
}

func (s *Service) CreateBlock(ctx context.Context, spaceID int64, cidr string) (Block, error) {
	block, err := buildBlock(spaceID, cidr)
	if err != nil {
		return Block{}, err
	}

	err = s.store.InTx(ctx, func(tx Store) error {
		if _, err := tx.GetSpace(ctx, spaceID); err != nil {
			return err
		}

		overlap, err := tx.FindOverlappingBlock(ctx, block)
		if err != nil {
			return err
		}
		if overlap.ID != 0 {
			return ErrBlockOverlap
		}

		id, err := tx.CreateBlock(ctx, block)
		if err != nil {
			return err
		}
		block.ID = id
		return nil
	})
	if err != nil {
		return Block{}, err
	}
	return block, nil
}

func (s *Service) ListBlocks(ctx context.Context, spaceID int64) ([]Block, error) {
	if _, err := s.store.GetSpace(ctx, spaceID); err != nil {
		return nil, err
	}
	return s.store.ListBlocks(ctx, spaceID)
}

func buildPrefix(cidr string, parentID, blockID *int64) (Prefix, error) {
	parsed, err := netip.ParsePrefix(cidr)
	if err != nil {
		return Prefix{}, fmt.Errorf("%w: %v", ErrInvalidPrefix, err)
	}

	canonical := parsed.Masked()
	start, end, err := prefixBounds(canonical)
	if err != nil {
		return Prefix{}, err
	}

	prefix := Prefix{
		ParentID:  parentID,
		BlockID:   blockID,
		CIDR:      canonical.String(),
		PrefixLen: canonical.Bits(),
		Start:     start,
		End:       end,
		Status:    "active",
	}
	applyRangeFields(&prefix.StartIPv4, &prefix.EndIPv4, &prefix.StartIPv6Hi, &prefix.StartIPv6Lo, &prefix.EndIPv6Hi, &prefix.EndIPv6Lo, canonical.Masked().Addr(), lastAddress(canonical))
	prefix.Family = familyForAddr(canonical.Addr())
	return prefix, nil
}

func buildBlock(spaceID int64, cidr string) (Block, error) {
	parsed, err := netip.ParsePrefix(cidr)
	if err != nil {
		return Block{}, fmt.Errorf("%w: %v", ErrInvalidPrefix, err)
	}

	canonical := parsed.Masked()
	start, end, err := prefixBounds(canonical)
	if err != nil {
		return Block{}, err
	}

	block := Block{
		SpaceID:   spaceID,
		CIDR:      canonical.String(),
		PrefixLen: canonical.Bits(),
		Start:     start,
		End:       end,
		Status:    "active",
		Family:    familyForAddr(canonical.Addr()),
	}
	applyRangeFields(&block.StartIPv4, &block.EndIPv4, &block.StartIPv6Hi, &block.StartIPv6Lo, &block.EndIPv6Hi, &block.EndIPv6Lo, canonical.Masked().Addr(), lastAddress(canonical))
	return block, nil
}

func newIPAddress(prefixID int64, addr netip.Addr, status string) IPAddress {
	ip := IPAddress{
		Family:   familyForAddr(addr),
		Address:  addr.String(),
		PrefixID: prefixID,
		Status:   status,
	}
	if addr.Is4() {
		value := ipv4ToUint32(addr)
		ip.IPv4 = &value
		return ip
	}
	hi, lo := ipv6ToHiLo(addr)
	ip.IPv6Hi = &hi
	ip.IPv6Lo = &lo
	return ip
}

func firstAvailableIP(start, end netip.Addr, allIPs []IPAddress) (netip.Addr, bool) {
	candidate := start
	for _, ip := range allIPs {
		used, err := netip.ParseAddr(ip.Address)
		if err != nil {
			continue
		}
		if compareAddr(used, candidate) < 0 {
			continue
		}
		if compareAddr(used, end) > 0 {
			break
		}
		if compareAddr(used, candidate) == 0 {
			candidate = nextAddr(candidate, 1)
			if compareAddr(candidate, end) > 0 {
				return netip.Addr{}, false
			}
		}
	}
	if compareAddr(candidate, end) > 0 {
		return netip.Addr{}, false
	}
	return candidate, true
}

func nextAvailableChildPrefix(parent netip.Prefix, children []Prefix, size int) (netip.Prefix, error) {
	if size < parent.Bits() || size > addrBits(parent.Addr()) {
		return netip.Prefix{}, ErrInvalidSubnetSize
	}

	type addrRange struct {
		start *big.Int
		end   *big.Int
	}

	used := make([]addrRange, 0, len(children))
	for _, child := range children {
		start, err := netip.ParseAddr(child.Start)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("parse child prefix start: %w", err)
		}
		end, err := netip.ParseAddr(child.End)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("parse child prefix end: %w", err)
		}
		used = append(used, addrRange{start: addrToBigInt(start), end: addrToBigInt(end)})
	}

	parentStart := addrToBigInt(parent.Masked().Addr())
	parentEnd := addrToBigInt(lastAddress(parent))
	step := new(big.Int).Lsh(big.NewInt(1), uint(addrBits(parent.Addr())-size))

	for candidateStart := new(big.Int).Set(parentStart); ; candidateStart = new(big.Int).Add(candidateStart, step) {
		candidateAddr := bigIntToAddr(parent.Addr(), candidateStart)
		candidate := netip.PrefixFrom(candidateAddr, size).Masked()
		candidateEnd := addrToBigInt(lastAddress(candidate))
		if candidateEnd.Cmp(parentEnd) > 0 {
			break
		}

		overlaps := false
		for _, child := range used {
			if child.start.Cmp(candidateEnd) <= 0 && child.end.Cmp(candidateStart) >= 0 {
				overlaps = true
				break
			}
		}
		if !overlaps {
			return candidate, nil
		}
	}

	return netip.Prefix{}, ErrNoAvailableSubnet
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
	return bigIntToAddr(prefix.Addr(), end)
}

func compareAddr(left, right netip.Addr) int {
	return addrToBigInt(left).Cmp(addrToBigInt(right))
}

func nextAddr(addr netip.Addr, step int) netip.Addr {
	value := new(big.Int).Set(addrToBigInt(addr))
	value.Add(value, big.NewInt(int64(step)))
	return bigIntToAddr(addr, value)
}

func prevAddr(addr netip.Addr, step int) netip.Addr {
	value := new(big.Int).Set(addrToBigInt(addr))
	value.Sub(value, big.NewInt(int64(step)))
	return bigIntToAddr(addr, value)
}

func addrToBigInt(addr netip.Addr) *big.Int {
	if addr.Is4() {
		b := [4]byte(addr.As4())
		return new(big.Int).SetBytes(b[:])
	}
	b := [16]byte(addr.As16())
	return new(big.Int).SetBytes(b[:])
}

func bigIntToAddr(familyAddr netip.Addr, value *big.Int) netip.Addr {
	buf := value.FillBytes(make([]byte, addrBytes(familyAddr)))
	if familyAddr.Is4() {
		return netip.AddrFrom4([4]byte(buf))
	}
	return netip.AddrFrom16([16]byte(buf))
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

func familyForAddr(addr netip.Addr) int {
	if addr.Is4() {
		return 4
	}
	return 6
}

func applyRangeFields(startIPv4, endIPv4 **uint32, startIPv6Hi, startIPv6Lo, endIPv6Hi, endIPv6Lo **uint64, start, end netip.Addr) {
	if start.Is4() {
		startValue := ipv4ToUint32(start)
		endValue := ipv4ToUint32(end)
		*startIPv4 = &startValue
		*endIPv4 = &endValue
		return
	}

	startHi, startLo := ipv6ToHiLo(start)
	endHi, endLo := ipv6ToHiLo(end)
	*startIPv6Hi = &startHi
	*startIPv6Lo = &startLo
	*endIPv6Hi = &endHi
	*endIPv6Lo = &endLo
}

func ipv4ToUint32(addr netip.Addr) uint32 {
	bytes := addr.As4()
	return binary.BigEndian.Uint32(bytes[:])
}

func ipv6ToHiLo(addr netip.Addr) (uint64, uint64) {
	bytes := addr.As16()
	return binary.BigEndian.Uint64(bytes[:8]), binary.BigEndian.Uint64(bytes[8:])
}

func int64Ptr(v int64) *int64 {
	return &v
}
