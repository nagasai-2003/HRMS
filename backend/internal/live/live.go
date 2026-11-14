// Package live pushes events (new messages, typing, notifications) to the
// people who should see them, over one WebSocket per open browser tab.
//
// With Redis, every API instance publishes to one Redis channel and each
// instance delivers to the sockets it holds, so it works behind a load
// balancer. Without Redis, events stay inside this process. Presence (the
// green dot) is a Redis key per person, refreshed while a socket is open.
package live

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	redisChannel = "hrms:live"
	presenceTTL  = 75 * time.Second
	sendBuffer   = 64
)

// Event is what the browser receives: {"type": "...", "data": {...}}.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type envelope struct {
	Users []uuid.UUID     `json:"u"`
	Event json.RawMessage `json:"e"`
}

// Client is one open socket. Out is drained by the socket's writer; when it
// is full the client is too slow and Gone is closed so the socket drops.
type Client struct {
	UserID uuid.UUID
	Out    chan []byte
	Gone   chan struct{}
	once   sync.Once
}

func (c *Client) drop() { c.once.Do(func() { close(c.Gone) }) }

type Hub struct {
	rdb *redis.Client
	log *slog.Logger

	mu      sync.Mutex
	clients map[uuid.UUID]map[*Client]struct{}
}

func New(rdb *redis.Client, log *slog.Logger) *Hub {
	return &Hub{rdb: rdb, log: log, clients: map[uuid.UUID]map[*Client]struct{}{}}
}

// Run listens for events from other instances. It returns when ctx ends.
func (h *Hub) Run(ctx context.Context) {
	if h == nil || h.rdb == nil {
		return
	}
	for ctx.Err() == nil {
		sub := h.rdb.Subscribe(ctx, redisChannel)
		ch := sub.Channel()
		for msg := range ch {
			var env envelope
			if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
				continue
			}
			h.deliver(env.Users, env.Event)
		}
		sub.Close()
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second): // Redis went away; try again.
		}
	}
}

// Send pushes an event to these people on every instance.
func (h *Hub) Send(ctx context.Context, users []uuid.UUID, ev Event) {
	if h == nil || len(users) == 0 {
		return
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		h.log.Error("live event", "err", err)
		return
	}
	if h.rdb != nil {
		env, _ := json.Marshal(envelope{Users: users, Event: raw})
		if err := h.rdb.Publish(ctx, redisChannel, env).Err(); err == nil {
			return
		}
		h.log.Warn("live publish failed; delivering locally", "err", err)
	}
	h.deliver(users, raw)
}

func (h *Hub) deliver(users []uuid.UUID, raw []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, u := range users {
		for c := range h.clients[u] {
			select {
			case c.Out <- raw:
			default:
				c.drop()
			}
		}
	}
}

// Join registers a socket. Call the returned function when it closes.
func (h *Hub) Join(ctx context.Context, userID uuid.UUID) (*Client, func()) {
	c := &Client{UserID: userID, Out: make(chan []byte, sendBuffer), Gone: make(chan struct{})}
	h.mu.Lock()
	if h.clients[userID] == nil {
		h.clients[userID] = map[*Client]struct{}{}
	}
	h.clients[userID][c] = struct{}{}
	h.mu.Unlock()
	h.Touch(ctx, userID)
	return c, func() {
		h.mu.Lock()
		delete(h.clients[userID], c)
		last := len(h.clients[userID]) == 0
		if last {
			delete(h.clients, userID)
		}
		h.mu.Unlock()
		c.drop()
		if last && h.rdb != nil {
			// Another instance may still hold a socket; its next Touch puts it back.
			h.rdb.Del(context.Background(), presenceKey(userID))
		}
	}
}

// Touch keeps a person shown as online.
func (h *Hub) Touch(ctx context.Context, userID uuid.UUID) {
	if h.rdb != nil {
		h.rdb.Set(ctx, presenceKey(userID), "1", presenceTTL)
	}
}

// Online says which of these people have the app open.
func (h *Hub) Online(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	if h == nil || len(ids) == 0 {
		return out
	}
	if h.rdb != nil {
		keys := make([]string, len(ids))
		for i, id := range ids {
			keys[i] = presenceKey(id)
		}
		vals, err := h.rdb.MGet(ctx, keys...).Result()
		if err == nil {
			for i, v := range vals {
				if v != nil {
					out[ids[i]] = true
				}
			}
			return out
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range ids {
		if len(h.clients[id]) > 0 {
			out[id] = true
		}
	}
	return out
}

func presenceKey(id uuid.UUID) string { return "live:on:" + id.String() }
