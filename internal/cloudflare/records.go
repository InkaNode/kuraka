package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const perPage = 100

// Record is a DNS record. Data is used for SRV only; Priority for MX only.
type Record struct {
	ID       string
	Type     string
	Name     string
	Content  string
	Priority int
	TTL      int
	Proxied  bool
	Comment  string
	Data     *SRVData
}

// SRVData is the structured content of an SRV record.
type SRVData struct {
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	Port     int    `json:"port"`
	Target   string `json:"target"`
}

type wireRecord struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type"`
	Name     string          `json:"name"`
	Content  string          `json:"content,omitempty"`
	Priority *int            `json:"priority,omitempty"`
	TTL      int             `json:"ttl,omitempty"`
	Proxied  *bool           `json:"proxied,omitempty"`
	Comment  *string         `json:"comment,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// MarshalJSON emits the request shape Cloudflare expects per record type:
// SRV uses data, MX carries priority, and proxied is only sent for types
// that support the proxy.
func (r Record) MarshalJSON() ([]byte, error) {
	w := wireRecord{ID: r.ID, Type: r.Type, Name: r.Name, TTL: r.TTL}
	if w.TTL == 0 {
		w.TTL = 1
	}
	switch r.Type {
	case "SRV":
		if r.Data != nil {
			b, err := json.Marshal(r.Data)
			if err != nil {
				return nil, err
			}
			w.Data = b
		}
	case "MX":
		p := r.Priority
		w.Priority = &p
		w.Content = r.Content
	case "A", "AAAA", "CNAME":
		p := r.Proxied
		w.Proxied = &p
		w.Content = r.Content
	default:
		w.Content = r.Content
	}
	if r.Comment != "" {
		cm := r.Comment
		w.Comment = &cm
	}
	return json.Marshal(w)
}

// UnmarshalJSON reads a record as returned by Cloudflare.
func (r *Record) UnmarshalJSON(b []byte) error {
	var w wireRecord
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*r = Record{ID: w.ID, Type: w.Type, Name: w.Name, Content: w.Content, TTL: w.TTL}
	if w.Priority != nil {
		r.Priority = *w.Priority
	}
	if w.Proxied != nil {
		r.Proxied = *w.Proxied
	}
	if w.Comment != nil {
		r.Comment = *w.Comment
	}
	if w.Type == "SRV" && len(w.Data) > 0 && string(w.Data) != "null" {
		var d SRVData
		if err := json.Unmarshal(w.Data, &d); err != nil {
			return err
		}
		r.Data = &d
	}
	return nil
}

func recordsPath(zoneID string) string {
	return "/zones/" + url.PathEscape(zoneID) + "/dns_records"
}

// ListRecords returns every DNS record in the zone.
func (c *Client) ListRecords(ctx context.Context, zoneID string) ([]Record, error) {
	var all []Record
	for page := 1; ; page++ {
		var recs []Record
		path := recordsPath(zoneID) + "?page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(perPage)
		env, err := c.do(ctx, http.MethodGet, path, nil, &recs)
		if err != nil {
			return nil, err
		}
		all = append(all, recs...)
		if env.ResultInfo == nil || page >= env.ResultInfo.TotalPages || len(recs) == 0 {
			return all, nil
		}
	}
}

// CreateRecord creates a DNS record and returns it as stored.
func (c *Client) CreateRecord(ctx context.Context, zoneID string, r Record) (Record, error) {
	r.ID = ""
	var out Record
	if _, err := c.do(ctx, http.MethodPost, recordsPath(zoneID), r, &out); err != nil {
		return Record{}, err
	}
	return out, nil
}

// UpdateRecord patches the DNS record id and returns it as stored.
func (c *Client) UpdateRecord(ctx context.Context, zoneID, id string, r Record) (Record, error) {
	if id == "" {
		return Record{}, fmt.Errorf("cloudflare: update record: empty id")
	}
	r.ID = ""
	var out Record
	if _, err := c.do(ctx, http.MethodPatch, recordsPath(zoneID)+"/"+url.PathEscape(id), r, &out); err != nil {
		return Record{}, err
	}
	return out, nil
}

// DeleteRecord deletes the DNS record id.
func (c *Client) DeleteRecord(ctx context.Context, zoneID, id string) error {
	if id == "" {
		return fmt.Errorf("cloudflare: delete record: empty id")
	}
	_, err := c.do(ctx, http.MethodDelete, recordsPath(zoneID)+"/"+url.PathEscape(id), nil, nil)
	return err
}
