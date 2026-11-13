package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hrms/backend/internal/rules"
	"hrms/backend/internal/store"
)

// Timesheets are weekly. Each day shows the hours the person logged next to
// the hours their check-in and check-out add up to, so a reviewer can spot
// a sheet that claims more than the person was at work.

// mismatchTolerance: logged hours may exceed attended hours by this much
// before the day is flagged.
const mismatchTolerance = 30 // minutes

type tsDay struct {
	Day         string     `json:"day"`
	AttendedMin int        `json:"attended_min"`
	LoggedMin   int        `json:"logged_min"`
	Mode        rules.Mode `json:"mode"`
	Holiday     string     `json:"holiday"`
	Flag        string     `json:"flag"` // "", "over" (logged > attended) or "no_attendance"
	Future      bool       `json:"future"`
}

func entryJSON(e store.ListEntriesRow) map[string]any {
	return map[string]any{
		"id": e.ID, "day": ymd(e.Day), "project_id": e.ProjectID, "project_code": e.ProjectCode,
		"project_name": e.ProjectName, "task_ref": e.TaskRef, "minutes": e.Minutes, "note": e.Note,
	}
}

func (s *Server) loadSheetUser(ctx context.Context, viewer store.User, userParam string) (store.User, bool, error) {
	if userParam == "" || userParam == viewer.ID.String() {
		return viewer, false, nil
	}
	id, err := uuid.Parse(userParam)
	if err != nil {
		return store.User{}, false, notFound("Person not found.")
	}
	sc, err := s.scopeOf(ctx, viewer)
	if err != nil {
		return store.User{}, false, err
	}
	if !sc.has(id) {
		return store.User{}, false, forbidden("You can only see timesheets of your team.")
	}
	u, err := s.Q.GetUser(ctx, id)
	if err != nil {
		return store.User{}, false, notFound("Person not found.")
	}
	return u, true, nil
}

// canDecideSheet: not your own; HR/Admin for anyone; a manager for their teams.
func (s *Server) canDecideSheet(ctx context.Context, viewer store.User, owner uuid.UUID) (bool, error) {
	if owner == viewer.ID {
		return false, nil
	}
	switch viewer.Role {
	case store.AppRoleHr, store.AppRoleAdmin:
		return true, nil
	case store.AppRoleManager:
		sc, err := s.scopeOf(ctx, viewer)
		return sc.has(owner), err
	}
	return false, nil
}

