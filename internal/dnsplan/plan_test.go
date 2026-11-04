package dnsplan

import (
	"reflect"
	"testing"

	"github.com/InkaNode/kuraka/internal/stalwart"
)

const testZone = `example.com. 3600 IN MX 10 mail.example.net.
example.com. IN TXT "v=spf1 mx -all"
selector._domainkey.example.com. IN TXT "v=DKIM1; p=newkey"
_imaps._tcp.example.com. IN SRV 0 1 993 mail.example.net.
autoconfig.example.com. IN CNAME mail.example.net.
`

func TestPlan(t *testing.T) {
	desired, err := Desired("example.com", testZone, nil)
	if err != nil {
		t.Fatal(err)
	}
	var matching []Existing
	for i, r := range desired {
		e := asExisting(r)
		e.ID = string(rune('a' + i))
		matching = append(matching, e)
	}
	mx, spf, dkim, srv, cname := desired[0], desired[1], desired[2], desired[3], desired[4]
	staleDKIM := Existing{ID: "old", Name: dkim.Name, Type: "TXT", Content: "v=DKIM1; p=oldkey", Managed: true}
	otherDKIM := staleDKIM
	otherDKIM.ID, otherDKIM.Content = "older", "v=DKIM1; p=olderkey"
	unmanagedDKIM := otherDKIM
	unmanagedDKIM.Managed = false
	unmanagedMX := Existing{ID: "purelymail", Name: mx.Name, Type: "MX", Content: "mx1.purelymail.com", Priority: 10}
	tests := []struct {
		name      string
		desired   []stalwart.Record
		actual    []Existing
		ops       []Op
		existing  []string
		conflicts [][]Existing
	}{
		{name: "fresh domain", desired: desired, ops: []Op{Create, Create, Create, Create, Create}},
		{name: "all ok", desired: desired, actual: matching, ops: []Op{OK, OK, OK, OK, OK}, existing: []string{"a", "b", "c", "d", "e"}},
		{name: "Purelymail unmanaged MX and SPF", desired: []stalwart.Record{mx, spf}, actual: []Existing{
			unmanagedMX,
			{ID: "spf", Type: "TXT", Name: spf.Name, Content: "v=spf1 include:_spf.purelymail.com ~all"},
		}, ops: []Op{Conflict, Conflict}, conflicts: [][]Existing{
			{unmanagedMX},
			{{ID: "spf", Type: "TXT", Name: spf.Name, Content: "v=spf1 include:_spf.purelymail.com ~all"}},
		}},
		{name: "managed stale DKIM", desired: []stalwart.Record{dkim}, actual: []Existing{staleDKIM}, ops: []Op{Update}, existing: []string{"old"}},
		{name: "managed duplicates to delete", desired: []stalwart.Record{dkim}, actual: []Existing{staleDKIM, otherDKIM}, ops: []Op{Update}, existing: []string{"old"}, conflicts: [][]Existing{{otherDKIM}}},
		{name: "mixed ownership", desired: []stalwart.Record{dkim}, actual: []Existing{staleDKIM, unmanagedDKIM}, ops: []Op{Conflict}, conflicts: [][]Existing{{staleDKIM, unmanagedDKIM}}},
		{name: "matching MX with foreign target", desired: []stalwart.Record{mx}, actual: []Existing{matching[0], unmanagedMX}, ops: []Op{Conflict}, conflicts: [][]Existing{{unmanagedMX}}},
		{name: "matching MX with managed stale target", desired: []stalwart.Record{mx}, actual: []Existing{matching[0], {Type: "MX", Name: mx.Name, Content: "old.example.net", Managed: true}}, ops: []Op{OK}, existing: []string{"a"}},
		{name: "matching TXT takes precedence", desired: []stalwart.Record{dkim}, actual: []Existing{unmanagedDKIM, matching[2]}, ops: []Op{OK}, existing: []string{"c"}},
		{name: "MX priority matters", desired: []stalwart.Record{mx}, actual: []Existing{{Type: "MX", Name: mx.Name, Content: mx.Value, Priority: 20}}, ops: []Op{Conflict}, conflicts: [][]Existing{{{Type: "MX", Name: mx.Name, Content: mx.Value, Priority: 20}}}},
		{name: "SRV priority matters", desired: []stalwart.Record{srv}, actual: []Existing{{ID: "srv", Type: "SRV", Name: srv.Name, Content: srv.Value, Priority: 10, Managed: true}}, ops: []Op{Update}, existing: []string{"srv"}},
		{name: "CNAME blocks addresses", desired: []stalwart.Record{cname}, actual: []Existing{
			{Type: "A", Name: cname.Name, Content: "192.0.2.1"},
			{Type: "AAAA", Name: cname.Name, Content: "2001:db8::1", Managed: true},
		}, ops: []Op{Conflict}, conflicts: [][]Existing{{
			{Type: "A", Name: cname.Name, Content: "192.0.2.1"},
			{Type: "AAAA", Name: cname.Name, Content: "2001:db8::1", Managed: true},
		}}},
		{name: "unrelated records ignored", desired: []stalwart.Record{spf}, actual: []Existing{
			{Type: "TXT", Name: spf.Name, Content: "google-site-verification=abc"},
			{Type: "TXT", Name: "other.example.com", Content: spf.Value},
			{Type: "MX", Name: spf.Name, Content: mx.Value},
		}, ops: []Op{Create}},
		{name: "no desired slots", actual: matching},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := append([]Existing(nil), tt.actual...)
			got := Plan(tt.desired, tt.actual)
			if len(got) != len(tt.ops) {
				t.Fatalf("got %d changes, want %d", len(got), len(tt.ops))
			}
			wantCounts := map[Op]int{}
			for i, c := range got {
				wantCounts[tt.ops[i]]++
				if c.Op != tt.ops[i] || c.Desired != tt.desired[i] {
					t.Errorf("change %d = %+v, want %s for %+v", i, c, tt.ops[i], tt.desired[i])
				}
				if len(tt.existing) > 0 {
					if c.Existing == nil || c.Existing.ID != tt.existing[i] {
						t.Errorf("change %d existing = %+v, want ID %s", i, c.Existing, tt.existing[i])
					}
				} else if c.Existing != nil {
					t.Errorf("unexpected existing record: %+v", c.Existing)
				}
				var conflicts []Existing
				if len(tt.conflicts) > 0 {
					conflicts = tt.conflicts[i]
				}
				if len(c.Conflicts) != len(conflicts) || (len(conflicts) > 0 && !reflect.DeepEqual(c.Conflicts, conflicts)) {
					t.Errorf("conflicts = %+v, want %+v", c.Conflicts, conflicts)
				}
			}
			if counts := Summary(got); !reflect.DeepEqual(counts, wantCounts) {
				t.Errorf("Summary = %v, want %v", counts, wantCounts)
			}
			if !reflect.DeepEqual(tt.actual, before) {
				t.Fatal("Plan modified actual")
			}
		})
	}
}

