package dnsplan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/InkaNode/kuraka/internal/stalwart"
)

type fakeResolver struct {
	mx             []*net.MX
	txt            []string
	cname          string
	srv            []*net.SRV
	err            error
	method, name   string
	service, proto string
	ctx            context.Context
}

func (f *fakeResolver) called(ctx context.Context, method, name string) {
	f.ctx, f.method, f.name = ctx, method, name
}

func (f *fakeResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	f.called(ctx, "MX", name)
	return f.mx, f.err
}

func (f *fakeResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	f.called(ctx, "TXT", name)
	return f.txt, f.err
}

func (f *fakeResolver) LookupCNAME(ctx context.Context, name string) (string, error) {
	f.called(ctx, "CNAME", name)
	return f.cname, f.err
}

func (f *fakeResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	f.called(ctx, "SRV", name)
	f.service, f.proto = service, proto
	return "", f.srv, f.err
}

func TestCheckPublic(t *testing.T) {
	mx := stalwart.Record{Type: "MX", Name: "example.com", Value: "mail.example.net", Priority: 10}
	txt := stalwart.Record{Type: "TXT", Name: "example.com", Value: "v=spf1 mx -all"}
	cname := stalwart.Record{Type: "CNAME", Name: "autoconfig.example.com", Value: "mail.example.net"}
	srv := stalwart.Record{Type: "SRV", Name: "_imaps._tcp.example.com", Value: "1 993 mail.example.net", Priority: 5}
	for _, tt := range []struct {
		name   string
		record stalwart.Record
		fake   fakeResolver
		status Status
		found  []string
	}{
		{"MX ok", mx, fakeResolver{mx: []*net.MX{{Host: "MAIL.example.net.", Pref: 10}}}, StatusOK, []string{"10 MAIL.example.net."}},
		{"MX foreign target", mx, fakeResolver{mx: []*net.MX{{Host: mx.Value, Pref: 10}, {Host: "mx.purelymail.com.", Pref: 20}}}, StatusDifferent, []string{"10 mail.example.net", "20 mx.purelymail.com."}},
		{"MX priority differs", mx, fakeResolver{mx: []*net.MX{{Host: mx.Value, Pref: 20}}}, StatusDifferent, []string{"20 mail.example.net"}},
		{"TXT split ok", txt, fakeResolver{txt: []string{`"v=spf1 " "mx -all"`}}, StatusOK, []string{`"v=spf1 " "mx -all"`}},
		{"TXT differs", txt, fakeResolver{txt: []string{"v=spf1 ~all"}}, StatusDifferent, []string{"v=spf1 ~all"}},
		{"TXT unrelated response", txt, fakeResolver{txt: []string{"verification=abc"}}, StatusDifferent, []string{"verification=abc"}},
		{"CNAME ok", cname, fakeResolver{cname: "MAIL.example.net."}, StatusOK, []string{"MAIL.example.net."}},
		{"CNAME differs", cname, fakeResolver{cname: "old.example.net."}, StatusDifferent, []string{"old.example.net."}},
		{"SRV ok", srv, fakeResolver{srv: []*net.SRV{{Target: "MAIL.example.net.", Priority: 5, Weight: 1, Port: 993}}}, StatusOK, []string{"5 1 993 MAIL.example.net."}},
		{"SRV differs", srv, fakeResolver{srv: []*net.SRV{{Target: "mail.example.net.", Priority: 6, Weight: 2, Port: 143}}}, StatusDifferent, []string{"6 2 143 mail.example.net."}},
		{"NXDOMAIN", mx, fakeResolver{err: &net.DNSError{Err: "no such host", IsNotFound: true}}, StatusMissing, nil},
		{"wrapped NXDOMAIN", txt, fakeResolver{err: fmt.Errorf("lookup: %w", &net.DNSError{Err: "no such host", IsNotFound: true})}, StatusMissing, nil},
		{"timeout", cname, fakeResolver{err: errors.New("DNS timeout")}, StatusDifferent, []string{"DNS timeout"}},
		{"cancelled", srv, fakeResolver{err: context.Canceled}, StatusDifferent, []string{"context canceled"}},
		{"empty MX", mx, fakeResolver{}, StatusMissing, nil},
		{"empty TXT", txt, fakeResolver{}, StatusMissing, nil},
		{"empty CNAME", cname, fakeResolver{}, StatusMissing, nil},
		{"empty SRV", srv, fakeResolver{}, StatusMissing, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got := CheckPublic(ctx, &tt.fake, []stalwart.Record{tt.record})
			want := []RecordStatus{{Record: tt.record, Status: tt.status, Found: tt.found}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
			if tt.fake.method != tt.record.Type || tt.fake.name != tt.record.Name || tt.fake.ctx != ctx {
				t.Errorf("incorrect lookup: %+v", tt.fake)
			}
			if tt.fake.service != "" || tt.fake.proto != "" {
				t.Error("SRV must use full name with empty service and proto")
			}
		})
	}
}

func TestCheckExisting(t *testing.T) {
	r := stalwart.Record{Type: "TXT", Name: "example.com", Value: "v=spf1 mx -all"}
	for _, tt := range []struct {
		name   string
		actual []Existing
		status Status
		found  []string
	}{
		{"ok", []Existing{asExisting(r)}, StatusOK, []string{r.Value}},
		{"missing", nil, StatusMissing, nil},
		{"managed update", []Existing{{Type: "TXT", Name: r.Name, Content: "v=spf1 ~all", Managed: true}}, StatusDifferent, []string{"v=spf1 ~all"}},
		{"unmanaged conflict", []Existing{{Type: "TXT", Name: r.Name, Content: "v=spf1 ~all"}}, StatusDifferent, []string{"v=spf1 ~all"}},
		{"unrelated slot", []Existing{{Type: "TXT", Name: r.Name, Content: "verification=abc"}}, StatusMissing, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckExisting([]stalwart.Record{r}, tt.actual)
			want := []RecordStatus{{Record: r, Status: tt.status, Found: tt.found}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestNewResolver(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	r := NewResolver(listener.LocalAddr().String())
	if !r.PreferGo || r.Dial == nil {
		t.Fatal("resolver must prefer Go and provide a custom dialer")
	}
	conn, err := r.Dial(context.Background(), "tcp", "unused.invalid:53")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if conn.RemoteAddr().Network() != "udp" || conn.RemoteAddr().String() != listener.LocalAddr().String() {
		t.Errorf("unexpected DNS destination: %v", conn.RemoteAddr())
	}
}
