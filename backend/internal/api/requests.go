package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"hrms/backend/internal/rules"
	"hrms/backend/internal/store"
)

func requestJSON(r store.Request) map[string]any {
	return map[string]any{
		"id": r.ID, "user_id": r.UserID, "kind": r.Kind, "day": ymd(r.Day), "reason": r.Reason,
		"fix_in": clockStr(r.FixIn), "fix_out": clockStr(r.FixOut),
		"status": r.Status, "is_extra": r.IsExtra, "decided_at": r.DecidedAt,
		"decision_note": r.DecisionNote, "created_at": r.CreatedAt,
	}
}

// GET /api/requests: my requests and this month's WFH count.
func (s *Server) myRequests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, _, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	month := today
	if v := r.URL.Query().Get("month"); v != "" {
		if month, err = parseDay(v + "-01"); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	from, to := rules.MonthRange(month)
	cnt, err := s.Q.CountWFHInRange(ctx, store.CountWFHInRangeParams{UserID: u.ID, FromDay: from, ToDay: to})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	list, err := s.Q.ListMyRequests(ctx, u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	hols, err := s.Q.ListHolidays(ctx, store.ListHolidaysParams{FromDay: from, ToDay: to})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, q := range list {
		out[i] = requestJSON(q)
	}
	holidays := make([]map[string]string, len(hols))
	for i, h := range hols {
		holidays[i] = map[string]string{"day": ymd(h.Day), "name": h.Name}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"requests": out, "today": ymd(today), "month": month.Format("2006-01"),
		"wfh":    map[string]int32{"approved": cnt.Approved, "pending": cnt.Pending, "quota": st.WfhQuota},
		"policy": u.WorkPolicy, "remote_weekdays": u.RemoteWeekdays, "holidays": holidays,
	})
}

