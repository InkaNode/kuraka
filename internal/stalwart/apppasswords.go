package stalwart

import (
	"context"
	"errors"
)

const objAppPassword = "x:AppPassword"

// ServiceKeyDisabled lists the permissions the Kuraka service API key is
// created without. Stalwart refuses to grant a permission the caller lacks,
// so app passwords Kuraka creates must disable the same set.
var ServiceKeyDisabled = []string{
	"sysDomainDestroy",
	"sysNetworkListenerCreate", "sysNetworkListenerUpdate", "sysNetworkListenerDestroy",
	"sysRoleCreate", "sysRoleUpdate", "sysRoleDestroy",
}

func disabledPermissions() map[string]any {
	set := make(map[string]bool, len(ServiceKeyDisabled))
	for _, p := range ServiceKeyDisabled {
		set[p] = true
	}
	return map[string]any{"@type": "Disable", "permissions": set}
}

// CreateAppPassword creates a revocable password for one mailbox. The secret
// is returned only here; Stalwart never shows it again.
func (c *Client) CreateAppPassword(ctx context.Context, accountID, description string) (id, secret string, err error) {
	r, err := c.set(ctx, objAppPassword, map[string]any{
		"accountId": accountID,
		"create":    map[string]any{"n": map[string]any{"description": description, "permissions": disabledPermissions()}},
	})
	if err != nil {
		return "", "", err
	}
	if e := r.NotCreated["n"]; e != nil {
		return "", "", e
	}
	cr, ok := r.Created["n"]
	if !ok || cr.ID == "" || cr.Secret == "" {
		return "", "", errors.New("stalwart: app password create returned no secret")
	}
	return cr.ID, cr.Secret, nil
}

// DeleteAppPassword revokes an app password of a mailbox.
func (c *Client) DeleteAppPassword(ctx context.Context, accountID, id string) error {
	r, err := c.set(ctx, objAppPassword, map[string]any{"accountId": accountID, "destroy": []string{id}})
	if err != nil {
		return err
	}
	if e := r.NotDestroyed[id]; e != nil {
		return e
	}
	return nil
}
