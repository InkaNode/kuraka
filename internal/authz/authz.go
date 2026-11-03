// Package authz is the single place that decides whether a principal may
// touch a domain or a mailbox. Every tenant-scoped handler goes through it.
package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

// Roles a Kuraka user can hold.
const (
	RoleSuperadmin  = "superadmin"
	RoleTenantAdmin = "tenant_admin"
)

var (
	// ErrNotFound hides resources outside the principal's tenant, so their
	// IDs cannot be probed.
	ErrNotFound = errors.New("authz: not found")
	// ErrForbidden is returned for actions the role may never perform.
	ErrForbidden = errors.New("authz: forbidden")
	// ErrForeignAlias is returned when an alias points to a domain owned by
	// another tenant, or to a domain Kuraka does not manage.
	ErrForeignAlias = errors.New("authz: alias domain not owned by tenant")
)

// Principal is the authenticated caller.
type Principal struct {
	UserID   int64
	TenantID *int64
	Role     string
}

// IsSuperadmin reports whether the principal has global access.
func (p Principal) IsSuperadmin() bool { return p.Role == RoleSuperadmin }

// Domains is the subset of the store authz needs.
type Domains interface {
	GetDomain(ctx context.Context, id int64) (store.Domain, error)
	GetDomainByStalwartID(ctx context.Context, sid string) (store.Domain, error)
}

// Accounts is the subset of the Stalwart client authz needs.
type Accounts interface {
	GetAccount(ctx context.Context, id string) (stalwart.Account, error)
}

// Authz evaluates access rules.
type Authz struct {
	domains  Domains
	accounts Accounts
}

// New returns an Authz backed by the given store and Stalwart client.
func New(domains Domains, accounts Accounts) *Authz {
	return &Authz{domains: domains, accounts: accounts}
}

// RequireSuperadmin fails unless the principal is a superadmin.
func RequireSuperadmin(p Principal) error {
	if !p.IsSuperadmin() {
		return ErrForbidden
	}
	return nil
}

// TenantScope returns the tenant filter for list queries: nil for
// superadmins (everything), the principal's tenant otherwise.
func TenantScope(p Principal) *int64 {
	if p.IsSuperadmin() {
		return nil
	}
	return p.TenantID
}

// Domain loads a domain the principal may access.
func (a *Authz) Domain(ctx context.Context, p Principal, id int64) (store.Domain, error) {
	d, err := a.domains.GetDomain(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Domain{}, ErrNotFound
	}
	if err != nil {
		return store.Domain{}, err
	}
	if !owns(p, d.TenantID) {
		return store.Domain{}, ErrNotFound
	}
	return d, nil
}

// Mailbox loads a mailbox that belongs to the given domain, after checking
// the principal may access that domain.
func (a *Authz) Mailbox(ctx context.Context, p Principal, domainID int64, accountID string) (store.Domain, stalwart.Account, error) {
	d, err := a.Domain(ctx, p, domainID)
	if err != nil {
		return store.Domain{}, stalwart.Account{}, err
	}
	acc, err := a.accounts.GetAccount(ctx, accountID)
	if errors.Is(err, stalwart.ErrNotFound) {
		return store.Domain{}, stalwart.Account{}, ErrNotFound
	}
	if err != nil {
		return store.Domain{}, stalwart.Account{}, err
	}
	if acc.DomainID != d.StalwartID {
		return store.Domain{}, stalwart.Account{}, ErrNotFound
	}
	return d, acc, nil
}

// CheckAliases verifies that every alias targets a domain owned by tenantID.
// tenantID is the owner of the mailbox, not the caller, so superadmins
// cannot accidentally mix tenants either.
func (a *Authz) CheckAliases(ctx context.Context, tenantID int64, aliases []stalwart.Alias) error {
	seen := map[string]bool{}
	for _, al := range aliases {
		if seen[al.DomainID] {
			continue
		}
		d, err := a.domains.GetDomainByStalwartID(ctx, al.DomainID)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: %s@%s", ErrForeignAlias, al.Name, al.DomainID)
		}
		if err != nil {
			return err
		}
		if d.TenantID != tenantID {
			return fmt.Errorf("%w: %s@%s", ErrForeignAlias, al.Name, al.DomainID)
		}
		seen[al.DomainID] = true
	}
	return nil
}

func owns(p Principal, tenantID int64) bool {
	if p.IsSuperadmin() {
		return true
	}
	return p.Role == RoleTenantAdmin && p.TenantID != nil && *p.TenantID == tenantID
}
