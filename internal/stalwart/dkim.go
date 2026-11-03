package stalwart

import "context"

const objDkim = "x:DkimSignature"

// ListDkim returns the DKIM keys of a domain.
func (c *Client) ListDkim(ctx context.Context, domainID string) ([]DkimKey, error) {
	ws, err := queryGet[wireDkim](ctx, c, objDkim, nil)
	if err != nil {
		return nil, err
	}
	out := []DkimKey{}
	for _, w := range ws {
		if w.DomainID == domainID {
			out = append(out, w.key())
		}
	}
	return out, nil
}
