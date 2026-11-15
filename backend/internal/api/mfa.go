package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"hrms/backend/internal/mfa"
	"hrms/backend/internal/store"
)

// Sign-in is two steps for everyone. The password gives a short "pending"
// session: stage "code" when the person has an authenticator, "setup" when
// they still have to set one up. Only after a correct code is it swapped for
// a full session, the one every other endpoint accepts.

const (
	pendingTTL    = 10 * time.Minute
	recoveryCount = 10
)

const (
	stageFull  = "full"
	stageCode  = "code"
	stageSetup = "setup"
)

type pendingKey struct{}

type pending struct {
	user  store.User
	stage string
	token string
}

func pendingFrom(ctx context.Context) pending { return ctx.Value(pendingKey{}).(pending) }

// startSession creates a session and sets its cookie.
func (s *Server) startSession(ctx context.Context, w http.ResponseWriter, r *http.Request, u store.User, stage string) error {
	raw := make([]byte, 32)
	rand.Read(raw)
	tok := base64.RawURLEncoding.EncodeToString(raw)
	ttl := sessionTTL
	if stage != stageFull {
		ttl = pendingTTL
	}
	exp := s.Now().Add(ttl)
	if err := s.Q.CreateSession(ctx, store.CreateSessionParams{
		TokenHash: tokenHash(tok), UserID: u.ID, ExpiresAt: exp, Ip: s.IP.IP(r).String(),
		UserAgent: clean(r.UserAgent(), 300), Stage: stage,
	}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", Expires: exp,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// finishSignIn swaps the pending session for a full one (a new token, so
// the pending one cannot be reused).
func (s *Server) finishSignIn(ctx context.Context, w http.ResponseWriter, r *http.Request, p pending) error {
	if err := s.Q.DeleteSession(ctx, tokenHash(p.token)); err != nil {
		return err
	}
	return s.startSession(ctx, w, r, p.user, stageFull)
}

func (s *Server) requirePending(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			writeErr(w, http.StatusUnauthorized, "Sign in with your email and password first.")
			return
		}
		row, err := s.Q.GetPendingSession(r.Context(), tokenHash(c.Value))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeErr(w, http.StatusUnauthorized, "This sign-in has expired. Enter your email and password again.")
				return
			}
			s.fail(w, r, err)
			return
		}
		p := pending{user: row.User, stage: row.Stage, token: c.Value}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), pendingKey{}, p)))
	})
}

// allowMFA: at most 10 code attempts per person in 15 minutes.
func (s *Server) allowMFA(ctx context.Context, u store.User) bool {
	if s.Redis == nil {
		return true
	}
	key := "mfa:user:" + u.ID.String()
	n, err := s.Redis.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		s.Redis.Expire(ctx, key, 15*time.Minute)
	}
	return n <= 10
}

var (
	errTooManyCodes = &apiError{Status: http.StatusTooManyRequests, Message: "Too many codes tried. Wait 15 minutes and try again."}
	errWrongCode    = bad("That code is not right. Type the newest 6-digit code from your authenticator app.")
)

func (s *Server) secretOf(u store.User) ([]byte, error) {
	if len(u.MfaSecret) == 0 {
		return nil, bad("The authenticator is not set up.")
	}
	return s.MFA.Open(u.MfaSecret, u.ID[:])
}

// checkTOTP accepts a current authenticator code once.
func (s *Server) checkTOTP(ctx context.Context, q *store.Queries, u store.User, code string) error {
	if !s.allowMFA(ctx, u) {
		return errTooManyCodes
	}
	if u.MfaEnabledAt == nil {
		return bad("The authenticator is not set up.")
	}
	secret, err := s.secretOf(u)
	if err != nil {
		return err
	}
	step, ok := mfa.Verify(secret, code, s.Now(), u.MfaLastStep)
	if !ok {
		return errWrongCode
	}
	n, err := q.UseMFAStep(ctx, store.UseMFAStepParams{ID: u.ID, Step: step})
	if err != nil {
		return err
	}
	if n == 0 {
		return errWrongCode // used a moment ago in another request
	}
	return nil
}

// newRecoveryCodes replaces a person's recovery codes and returns them.
func newRecoveryCodes(ctx context.Context, q *store.Queries, u store.User) ([]string, error) {
	if err := q.DeleteRecoveryCodes(ctx, u.ID); err != nil {
		return nil, err
	}
	codes := mfa.NewRecoveryCodes(recoveryCount)
	for _, c := range codes {
		if err := q.AddRecoveryCode(ctx, store.AddRecoveryCodeParams{UserID: u.ID, CodeHash: mfa.HashRecovery(mfa.CleanRecovery(c))}); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// GET /api/auth/mfa: which step a pending sign-in is on.
func (s *Server) mfaState(w http.ResponseWriter, r *http.Request) {
	p := pendingFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"stage": p.stage, "name": p.user.Name, "email": p.user.Email})
}