// POST /api/requests
func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind   string `json:"kind"`
		Day    string `json:"day"`
		Reason string `json:"reason"`
		FixIn  string `json:"fix_in"`
		FixOut string `json:"fix_out"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	day, err := parseDay(in.Day)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	in.Reason = clean(in.Reason, 500)
	if in.Reason == "" {
		s.fail(w, r, bad("Add a reason."))
		return
	}
	st, _, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := store.CreateRequestParams{UserID: u.ID, Kind: in.Kind, Day: day, Reason: in.Reason}
	switch in.Kind {
	case "wfh":
		if day.Before(today) {
			s.fail(w, r, bad("WFH can only be asked for today or a later day."))
			return
		}
		if day.After(today.AddDate(0, 3, 0)) {
			s.fail(w, r, bad("WFH can be asked for up to 3 months ahead."))
			return
		}
		mode := rules.ModeFor(rules.ModeInput{Policy: string(u.WorkPolicy), RemoteWeekdays: u.RemoteWeekdays, Day: day})
		if mode.Remote() {
			s.fail(w, r, bad("That day is already a remote day for you. No request is needed."))
			return
		}
		from, to := rules.MonthRange(day)
		cnt, err := s.Q.CountWFHInRange(ctx, store.CountWFHInRangeParams{UserID: u.ID, FromDay: from, ToDay: to})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		p.IsExtra = cnt.Approved+cnt.Pending >= st.WfhQuota
	case "on_duty":
		if day.Before(today.AddDate(0, 0, -30)) {
			s.fail(w, r, bad("On-duty can be asked for up to 30 days back."))
			return
		}
	case "correction":
		if day.After(today) || day.Before(today.AddDate(0, 0, -30)) {
			s.fail(w, r, bad("Corrections are for today or the last 30 days."))
			return
		}
		fi, err := parseClock(in.FixIn)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		fo, err := parseClock(in.FixOut)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if fo.Microseconds <= fi.Microseconds {
			s.fail(w, r, bad("Check-out must be after check-in."))
			return
		}
		p.FixIn, p.FixOut = fi, fo
	default:
		s.fail(w, r, bad("Pick WFH, on-duty or correction."))
		return
	}
	req, err := s.Q.CreateRequest(ctx, p)
	if err != nil {
		s.fail(w, r, uniqueAs(err, conflict("You already have a request of this kind for that day.")))
		return
	}
	writeJSON(w, http.StatusCreated, requestJSON(req))
}

// POST /api/requests/{id}/cancel: pending ones, or approved ones for a day
// that has not passed (corrections cannot be undone this way).
func (s *Server) cancelRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, notFound("Request not found."))
		return
	}
	req, err := s.Q.GetRequest(ctx, id)
	if err != nil || req.UserID != u.ID {
		s.fail(w, r, notFound("Request not found."))
		return
	}
	_, _, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Status == "approved" && (req.Kind == "correction" || req.Day.Before(today)) {
		s.fail(w, r, conflict("This approved request can no longer be cancelled."))
		return
	}
	out, err := s.Q.CancelRequest(ctx, store.CancelRequestParams{ID: id, UserID: u.ID})
	if err != nil {
		s.fail(w, r, conflictIfNone(err, "This request can no longer be cancelled."))
		return
	}
	writeJSON(w, http.StatusOK, requestJSON(out))
}

// GET /api/approvals?status=pending
// HR and Admin see everyone and can decide. A manager sees their team's
// requests (read only).
func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	sc, err := s.scopeOf(ctx, u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var status *string
	if v := r.URL.Query().Get("status"); v != "" && v != "all" {
		status = &v
	}
	rows, err := s.Q.ListRequestsForReview(ctx, store.ListRequestsForReviewParams{Status: status, UserIds: sc.list()})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// WFH used this month, per person, for the "2 of 2 used" column.
	_, _, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st, _ := s.Q.GetSettings(ctx)
	used := map[string]int32{}
	out := make([]map[string]any, len(rows))
	for i, q := range rows {
		from, to := rules.MonthRange(q.Day)
		key := q.UserID.String() + from.Format("2006-01")
		if _, ok := used[key]; !ok {
			c, err := s.Q.CountWFHInRange(ctx, store.CountWFHInRangeParams{UserID: q.UserID, FromDay: from, ToDay: to})
			if err == nil {
				used[key] = c.Approved
			}
		}
		out[i] = map[string]any{
			"id": q.ID, "user_id": q.UserID, "user_name": q.UserName, "kind": q.Kind, "day": ymd(q.Day),
			"reason": q.Reason, "fix_in": clockStr(q.FixIn), "fix_out": clockStr(q.FixOut),
			"status": q.Status, "is_extra": q.IsExtra, "decided_by_name": q.DecidedByName,
			"decided_at": q.DecidedAt, "decision_note": q.DecisionNote, "created_at": q.CreatedAt,
			"wfh_used": used[key], "can_decide": (u.Role == store.AppRoleHr || u.Role == store.AppRoleAdmin) && q.UserID != u.ID && q.Status == "pending",
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out, "quota": st.WfhQuota, "today": ymd(today)})
}

// POST /api/approvals/{id}/decide {approve, note}
func (s *Server) decideRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, notFound("Request not found."))
		return
	}
	in.Note = clean(in.Note, 500)
	if !in.Approve && in.Note == "" {
		s.fail(w, r, bad("Add a note saying why it is rejected."))
		return
	}
	var out store.Request
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		req, err := q.GetRequest(ctx, id)
		if err != nil {
			return notFound("Request not found.")
		}
		if req.UserID == u.ID {
			return forbidden("Someone else must decide your own request.")
		}
		status := "rejected"
		if in.Approve {
			status = "approved"
		}
		out, err = q.DecideRequest(ctx, store.DecideRequestParams{ID: id, Status: status, DecidedBy: &u.ID, DecisionNote: in.Note})
		if err != nil {
			return conflictIfNone(err, "This request was already decided.")
		}
		if in.Approve && req.Kind == "correction" {
			return s.applyCorrection(ctx, q, tx, req)
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, requestJSON(out))
}

// applyCorrection replaces the day's events with the approved times.
func (s *Server) applyCorrection(ctx context.Context, q *store.Queries, tx pgx.Tx, req store.Request) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", req.UserID.String()); err != nil {
		return err
	}
	_, loc, err := s.settings(ctx, q)
	if err != nil {
		return err
	}
	if err := q.DeleteDayEvents(ctx, store.DeleteDayEventsParams{UserID: req.UserID, Day: req.Day}); err != nil {
		return err
	}
	note := "Correction approved: " + req.Reason
	for _, ev := range []struct {
		kind string
		t    pgtype.Time
	}{{rules.ActCheckIn, req.FixIn}, {rules.ActCheckOut, req.FixOut}} {
		if _, err := q.InsertEvent(ctx, store.InsertEventParams{
			UserID: req.UserID, Day: req.Day, Kind: ev.kind, At: rules.At(req.Day, clockMin(ev.t), loc),
			Mode: "correction", Note: clean(note, 300),
		}); err != nil {
			return err
		}
	}
	return nil
}

func conflictIfNone(err error, msg string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict(msg)
	}
	return err
}
