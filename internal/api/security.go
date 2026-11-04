package api

import (
	"net/http"
	"net/netip"
	"slices"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/stalwart"
)

func (s *Server) securityRoutes(route func(string, string, bool, bool, endpoint)) {
	route("GET /api/v1/blocked-ips", "blockedip.list", false, true, s.blockedIPs)
	route("DELETE /api/v1/blocked-ips/{id}", "blockedip.delete", false, true, s.unblockIP)
}

// trustNetwork returns the network to trust for a private or loopback
// address (/24 for IPv4, /64 for IPv6) and "" for public ones. IPv6
// loopback stays a single address: ::/64 would cover every mapped IPv4.
func trustNetwork(address string) string {
	addr, err := netip.ParseAddr(address)
	if err != nil {
		prefix, err := netip.ParsePrefix(address)
		if err != nil {
			return ""
		}
		addr = prefix.Addr()
	}
	addr = addr.Unmap().WithZone("")
	if !addr.IsPrivate() && !addr.IsLoopback() {
		return ""
	}
	bits := 64
	if addr.Is4() {
		bits = 24
	} else if addr.IsLoopback() {
		bits = 128
	}
	return netip.PrefixFrom(addr, bits).Masked().String()
}

func (s *Server) blockedIPs(_ http.ResponseWriter, r *http.Request) (any, error) {
	ips, err := s.sw.ListBlockedIPs(r.Context())
	if err != nil {
		return nil, upstream(err)
	}
	out := make([]any, 0, len(ips))
	for _, ip := range ips {
		trust := trustNetwork(ip.Address)
		out = append(out, map[string]any{"id": ip.ID, "address": ip.Address, "reason": ip.Reason, "createdAt": ip.CreatedAt, "expiresAt": ip.ExpiresAt, "internal": trust != "", "trust": trust})
	}
	return out, nil
}

// unblockIP derives the trusted network from Stalwart's own record; the
// client never chooses it.
func (s *Server) unblockIP(_ http.ResponseWriter, r *http.Request) (any, error) {
	ips, err := s.sw.ListBlockedIPs(r.Context())
	if err != nil {
		return nil, upstream(err)
	}
	i := slices.IndexFunc(ips, func(ip stalwart.BlockedIP) bool { return ip.ID == r.PathValue("id") })
	if i < 0 {
		return nil, authz.ErrNotFound
	}
	ip := ips[i]
	trust := trustNetwork(ip.Address)
	state(r).detail = ip.Address
	if trust != "" {
		state(r).detail += " trust " + trust
	}
	if err := s.sw.UnblockIP(r.Context(), ip.ID, trust); err != nil {
		return nil, upstream(err)
	}
	return reply{http.StatusNoContent, nil}, nil
}
