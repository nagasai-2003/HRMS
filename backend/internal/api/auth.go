package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"hrms/backend/internal/store"
)

const (
	cookieName = "hrms_session"
	sessionTTL = 7 * 24 * time.Hour
)

type ctxKey struct{}

func userFrom(ctx context.Context) store.User { return ctx.Value(ctxKey{}).(store.User) }

func HashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), 12)
	return string(b), err
}

// A fixed hash so a missing account takes as long as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), 12)

func tokenHash(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	ip := s.IP.IP(r).String()
	if !s.allowLogin(r.Context(), ip, email) {
		writeErr(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait 15 minutes and try again.")
		return
	}
	u, err := s.Q.GetUserByEmail(r.Context(), email)
	hash := dummyHash
	if err == nil {
		hash = []byte(u.PasswordHash)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		s.fail(w, r, err)
		return
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) != nil || err != nil || !u.Active {
		writeErr(w, http.StatusUnauthorized, "Email or password is wrong.")
		return
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	tok := base64.RawURLEncoding.EncodeToString(raw)
	exp := s.Now().Add(sessionTTL)
	if err := s.Q.CreateSession(r.Context(), store.CreateSessionParams{
		TokenHash: tokenHash(tok), UserID: u.ID, ExpiresAt: exp, Ip: ip, UserAgent: clean(r.UserAgent(), 300),
	}); err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", Expires: exp,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "role": u.Role})
}

// allowLogin: at most 20 tries per IP and 10 per email in 15 minutes.
func (s *Server) allowLogin(ctx context.Context, ip, email string) bool {
	if s.Redis == nil {
		return true
	}
	check := func(key string, max int64) bool {
		n, err := s.Redis.Incr(ctx, key).Result()
		if err != nil {
			return true // Redis down: do not lock everyone out
		}
		if n == 1 {
			s.Redis.Expire(ctx, key, 15*time.Minute)
		}
		return n <= max
	}
	return check("login:ip:"+ip, 20) && check("login:email:"+email, 10)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.Q.DeleteSession(r.Context(), tokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			writeErr(w, http.StatusUnauthorized, "Sign in first.")
			return
		}
		u, err := s.Q.GetSessionUser(r.Context(), tokenHash(c.Value))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeErr(w, http.StatusUnauthorized, "Your session has ended. Sign in again.")
				return
			}
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

func (s *Server) requireRole(roles ...store.AppRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !slices.Contains(roles, userFrom(r.Context()).Role) {
				writeErr(w, http.StatusForbidden, "You do not have access to this.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// scope is who a person may see in team views: everyone for HR and Admin,
// their teams' members for a manager, only themselves otherwise.
type scope struct {
	all bool
	ids map[uuid.UUID]bool
}

func (sc scope) has(id uuid.UUID) bool { return sc.all || sc.ids[id] }

// list is nil for "everyone", which the queries read as no filter.
func (sc scope) list() []uuid.UUID {
	if sc.all {
		return nil
	}
	out := make([]uuid.UUID, 0, len(sc.ids))
	for id := range sc.ids {
		out = append(out, id)
	}
	return out
}

func (s *Server) scopeOf(ctx context.Context, u store.User) (scope, error) {
	switch u.Role {
	case store.AppRoleAdmin, store.AppRoleHr:
		return scope{all: true}, nil
	case store.AppRoleManager:
		ids, err := s.Q.ManagedUserIDs(ctx, u.ID)
		if err != nil {
			return scope{}, err
		}
		m := map[uuid.UUID]bool{u.ID: true}
		for _, id := range ids {
			m[id] = true
		}
		return scope{ids: m}, nil
	}
	return scope{ids: map[uuid.UUID]bool{u.ID: true}}, nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	teams, err := s.Q.ListUserTeams(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var office any
	if u.OfficeID != nil {
		offs, _ := s.Q.ListOffices(r.Context())
		for _, o := range offs {
			if o.ID == *u.OfficeID {
				office = map[string]any{"id": o.ID, "name": o.Name}
			}
		}
	}
	role := u.Role
	writeJSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "name": u.Name, "email": u.Email, "title": u.Title, "role": role,
		"work_policy": u.WorkPolicy, "remote_weekdays": u.RemoteWeekdays, "office": office,
		"teams": teams,
		"can": map[string]bool{
			"team_views":        role == store.AppRoleManager || role == store.AppRoleHr || role == store.AppRoleAdmin,
			"decide_requests":   role == store.AppRoleHr || role == store.AppRoleAdmin,
			"review_timesheets": role == store.AppRoleManager || role == store.AppRoleHr || role == store.AppRoleAdmin,
			"admin":             role == store.AppRoleAdmin,
			"holidays":          role == store.AppRoleHr || role == store.AppRoleAdmin,
		},
	})
}
