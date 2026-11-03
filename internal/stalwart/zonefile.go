package stalwart

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Record is one DNS record from a zone file. Names and targets carry no
// trailing dot. For MX, Priority is the preference and Value the target;
// for SRV, Priority is the priority and Value is "weight port target".
type Record struct {
	Name     string
	Type     string
	Value    string
	Priority int
	TTL      int
}

type zoneToken struct {
	text   string
	quoted bool
}

type zoneLine struct {
	line   int
	tokens []zoneToken
}

// ParseZone parses the dnsZoneFile text that Stalwart generates for a domain.
func ParseZone(text string) ([]Record, error) {
	lines, err := zoneLines(text)
	if err != nil {
		return nil, err
	}
	var recs []Record
	for _, l := range lines {
		if strings.HasPrefix(l.tokens[0].text, "$") && !l.tokens[0].quoted {
			continue
		}
		r, err := parseRecord(l.tokens)
		if err != nil {
			return nil, fmt.Errorf("stalwart: zone line %d: %w", l.line, err)
		}
		recs = append(recs, r)
	}
	return recs, nil
}

// zoneLines tokenizes text into records, joining lines inside ( ... ) and
// dropping comments.
func zoneLines(text string) ([]zoneLine, error) {
	var (
		out   []zoneLine
		cur   zoneLine
		depth int
		line  = 1
	)
	flush := func() {
		if len(cur.tokens) > 0 {
			out = append(out, cur)
		}
		cur = zoneLine{}
	}
	for i := 0; i < len(text); {
		ch := text[i]
		switch {
		case ch == '\n':
			line++
			if depth == 0 {
				flush()
			}
			i++
		case ch == ' ' || ch == '\t' || ch == '\r':
			i++
		case ch == ';':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case ch == '(':
			depth++
			i++
		case ch == ')':
			if depth == 0 {
				return nil, fmt.Errorf("stalwart: zone line %d: unbalanced )", line)
			}
			depth--
			i++
		case ch == '"':
			s, n, err := readQuoted(text[i+1:])
			if err != nil {
				return nil, fmt.Errorf("stalwart: zone line %d: %w", line, err)
			}
			if len(cur.tokens) == 0 {
				cur.line = line
			}
			cur.tokens = append(cur.tokens, zoneToken{text: s, quoted: true})
			line += strings.Count(text[i:i+1+n], "\n")
			i += 1 + n
		default:
			j := i
			for j < len(text) && !strings.ContainsRune(" \t\r\n;()\"", rune(text[j])) {
				j++
			}
			if len(cur.tokens) == 0 {
				cur.line = line
			}
			cur.tokens = append(cur.tokens, zoneToken{text: text[i:j]})
			i = j
		}
	}
	if depth != 0 {
		return nil, errors.New("stalwart: zone: unbalanced (")
	}
	flush()
	return out, nil
}

// readQuoted reads a quoted string body (after the opening quote) and
// returns its unescaped value and the bytes consumed, closing quote included.
func readQuoted(s string) (string, int, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			return b.String(), i + 1, nil
		case '\\':
			if i+3 < len(s) && isDigit(s[i+1]) && isDigit(s[i+2]) && isDigit(s[i+3]) {
				n, _ := strconv.Atoi(s[i+1 : i+4])
				if n > 255 {
					return "", 0, fmt.Errorf("bad escape \\%s", s[i+1:i+4])
				}
				b.WriteByte(byte(n))
				i += 3
				continue
			}
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			return "", 0, errors.New("unterminated string")
		default:
			b.WriteByte(s[i])
		}
	}
	return "", 0, errors.New("unterminated string")
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func parseRecord(toks []zoneToken) (Record, error) {
	r := Record{Name: trimDot(toks[0].text)}
	rest := toks[1:]
	for len(rest) > 0 && !rest[0].quoted {
		t := rest[0].text
		if n, err := strconv.Atoi(t); err == nil {
			r.TTL = n
		} else if !isClass(t) {
			break
		}
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return r, errors.New("missing record type")
	}
	r.Type = strings.ToUpper(rest[0].text)
	rdata := rest[1:]
	switch r.Type {
	case "TXT", "SPF":
		var b strings.Builder
		for _, t := range rdata {
			b.WriteString(t.text)
		}
		r.Value = b.String()
	case "MX":
		if len(rdata) != 2 {
			return r, errors.New("MX needs preference and target")
		}
		p, err := strconv.Atoi(rdata[0].text)
		if err != nil {
			return r, fmt.Errorf("MX preference: %w", err)
		}
		r.Priority, r.Value = p, trimDot(rdata[1].text)
	case "SRV":
		if len(rdata) != 4 {
			return r, errors.New("SRV needs priority, weight, port and target")
		}
		var n [3]int
		for i := range n {
			v, err := strconv.Atoi(rdata[i].text)
			if err != nil {
				return r, fmt.Errorf("SRV field %d: %w", i+1, err)
			}
			n[i] = v
		}
		r.Priority = n[0]
		r.Value = fmt.Sprintf("%d %d %s", n[1], n[2], trimDot(rdata[3].text))
	case "CNAME", "NS", "PTR":
		if len(rdata) != 1 {
			return r, fmt.Errorf("%s needs one target", r.Type)
		}
		r.Value = trimDot(rdata[0].text)
	default:
		parts := make([]string, 0, len(rdata))
		for _, t := range rdata {
			parts = append(parts, t.text)
		}
		r.Value = strings.Join(parts, " ")
	}
	return r, nil
}

func isClass(s string) bool {
	switch strings.ToUpper(s) {
	case "IN", "CH", "HS", "CS":
		return true
	}
	return false
}

func trimDot(s string) string { return strings.TrimSuffix(s, ".") }

// RewriteForAlias returns copies of the records owned by primary (or its
// subdomains) with the primary suffix of the owner name replaced by alias.
// Values are left untouched. Records outside primary are dropped.
func RewriteForAlias(recs []Record, primary, alias string) []Record {
	p := strings.ToLower(trimDot(primary))
	a := trimDot(alias)
	out := make([]Record, 0, len(recs))
	for _, r := range recs {
		name := strings.ToLower(r.Name)
		switch {
		case name == p:
			r.Name = a
		case strings.HasSuffix(name, "."+p):
			r.Name = r.Name[:len(r.Name)-len(p)] + a
		default:
			continue
		}
		out = append(out, r)
	}
	return out
}
