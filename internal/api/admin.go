package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/store"
)

func tenantJSON(t store.Tenant) map[string]any {
	return map[string]any{"id": t.ID, "name": t.Name, "created_at": t.CreatedAt}
}
func (s *Server) tenants(_ http.ResponseWriter, r *http.Request) (any, error) {
	rows, err := s.st.ListTenants(r.Context())
	out := make([]any, 0, len(rows))
	for _, t := range rows {
		out = append(out, tenantJSON(t))
	}
	return out, err
}
func tenantName(r *http.Request) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", invalid("name")
	}
	return name, nil
}
func (s *Server) createTenant(_ http.ResponseWriter, r *http.Request) (any, error) {
	name, err := tenantName(r)
	if err != nil {
		return nil, err
	}
	t, err := s.st.CreateTenant(r.Context(), name)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &t.ID
	return reply{201, tenantJSON(t)}, nil
}
func (s *Server) updateTenant(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &id
	name, err := tenantName(r)
	if err != nil {
		return nil, err
	}
	if err := s.st.RenameTenant(r.Context(), id, name); err != nil {
		return nil, err
	}
	t, err := s.st.GetTenant(r.Context(), id)
	return tenantJSON(t), err
}
func (s *Server) deleteTenant(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	state(r).tenant = &id
	return reply{204, nil}, s.st.DeleteTenant(r.Context(), id)
}
func (s *Server) users(_ http.ResponseWriter, r *http.Request) (any, error) {
	var tenant *int64
	if raw := r.URL.Query().Get("tenant_id"); raw != "" {
		id, err := positiveID(raw, "tenant_id")
		if err != nil {
			return nil, err
		}
		tenant = &id
	}
	rows, err := s.st.ListUsers(r.Context(), tenant)
	out := make([]any, 0, len(rows))
	for _, u := range rows {
		out = append(out, userJSON(u))
	}
	return out, err
}

// Check role/tenant shape before any database call, so invalid combinations
// cannot be confused with foreign-key or uniqueness conflicts.
func validRoleTenant(role string, tenant *int64) error {
	switch role {
	case authz.RoleSuperadmin:
		if tenant != nil {
			return invalid("tenant_id: superadmin must have no tenant")
		}
	case authz.RoleTenantAdmin:
		if tenant == nil || *tenant <= 0 {
			return invalid("tenant_id: tenant_admin requires a tenant")
		}
	default:
		return invalid("role")
	}
	return nil
}
func (s *Server) createUser(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Role     string `json:"role"`
		TenantID *int64 `json:"tenant_id"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := validRoleTenant(in.Role, in.TenantID); err != nil {
		return nil, err
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	if !validEmail(in.Email) {
		return nil, invalid("email")
	}
	if in.Name == "" {
		return nil, invalid("name")
	}
	state(r).tenant = in.TenantID
	if in.TenantID != nil {
		if _, err := s.st.GetTenant(r.Context(), *in.TenantID); err != nil {
			return nil, err
		}
	}
	generated := ""
	if in.Password == "" {
		var err error
		generated, err = generatedPassword()
		if err != nil {
			return nil, err
		}
		in.Password = generated
	}
	hash, err := passwordHash(in.Password)
	if err != nil {
		return nil, err
	}
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	u, err := s.st.CreateUser(r.Context(), store.User{Email: in.Email, Name: in.Name, Role: in.Role, TenantID: in.TenantID, PasswordHash: hash})
	if err != nil {
		return nil, err
	}
	out := userJSON(u)
	if generated != "" {
		out["generated_password"] = generated
	}
	return reply{201, out}, nil
}

func (s *Server) updateUser(_ http.ResponseWriter, r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Name          *string         `json:"name"`
		Role          *string         `json:"role"`
		TenantID      json.RawMessage `json:"tenant_id"`
		Disabled      *bool           `json:"disabled"`
		ResetPassword *bool           `json:"reset_password"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, invalid("name")
		}
		in.Name = &name
	}
	var newTenant *int64
	if in.TenantID != nil && !bytes.Equal(bytes.TrimSpace(in.TenantID), []byte("null")) {
		var n int64
		if err := json.Unmarshal(in.TenantID, &n); err != nil || n <= 0 {
			return nil, invalid("tenant_id")
		}
		newTenant = &n
	}
	if in.Role != nil && *in.Role != authz.RoleSuperadmin && *in.Role != authz.RoleTenantAdmin {
		return nil, invalid("role")
	}
	if in.Role != nil && in.TenantID != nil {
		if err := validRoleTenant(*in.Role, newTenant); err != nil {
			return nil, err
		}
	}
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	u, err := s.st.GetUser(r.Context(), id)
	if err != nil {
		return nil, err
	}
	next := u
	p := store.UserPatch{Name: in.Name, Role: in.Role, Disabled: in.Disabled}
	if in.Role != nil {
		next.Role = *in.Role
	}
	if in.TenantID != nil {
		next.TenantID = newTenant
		p.TenantID = &newTenant
	}
	if in.Disabled != nil {
		next.Disabled = *in.Disabled
	}
	if err := validRoleTenant(next.Role, next.TenantID); err != nil {
		return nil, err
	}
	state(r).tenant = u.TenantID
	if u.Role == authz.RoleSuperadmin && !u.Disabled && (next.Role != authz.RoleSuperadmin || next.Disabled) {
		users, err := s.st.ListUsers(r.Context(), nil)
		if err != nil {
			return nil, err
		}
		enabled := 0
		for _, candidate := range users {
			if candidate.Role == authz.RoleSuperadmin && !candidate.Disabled {
				enabled++
			}
		}
		if enabled <= 1 {
			return nil, conflict("Cannot remove the last enabled superadmin")
		}
	}
	if next.TenantID != nil {
		if _, err := s.st.GetTenant(r.Context(), *next.TenantID); err != nil {
			return nil, err
		}
	}
	generated := ""
	if in.ResetPassword != nil && *in.ResetPassword {
		generated, err = generatedPassword()
		if err != nil {
			return nil, err
		}
		hash, err := passwordHash(generated)
		if err != nil {
			return nil, err
		}
		p.PasswordHash = &hash
	}
	if err := s.st.UpdateUser(r.Context(), id, p); err != nil {
		return nil, err
	}
	// Tenant transfers also revoke sessions, alongside role changes, disabling,
	// and password resets. This avoids carrying stale privileges across tenants.
	changedTenant := (u.TenantID == nil) != (next.TenantID == nil) || (u.TenantID != nil && next.TenantID != nil && *u.TenantID != *next.TenantID)
	if next.Disabled || next.Role != u.Role || changedTenant || generated != "" {
		if err := s.st.DeleteUserSessions(r.Context(), id); err != nil {
			return nil, err
		}
	}
	u, err = s.st.GetUser(r.Context(), id)
	if err != nil {
		return nil, err
	}
	out := userJSON(u)
	if generated != "" {
		out["generated_password"] = generated
	}
	return out, nil
}
