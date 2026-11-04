package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/InkaNode/kuraka/internal/auth"
	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/store"
)

const cookieName = "kuraka_session"

func userJSON(u store.User) map[string]any {
	return map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role, "tenant_id": u.TenantID, "totp_enabled": u.TOTPEnabled, "disabled": u.Disabled, "created_at": u.CreatedAt, "last_login_at": u.LastLoginAt}
}
func validEmail(email string) bool {
	a, err := mail.ParseAddress(email)
	return err == nil && a.Address == email && strings.Contains(email, "@") && len(email) <= 254
}
func generatedPassword() (string, error) {
	t, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	return t[:20], nil
}
func passwordHash(password string) (string, error) {
	if err := auth.ValidatePassword(password); err != nil {
		return "", invalid("password: " + err.Error())
	}
	return auth.HashPassword(password)
}

func (s *Server) cookie(w http.ResponseWriter, value string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: value, Path: "/", HttpOnly: true, Secure: !s.cfg.InsecureCookies, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge})
}
func (s *Server) newSession(w http.ResponseWriter, r *http.Request, u store.User, pending bool) error {
	tok, err := auth.NewToken()
	if err != nil {
		return err
	}
	now := s.now()
	expires := now.Add(12 * time.Hour)
	err = s.st.CreateSession(r.Context(), store.Session{ID: auth.HashToken(tok), UserID: u.ID, MFAPending: pending, CreatedAt: now, ExpiresAt: expires, IP: clientIP(r), UserAgent: r.UserAgent()})
	if err == nil {
		s.cookie(w, tok, expires, 12*60*60)
	}
	return err
}
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return unauthorized
	}
	sess, err := s.st.GetSession(r.Context(), auth.HashToken(c.Value), s.now())
	if errors.Is(err, store.ErrNotFound) {
		s.cookie(w, "", time.Unix(1, 0), -1)
		return unauthorized
	}
	if err != nil {
		return err
	}
	u, err := s.st.GetUser(r.Context(), sess.UserID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	q := state(r)
	q.user = u
	if err != nil || u.Disabled {
		if err := s.st.DeleteSession(r.Context(), sess.ID); err != nil {
			return err
		}
		s.cookie(w, "", time.Unix(1, 0), -1)
		return unauthorized
	}
	q.session = sess
	q.principal = authz.Principal{UserID: u.ID, TenantID: u.TenantID, Role: u.Role}
	if sess.MFAPending && !(r.Method == "POST" && (r.URL.Path == "/api/v1/auth/totp" || r.URL.Path == "/api/v1/auth/logout")) {
		return unauthorized
	}
	return nil
}

