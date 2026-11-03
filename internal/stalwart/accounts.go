package stalwart

import (
	"context"
	"errors"
	"fmt"
)

const objAccount = "x:Account"

// ListAccounts returns the users and groups of a domain.
func (c *Client) ListAccounts(ctx context.Context, domainID string) ([]Account, error) {
	ws, err := queryGet[wireAccount](ctx, c, objAccount, map[string]any{"domainId": domainID})
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.account())
	}
	return out, nil
}

// GetAccount returns the account with the given id, or ErrNotFound.
func (c *Client) GetAccount(ctx context.Context, id string) (Account, error) {
	w, err := getOne[wireAccount](ctx, c, objAccount, id)
	if err != nil {
		return Account{}, err
	}
	return w.account(), nil
}

// CreateAccount creates a User (with a password) or a Group and returns its id.
func (c *Client) CreateAccount(ctx context.Context, a NewAccount) (string, error) {
	kind := a.Kind
	if kind == "" {
		kind = KindUser
	}
	obj := map[string]any{
		"@type":    kind,
		"name":     a.Name,
		"domainId": a.DomainID,
	}
	switch kind {
	case KindUser:
		if a.Password == "" {
			return "", errors.New("stalwart: password required for User")
		}
		obj["credentials"] = passwordCredentials(a.Password)
	case KindGroup:
	default:
		return "", fmt.Errorf("stalwart: unknown account kind %q", kind)
	}
	if a.Description != "" {
		obj["description"] = a.Description
	}
	if a.QuotaBytes > 0 {
		obj["quotas"] = map[string]any{"maxDiskQuota": a.QuotaBytes}
	}
	if len(a.Aliases) > 0 {
		obj["aliases"] = toWireAliases(a.Aliases)
	}
	return c.create(ctx, objAccount, obj)
}

// UpdateAccount applies the non-nil fields of p.
func (c *Client) UpdateAccount(ctx context.Context, id string, p AccountPatch) error {
	patch := map[string]any{}
	if p.Description != nil {
		patch["description"] = *p.Description
	}
	if p.Password != nil {
		patch["credentials"] = passwordCredentials(*p.Password)
	}
	if p.QuotaBytes != nil {
		if *p.QuotaBytes > 0 {
			patch["quotas"] = map[string]any{"maxDiskQuota": *p.QuotaBytes}
		} else {
			patch["quotas"] = map[string]any{}
		}
	}
	if p.Aliases != nil {
		patch["aliases"] = toWireAliases(*p.Aliases)
	}
	if len(patch) == 0 {
		return nil
	}
	return c.update(ctx, objAccount, id, patch)
}

// DeleteAccount destroys an account.
func (c *Client) DeleteAccount(ctx context.Context, id string) error {
	return c.destroy(ctx, objAccount, id)
}

func passwordCredentials(pw string) list[map[string]any] {
	return list[map[string]any]{{"@type": "Password", "secret": pw}}
}
