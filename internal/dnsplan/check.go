package dnsplan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/InkaNode/kuraka/internal/stalwart"
)

type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupCNAME(ctx context.Context, name string) (string, error)
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
}

type Status string

const (
	StatusOK        Status = "ok"
	StatusMissing   Status = "missing"
	StatusDifferent Status = "different"
)

type RecordStatus struct {
	Record stalwart.Record
	Status Status
	Found  []string
}

// CheckExisting uses the same slot and ownership rules as Plan.
func CheckExisting(desired []stalwart.Record, actual []Existing) []RecordStatus {
	changes := Plan(desired, actual)
	out := make([]RecordStatus, 0, len(changes))
	for _, c := range changes {
		s := RecordStatus{Record: c.Desired, Status: StatusDifferent}
		switch c.Op {
		case OK:
			s.Status = StatusOK
		case Create:
			s.Status = StatusMissing
		}
		for _, e := range actual {
			if sameSlot(c.Desired, e) {
				s.Found = append(s.Found, foundValue(e))
			}
		}
		out = append(out, s)
	}
	return out
}

// CheckPublic queries every desired record. Public records have no management
// metadata, so additional differing MX records are treated as conflicts.
func CheckPublic(ctx context.Context, r Resolver, desired []stalwart.Record) []RecordStatus {
	out := make([]RecordStatus, 0, len(desired))
	for _, record := range desired {
		actual, err := lookup(ctx, r, record)
		if err != nil {
			s := RecordStatus{Record: record, Status: StatusDifferent, Found: []string{err.Error()}}
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				s.Status, s.Found = StatusMissing, nil
			}
			out = append(out, s)
			continue
		}
		s := CheckExisting([]stalwart.Record{record}, actual)[0]
		// A response containing other TXT families is different from no answer.
		if s.Status == StatusMissing && len(actual) > 0 {
			s.Status = StatusDifferent
		}
		s.Found = nil
		for _, e := range actual {
			s.Found = append(s.Found, foundValue(e))
		}
		out = append(out, s)
	}
	return out
}

func lookup(ctx context.Context, r Resolver, record stalwart.Record) ([]Existing, error) {
	var out []Existing
	add := func(value string, priority int) {
		out = append(out, Existing{Name: record.Name, Type: record.Type, Content: value, Priority: priority})
	}
	switch recordType(record.Type) {
	case "MX":
		records, err := r.LookupMX(ctx, record.Name)
		if err != nil {
			return nil, err
		}
		for _, mx := range records {
			if mx != nil {
				add(mx.Host, int(mx.Pref))
			}
		}
	case "TXT":
		records, err := r.LookupTXT(ctx, record.Name)
		if err != nil {
			return nil, err
		}
		for _, txt := range records {
			add(txt, 0)
		}
	case "CNAME":
		target, err := r.LookupCNAME(ctx, record.Name)
		if err != nil {
			return nil, err
		}
		if target != "" {
			add(target, 0)
		}
	case "SRV":
		_, records, err := r.LookupSRV(ctx, "", "", record.Name)
		if err != nil {
			return nil, err
		}
		for _, srv := range records {
			if srv != nil {
				add(fmt.Sprintf("%d %d %s", srv.Weight, srv.Port, srv.Target), int(srv.Priority))
			}
		}
	default:
		return nil, fmt.Errorf("dnsplan: unsupported public DNS record type %q", record.Type)
	}
	return out, nil
}

func foundValue(e Existing) string {
	if t := recordType(e.Type); t == "MX" || t == "SRV" {
		return fmt.Sprintf("%d %s", e.Priority, e.Content)
	}
	return e.Content
}

// NewResolver sends Go DNS queries to addr (host:port) using UDP.
func NewResolver(addr string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "udp", addr)
		},
	}
}
