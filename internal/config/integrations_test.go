package config

import (
	"strings"
	"testing"
)

func TestIntegrationConfig(t *testing.T) {
	c, err := LoadFrom(env(with(EnvMailHost, "mail.example.com", EnvMailTLSSkipVerify, "true", EnvDNSResolver, "[::1]:5353", EnvMigrationWorkers, "8")))
	if err != nil {
		t.Fatal(err)
	}
	if c.MailHost != "mail.example.com" || !c.MailTLSSkipVerify || c.DNSResolver != "[::1]:5353" || c.MigrationWorkers != 8 {
		t.Fatalf("unexpected config: %+v", c)
	}
	for _, tc := range []struct{ key, value string }{
		{EnvMailHost, "https://mail.example.com"}, {EnvMailHost, "mail.example.com:993"}, {EnvMailHost, "mail.example.com/path"}, {EnvMailHost, "-bad.test"},
		{EnvMailTLSSkipVerify, "maybe"}, {EnvDNSResolver, "1.1.1.1"}, {EnvDNSResolver, ":53"}, {EnvDNSResolver, "dns:0"}, {EnvDNSResolver, "dns:65536"}, {EnvDNSResolver, "dns:abc"}, {EnvDNSResolver, "https://dns:53"},
		{EnvMigrationWorkers, "0"}, {EnvMigrationWorkers, "9"}, {EnvMigrationWorkers, "many"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			_, err := LoadFrom(env(with(tc.key, tc.value)))
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
