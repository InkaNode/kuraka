package stalwart

import "context"

const objBlockedIP = "x:BlockedIp"

type BlockedIP struct {
	ID        string  `json:"id"`
	Address   string  `json:"address"`
	Reason    string  `json:"reason"`
	CreatedAt string  `json:"createdAt"`
	ExpiresAt *string `json:"expiresAt"`
}

func (c *Client) ListBlockedIPs(ctx context.Context) ([]BlockedIP, error) {
	resps, err := c.call(ctx, []invocation{{objBlockedIP + "/get", map[string]any{}, "g"}})
	if err != nil {
		return nil, err
	}
	var g getResult[BlockedIP]
	if err := result(resps, "g", &g); err != nil {
		return nil, err
	}
	return g.List, nil
}

// UnblockIP removes the ban, trusts the network when trust is set, and
// reloads the bans Stalwart caches in memory, all in one request.
func (c *Client) UnblockIP(ctx context.Context, id, trust string) error {
	calls := []invocation{{objBlockedIP + "/set", map[string]any{"destroy": []string{id}}, "d"}}
	if trust != "" {
		calls = append(calls, invocation{"x:AllowedIp/set", map[string]any{"create": map[string]any{"a": map[string]any{"address": trust}}}, "a"})
	}
	calls = append(calls, invocation{"x:Action/set", map[string]any{"create": map[string]any{"r": map[string]any{"@type": "ReloadBlockedIps"}}}, "r"})
	resps, err := c.call(ctx, calls)
	if err != nil {
		return err
	}
	var d setResult
	if err := result(resps, "d", &d); err != nil {
		return err
	}
	if e := d.NotDestroyed[id]; e != nil {
		return e
	}
	if trust != "" {
		var a setResult
		if err := result(resps, "a", &a); err != nil {
			return err
		}
		if e := a.NotCreated["a"]; e != nil {
			return e
		}
	}
	var r setResult
	if err := result(resps, "r", &r); err != nil {
		return err
	}
	if e := r.NotCreated["r"]; e != nil {
		return e
	}
	return nil
}
