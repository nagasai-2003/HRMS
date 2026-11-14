package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hrms/backend/internal/live"
)

const (
	pingEvery    = 30 * time.Second
	writeTimeout = 10 * time.Second
	typingGap    = 2 * time.Second
)

// GET /api/live: one WebSocket per open tab. The server pushes events; the
// browser only sends "typing". The session is checked again every 30
// seconds, so signing out or deactivation closes the socket.
func (s *Server) liveSocket(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	// The cookie makes this request trusted, so only our own pages may open
	// it: the Origin must be this host or one listed in ALLOWED_ORIGINS.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.Origins})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(4096)

	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	client, leave := s.Live.Join(ctx, u.ID)
	defer leave()

	go func() {
		defer cancel()
		lastTyping := map[uuid.UUID]time.Time{}
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var in struct {
				Type      string    `json:"type"`
				ChannelID uuid.UUID `json:"channel_id"`
			}
			if json.Unmarshal(data, &in) != nil || in.Type != "typing" {
				continue
			}
			if time.Since(lastTyping[in.ChannelID]) < typingGap {
				continue
			}
			lastTyping[in.ChannelID] = time.Now()
			s.typing(ctx, u.ID, u.Name, in.ChannelID)
		}
	}()

	send := func(raw []byte) error {
		wctx, done := context.WithTimeout(ctx, writeTimeout)
		defer done()
		return c.Write(wctx, websocket.MessageText, raw)
	}
	hello, _ := json.Marshal(live.Event{Type: "hello", Data: map[string]any{"user_id": u.ID}})
	if send(hello) != nil {
		return
	}
	tick := time.NewTicker(pingEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.Gone:
			c.Close(websocket.StatusTryAgainLater, "too slow")
			return
		case raw := <-client.Out:
			if send(raw) != nil {
				return
			}
		case <-tick.C:
			if _, err := s.Q.GetSessionUser(ctx, tokenHash(cookie.Value)); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					c.Close(websocket.StatusPolicyViolation, "session ended")
					return
				}
				continue // database hiccup: try again next tick
			}
			pctx, done := context.WithTimeout(ctx, writeTimeout)
			err := c.Ping(pctx)
			done()
			if err != nil {
				return
			}
			s.Live.Touch(ctx, u.ID)
		}
	}
}

// typing tells the others in a conversation that someone is typing.
func (s *Server) typing(ctx context.Context, userID uuid.UUID, name string, channelID uuid.UUID) {
	aud, err := s.Q.ChannelAudience(ctx, channelID)
	if err != nil {
		return
	}
	var others []uuid.UUID
	in := false
	for _, id := range aud {
		if id == userID {
			in = true
		} else {
			others = append(others, id)
		}
	}
	if !in {
		return
	}
	s.Live.Send(ctx, others, live.Event{Type: "typing", Data: map[string]any{
		"channel_id": channelID, "user": person(userID, name),
	}})
}