// GET /api/timesheets/week?start=YYYY-MM-DD&user_id=
func (s *Server) timesheetWeek(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	viewer := userFrom(ctx)
	owner, other, err := s.loadSheetUser(ctx, viewer, r.URL.Query().Get("user_id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, loc, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	start := rules.WeekStart(today)
	if v := r.URL.Query().Get("start"); v != "" {
		d, err := parseDay(v)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		start = rules.WeekStart(d)
	}
	end := start.AddDate(0, 0, 6)

	sheet, err := s.Q.FindTimesheet(ctx, store.FindTimesheetParams{UserID: owner.ID, WeekStart: start})
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.fail(w, r, err)
		return
	}
	entries := []store.ListEntriesRow{}
	if exists {
		if entries, err = s.Q.ListEntries(ctx, sheet.ID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	days, err := s.weekDays(ctx, owner, start, today, loc, entries)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	projects, err := s.Q.ListProjects(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var active []map[string]any
	for _, p := range projects {
		if p.Active || slices.ContainsFunc(entries, func(e store.ListEntriesRow) bool { return e.ProjectID == p.ID }) {
			active = append(active, map[string]any{"id": p.ID, "code": p.Code, "name": p.Name, "active": p.Active})
		}
	}
	ej := make([]map[string]any, len(entries))
	for i, e := range entries {
		ej[i] = entryJSON(e)
	}
	status := "draft"
	var sheetJSON map[string]any
	if exists {
		status = sheet.Status
		decidedBy := ""
		if sheet.DecidedBy != nil {
			if d, err := s.Q.GetUser(ctx, *sheet.DecidedBy); err == nil {
				decidedBy = d.Name
			}
		}
		sheetJSON = map[string]any{
			"id": sheet.ID, "status": sheet.Status, "submitted_at": sheet.SubmittedAt,
			"decided_at": sheet.DecidedAt, "decided_by_name": decidedBy, "decision_note": sheet.DecisionNote,
		}
	}
	canDecide := false
	if exists && status == "submitted" {
		if canDecide, err = s.canDecideSheet(ctx, viewer, owner.ID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"week_start": ymd(start), "week_end": ymd(end), "today": ymd(today),
		"person": map[string]any{"id": owner.ID, "name": owner.Name, "title": owner.Title},
		"status": status, "sheet": sheetJSON, "entries": ej, "days": days, "projects": active,
		"can_edit":   !other && (status == "draft" || status == "rejected"),
		"can_submit": !other && (status == "draft" || status == "rejected") && len(entries) > 0,
		"can_recall": !other && status == "submitted",
		"can_decide": canDecide,
	})
}

func (s *Server) weekDays(ctx context.Context, u store.User, start, today time.Time, loc *time.Location, entries []store.ListEntriesRow) ([]tsDay, error) {
	end := start.AddDate(0, 0, 6)
	rows, err := s.daySummaries(ctx, u, start, end, loc)
	if err != nil {
		return nil, err
	}
	logged := map[string]int{}
	for _, e := range entries {
		logged[ymd(e.Day)] += int(e.Minutes)
	}
	out := make([]tsDay, 0, 7)
	for i := len(rows) - 1; i >= 0; i-- { // daySummaries is newest first
		d := rows[i]
		day := tsDay{Day: d.Day, AttendedMin: d.Summary.WorkedMin, LoggedMin: logged[d.Day], Mode: d.Mode, Holiday: d.Holiday}
		dt, _ := parseDay(d.Day)
		day.Future = dt.After(today)
		switch {
		case day.LoggedMin > 0 && day.AttendedMin == 0:
			day.Flag = "no_attendance"
		case day.LoggedMin > day.AttendedMin+mismatchTolerance:
			day.Flag = "over"
		}
		out = append(out, day)
	}
	return out, nil
}

// PUT /api/timesheets/week {start, entries}: replaces the week's entries.
func (s *Server) saveTimesheetWeek(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Start   string `json:"start"`
		Entries []struct {
			Day       string    `json:"day"`
			ProjectID uuid.UUID `json:"project_id"`
			TaskRef   string    `json:"task_ref"`
			Minutes   int32     `json:"minutes"`
			Note      string    `json:"note"`
		} `json:"entries"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	start, err := parseDay(in.Start)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if rules.WeekStart(start) != start {
		s.fail(w, r, bad("A week starts on Monday."))
		return
	}
	if len(in.Entries) > 200 {
		s.fail(w, r, bad("Too many rows for one week."))
		return
	}
	_, _, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if start.After(today) {
		s.fail(w, r, bad("This week has not started yet."))
		return
	}
	projects, err := s.Q.ListProjects(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	end := start.AddDate(0, 0, 6)
	perDay := map[string]int32{}
	for _, e := range in.Entries {
		d, err := parseDay(e.Day)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if d.Before(start) || d.After(end) {
			s.fail(w, r, bad("Every row must be inside this week."))
			return
		}
		if d.After(today) {
			s.fail(w, r, bad("You cannot log time for a day that has not come yet."))
			return
		}
		if e.Minutes < 1 || e.Minutes > 24*60 {
			s.fail(w, r, bad("Each row needs between 1 minute and 24 hours."))
			return
		}
		if !slices.ContainsFunc(projects, func(p store.Project) bool { return p.ID == e.ProjectID }) {
			s.fail(w, r, bad("Pick a project for every row."))
			return
		}
		perDay[e.Day] += e.Minutes
		if perDay[e.Day] > 24*60 {
			s.fail(w, r, bad("A day cannot have more than 24 hours ("+e.Day+")."))
			return
		}
	}
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		sheet, err := q.EnsureTimesheet(ctx, store.EnsureTimesheetParams{UserID: u.ID, WeekStart: start})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM timesheets WHERE id = $1 FOR UPDATE", sheet.ID); err != nil {
			return err
		}
		if sheet, err = q.GetTimesheet(ctx, sheet.ID); err != nil {
			return err
		}
		if sheet.Status != "draft" && sheet.Status != "rejected" {
			return conflict("This timesheet is " + sheet.Status + ". Recall it before editing.")
		}
		if err := q.DeleteEntries(ctx, sheet.ID); err != nil {
			return err
		}
		for i, e := range in.Entries {
			d, _ := parseDay(e.Day)
			if err := q.InsertEntry(ctx, store.InsertEntryParams{
				TimesheetID: sheet.ID, Day: d, ProjectID: e.ProjectID,
				TaskRef: clean(e.TaskRef, 40), Minutes: e.Minutes, Note: clean(e.Note, 300), Sort: int32(i),
			}); err != nil {
				return err
			}
		}
		return q.TouchTimesheet(ctx, sheet.ID)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	q.Set("start", in.Start)
	r.URL.RawQuery = q.Encode()
	s.timesheetWeek(w, r)
}

func (s *Server) ownSheet(r *http.Request) (store.Timesheet, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return store.Timesheet{}, notFound("Timesheet not found.")
	}
	t, err := s.Q.GetTimesheet(r.Context(), id)
	if err != nil || t.UserID != userFrom(r.Context()).ID {
		return store.Timesheet{}, notFound("Timesheet not found.")
	}
	return t, nil
}

func (s *Server) respondWeek(w http.ResponseWriter, r *http.Request, t store.Timesheet, userID string) {
	q := r.URL.Query()
	q.Set("start", ymd(t.WeekStart))
	if userID != "" {
		q.Set("user_id", userID)
	}
	r.URL.RawQuery = q.Encode()
	s.timesheetWeek(w, r)
}

// POST /api/timesheets/{id}/submit
func (s *Server) submitTimesheet(w http.ResponseWriter, r *http.Request) {
	t, err := s.ownSheet(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	entries, err := s.Q.ListEntries(r.Context(), t.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(entries) == 0 {
		s.fail(w, r, bad("Add at least one row before submitting."))
		return
	}
	if t, err = s.Q.SubmitTimesheet(r.Context(), t.ID); err != nil {
		s.fail(w, r, conflictIfNone(err, "This timesheet was already submitted."))
		return
	}
	s.respondWeek(w, r, t, "")
}

// POST /api/timesheets/{id}/recall: back to draft before anyone decides.
func (s *Server) recallTimesheet(w http.ResponseWriter, r *http.Request) {
	t, err := s.ownSheet(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if t, err = s.Q.RecallTimesheet(r.Context(), t.ID); err != nil {
		s.fail(w, r, conflictIfNone(err, "Only a submitted timesheet can be recalled."))
		return
	}
	s.respondWeek(w, r, t, "")
}

// GET /api/timesheets: my recent weeks.
func (s *Server) myTimesheets(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Q.ListMyTimesheets(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]map[string]any, len(rows))
	for i, t := range rows {
		out[i] = map[string]any{"id": t.ID, "week_start": ymd(t.WeekStart), "status": t.Status, "total_minutes": t.TotalMinutes, "decision_note": t.DecisionNote}
	}
	writeJSON(w, http.StatusOK, map[string]any{"timesheets": out})
}

// GET /api/timesheets/review?status=submitted
func (s *Server) reviewTimesheets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	viewer := userFrom(ctx)
	sc, err := s.scopeOf(ctx, viewer)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var status *string
	if v := r.URL.Query().Get("status"); v != "" && v != "all" {
		status = &v
	}
	rows, err := s.Q.ListTimesheetsForReview(ctx, store.ListTimesheetsForReviewParams{Status: status, UserIds: sc.list()})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, loc, _, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, t := range rows {
		if t.UserID == viewer.ID {
			continue // your own sheet is on the Timesheets page
		}
		attended := 0
		owner, err := s.Q.GetUser(ctx, t.UserID)
		if err == nil {
			days, err := s.daySummaries(ctx, owner, t.WeekStart, t.WeekStart.AddDate(0, 0, 6), loc)
			if err == nil {
				for _, d := range days {
					attended += d.Summary.WorkedMin
				}
			}
		}
		out = append(out, map[string]any{
			"id": t.ID, "user_id": t.UserID, "user_name": t.UserName, "week_start": ymd(t.WeekStart),
			"status": t.Status, "total_minutes": t.TotalMinutes, "attended_minutes": attended,
			"submitted_at": t.SubmittedAt, "decision_note": t.DecisionNote,
			"can_decide": t.Status == "submitted",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"timesheets": out})
}

// POST /api/timesheets/{id}/decide {approve, note}
func (s *Server) decideTimesheet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	viewer := userFrom(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, notFound("Timesheet not found."))
		return
	}
	t, err := s.Q.GetTimesheet(ctx, id)
	if err != nil {
		s.fail(w, r, notFound("Timesheet not found."))
		return
	}
	ok, err := s.canDecideSheet(ctx, viewer, t.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.fail(w, r, forbidden("You cannot decide this timesheet."))
		return
	}
	in.Note = clean(in.Note, 500)
	if !in.Approve && in.Note == "" {
		s.fail(w, r, bad("Add a note saying what to fix."))
		return
	}
	status := "rejected"
	if in.Approve {
		status = "approved"
	}
	if t, err = s.Q.DecideTimesheet(ctx, store.DecideTimesheetParams{ID: id, Status: status, DecidedBy: &viewer.ID, DecisionNote: in.Note}); err != nil {
		s.fail(w, r, conflictIfNone(err, "This timesheet is not waiting for a decision."))
		return
	}
	s.respondWeek(w, r, t, t.UserID.String())
}
