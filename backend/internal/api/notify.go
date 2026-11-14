package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"hrms/backend/internal/live"
	"hrms/backend/internal/store"
)

// Notifications: someone assigned you a task, mentioned you (task comment or
// chat), or commented on a task you are assigned to or reported. Direct
// messages show as unread counts in Chat instead.

type notice struct {
	To                      uuid.UUID
	Kind, Title, Body, Link string
}

func notificationJSON(n store.Notification, actor string) map[string]any {
	return map[string]any{
		"id": n.ID, "kind": n.Kind, "title": n.Title, "body": n.Body, "link": n.Link,
		"actor_name": actor, "created_at": n.CreatedAt, "read": n.ReadAt != nil,
	}
}

// notify saves notices (one per person per call, never to the actor) and
// pushes each to the person's open tabs. Failures are logged: the action
// that caused them has already happened.
func (s *Server) notify(ctx context.Context, actor store.User, list []notice) {
	ctx = context.WithoutCancel(ctx)
	seen := map[uuid.UUID]bool{actor.ID: true}
	for _, n := range list {
		if seen[n.To] {
			continue
		}
		seen[n.To] = true
		row, err := s.Q.CreateNotification(ctx, store.CreateNotificationParams{
			UserID: n.To, Kind: n.Kind, ActorID: &actor.ID, Title: n.Title, Body: n.Body, Link: n.Link,
		})
		if err != nil {
			s.Log.Error("notification", "err", err)
			continue
		}
		s.Live.Send(ctx, []uuid.UUID{n.To}, live.Event{Type: "notification", Data: notificationJSON(row, actor.Name)})
	}
}

// GET /api/notifications
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	rows, err := s.Q.ListNotifications(ctx, u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	unread, err := s.Q.CountUnreadNotifications(ctx, u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items := make([]map[string]any, len(rows))
	for i, n := range rows {
		items[i] = notificationJSON(store.Notification{
			ID: n.ID, UserID: n.UserID, Kind: n.Kind, ActorID: n.ActorID, Title: n.Title, Body: n.Body,
			Link: n.Link, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt,
		}, n.ActorName)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

// POST /api/notifications/read {ids} or {all: true}
func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []uuid.UUID `json:"ids"`
		All bool        `json:"all"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	if in.IDs == nil {
		in.IDs = []uuid.UUID{}
	}
	if err := s.Q.ReadNotifications(ctx, store.ReadNotificationsParams{UserID: u.ID, Every: in.All, Ids: in.IDs}); err != nil {
		s.fail(w, r, err)
		return
	}
	unread, err := s.Q.CountUnreadNotifications(ctx, u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.Live.Send(ctx, []uuid.UUID{u.ID}, live.Event{Type: "notifications.read", Data: map[string]any{"unread": unread}})
	writeJSON(w, http.StatusOK, map[string]any{"unread": unread})
}
