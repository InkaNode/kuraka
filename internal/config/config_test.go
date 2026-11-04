package config

import (
	"strings"
	"testing"
)

const testKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func base() map[string]string {
	return map[string]string{
		EnvPublicURL:      "https://panel.inkanode.com",
		EnvSecretKey:      testKey,
		EnvStalwartAPIKey: "API_supersecretvalue",
	}
}

func with(kv ...string) map[string]string {
	m := base()
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func TestLoadFromValid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{
			name: "defaults",
			env:  base(),
			want: Config{
				Listen: ":8080", DBPath: "kuraka.db",
				PublicURL: "https://panel.inkanode.com", SecretKeyHex: testKey,
				StalwartURL: "http://stalwart:8080", StalwartAPIKey: "API_supersecretvalue",
			},
		},
		{
			name: "overrides",
			env: with(
				EnvListen, "127.0.0.1:9000",
				EnvDB, "/data/k.db",
				EnvPublicURL, "http://LocalHost:8080/",
				EnvStalwartURL, "https://mail.example.com/api/",
				EnvInsecureCookies, "true",
			),
			want: Config{
				Listen: "127.0.0.1:9000", DBPath: "/data/k.db",
				PublicURL: "http://localhost:8080", SecretKeyHex: testKey,
				StalwartURL: "https://mail.example.com/api", StalwartAPIKey: "API_supersecretvalue",
				InsecureCookies: true,
			},
		},
		{
			name: "uppercase hex key and explicit false",
			env:  with(EnvSecretKey, strings.ToUpper(testKey), EnvInsecureCookies, "0"),
			want: Config{
				Listen: ":8080", DBPath: "kuraka.db",
				PublicURL: "https://panel.inkanode.com", SecretKeyHex: strings.ToUpper(testKey),
				StalwartURL: "http://stalwart:8080", StalwartAPIKey: "API_supersecretvalue",
			},
		},
		{
			name: "alert email",
			env:  with(EnvMailHost, "mail.example.com", EnvAlertEmailFrom, "alertas@example.com"),
			want: Config{
				Listen: ":8080", DBPath: "kuraka.db",
				PublicURL: "https://panel.inkanode.com", SecretKeyHex: testKey,
				StalwartURL: "http://stalwart:8080", StalwartAPIKey: "API_supersecretvalue",
				MailHost: "mail.example.com", AlertEmailFrom: "alertas@example.com",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.want.DNSResolver = DefaultDNSResolver
			tt.want.MigrationWorkers = DefaultMigrationWorkers
			got, err := LoadFrom(env(tt.env))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestLoadFromInvalid(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		wants []string
	}{
		{"all missing", map[string]string{}, []string{EnvPublicURL, EnvSecretKey, EnvStalwartAPIKey}},
		{"public url relative", with(EnvPublicURL, "panel.inkanode.com"), []string{EnvPublicURL}},
		{"public url ftp", with(EnvPublicURL, "ftp://panel.inkanode.com"), []string{EnvPublicURL}},
		{"public url path", with(EnvPublicURL, "https://panel.inkanode.com/app"), []string{EnvPublicURL}},
		{"public url query", with(EnvPublicURL, "https://panel.inkanode.com/?x=1"), []string{EnvPublicURL}},
		{"public url no host", with(EnvPublicURL, "https:///"), []string{EnvPublicURL}},
		{"key short", with(EnvSecretKey, testKey[:62]), []string{EnvSecretKey}},
		{"key long", with(EnvSecretKey, testKey+"00"), []string{EnvSecretKey}},
		{"key not hex", with(EnvSecretKey, strings.Repeat("zz", 32)), []string{EnvSecretKey}},
		{"stalwart url bad scheme", with(EnvStalwartURL, "stalwart:8080"), []string{EnvStalwartURL}},
		{"stalwart url credentials", with(EnvStalwartURL, "http://u:hunter2@stalwart"), []string{EnvStalwartURL}},
		{"insecure cookies", with(EnvInsecureCookies, "maybe"), []string{EnvInsecureCookies}},
		{"alert email invalid", with(EnvMailHost, "mail.example.com", EnvAlertEmailFrom, "Alertas <alertas@example.com>"), []string{EnvAlertEmailFrom}},
		{"alert email without mail host", with(EnvAlertEmailFrom, "alertas@example.com"), []string{EnvAlertEmailFrom, EnvMailHost}},
		{
			"several at once",
			with(EnvPublicURL, "nope", EnvSecretKey, "abc", EnvInsecureCookies, "x"),
			[]string{EnvPublicURL, EnvSecretKey, EnvInsecureCookies},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFrom(env(tt.env))
			if err == nil {
				t.Fatal("expected error")
			}
			msg := err.Error()
			for _, w := range tt.wants {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q does not mention %s", msg, w)
				}
			}
			for _, secret := range []string{tt.env[EnvSecretKey], tt.env[EnvStalwartAPIKey], "hunter2"} {
				if len(secret) >= 3 && strings.Contains(msg, secret) {
					t.Errorf("error leaks secret %q: %s", secret, msg)
				}
			}
		})
	}
}