func (s *Server) setup(_ http.ResponseWriter, r *http.Request) (any, error) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	n, err := s.st.CountSuperadmins(r.Context())
	if err != nil {
		return nil, err
	}
	if n != 0 || s.setupToken == "" {
		return nil, conflict("Setup has already been completed")
	}
	var in struct {
		Token    string `json:"token"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	// Compare fixed-size digests to avoid leaking the token's length or prefix.
	if subtle.ConstantTimeCompare([]byte(auth.HashToken(in.Token)), []byte(auth.HashToken(s.setupToken))) != 1 {
		return nil, &apiError{403, "forbidden", "Invalid setup token"}
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	if !validEmail(in.Email) {
		return nil, invalid("email")
	}
	if in.Name == "" {
		return nil, invalid("name")
	}
	hash, err := passwordHash(in.Password)
	if err != nil {
		return nil, err
	}
	u, err := s.st.CreateUser(r.Context(), store.User{Email: in.Email, Name: in.Name, Role: authz.RoleSuperadmin, PasswordHash: hash})
	if err != nil {
		return nil, err
	}
	s.setupToken = ""
	state(r).user = u
	return reply{201, userJSON(u)}, nil
}

func loginKey(r *http.Request, email string) string {
	return clientIP(r) + "\x00" + strings.ToLower(strings.TrimSpace(email))
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	key := loginKey(r, in.Email)
	if s.limiter.blocked(key, s.now()) {
		return nil, rateLimited
	}
	u, err := s.st.GetUserByEmail(r.Context(), in.Email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	ok := false
	if err != nil || u.Disabled {
		auth.Burn(in.Password)
	} else {
		ok, err = auth.VerifyPassword(in.Password, u.PasswordHash)
		if err != nil {
			s.log.Error("password verification failed", "user_id", u.ID, "error", err)
		}
	}
	if u.ID != 0 {
		state(r).user = u
	}
	if !ok {
		if s.limiter.fail(key, s.now()) {
			return nil, rateLimited
		}
		return nil, invalidCredentials
	}
	if err := s.st.TouchLogin(r.Context(), u.ID, s.now()); err != nil {
		return nil, err
	}
	if err := s.newSession(w, r, u, u.TOTPEnabled); err != nil {
		return nil, err
	}
	out := map[string]any{"mfa_required": u.TOTPEnabled}
	if !u.TOTPEnabled {
		s.limiter.clear(key)
		out["user"] = userJSON(u)
	}
	return out, nil
}

func (s *Server) totp(_ http.ResponseWriter, r *http.Request) (any, error) {
	q := state(r)
	if !q.session.MFAPending || !q.user.TOTPEnabled {
		return nil, unauthorized
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := s.checkTOTP(r, in.Code); err != nil {
		return nil, err
	}
	if err := s.st.CompleteMFA(r.Context(), q.session.ID); err != nil {
		return nil, err
	}
	return map[string]any{"mfa_required": false, "user": userJSON(q.user)}, nil
}
func (s *Server) checkTOTP(r *http.Request, code string) error {
	u := state(r).user
	key := loginKey(r, u.Email)
	if s.limiter.blocked(key, s.now()) {
		return rateLimited
	}
	if u.TOTPSecretEnc == "" {
		return invalidCredentials
	}
	secret, err := s.box.Open(u.TOTPSecretEnc)
	if err != nil {
		return err
	}
	if !auth.VerifyTOTP(string(secret), code, s.now()) {
		if s.limiter.fail(key, s.now()) {
			return rateLimited
		}
		return invalidCredentials
	}
	s.limiter.clear(key)
	return nil
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := s.st.DeleteSession(r.Context(), state(r).session.ID); err != nil {
		return nil, err
	}
	s.cookie(w, "", time.Unix(1, 0), -1)
	return reply{204, nil}, nil
}
func (s *Server) me(_ http.ResponseWriter, r *http.Request) (any, error) {
	return userJSON(state(r).user), nil
}
func (s *Server) password(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	u := state(r).user
	ok, err := auth.VerifyPassword(in.Current, u.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, invalidCredentials
	}
	if err := auth.ValidatePassword(in.New); err != nil {
		return nil, invalid("new: " + err.Error())
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		return nil, err
	}
	if err := s.st.UpdateUser(r.Context(), u.ID, store.UserPatch{PasswordHash: &hash}); err != nil {
		return nil, err
	}
	if err := s.st.DeleteUserSessions(r.Context(), u.ID); err != nil {
		return nil, err
	}
	if err := s.newSession(w, r, u, false); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
func (s *Server) enroll(_ http.ResponseWriter, r *http.Request) (any, error) {
	u := state(r).user
	if u.TOTPEnabled {
		return nil, conflict("TOTP is already enabled")
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return nil, err
	}
	enc, err := s.box.Seal([]byte(secret))
	if err != nil {
		return nil, err
	}
	disabled := false
	if err := s.st.UpdateUser(r.Context(), u.ID, store.UserPatch{TOTPSecretEnc: &enc, TOTPEnabled: &disabled}); err != nil {
		return nil, err
	}
	return map[string]string{"secret": secret, "otpauth_url": auth.OTPAuthURL("Kuraka", u.Email, secret)}, nil
}
func (s *Server) confirm(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if state(r).user.TOTPEnabled {
		return nil, conflict("TOTP is already enabled")
	}
	if err := s.checkTOTP(r, in.Code); err != nil {
		return nil, err
	}
	enabled := true
	u := state(r).user
	if err := s.st.UpdateUser(r.Context(), u.ID, store.UserPatch{TOTPEnabled: &enabled}); err != nil {
		return nil, err
	}
	if err := s.st.DeleteUserSessions(r.Context(), u.ID); err != nil {
		return nil, err
	}
	if err := s.newSession(w, r, u, false); err != nil {
		return nil, err
	}
	return map[string]bool{"totp_enabled": true}, nil
}
func (s *Server) disableTOTP(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	u := state(r).user
	ok, err := auth.VerifyPassword(in.Password, u.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, invalidCredentials
	}
	if !u.TOTPEnabled {
		return nil, conflict("TOTP is not enabled")
	}
	if err := s.checkTOTP(r, in.Code); err != nil {
		return nil, err
	}
	disabled, empty := false, ""
	if err := s.st.UpdateUser(r.Context(), u.ID, store.UserPatch{TOTPEnabled: &disabled, TOTPSecretEnc: &empty}); err != nil {
		return nil, err
	}
	if err := s.st.DeleteUserSessions(r.Context(), u.ID); err != nil {
		return nil, err
	}
	if err := s.newSession(w, r, u, false); err != nil {
		return nil, err
	}
	return map[string]bool{"totp_enabled": false}, nil
}
