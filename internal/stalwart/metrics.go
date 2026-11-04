package stalwart

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const MetricsUser = "kuraka-metrics"

var metricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

var MetricNames = []string{
	"stalwart_queue_messages",
	"stalwart_queue_message_queued_total",
	"stalwart_delivery_delivered_total",
	"stalwart_delivery_dsn_perm_fail_total",
	"stalwart_auth_failed_total",
	"stalwart_smtp_connection_start_total",
	"stalwart_smtp_active_connections",
}

func (c *Client) Metrics(ctx context.Context, user, secret string) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/metrics/prometheus", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, secret)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stalwart: metrics: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("stalwart: read metrics: %w", err)
	}
	if len(data) > maxBody {
		return nil, fmt.Errorf("stalwart: metrics response exceeds limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, newHTTPError(resp.StatusCode, data)
	}
	return parseMetrics(string(data)), nil
}

func parseMetrics(data string) map[string]float64 {
	out := map[string]float64{}
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexAny(line, "{ \t")
		if i <= 0 {
			continue
		}
		name, rest := line[:i], line[i:]
		if !metricName.MatchString(name) {
			continue
		}
		if rest[0] == '{' {
			// Quoted labels may contain spaces, braces and escaped quotes.
			quoted, escaped, end := false, false, -1
			for j := 1; j < len(rest); j++ {
				ch := rest[j]
				if escaped {
					escaped = false
					continue
				}
				if quoted && ch == '\\' {
					escaped = true
					continue
				}
				if ch == '"' {
					quoted = !quoted
				}
				if ch == '}' && !quoted {
					end = j
					break
				}
			}
			if end < 0 || end+1 >= len(rest) || (rest[end+1] != ' ' && rest[end+1] != '\t') {
				continue
			}
			rest = rest[end+1:]
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 || len(fields) > 2 {
			continue
		}
		if len(fields) == 2 {
			if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
				continue
			}
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		if sum := out[name] + value; !math.IsNaN(sum) && !math.IsInf(sum, 0) {
			out[name] = sum
		}
	}
	return out
}
