package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

const (
	DefaultIPv4PartitionPrefix = 24
	DefaultIPv6PartitionPrefix = 64
	DefaultMaxPartitions       = 100000
	maxScopeCIDRs              = 4096
	maxPartitionCIDRs          = 64
)

// TargetScope is an immutable, versioned selection policy. Planning is kept
// operation-independent so later reviewed protocols can reuse it without
// changing local.v1 or giving ServiceNow executable authority.
type TargetScope struct {
	ScopeID             string
	Revision            int
	CIDRs               []string
	Exclusions          []string
	IPv4PartitionPrefix int
	IPv6PartitionPrefix int
	MaxPartitions       int
}

// PlanTargetPartitions returns stable, ordered, non-overlapping CIDR
// partitions after exclusions. It does not perform discovery or authorize a
// target: a worker must still intersect every partition with local policy.
func PlanTargetPartitions(scope TargetScope) ([]TargetPartition, error) {
	if !safeID.MatchString(scope.ScopeID) {
		return nil, errors.New("target scope ID is invalid")
	}
	if scope.Revision < 1 || scope.Revision > 1000000 {
		return nil, errors.New("target scope revision must be between 1 and 1000000")
	}
	if len(scope.CIDRs) == 0 || len(scope.CIDRs) > maxScopeCIDRs || len(scope.Exclusions) > maxScopeCIDRs {
		return nil, fmt.Errorf("target scope must contain 1-%d CIDRs and at most %d exclusions", maxScopeCIDRs, maxScopeCIDRs)
	}
	v4Prefix := scope.IPv4PartitionPrefix
	if v4Prefix == 0 {
		v4Prefix = DefaultIPv4PartitionPrefix
	}
	v6Prefix := scope.IPv6PartitionPrefix
	if v6Prefix == 0 {
		v6Prefix = DefaultIPv6PartitionPrefix
	}
	if v4Prefix < 8 || v4Prefix > 32 || v6Prefix < 32 || v6Prefix > 128 {
		return nil, errors.New("target partition prefixes must be IPv4 /8-/32 and IPv6 /32-/128")
	}
	maximum := scope.MaxPartitions
	if maximum == 0 {
		maximum = DefaultMaxPartitions
	}
	if maximum < 1 || maximum > DefaultMaxPartitions {
		return nil, fmt.Errorf("maximum partitions must be between 1 and %d", DefaultMaxPartitions)
	}

	included, err := parsePrefixes(scope.CIDRs)
	if err != nil {
		return nil, fmt.Errorf("target scope CIDRs: %w", err)
	}
	excluded, err := parsePrefixes(scope.Exclusions)
	if err != nil {
		return nil, fmt.Errorf("target scope exclusions: %w", err)
	}
	included = removeCovered(included)
	excluded = removeCovered(excluded)

	allowed := included
	for _, deny := range excluded {
		next := make([]netip.Prefix, 0, len(allowed))
		for _, candidate := range allowed {
			if candidate.Addr().BitLen() != deny.Addr().BitLen() || !candidate.Overlaps(deny) {
				next = append(next, candidate)
				continue
			}
			parts, err := subtractPrefix(candidate, deny, maximum-len(next))
			if err != nil {
				return nil, err
			}
			next = append(next, parts...)
			if len(next) > maximum {
				return nil, fmt.Errorf("target scope exceeds %d partitions after exclusions", maximum)
			}
		}
		allowed = next
	}

	planned := make([]netip.Prefix, 0, len(allowed))
	for _, prefix := range allowed {
		targetBits := v6Prefix
		if prefix.Addr().Is4() {
			targetBits = v4Prefix
		}
		parts, err := splitToPrefix(prefix, targetBits, maximum-len(planned))
		if err != nil {
			return nil, err
		}
		planned = append(planned, parts...)
		if len(planned) > maximum {
			return nil, fmt.Errorf("target scope exceeds %d partitions", maximum)
		}
	}
	sortPrefixes(planned)

	partitions := make([]TargetPartition, len(planned))
	for index, prefix := range planned {
		canonical := prefix.Masked().String()
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%d\n%s", scope.ScopeID, scope.Revision, canonical)))
		partitions[index] = TargetPartition{
			Key:     hex.EncodeToString(sum[:]),
			Ordinal: index,
			Count:   len(planned),
			CIDRs:   []string{canonical},
		}
	}
	return partitions, nil
}

