package stalwart

import (
	"context"
	"strings"
)

const objDomain = "x:Domain"

// ListDomains returns every domain.
func (c *Client) ListDomains(ctx context.Context) ([]Domain, error) {
	ws, err := queryGet[wireDomain](ctx, c, objDomain, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.domain())
	}
	return out, nil
}

// GetDomain returns the domain with the given id, or ErrNotFound.
func (c *Client) GetDomain(ctx context.Context, id string) (Domain, error) {
	w, err := getOne[wireDomain](ctx, c, objDomain, id)
	if err != nil {
		return Domain{}, err
	}
	return w.domain(), nil
}

// FindDomainByName returns the domain whose name matches exactly,
// ignoring case, or ErrNotFound.
func (c *Client) FindDomainByName(ctx context.Context, name string) (Domain, error) {
	ws, err := queryGet[wireDomain](ctx, c, objDomain, map[string]any{"name": name})
	if err != nil {
		return Domain{}, err
	}
	want := strings.TrimSuffix(name, ".")
	for _, w := range ws {
		if strings.EqualFold(w.Name, want) {
			return w.domain(), nil
		}
	}
	return Domain{}, ErrNotFound
}

// CreateDomain creates a domain and returns its id.
func (c *Client) CreateDomain(ctx context.Context, name, description string, aliases []string) (string, error) {
	obj := map[string]any{"name": name}
	if description != "" {
		obj["description"] = description
	}
	if len(aliases) > 0 {
		obj["aliases"] = stringSet(aliases)
	}
	return c.create(ctx, objDomain, obj)
}

// SetDomainAliases replaces the alias domains of a domain.
func (c *Client) SetDomainAliases(ctx context.Context, id string, aliases []string) error {
	return c.update(ctx, objDomain, id, map[string]any{"aliases": stringSet(aliases)})
}
