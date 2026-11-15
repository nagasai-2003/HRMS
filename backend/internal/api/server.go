// Package api is the HTTP layer: auth, attendance, requests, timesheets,
// team task boards, chat and notifications, team views for managers/HR and admin settings.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"hrms/backend/internal/clientip"
	"hrms/backend/internal/live"
	"hrms/backend/internal/mfa"
	"hrms/backend/internal/store"
)

type Server struct {
	Pool   *pgxpool.Pool
	Q      *store.Queries
	Redis  *redis.Client // optional: login rate limits
	IP     clientip.Resolver
	Secure bool // Secure cookies (production)
	Now    func() time.Time
	Log    *slog.Logger
	// Live pushes chat messages and notifications to open tabs.
	Live *live.Hub
	// Origins may open the live socket besides this host (ALLOWED_ORIGINS).
	Origins []string
	// MFA encrypts authenticator secrets (MFA_KEY); Issuer is the name
	// people see in their authenticator app.
	MFA    *mfa.Box
	Issuer string

	locMu sync.Mutex
	locs  map[string]*time.Location
}

func New(pool *pgxpool.Pool, rdb *redis.Client, ip clientip.Resolver, secure bool, log *slog.Logger) *Server {
	return &Server{
		Pool: pool, Q: store.New(pool), Redis: rdb, IP: ip, Secure: secure,
		Now: time.Now, Log: log, locs: map[string]*time.Location{},
		Live: live.New(rdb, log),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, s.jsonOnly)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Pool.Ping(r.Context()); err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", s.login)
		r.Post("/auth/logout", s.logout)

		// The second sign-in step: a pending session from the password.
		r.Group(func(r chi.Router) {
			r.Use(s.requirePending)
			r.Get("/auth/mfa", s.mfaState)
			r.Post("/auth/mfa/setup", s.mfaSetupStart)
			r.Post("/auth/mfa/setup/confirm", s.mfaSetupConfirm)
			r.Post("/auth/mfa/verify", s.mfaVerify)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.requireUser)
			r.Get("/me", s.me)
			r.Get("/me/summary", s.mySummary)
			r.Get("/me/security", s.mySecurity)
			r.Post("/me/security/recovery-codes", s.myNewRecoveryCodes)

			r.Get("/attendance/today", s.attendanceToday)
			r.Post("/attendance/check", s.attendanceCheck)
			r.Post("/attendance/action", s.attendanceAction)
			r.Get("/attendance/history", s.attendanceHistory)

			r.Get("/requests", s.myRequests)
			r.Post("/requests", s.createRequest)
			r.Post("/requests/{id}/cancel", s.cancelRequest)

			r.Get("/timesheets", s.myTimesheets)
			r.Get("/timesheets/week", s.timesheetWeek)
			r.Put("/timesheets/week", s.saveTimesheetWeek)
			r.Post("/timesheets/{id}/submit", s.submitTimesheet)
			r.Post("/timesheets/{id}/recall", s.recallTimesheet)

			// Team boards: members and Admin; status groups: team managers and Admin.
			r.Get("/tasks/teams", s.taskTeams)
			r.Get("/tasks/board", s.taskBoard)
			r.Get("/tasks/mine", s.myTasks)
			r.Post("/tasks", s.createTask)
			r.Get("/tasks/{id}", s.getTask)
			r.Patch("/tasks/{id}", s.updateTask)
			r.Delete("/tasks/{id}", s.deleteTask)
			r.Post("/tasks/{id}/move", s.moveTaskHandler)
			r.Post("/tasks/{id}/checklist", s.addChecklistItem)
			r.Patch("/tasks/{id}/checklist/{itemID}", s.updateChecklistItem)
			r.Delete("/tasks/{id}/checklist/{itemID}", s.deleteChecklistItem)
			r.Post("/tasks/{id}/comments", s.addComment)
			r.Delete("/tasks/{id}/comments/{commentID}", s.deleteComment)
			r.Post("/tasks/teams/{teamID}/statuses", s.createStatus)
			r.Put("/tasks/teams/{teamID}/statuses/order", s.orderStatuses)
			r.Post("/tasks/teams/{teamID}/statuses/template", s.applyTemplate)
			r.Patch("/tasks/teams/{teamID}/statuses/{statusID}", s.updateStatus)
			r.Delete("/tasks/teams/{teamID}/statuses/{statusID}", s.deleteStatus)

			// Chat: team channels (members only) and direct messages (anyone).
			r.Get("/live", s.liveSocket)
			r.Get("/chat/conversations", s.chatConversations)
			r.Get("/chat/unread", s.chatUnread)
			r.Get("/chat/people", s.chatPeople)
			r.Get("/chat/search", s.searchMessages)
			r.Post("/chat/dms", s.openDM)
			r.Post("/chat/teams/{teamID}/channels", s.createChannel)
			r.Get("/chat/channels/{id}", s.getChannel)
			r.Patch("/chat/channels/{id}", s.updateChannel)
			r.Get("/chat/channels/{id}/messages", s.listMessages)
			r.Post("/chat/channels/{id}/messages", s.postMessage)
			r.Post("/chat/channels/{id}/read", s.markRead)
			r.Get("/chat/messages/{msgID}", s.getMessage)
			r.Patch("/chat/messages/{msgID}", s.editMessage)
			r.Delete("/chat/messages/{msgID}", s.deleteMessage)
			r.Get("/notifications", s.listNotifications)
			r.Post("/notifications/read", s.readNotifications)

			// Managers (their teams), HR and Admin (everyone).
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(store.AppRoleManager, store.AppRoleHr, store.AppRoleAdmin))
				r.Get("/team/attendance", s.teamAttendance)
				r.Get("/team/people", s.teamPeople)
				r.Get("/team/people/{id}/summary", s.personSummary)
				r.Get("/approvals", s.listApprovals)
				r.Get("/timesheets/review", s.reviewTimesheets)
				r.Post("/timesheets/{id}/decide", s.decideTimesheet)
			})

			// HR and Admin decide WFH, on-duty and corrections.
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(store.AppRoleHr, store.AppRoleAdmin))
				r.Post("/approvals/{id}/decide", s.decideRequest)
				r.Get("/admin/holidays", s.listHolidays)
				r.Put("/admin/holidays/{day}", s.putHoliday)
				r.Delete("/admin/holidays/{day}", s.deleteHoliday)
				r.Get("/admin/projects", s.listProjects)
				r.Post("/admin/projects", s.createProject)
				r.Patch("/admin/projects/{id}", s.updateProject)
			})

			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(store.AppRoleAdmin))
				r.Get("/admin/people", s.adminPeople)
				r.Post("/admin/people", s.adminCreatePerson)
				r.Patch("/admin/people/{id}", s.adminUpdatePerson)
				r.Post("/admin/people/{id}/reset-mfa", s.adminResetMFA)
				r.Post("/admin/people/{id}/make-owner", s.adminMakeOwner)
				r.Post("/admin/teams", s.adminCreateTeam)
				r.Patch("/admin/teams/{id}", s.adminRenameTeam)
				r.Delete("/admin/teams/{id}", s.adminDeleteTeam)
				r.Put("/admin/teams/{id}/members/{userID}", s.adminPutMember)
				r.Delete("/admin/teams/{id}/members/{userID}", s.adminRemoveMember)
				r.Get("/admin/rules", s.adminRules)
				r.Put("/admin/rules", s.adminSaveRules)
				r.Post("/admin/offices", s.adminCreateOffice)
				r.Patch("/admin/offices/{id}", s.adminUpdateOffice)
				r.Delete("/admin/offices/{id}", s.adminDeleteOffice)
				r.Post("/admin/networks", s.adminCreateNetwork)
				r.Delete("/admin/networks/{id}", s.adminDeleteNetwork)
			})
		})
	})
	return r
}

