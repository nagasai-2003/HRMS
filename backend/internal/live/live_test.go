package live

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

func TestLocalDelivery(t *testing.T) {
	ctx := context.Background()
	h := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	a, b := uuid.New(), uuid.New()
	ca, leaveA := h.Join(ctx, a)
	cb, leaveB := h.Join(ctx, b)

	h.Send(ctx, []uuid.UUID{a}, Event{Type: "ping", Data: 1})
	var ev Event
	if err := json.Unmarshal(<-ca.Out, &ev); err != nil || ev.Type != "ping" {
		t.Fatalf("a got %v %v", ev, err)
	}
	if len(cb.Out) != 0 {
		t.Fatal("b should get nothing")
	}
	if on := h.Online(ctx, []uuid.UUID{a, b, uuid.New()}); !on[a] || !on[b] || len(on) != 2 {
		t.Fatalf("online %v", on)
	}
	leaveB()
	if h.Online(ctx, []uuid.UUID{b})[b] {
		t.Fatal("b left")
	}
	select {
	case <-cb.Gone:
	default:
		t.Fatal("leaving closes Gone")
	}

	// A client that does not read is dropped instead of blocking others.
	for i := 0; i < sendBuffer+1; i++ {
		h.Send(ctx, []uuid.UUID{a}, Event{Type: "x"})
	}
	select {
	case <-ca.Gone:
	default:
		t.Fatal("slow client should be dropped")
	}
	leaveA()
	var nilHub *Hub
	nilHub.Send(ctx, []uuid.UUID{a}, Event{Type: "x"}) // must not panic
}
