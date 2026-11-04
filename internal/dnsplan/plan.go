// Package dnsplan builds DNS changes without modifying provider records.
package dnsplan

import (
	"strings"
	"unicode"

	"github.com/InkaNode/kuraka/internal/stalwart"
)

// Existing is a provider record. Managed identifies records owned by Kuraka.
type Existing struct {
	ID, Type, Name, Content string
	Priority                int
	Managed                 bool
}

type Op string

const (
	OK       Op = "ok"
	Create   Op = "create"
	Update   Op = "update"
	Conflict Op = "conflict"
)

// Change describes one desired record. On Update, Conflicts contains additional
// managed records to delete; on Conflict, it contains all differing slot records.
type Change struct {
	Op        Op
	Desired   stalwart.Record
	Existing  *Existing
	Conflicts []Existing
}

// Desired parses Stalwart's primary zone and adds rewritten alias records,
// retaining the first occurrence of each equivalent record.
func Desired(primary, zone string, aliases []string) ([]stalwart.Record, error) {
	base, err := stalwart.ParseZone(zone)
	if err != nil {
		return nil, err
	}
	var out []stalwart.Record
	appendUnique := func(records []stalwart.Record) {
		for _, r := range records {
			duplicate := false
			for _, prev := range out {
				if sameRecord(prev, asExisting(r)) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				out = append(out, r)
			}
		}
	}
	appendUnique(base)
	for _, alias := range aliases {
		appendUnique(stalwart.RewriteForAlias(base, primary, alias))
	}
	return out, nil
}

// Plan reports only slots requested by desired and leaves the inputs unchanged.
// TTL is not compared: Existing deliberately has no provider TTL field.
func Plan(desired []stalwart.Record, actual []Existing) []Change {
	out := make([]Change, 0, len(desired))
	for _, r := range desired {
		change := Change{Op: Create, Desired: r}
		var differing []Existing
		unmanaged := false
		for i := range actual {
			e := &actual[i]
			if !sameSlot(r, *e) {
				continue
			}
			if sameRecord(r, *e) {
				if change.Existing == nil {
					change.Existing = e
				}
			} else {
				differing = append(differing, *e)
				unmanaged = unmanaged || !e.Managed
			}
		}
		switch {
		case change.Existing != nil && !(recordType(r.Type) == "MX" && unmanaged):
			change.Op = OK
		case unmanaged:
			change.Op = Conflict
			change.Existing = nil
			change.Conflicts = differing
		case len(differing) > 0:
			change.Op = Update
			first := differing[0]
			change.Existing = &first
			change.Conflicts = differing[1:]
		}
		out = append(out, change)
	}
	return out
}

func Summary(changes []Change) map[Op]int {
	counts := make(map[Op]int)
	for _, c := range changes {
		counts[c.Op]++
	}
	return counts
}

func recordType(s string) string { return strings.ToUpper(s) }

func dnsName(s string) string { return strings.ToLower(strings.TrimRight(s, ".")) }

func asExisting(r stalwart.Record) Existing {
	return Existing{Type: r.Type, Name: r.Name, Content: r.Value, Priority: r.Priority}
}

func sameSlot(r stalwart.Record, e Existing) bool {
	if dnsName(r.Name) != dnsName(e.Name) {
		return false
	}
	t, et := recordType(r.Type), recordType(e.Type)
	if t == "CNAME" {
		return et == "A" || et == "AAAA" || et == "CNAME"
	}
	// A CNAME also obstructs an explicitly requested address record.
	if (t == "A" || t == "AAAA") && et == "CNAME" {
		return true
	}
	if t != et {
		return false
	}
	if t == "TXT" {
		family := txtFamily(r.Value)
		if family != "" {
			return family == txtFamily(e.Content)
		}
		return normalizeTXT(r.Value) == normalizeTXT(e.Content)
	}
	return true
}

func sameRecord(r stalwart.Record, e Existing) bool {
	if dnsName(r.Name) != dnsName(e.Name) || recordType(r.Type) != recordType(e.Type) {
		return false
	}
	switch recordType(r.Type) {
	case "TXT":
		return normalizeTXT(r.Value) == normalizeTXT(e.Content)
	case "MX":
		return r.Priority == e.Priority && dnsName(r.Value) == dnsName(e.Content)
	case "CNAME", "NS", "PTR":
		return dnsName(r.Value) == dnsName(e.Content)
	case "SRV":
		return r.Priority == e.Priority && normalizeSRV(r.Value) == normalizeSRV(e.Content)
	default:
		return r.Value == e.Content
	}
}

func normalizeSRV(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 3 {
		fields[2] = dnsName(fields[2])
	}
	return strings.Join(fields, " ")
}

func txtFamily(s string) string {
	s = strings.ToLower(normalizeTXT(s))
	for _, family := range []string{"v=spf1", "v=dmarc1", "v=dkim1", "v=stsv1", "v=tlsrptv1", "v=uaac1"} {
		if strings.HasPrefix(s, family) && (len(s) == len(family) || s[len(family)] == ';' || unicode.IsSpace(rune(s[len(family)]))) {
			return family
		}
	}
	return ""
}

// normalizeTXT joins DNS presentation-format quoted chunks without inserting
// spaces, then collapses whitespace. Unquoted Stalwart values retain literals.
func normalizeTXT(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "\"") {
		if decoded, ok := quotedTXT(s); ok {
			s = decoded
		}
	}
	return strings.Join(strings.Fields(s), " ")
}

func quotedTXT(s string) (string, bool) {
	var b strings.Builder
	for len(s) > 0 {
		if s[0] != '"' {
			return "", false
		}
		s = s[1:]
		closed := false
		for len(s) > 0 {
			c := s[0]
			s = s[1:]
			if c == '"' {
				closed = true
				break
			}
			if c == '\\' {
				if len(s) == 0 {
					return "", false
				}
				if len(s) >= 3 && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9' && s[2] >= '0' && s[2] <= '9' {
					n := int(s[0]-'0')*100 + int(s[1]-'0')*10 + int(s[2]-'0')
					if n > 255 {
						return "", false
					}
					c, s = byte(n), s[3:]
				} else {
					c, s = s[0], s[1:]
				}
			}
			b.WriteByte(c)
		}
		if !closed {
			return "", false
		}
		s = strings.TrimSpace(s)
	}
	return b.String(), true
}