// jsonOnly: requests that change data must be JSON. Browsers cannot send a
// cross-site JSON request without a CORS preflight (which we never allow), so
// together with SameSite cookies this blocks CSRF.
func (s *Server) jsonOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeErr(w, http.StatusUnsupportedMediaType, "Send JSON.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- small helpers ----

type apiError struct {
	Status  int
	Message string
	Code    string
}

func (e *apiError) Error() string { return e.Message }

func bad(msg string) error       { return &apiError{Status: http.StatusBadRequest, Message: msg} }
func forbidden(msg string) error { return &apiError{Status: http.StatusForbidden, Message: msg} }
func notFound(msg string) error  { return &apiError{Status: http.StatusNotFound, Message: msg} }
func conflict(msg string) error  { return &apiError{Status: http.StatusConflict, Message: msg} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail turns an error into a response. Unknown errors are logged and hidden.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		body := map[string]string{"error": ae.Message}
		if ae.Code != "" {
			body["code"] = ae.Code
		}
		writeJSON(w, ae.Status, body)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "Not found.")
		return
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505":
			writeErr(w, http.StatusConflict, "That already exists.")
			return
		case "23503":
			writeErr(w, http.StatusConflict, "It is still in use.")
			return
		case "23514", "22P02":
			writeErr(w, http.StatusBadRequest, "Some values are not valid.")
			return
		}
	}
	s.Log.Error("request failed", "path", r.URL.Path, "err", err)
	writeErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return bad("The request body is not valid: " + err.Error())
	}
	return nil
}

func (s *Server) tx(ctx context.Context, fn func(q *store.Queries, tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Q.WithTx(tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) loc(name string) *time.Location {
	s.locMu.Lock()
	defer s.locMu.Unlock()
	if l, ok := s.locs[name]; ok {
		return l
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		l = time.UTC
	}
	s.locs[name] = l
	return l
}

const dayLayout = "2006-01-02"

func ymd(t time.Time) string { return t.Format(dayLayout) }

func parseDay(s string) (time.Time, error) {
	t, err := time.Parse(dayLayout, s)
	if err != nil {
		return time.Time{}, bad("Dates must look like 2026-11-13.")
	}
	return t, nil
}

func clockMin(t pgtype.Time) int { return int(t.Microseconds / 60_000_000) }

func clockStr(t pgtype.Time) string {
	if !t.Valid {
		return ""
	}
	m := clockMin(t)
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func parseClock(s string) (pgtype.Time, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 || len(s) != 5 {
		return pgtype.Time{}, bad("Times must look like 09:30.")
	}
	return pgtype.Time{Microseconds: int64(h*60+m) * 60_000_000, Valid: true}, nil
}

// clean trims and cuts text to at most max characters.
func clean(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		s = strings.TrimSpace(string(r[:max]))
	}
	return s
}
