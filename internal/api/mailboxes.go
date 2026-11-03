package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/InkaNode/kuraka/internal/auth"
	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

var localPartPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)

const mebibyte uint64 = 1024 * 1024

type aliasInput struct {
	Name     string `json:"name"`
	DomainID int64  `json:"domain_id"`
}

func localPart(raw, field string) (string, error) {
	name := strings.ToLower(raw)
	if !localPartPattern.MatchString(name) {
		return "", invalid(field)
	}
	return name, nil
}
func quotaBytes(mb uint64) (uint64, error) {
	if mb > ^uint64(0)/mebibyte {
		return 0, invalid("quota_mb")
	}
	return mb * mebibyte, nil
}
func (s *Server) mailboxAliases(r *http.Request, tenant int64, input []aliasInput) ([]stalwart.Alias, error) {
	aliases := make([]stalwart.Alias, 0, len(input))
	seen := map[aliasInput]bool{}
	for _, a := range input {
		name, err := localPart(a.Name, "aliases.name")
		if err != nil {
			return nil, err
		}
		a.Name = name
		if seen[a] {
			return nil, invalid("aliases: duplicate address")
		}
		seen[a] = true
		d, err := s.st.GetDomain(r.Context(), a.DomainID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, authz.ErrForeignAlias
		}
		if err != nil {
			return nil, err
		}
		if d.TenantID != tenant {
			return nil, authz.ErrForeignAlias
		}
		aliases = append(aliases, stalwart.Alias{Name: name, DomainID: d.StalwartID, Enable: true})
	}
	if err := s.az.CheckAliases(r.Context(), tenant, aliases); err != nil {
		return nil, err
	}
	return aliases, nil
}
func (s *Server) mailboxJSON(r *http.Request, d store.Domain, a stalwart.Account) (map[string]any, error) {
	aliases := make([]any, 0, len(a.Aliases))
	for _, alias := range a.Aliases {
		target, err := s.st.GetDomainByStalwartID(r.Context(), alias.DomainID)
		// Out-of-band foreign aliases must never expose other tenants' identifiers.
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if target.TenantID != d.TenantID {
			continue
		}
		aliases = append(aliases, map[string]any{"name": alias.Name, "domain_id": target.ID, "email": alias.Name + "@" + target.Name})
	}
	kind := "user"
	if a.Kind == stalwart.KindGroup {
		kind = "shared"
	}
	email := a.Email
	if email == "" {
		email = a.Name + "@" + d.Name
	}
	return map[string]any{"id": a.ID, "kind": kind, "name": a.Name, "email": email, "display_name": a.Description, "aliases": aliases, "quota_mb": a.QuotaBytes / mebibyte, "used_mb": float64(a.UsedBytes) / float64(mebibyte)}, nil
}
func (s *Server) mailboxes(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	rows, err := s.sw.ListAccounts(r.Context(), d.StalwartID)
	if err != nil {
		return nil, upstream(err)
	}
	out := make([]any, 0, len(rows))
	for _, a := range rows {
		if a.DomainID != d.StalwartID || (a.Kind != stalwart.KindUser && a.Kind != stalwart.KindGroup) {
			continue
		}
		v, err := s.mailboxJSON(r, d, a)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Server) authorizedMailbox(r *http.Request) (store.Domain, stalwart.Account, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return store.Domain{}, stalwart.Account{}, err
	}
	d, a, err := s.az.Mailbox(r.Context(), state(r).principal, d.ID, r.PathValue("mid"))
	if err != nil && !errors.Is(err, authz.ErrNotFound) {
		return d, a, upstream(err)
	}
	return d, a, err
}
func (s *Server) mailbox(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, a, err := s.authorizedMailbox(r)
	if err != nil {
		return nil, err
	}
	return s.mailboxJSON(r, d, a)
}
func (s *Server) createMailbox(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Name        string       `json:"name"`
		DisplayName string       `json:"display_name"`
		Password    string       `json:"password"`
		QuotaMB     uint64       `json:"quota_mb"`
		Aliases     []aliasInput `json:"aliases"`
		Shared      bool         `json:"shared"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	name, err := localPart(in.Name, "name")
	if err != nil {
		return nil, err
	}
	quota, err := quotaBytes(in.QuotaMB)
	if err != nil {
		return nil, err
	}
	aliases, err := s.mailboxAliases(r, d.TenantID, in.Aliases)
	if err != nil {
		return nil, err
	}
	generated := ""
	kind := stalwart.KindUser
	if in.Shared {
		kind = stalwart.KindGroup
	}
	if !in.Shared && in.Password == "" {
		generated, err = generatedPassword()
		if err != nil {
			return nil, err
		}
		in.Password = generated
	}
	if in.Password != "" {
		if err := auth.ValidatePassword(in.Password); err != nil {
			return nil, invalid("password: " + err.Error())
		}
	}
	id, err := s.sw.CreateAccount(r.Context(), stalwart.NewAccount{Kind: kind, Name: name, DomainID: d.StalwartID, Description: in.DisplayName, Password: in.Password, QuotaBytes: quota, Aliases: aliases})
	if err != nil {
		return nil, upstream(err)
	}
	out, err := s.mailboxJSON(r, d, stalwart.Account{ID: id, Kind: kind, Name: name, DomainID: d.StalwartID, Description: in.DisplayName, QuotaBytes: quota, Aliases: aliases})
	if err != nil {
		return nil, err
	}
	if generated != "" {
		out["generated_password"] = generated
	}
	return reply{201, out}, nil
}
func (s *Server) updateMailbox(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, a, err := s.authorizedMailbox(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		DisplayName *string       `json:"display_name"`
		Password    *string       `json:"password"`
		QuotaMB     *uint64       `json:"quota_mb"`
		Aliases     *[]aliasInput `json:"aliases"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	p := stalwart.AccountPatch{Description: in.DisplayName, Password: in.Password}
	generated := ""
	if in.Password != nil {
		if *in.Password == "" && a.Kind != stalwart.KindGroup {
			generated, err = generatedPassword()
			if err != nil {
				return nil, err
			}
			in.Password = &generated
			p.Password = &generated
		}
		if err := auth.ValidatePassword(*in.Password); err != nil {
			return nil, invalid("password: " + err.Error())
		}
	}
	if in.QuotaMB != nil {
		quota, err := quotaBytes(*in.QuotaMB)
		if err != nil {
			return nil, err
		}
		p.QuotaBytes = &quota
		a.QuotaBytes = quota
	}
	if in.Aliases != nil {
		aliases, err := s.mailboxAliases(r, d.TenantID, *in.Aliases)
		if err != nil {
			return nil, err
		}
		p.Aliases = &aliases
		a.Aliases = aliases
	}
	if in.DisplayName != nil {
		a.Description = *in.DisplayName
	}
	if err := s.sw.UpdateAccount(r.Context(), a.ID, p); err != nil {
		return nil, upstream(err)
	}
	out, err := s.mailboxJSON(r, d, a)
	if err != nil {
		return nil, err
	}
	if generated != "" {
		out["generated_password"] = generated
	}
	return out, nil
}
func (s *Server) deleteMailbox(_ http.ResponseWriter, r *http.Request) (any, error) {
	_, a, err := s.authorizedMailbox(r)
	if err != nil {
		return nil, err
	}
	return reply{204, nil}, upstream(s.sw.DeleteAccount(r.Context(), a.ID))
}
