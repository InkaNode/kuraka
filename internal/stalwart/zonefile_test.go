package stalwart

import (
	"reflect"
	"testing"
)

const sampleZone = `cliente1.test. IN TXT "v=spf1 mx -all"
cliente1.test. IN MX 10 mail.lab.test.
_dmarc.cliente1.test. IN TXT "v=DMARC1; p=reject; rua=mailto:postmaster@cliente1.test"
_imaps._tcp.cliente1.test. IN SRV 0 1 993 mail.lab.test.
autoconfig.cliente1.test. IN CNAME mail.lab.test.
v1-ed25519-20261103._domainkey.cliente1.test. IN TXT "v=DKIM1; k=ed25519; h=sha256; p=JyImYQI74mcTcYpMZNMGp7mQXGgjleRIhkOoSOqUlsY="
v1-rsa-20261103._domainkey.cliente1.test. IN TXT (
  "v=DKIM1; k=rsa; h=sha256; p=MIIBIjAN"
  "BgkqhkiG9w0BAQEFAAOC" )
`

var sampleRecords = []Record{
	{Name: "cliente1.test", Type: "TXT", Value: "v=spf1 mx -all"},
	{Name: "cliente1.test", Type: "MX", Value: "mail.lab.test", Priority: 10},
	{Name: "_dmarc.cliente1.test", Type: "TXT", Value: "v=DMARC1; p=reject; rua=mailto:postmaster@cliente1.test"},
	{Name: "_imaps._tcp.cliente1.test", Type: "SRV", Value: "1 993 mail.lab.test", Priority: 0},
	{Name: "autoconfig.cliente1.test", Type: "CNAME", Value: "mail.lab.test"},
	{Name: "v1-ed25519-20261103._domainkey.cliente1.test", Type: "TXT", Value: "v=DKIM1; k=ed25519; h=sha256; p=JyImYQI74mcTcYpMZNMGp7mQXGgjleRIhkOoSOqUlsY="},
	{Name: "v1-rsa-20261103._domainkey.cliente1.test", Type: "TXT", Value: "v=DKIM1; k=rsa; h=sha256; p=MIIBIjANBgkqhkiG9w0BAQEFAAOC"},
}

func TestParseZoneSample(t *testing.T) {
	got, err := ParseZone(sampleZone)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("got %d records, want 7: %+v", len(got), got)
	}
	for i := range sampleRecords {
		if got[i] != sampleRecords[i] {
			t.Errorf("record %d = %+v, want %+v", i, got[i], sampleRecords[i])
		}
	}
}

func TestParseZoneMisc(t *testing.T) {
	text := "$ORIGIN example.\n; comment line\n\r\n" +
		"a.test. 3600 IN TXT \"say \\\"hi\\\"\" ; trailing comment\n" +
		"a.test. IN A 192.0.2.1\n" +
		"a.test.\tIN\tCAA 0 issue \"letsencrypt.org\"\n"
	got, err := ParseZone(text)
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{
		{Name: "a.test", Type: "TXT", Value: `say "hi"`, TTL: 3600},
		{Name: "a.test", Type: "A", Value: "192.0.2.1"},
		{Name: "a.test", Type: "CAA", Value: "0 issue letsencrypt.org"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseZoneErrors(t *testing.T) {
	for _, text := range []string{
		"a.test. IN TXT (\n \"x\"\n",
		"a.test. IN TXT \"unterminated\n",
		"a.test. IN MX ten mail.test.\n",
		"a.test. IN SRV 0 1 mail.test.\n",
		"a.test. IN\n",
		"a.test. IN TXT \"x\" )\n",
	} {
		if _, err := ParseZone(text); err == nil {
			t.Errorf("ParseZone(%q) succeeded", text)
		}
	}
}

func TestRewriteForAlias(t *testing.T) {
	recs := append([]Record{{Name: "mail.lab.test", Type: "A", Value: "192.0.2.1"}}, sampleRecords...)
	got := RewriteForAlias(recs, "cliente1.test.", "Alt.test")
	if len(got) != len(sampleRecords) {
		t.Fatalf("got %d records, want %d", len(got), len(sampleRecords))
	}
	wantNames := []string{
		"Alt.test", "Alt.test", "_dmarc.Alt.test", "_imaps._tcp.Alt.test", "autoconfig.Alt.test",
		"v1-ed25519-20261103._domainkey.Alt.test", "v1-rsa-20261103._domainkey.Alt.test",
	}
	for i, r := range got {
		if r.Name != wantNames[i] {
			t.Errorf("record %d name = %q, want %q", i, r.Name, wantNames[i])
		}
		if r.Value != sampleRecords[i].Value || r.Type != sampleRecords[i].Type || r.Priority != sampleRecords[i].Priority {
			t.Errorf("record %d changed: %+v", i, r)
		}
	}
	if sampleRecords[0].Name != "cliente1.test" {
		t.Fatal("input records were modified")
	}
}

func TestRewriteForAliasSkipsMailHost(t *testing.T) {
	recs := []Record{
		{Name: "cliente1.test", Type: "MX", Value: "mail.cliente1.test.", Priority: 10},
		{Name: "mail.cliente1.test", Type: "TXT", Value: "v=spf1 a -all"},
		{Name: "_25._tcp.Mail.cliente1.test", Type: "TLSA", Value: "3 1 1 aa"},
		{Name: "cliente1.test", Type: "TXT", Value: "v=spf1 mx -all"},
	}
	got := RewriteForAlias(recs, "cliente1.test", "alt.test")
	if len(got) != 2 || got[0].Type != "MX" || got[0].Name != "alt.test" || got[1].Name != "alt.test" {
		t.Fatalf("got %+v", got)
	}
}
