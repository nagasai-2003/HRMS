package api

import (
	"context"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"hrms/backend/internal/rules"
	"hrms/backend/internal/store"
)

// peopleInScope lists active people the viewer may see, optionally only one team.
func (s *Server) peopleInScope(ctx context.Context, viewer store.User, teamID string) ([]store.User, map[uuid.UUID][]string, error) {
	sc, err := s.scopeOf(ctx, viewer)
	if err != nil {
		return nil, nil, err
	}
	users, err := s.Q.ListUsers(ctx)
	if err != nil {
		return nil, nil, err
	}
	mems, err := s.Q.ListAllMemberships(ctx)
	if err != nil {
		return nil, nil, err
	}
	teamsOf := map[uuid.UUID][]string{}
	inTeam := map[uuid.UUID]bool{}
	for _, m := range mems {
		teamsOf[m.UserID] = append(teamsOf[m.UserID], m.TeamName)
		if m.TeamID.String() == teamID {
			inTeam[m.UserID] = true
		}
	}
	var out []store.User
	for _, u := range users {
		if !u.Active || !sc.has(u.ID) || (teamID != "" && !inTeam[u.ID]) {
			continue
		}
		out = append(out, u)
	}
	return out, teamsOf, nil
}

// GET /api/team/people?team_id=
func (s *Server) teamPeople(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	viewer := userFrom(ctx)
	people, teamsOf, err := s.peopleInScope(ctx, viewer, r.URL.Query().Get("team_id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	teams, err := s.visibleTeams(ctx, viewer)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]map[string]any, len(people))
	for i, u := range people {
		out[i] = map[string]any{"id": u.ID, "name": u.Name, "title": u.Title, "role": u.Role, "work_policy": u.WorkPolicy, "teams": teamsOf[u.ID]}
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": out, "teams": teams})
}

// visibleTeams: every team for HR/Admin, managed teams for a manager.
func (s *Server) visibleTeams(ctx context.Context, viewer store.User) ([]map[string]any, error) {
	out := []map[string]any{}
	if viewer.Role == store.AppRoleHr || viewer.Role == store.AppRoleAdmin {
		ts, err := s.Q.ListTeams(ctx)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			out = append(out, map[string]any{"id": t.ID, "name": t.Name})
		}
		return out, nil
	}
	ts, err := s.Q.ListUserTeams(ctx, viewer.ID)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if t.IsManager {
			out = append(out, map[string]any{"id": t.ID, "name": t.Name})
		}
	}
	return out, nil
}

// GET /api/team/attendance?day=&team_id=
func (s *Server) teamAttendance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	viewer := userFrom(ctx)
	st, loc, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	day := today
	if v := r.URL.Query().Get("day"); v != "" {
		if day, err = parseDay(v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	people, teamsOf, err := s.peopleInScope(ctx, viewer, r.URL.Query().Get("team_id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids := make([]uuid.UUID, len(people))
	for i, u := range people {
		ids[i] = u.ID
	}
	evs, err := s.Q.ListEventsForDay(ctx, store.ListEventsForDayParams{Day: day, UserIds: ids})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	reqs, err := s.Q.ListApprovedRemoteInRange(ctx, store.ListApprovedRemoteInRangeParams{FromDay: day, ToDay: day, UserIds: ids})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	blocked, err := s.Q.ListBlockedForDay(ctx, store.ListBlockedForDayParams{Day: day, UserIds: ids})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	holiday := ""
	if h, err := s.Q.GetHoliday(ctx, day); err == nil {
		holiday = h.Name
	}
	names := s.officeNames(ctx)
	byUser := map[uuid.UUID][]store.AttendanceEvent{}
	for _, e := range evs {
		byUser[e.UserID] = append(byUser[e.UserID], e)
	}
	approved := map[uuid.UUID]string{}
	for _, q := range reqs {
		if approved[q.UserID] != "on_duty" {
			approved[q.UserID] = q.Kind
		}
	}
	blockedCount := map[uuid.UUID]int{}
	for _, b := range blocked {
		blockedCount[b.UserID]++
	}
	now := s.Now()
	if day.Before(today) {
		now = rules.At(day, 24*60-1, loc)
	}
	counts := map[string]int{}
	rows := make([]map[string]any, 0, len(people))
	for _, u := range people {
		mine := byUser[u.ID]
		sum := rules.Summarise(toRuleEvents(mine), now)
		mode := rules.ModeFor(rules.ModeInput{Policy: string(u.WorkPolicy), RemoteWeekdays: u.RemoteWeekdays, Day: day, ApprovedKind: approved[u.ID], HolidayName: holiday})
		status := string(sum.Phase)
		breakReason := ""
		if sum.Phase == rules.PhaseBreak && len(sum.Breaks) > 0 {
			breakReason = sum.Breaks[len(sum.Breaks)-1].Reason
		}
		// Bucket for the summary tiles.
		bucket := "not_in"
		switch {
		case sum.Phase == rules.PhaseBreak:
			bucket = "break"
		case sum.Phase == rules.PhaseOut && mode == rules.ModeHoliday:
			bucket = "holiday"
		case sum.Phase == rules.PhaseOut:
			bucket = "not_in"
		case mode == rules.ModeWFH:
			bucket = "wfh"
		case mode == rules.ModeHybridRemote || mode == rules.ModeRemote:
			bucket = "remote"
		case mode == rules.ModeOnDuty:
			bucket = "on_duty"
		default:
			bucket = "office"
		}
		counts[bucket]++
		row := map[string]any{
			"id": u.ID, "name": u.Name, "title": u.Title, "teams": teamsOf[u.ID],
			"mode": mode, "status": status, "bucket": bucket, "break_reason": breakReason,
			"first_in": sum.FirstIn, "last_out": sum.LastOut, "worked_min": sum.WorkedMin, "break_min": sum.BreakMin,
			"late":    sum.FirstIn != nil && minutesOfDay(*sum.FirstIn, loc) > clockMin(st.LateAfter),
			"blocked": blockedCount[u.ID], "auto_closed": sum.AutoClosed,
		}
		if n := len(mine); n > 0 {
			last := mine[n-1]
			row["ip"] = last.Ip
			if last.OfficeID != nil {
				row["office"] = names[*last.OfficeID]
			}
			if last.DistanceM != nil {
				row["distance_m"] = math.Round(*last.DistanceM)
			}
		}
		rows = append(rows, row)
	}
	order := map[string]int{"break": 0, "not_in": 1, "office": 2, "on_duty": 3, "wfh": 4, "remote": 5, "holiday": 6}
	slices.SortStableFunc(rows, func(a, b map[string]any) int {
		return order[a["bucket"].(string)] - order[b["bucket"].(string)]
	})
	bl := make([]map[string]any, len(blocked))
	for i, b := range blocked {
		bl[i] = map[string]any{"at": b.At, "user_id": b.UserID, "user_name": b.UserName, "action": b.Action, "code": b.Code, "message": b.Message, "ip": b.Ip}
	}
	teams, err := s.visibleTeams(ctx, viewer)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"day": ymd(day), "today": ymd(today), "holiday": holiday, "people": rows, "counts": counts,
		"blocked": bl, "teams": teams,
	})
}

type monthStats struct {
	Month        string   `json:"month"`
	DaysPresent  int      `json:"days_present"`
	AvgFirstIn   string   `json:"avg_first_in"`
	AvgLastOut   string   `json:"avg_last_out"`
	AvgWorkedMin int      `json:"avg_worked_min"`
	AvgBreakMin  int      `json:"avg_break_min"`
	LateDays     int      `json:"late_days"`
	WFHUsed      int32    `json:"wfh_used"`
	WFHQuota     int32    `json:"wfh_quota"`
	MissedOut    int      `json:"missed_checkouts"`
	LoggedMin    int      `json:"logged_min"`
	AttendedMin  int      `json:"attended_min"`
	Days         []dayRow `json:"days"`
}

func (s *Server) monthStats(ctx context.Context, u store.User, month time.Time) (monthStats, error) {
	st, loc, today, err := s.today(ctx, s.Q)
	if err != nil {
		return monthStats{}, err
	}
	from, to := rules.MonthRange(month)
	if to.After(today) {
		to = today
	}
	out := monthStats{Month: from.Format("2006-01"), WFHQuota: st.WfhQuota, Days: []dayRow{}}
	if to.Before(from) {
		return out, nil
	}
	days, err := s.daySummaries(ctx, u, from, to, loc)
	if err != nil {
		return out, err
	}
	out.Days = days
	var inSum, outSum, outN, worked, brk int
	for _, d := range days {
		if d.Summary.FirstIn == nil {
			continue
		}
		out.DaysPresent++
		inSum += minutesOfDay(*d.Summary.FirstIn, loc)
		if d.Summary.LastOut != nil && !d.Summary.AutoClosed {
			outSum += minutesOfDay(*d.Summary.LastOut, loc)
			outN++
		}
		if d.Summary.AutoClosed {
			out.MissedOut++
		}
		worked += d.Summary.WorkedMin
		brk += d.Summary.BreakMin
		if d.Late {
			out.LateDays++
		}
	}
	out.AttendedMin = worked
	if out.DaysPresent > 0 {
		out.AvgFirstIn = hhmm(inSum / out.DaysPresent)
		out.AvgWorkedMin = worked / out.DaysPresent
		out.AvgBreakMin = brk / out.DaysPresent
	}
	if outN > 0 {
		out.AvgLastOut = hhmm(outSum / outN)
	}
	cnt, err := s.Q.CountWFHInRange(ctx, store.CountWFHInRangeParams{UserID: u.ID, FromDay: from, ToDay: from.AddDate(0, 1, -1)})
	if err != nil {
		return out, err
	}
	out.WFHUsed = cnt.Approved
	var logged int
	err = s.Pool.QueryRow(ctx, `
		SELECT coalesce(sum(e.minutes), 0)::int FROM timesheet_entries e
		JOIN timesheets t ON t.id = e.timesheet_id
		WHERE t.user_id = $1 AND e.day BETWEEN $2 AND $3`, u.ID, from, to).Scan(&logged)
	out.LoggedMin = logged
	return out, err
}

func hhmm(m int) string {
	return time.Date(0, 1, 1, m/60, m%60, 0, 0, time.UTC).Format("15:04")
}

func (s *Server) monthParam(r *http.Request) (time.Time, error) {
	if v := r.URL.Query().Get("month"); v != "" {
		return parseDay(v + "-01")
	}
	_, _, today, err := s.today(r.Context(), s.Q)
	return today, err
}

// GET /api/me/summary?month=2026-11
func (s *Server) mySummary(w http.ResponseWriter, r *http.Request) {
	month, err := s.monthParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u := userFrom(r.Context())
	st, err := s.monthStats(r.Context(), u, month)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"person": map[string]any{"id": u.ID, "name": u.Name, "title": u.Title}, "stats": st})
}

// GET /api/team/people/{id}/summary?month=: the same dashboard for someone
// in the viewer's scope.
func (s *Server) personSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, notFound("Person not found."))
		return
	}
	sc, err := s.scopeOf(ctx, userFrom(ctx))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !sc.has(id) {
		s.fail(w, r, forbidden("You can only see people in your team."))
		return
	}
	u, err := s.Q.GetUser(ctx, id)
	if err != nil {
		s.fail(w, r, notFound("Person not found."))
		return
	}
	month, err := s.monthParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := s.monthStats(ctx, u, month)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"person": map[string]any{"id": u.ID, "name": u.Name, "title": u.Title}, "stats": st})
}