// POST /api/auth/mfa/setup: a new secret to scan. Each call replaces the
// last one until a code confirms it.
func (s *Server) mfaSetupStart(w http.ResponseWriter, r *http.Request) {
	p := pendingFrom(r.Context())
	if p.stage != stageSetup {
		s.fail(w, r, bad("Your authenticator is already set up. Enter a code from it."))
		return
	}
	secret := mfa.NewSecret()
	if err := s.Q.SetMFASecret(r.Context(), store.SetMFASecretParams{ID: p.user.ID, MfaSecret: s.MFA.Seal(secret, p.user.ID[:])}); err != nil {
		s.fail(w, r, err)
		return
	}
	uri := mfa.URI(s.Issuer, p.user.Email, secret)
	svg, err := mfa.QRSVG(uri)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": mfa.Key(secret), "uri": uri, "qr_svg": svg, "issuer": s.Issuer})
}

type codeIn struct {
	Code string `json:"code"`
}

// POST /api/auth/mfa/setup/confirm: the first code turns the authenticator
// on, gives recovery codes and finishes the sign-in.
func (s *Server) mfaSetupConfirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := pendingFrom(ctx)
	var in codeIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if p.stage != stageSetup {
		s.fail(w, r, bad("Your authenticator is already set up. Enter a code from it."))
		return
	}
	if !s.allowMFA(ctx, p.user) {
		s.fail(w, r, errTooManyCodes)
		return
	}
	u, err := s.Q.GetUser(ctx, p.user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	secret, err := s.secretOf(u)
	if err != nil {
		s.fail(w, r, bad("Scan the QR code first."))
		return
	}
	step, ok := mfa.Verify(secret, in.Code, s.Now(), 0)
	if !ok {
		s.fail(w, r, bad("That code is not right. Check that you scanned the code on this page, then type the newest 6-digit code."))
		return
	}
	var codes []string
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		n, err := q.EnableMFA(ctx, store.EnableMFAParams{ID: u.ID, Step: step})
		if err != nil {
			return err
		}
		if n == 0 {
			return conflict("The authenticator was set up in another window. Sign in again.")
		}
		codes, err = newRecoveryCodes(ctx, q, u)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.finishSignIn(ctx, w, r, p); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recovery_codes": codes})
}

// POST /api/auth/mfa/verify: a code from the app, or a recovery code.
func (s *Server) mfaVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := pendingFrom(ctx)
	var in codeIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if p.stage != stageCode {
		s.fail(w, r, bad("Set up your authenticator first."))
		return
	}
	u, err := s.Q.GetUser(ctx, p.user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	usedRecovery := false
	if mfa.CleanCode(in.Code) != "" {
		if err := s.checkTOTP(ctx, s.Q, u, in.Code); err != nil {
			s.fail(w, r, err)
			return
		}
	} else if rc := mfa.CleanRecovery(in.Code); rc != "" {
		if !s.allowMFA(ctx, u) {
			s.fail(w, r, errTooManyCodes)
			return
		}
		n, err := s.Q.UseRecoveryCode(ctx, store.UseRecoveryCodeParams{UserID: u.ID, CodeHash: mfa.HashRecovery(rc)})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if n == 0 {
			s.fail(w, r, bad("That recovery code is not right, or it was already used."))
			return
		}
		usedRecovery = true
	} else {
		s.fail(w, r, bad("Type the 6-digit code from your app, or one of your recovery codes."))
		return
	}
	if err := s.finishSignIn(ctx, w, r, p); err != nil {
		s.fail(w, r, err)
		return
	}
	left, _ := s.Q.CountRecoveryCodesLeft(ctx, u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "used_recovery": usedRecovery, "recovery_left": left})
}

// GET /api/me/security
func (s *Server) mySecurity(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	left, err := s.Q.CountRecoveryCodesLeft(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mfa_enabled_at": u.MfaEnabledAt, "recovery_left": left})
}

// POST /api/me/security/recovery-codes: new codes; the old ones stop working.
func (s *Server) myNewRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in codeIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	var codes []string
	err := s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		if err := s.checkTOTP(ctx, q, u, in.Code); err != nil {
			return err
		}
		var err error
		codes, err = newRecoveryCodes(ctx, q, u)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}