func TestNormalization(t *testing.T) {
	tests := []struct {
		name, typ, desired, actual string
		want                       Op
	}{
		{"quoted SPF", "TXT", "v=spf1 mx -all", `"v=spf1 mx -all"`, OK},
		{"split SPF", "TXT", "v=spf1 mx -all", `"v=spf1 " "mx -all"`, OK},
		{"split key", "TXT", "v=DKIM1; p=abcd", `"v=DKIM1; p=ab" "cd"`, OK},
		{"whitespace", "TXT", "v=spf1 mx -all", "\"v=spf1   mx\t-all\"", OK},
		{"escaped quotes", "TXT", `verification="abc"`, `"verification=\"abc\""`, OK},
		{"decimal escape", "TXT", "v=spf1 mx -all", `"v=spf1\032mx -all"`, OK},
		{"case sensitive TXT data", "TXT", "v=DKIM1; p=ABCD", "v=DKIM1; p=abcd", Conflict},
		{"other TXT exact only", "TXT", "verification=abc", "verification=def", Create},
		{"SPF family boundary", "TXT", "v=spf1 mx -all", "v=spf10 mx -all", Create},
		{"CNAME target", "CNAME", "Mail.Example.net.", "mail.example.NET", OK},
		{"MX target", "MX", "Mail.Example.net.", "mail.example.NET", OK},
		{"SRV target", "SRV", "1 993 Mail.Example.net.", "1\t993 mail.example.NET", OK},
		{"SRV weight", "SRV", "1 993 mail.example.net", "2 993 mail.example.net", Conflict},
		{"SRV port", "SRV", "1 993 mail.example.net", "1 143 mail.example.net", Conflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := stalwart.Record{Type: tt.typ, Name: "EXAMPLE.com.", Value: tt.desired}
			e := Existing{Type: tt.typ, Name: "example.COM", Content: tt.actual}
			if c := Plan([]stalwart.Record{r}, []Existing{e})[0]; c.Op != tt.want {
				t.Errorf("Plan = %+v, want %s", c, tt.want)
			}
		})
	}
	for _, family := range []string{"v=spf1", "v=DMARC1", "v=DKIM1", "v=STSv1", "v=TLSRPTv1", "v=UAAC1"} {
		t.Run(family, func(t *testing.T) {
			r := stalwart.Record{Type: "TXT", Name: "example.com", Value: family + "; p=new"}
			e := Existing{Type: "TXT", Name: r.Name, Content: `"` + family + `; p=old"`, Managed: true}
			if got := Plan([]stalwart.Record{r}, []Existing{e})[0].Op; got != Update {
				t.Errorf("got %s, want update", got)
			}
		})
	}
}

func TestDesired(t *testing.T) {
	for _, tt := range []struct {
		name    string
		zone    string
		aliases []string
		count   int
		wantErr bool
	}{
		{"primary", testZone, nil, 5, false},
		{"alias rewrite dedupe", testZone + "EXAMPLE.COM. IN MX 10 MAIL.EXAMPLE.NET.\n", []string{"alias.com", "ALIAS.COM.", "example.com."}, 10, false},
		{"outside owner not aliased", testZone + "other.net. IN TXT \"verification=abc\"\n", []string{"alias.com"}, 11, false},
		{"invalid zone", "example.com. IN MX ten mail.example.net.", nil, 0, true},
		{"empty zone", "", []string{"alias.com"}, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Desired("example.com.", tt.zone, tt.aliases)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != tt.count {
				t.Fatalf("got %d records, want %d: %+v", len(got), tt.count, got)
			}
			if len(tt.aliases) > 0 && len(got) > 0 {
				base, err := stalwart.ParseZone(testZone)
				if err != nil {
					t.Fatal(err)
				}
				offset := len(got) - len(base)
				for i, r := range got[offset:] {
					if r.Name != stalwart.RewriteForAlias(base, "example.com", "alias.com")[i].Name || r.Value != base[i].Value || r.Priority != base[i].Priority || r.TTL != base[i].TTL {
						t.Errorf("unexpected alias record %+v", r)
					}
				}
			}
		})
	}
}