func parsePrefixes(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Zone() != "" {
			return nil, fmt.Errorf("invalid CIDR %q", value)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func removeCovered(prefixes []netip.Prefix) []netip.Prefix {
	sort.Slice(prefixes, func(i, j int) bool {
		if prefixes[i].Addr().BitLen() != prefixes[j].Addr().BitLen() {
			return prefixes[i].Addr().BitLen() < prefixes[j].Addr().BitLen()
		}
		if prefixes[i].Bits() != prefixes[j].Bits() {
			return prefixes[i].Bits() < prefixes[j].Bits()
		}
		return prefixes[i].Addr().Compare(prefixes[j].Addr()) < 0
	})
	result := make([]netip.Prefix, 0, len(prefixes))
	for _, prefix := range prefixes {
		covered := false
		for _, existing := range result {
			if existing.Addr().BitLen() == prefix.Addr().BitLen() && existing.Contains(prefix.Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, prefix)
		}
	}
	return result
}

func subtractPrefix(base, deny netip.Prefix, budget int) ([]netip.Prefix, error) {
	if budget < 0 {
		return nil, errors.New("target scope partition limit exceeded")
	}
	if !base.Overlaps(deny) {
		return []netip.Prefix{base}, nil
	}
	if deny.Bits() <= base.Bits() && deny.Contains(base.Addr()) {
		return nil, nil
	}
	left, right, err := splitPrefix(base)
	if err != nil {
		return nil, err
	}
	leftParts, err := subtractPrefix(left, deny, budget)
	if err != nil {
		return nil, err
	}
	rightParts, err := subtractPrefix(right, deny, budget-len(leftParts))
	if err != nil {
		return nil, err
	}
	return append(leftParts, rightParts...), nil
}

func splitToPrefix(prefix netip.Prefix, targetBits, budget int) ([]netip.Prefix, error) {
	if budget < 1 {
		return nil, errors.New("target scope partition limit exceeded")
	}
	if prefix.Bits() >= targetBits {
		return []netip.Prefix{prefix}, nil
	}
	left, right, err := splitPrefix(prefix)
	if err != nil {
		return nil, err
	}
	leftParts, err := splitToPrefix(left, targetBits, budget)
	if err != nil {
		return nil, err
	}
	rightParts, err := splitToPrefix(right, targetBits, budget-len(leftParts))
	if err != nil {
		return nil, err
	}
	return append(leftParts, rightParts...), nil
}

func splitPrefix(prefix netip.Prefix) (netip.Prefix, netip.Prefix, error) {
	prefix = prefix.Masked()
	bits := prefix.Bits()
	bitLen := prefix.Addr().BitLen()
	if bits < 0 || bits >= bitLen {
		return netip.Prefix{}, netip.Prefix{}, errors.New("cannot split a host prefix")
	}
	left := netip.PrefixFrom(prefix.Addr(), bits+1)
	if prefix.Addr().Is4() {
		value := prefix.Addr().As4()
		value[bits/8] |= byte(1 << (7 - uint(bits%8)))
		return left, netip.PrefixFrom(netip.AddrFrom4(value), bits+1), nil
	}
	value := prefix.Addr().As16()
	value[bits/8] |= byte(1 << (7 - uint(bits%8)))
	return left, netip.PrefixFrom(netip.AddrFrom16(value), bits+1), nil
}

func sortPrefixes(prefixes []netip.Prefix) {
	sort.Slice(prefixes, func(i, j int) bool {
		if prefixes[i].Addr().BitLen() != prefixes[j].Addr().BitLen() {
			return prefixes[i].Addr().BitLen() < prefixes[j].Addr().BitLen()
		}
		if comparison := prefixes[i].Addr().Compare(prefixes[j].Addr()); comparison != 0 {
			return comparison < 0
		}
		return prefixes[i].Bits() < prefixes[j].Bits()
	})
}
