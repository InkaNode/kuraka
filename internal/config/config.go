// Package config loads Kuraka settings from KURAKA_* environment variables.
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Environment variable names.
const (
	EnvListen                = "KURAKA_LISTEN"
	EnvDB                    = "KURAKA_DB"
	EnvPublicURL             = "KURAKA_PUBLIC_URL"
	EnvSecretKey             = "KURAKA_SECRET_KEY"
	EnvStalwartURL           = "KURAKA_STALWART_URL"
	EnvStalwartAPIKey        = "KURAKA_STALWART_API_KEY"
	EnvStalwartMetricsSecret = "KURAKA_STALWART_METRICS_SECRET"
	EnvAlertWebhookURL       = "KURAKA_ALERT_WEBHOOK_URL"
	EnvAlertEmailFrom        = "KURAKA_ALERT_EMAIL_FROM"
	EnvInsecureCookies       = "KURAKA_INSECURE_COOKIES"
	EnvMailHost              = "KURAKA_MAIL_HOST"
	EnvMailTLSSkipVerify     = "KURAKA_MAIL_TLS_SKIP_VERIFY"
	EnvDNSResolver           = "KURAKA_DNS_RESOLVER"
	EnvMigrationWorkers      = "KURAKA_MIGRATION_WORKERS"
	EnvUpdatesDir            = "KURAKA_UPDATES_DIR"
)

// Defaults for optional variables.
const (
	DefaultListen           = ":8080"
	DefaultDBPath           = "kuraka.db"
	DefaultStalwartURL      = "http://stalwart:8080"
	DefaultDNSResolver      = "1.1.1.1:53"
	DefaultMigrationWorkers = 2
)

// Config is the validated runtime configuration.
type Config struct {
	Listen                string
	DBPath                string
	PublicURL             string // scheme://host[:port], no trailing slash
	SecretKeyHex          string // 64 hex chars
	StalwartURL           string // no trailing slash
	StalwartAPIKey        string
	StalwartMetricsSecret string
	AlertWebhookURL       string
	AlertEmailFrom        string // mailbox in a Kuraka domain; needs MailHost
	InsecureCookies       bool
	MailHost              string
	MailTLSSkipVerify     bool
	DNSResolver           string
	MigrationWorkers      int
	UpdatesDir            string // absolute; empty disables Libredesk updates
}

// Load reads the configuration from the process environment.
func Load() (Config, error) {
	return LoadFrom(os.Getenv)
}

// LoadFrom reads the configuration using getenv. All problems are reported
// together in one error. Error messages never contain secret values.
func LoadFrom(getenv func(string) string) (Config, error) {
	get := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
		return def
	}
	var errs []error
	c := Config{
		Listen: get(EnvListen, DefaultListen),
		DBPath: get(EnvDB, DefaultDBPath),
	}

	if raw := get(EnvPublicURL, ""); raw == "" {
		errs = append(errs, fmt.Errorf("%s is required", EnvPublicURL))
	} else if u, err := parseHTTPURL(raw); err != nil {
		errs = append(errs, fmt.Errorf("%s %w", EnvPublicURL, err))
	} else if u.Path != "" && u.Path != "/" {
		errs = append(errs, fmt.Errorf("%s must not have a path", EnvPublicURL))
	} else {
		c.PublicURL = u.Scheme + "://" + strings.ToLower(u.Host)
	}

	if key := get(EnvSecretKey, ""); key == "" {
		errs = append(errs, fmt.Errorf("%s is required", EnvSecretKey))
	} else if _, err := hex.DecodeString(key); err != nil || len(key) != 64 {
		errs = append(errs, fmt.Errorf("%s must be exactly 64 hex characters", EnvSecretKey))
	} else {
		c.SecretKeyHex = key
	}

	stalwart := get(EnvStalwartURL, DefaultStalwartURL)
	if _, err := parseHTTPURL(stalwart); err != nil {
		errs = append(errs, fmt.Errorf("%s %w", EnvStalwartURL, err))
	} else {
		c.StalwartURL = strings.TrimRight(stalwart, "/")
	}

	if c.StalwartAPIKey = get(EnvStalwartAPIKey, ""); c.StalwartAPIKey == "" {
		errs = append(errs, fmt.Errorf("%s is required", EnvStalwartAPIKey))
	}
	c.StalwartMetricsSecret = get(EnvStalwartMetricsSecret, "")
	c.AlertWebhookURL = get(EnvAlertWebhookURL, "")
	if c.AlertWebhookURL != "" {
		if _, err := parseHTTPURL(c.AlertWebhookURL); err != nil {
			errs = append(errs, fmt.Errorf("%s %w", EnvAlertWebhookURL, err))
		}
	}

	if raw := get(EnvInsecureCookies, ""); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be a boolean", EnvInsecureCookies))
		}
		c.InsecureCookies = b
	}

	c.MailHost = get(EnvMailHost, "")
	if c.MailHost != "" && !ValidHostname(c.MailHost) {
		errs = append(errs, fmt.Errorf("%s must be a hostname without scheme or port", EnvMailHost))
	}
	c.AlertEmailFrom = get(EnvAlertEmailFrom, "")
	if c.AlertEmailFrom != "" {
		if a, err := mail.ParseAddress(c.AlertEmailFrom); err != nil || a.Address != c.AlertEmailFrom {
			errs = append(errs, fmt.Errorf("%s must be an email address", EnvAlertEmailFrom))
		} else if c.MailHost == "" {
			errs = append(errs, fmt.Errorf("%s requires %s", EnvAlertEmailFrom, EnvMailHost))
		}
	}
	c.DNSResolver = get(EnvDNSResolver, DefaultDNSResolver)
	host, port, err := net.SplitHostPort(c.DNSResolver)
	p, portErr := strconv.Atoi(port)
	if err != nil || host == "" || (!ValidHostname(host) && net.ParseIP(host) == nil) || portErr != nil || p < 1 || p > 65535 {
		errs = append(errs, fmt.Errorf("%s must be host:port", EnvDNSResolver))
	}
	if raw := get(EnvMailTLSSkipVerify, "false"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be a boolean", EnvMailTLSSkipVerify))
		}
		c.MailTLSSkipVerify = b
	}
	c.MigrationWorkers, err = strconv.Atoi(get(EnvMigrationWorkers, "2"))
	if err != nil || c.MigrationWorkers < 1 || c.MigrationWorkers > 8 {
		errs = append(errs, fmt.Errorf("%s must be between 1 and 8", EnvMigrationWorkers))
	}
	c.UpdatesDir = get(EnvUpdatesDir, "")
	if c.UpdatesDir != "" && !filepath.IsAbs(c.UpdatesDir) {
		errs = append(errs, fmt.Errorf("%s must be an absolute path", EnvUpdatesDir))
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return c, nil
}

var hostLabel = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidHostname reports whether host is a DNS name of LDH labels.
func ValidHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// parseHTTPURL checks that raw is an absolute http(s) URL with a host and no
// credentials, query or fragment. Errors omit the value, which may carry
// credentials.
func parseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("must be an absolute http or https URL")
	}
	if u.Host == "" || u.Hostname() == "" {
		return nil, errors.New("must include a host")
	}
	if u.User != nil {
		return nil, errors.New("must not contain credentials")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("must not have a query or fragment")
	}
	return u, nil
}
