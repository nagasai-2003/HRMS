package api

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"hrms/backend/internal/rules"
	"hrms/backend/internal/store"
)

// RunJobs closes days nobody checked out of and clears old sessions, once a
// minute. A Postgres advisory lock keeps two API instances from doing the
// same work. (Email and reports move to a River queue in a later step.)
func (s *Server) RunJobs(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if err := s.autoCheckout(ctx); err != nil && ctx.Err() == nil {
			s.Log.Error("auto check-out", "err", err)
		}
		s.Q.DeleteExpiredSessions(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// autoCheckout adds a check-out at the company's auto check-out time to every
// day of the last two weeks that was left open. Today counts once that time
// has passed.
func (s *Server) autoCheckout(ctx context.Context) error {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(424242)").Scan(&got); err != nil || !got {
		return err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock(424242)")

	st, loc, today, err := s.today(ctx, s.Q)
	if err != nil {
		return err
	}
	cutoff := clockMin(st.AutoCheckout)
	before := today
	if minutesOfDay(s.Now(), loc) >= cutoff {
		before = today.AddDate(0, 0, 1)
	}
	open, err := s.Q.LastEventsBefore(ctx, store.LastEventsBeforeParams{Before: before, Since: today.AddDate(0, 0, -14)})
	if err != nil {
		return err
	}
	for _, o := range open {
		if o.Kind == rules.ActCheckOut {
			continue
		}
		at := rules.At(o.Day, cutoff, loc)
		if at.Before(o.At) {
			at = o.At // a late event after the cutoff: close at that moment
		}
		err := s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", o.UserID.String()); err != nil {
				return err
			}
			evs, err := q.ListEventsRange(ctx, store.ListEventsRangeParams{UserID: o.UserID, FromDay: o.Day, ToDay: o.Day})
			if err != nil {
				return err
			}
			sum := rules.Summarise(toRuleEvents(evs), at)
			if sum.Phase != rules.PhaseIn && sum.Phase != rules.PhaseBreak {
				return nil
			}
			mode := evs[len(evs)-1].Mode
			_, err = q.InsertEvent(ctx, store.InsertEventParams{
				UserID: o.UserID, Day: o.Day, Kind: rules.ActCheckOut, At: at, Mode: mode,
				Note: "Checked out by the system: no check-out was recorded.", Auto: true,
			})
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
