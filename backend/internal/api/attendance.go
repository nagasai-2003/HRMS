package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hrms/backend/internal/rules"
	"hrms/backend/internal/store"
)

// dayInfo is everything that decides how a person's day works.
type dayInfo struct {
	Settings store.AttendanceSetting
	Loc      *time.Location
	Day      time.Time
	Mode     rules.Mode
	Holiday  string
}

func (s *Server) settings(ctx context.Context, q *store.Queries) (store.AttendanceSetting, *time.Location, error) {
	st, err := q.GetSettings(ctx)
	if err != nil {
		return st, nil, err
	}
	return st, s.loc(st.Timezone), nil
}

func (s *Server) today(ctx context.Context, q *store.Queries) (store.AttendanceSetting, *time.Location, time.Time, error) {
	st, loc, err := s.settings(ctx, q)
	if err != nil {
		return st, nil, time.Time{}, err
	}
	return st, loc, rules.DayOf(s.Now(), loc), nil
}

func (s *Server) dayFor(ctx context.Context, q *store.Queries, u store.User, day time.Time) (rules.Mode, string, error) {
	holiday := ""
	if h, err := q.GetHoliday(ctx, day); err == nil {
		holiday = h.Name
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", "", err
	}
	kind, err := q.ApprovedRemoteKind(ctx, store.ApprovedRemoteKindParams{UserID: u.ID, Day: day})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", "", err
	}
	return rules.ModeFor(rules.ModeInput{
		Policy: string(u.WorkPolicy), RemoteWeekdays: u.RemoteWeekdays, Day: day,
		ApprovedKind: kind, HolidayName: holiday,
	}), holiday, nil
}

func (s *Server) info(ctx context.Context, q *store.Queries, u store.User) (dayInfo, error) {
	st, loc, day, err := s.today(ctx, q)
	if err != nil {
		return dayInfo{}, err
	}
	mode, holiday, err := s.dayFor(ctx, q, u, day)
	return dayInfo{Settings: st, Loc: loc, Day: day, Mode: mode, Holiday: holiday}, err
}

func toRuleEvents(evs []store.AttendanceEvent) []rules.Event {
	out := make([]rules.Event, len(evs))
	for i, e := range evs {
		out[i] = rules.Event{Kind: e.Kind, At: e.At, Mode: e.Mode, Reason: e.Reason, Auto: e.Auto}
	}
	return out
}

type locIn struct {
	Lat       *float64 `json:"lat"`
	Lng       *float64 `json:"lng"`
	AccuracyM *float64 `json:"accuracy_m"`
}

func (l locIn) parse() (*rules.Location, error) {
	if l.Lat == nil || l.Lng == nil {
		return nil, nil
	}
	if *l.Lat < -90 || *l.Lat > 90 || *l.Lng < -180 || *l.Lng > 180 || math.IsNaN(*l.Lat) || math.IsNaN(*l.Lng) {
		return nil, bad("The location is not valid.")
	}
	acc := 99999.0
	if l.AccuracyM != nil && *l.AccuracyM > 0 {
		acc = *l.AccuracyM
	}
	return &rules.Location{Lat: *l.Lat, Lng: *l.Lng, AccuracyM: acc}, nil
}

func (s *Server) decide(ctx context.Context, q *store.Queries, di dayInfo, loc *rules.Location, ip netip.Addr) (rules.Result, error) {
	offs, err := q.ListOffices(ctx)
	if err != nil {
		return rules.Result{}, err
	}
	nets, err := q.ListNetworks(ctx)
	if err != nil {
		return rules.Result{}, err
	}
	in := rules.Input{
		Mode: di.Mode, HolidayName: di.Holiday, Location: loc, IP: ip,
		Rule: di.Settings.Rule, MaxAccuracy: float64(di.Settings.MaxAccuracyM),
	}
	for _, o := range offs {
		if o.Active {
			in.Offices = append(in.Offices, rules.Office{ID: o.ID, Name: o.Name, Lat: o.Lat, Lng: o.Lng, RadiusM: float64(o.RadiusM)})
		}
	}
	for _, n := range nets {
		if p, err := netip.ParsePrefix(n.Cidr); err == nil {
			in.Networks = append(in.Networks, p)
		}
	}
	return rules.Decide(in), nil
}

func resultJSON(r rules.Result) map[string]any {
	out := map[string]any{
		"allowed": r.Allowed, "code": r.Code, "message": r.Message,
		"in_fence": r.InFence, "accuracy_ok": r.AccuracyOK, "network_ok": r.NetworkOK,
		"fence_check": r.FenceCheck, "network_check": r.NetworkCheck,
	}
	if r.Office != nil {
		out["office"] = r.Office.Name
		out["radius_m"] = r.Office.RadiusM
	}
	if r.DistanceM != nil {
		out["distance_m"] = math.Round(*r.DistanceM)
	}
	return out
}

func eventJSON(e store.AttendanceEvent, offices map[uuid.UUID]string) map[string]any {
	m := map[string]any{
		"id": e.ID, "kind": e.Kind, "at": e.At, "mode": e.Mode, "reason": e.Reason, "note": e.Note,
		"ip": e.Ip, "auto": e.Auto, "accuracy_m": e.AccuracyM,
	}
	if e.OfficeID != nil {
		m["office"] = offices[*e.OfficeID]
	}
	if e.DistanceM != nil {
		m["distance_m"] = math.Round(*e.DistanceM)
	}
	return m
}

func (s *Server) officeNames(ctx context.Context) map[uuid.UUID]string {
	offs, _ := s.Q.ListOffices(ctx)
	m := map[uuid.UUID]string{}
	for _, o := range offs {
		m[o.ID] = o.Name
	}
	return m
}

// GET /api/attendance/today
func (s *Server) attendanceToday(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	di, err := s.info(ctx, s.Q, u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	evs, err := s.Q.ListEventsRange(ctx, store.ListEventsRangeParams{UserID: u.ID, FromDay: di.Day, ToDay: di.Day})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	reasons, err := s.Q.ListBreakReasons(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	blocked, err := s.Q.ListMyBlocked(ctx, store.ListMyBlockedParams{UserID: u.ID, Day: di.Day})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	offs, err := s.Q.ListOffices(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	names := map[uuid.UUID]string{}
	var offices []map[string]any
	for _, o := range offs {
		names[o.ID] = o.Name
		if o.Active {
			offices = append(offices, map[string]any{"name": o.Name, "lat": o.Lat, "lng": o.Lng, "radius_m": o.RadiusM})
		}
	}
	evJSON := make([]map[string]any, len(evs))
	for i, e := range evs {
		evJSON[i] = eventJSON(e, names)
	}
	labels := make([]string, len(reasons))
	for i, b := range reasons {
		labels[i] = b.Label
	}
	blockedJSON := make([]map[string]any, len(blocked))
	for i, b := range blocked {
		blockedJSON[i] = map[string]any{"at": b.At, "action": b.Action, "code": b.Code, "message": b.Message}
	}
	sum := rules.Summarise(toRuleEvents(evs), s.Now())
	late := sum.FirstIn != nil && minutesOfDay(*sum.FirstIn, di.Loc) > clockMin(di.Settings.LateAfter)
	ip := s.IP.IP(r)
	nets, _ := s.Q.ListNetworks(ctx)
	netOK := false
	for _, n := range nets {
		if p, err := netip.ParsePrefix(n.Cidr); err == nil && ip.IsValid() && p.Contains(ip.Unmap()) {
			netOK = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"day": ymd(di.Day), "mode": di.Mode, "holiday": di.Holiday, "remote": di.Mode.Remote(),
		"summary": sum, "late": late, "events": evJSON, "blocked": blockedJSON,
		"break_reasons": labels, "offices": offices,
		"rule": di.Settings.Rule, "max_accuracy_m": di.Settings.MaxAccuracyM,
		"shift_start": clockStr(di.Settings.ShiftStart), "late_after": clockStr(di.Settings.LateAfter),
		"auto_checkout": clockStr(di.Settings.AutoCheckout),
		"ip":            ip.String(), "network_ok": netOK,
	})
}

func minutesOfDay(t time.Time, loc *time.Location) int {
	l := t.In(loc)
	return l.Hour()*60 + l.Minute()
}

// POST /api/attendance/check: runs the checks without recording anything,
// so the screen can show what will happen before the button is pressed.
func (s *Server) attendanceCheck(w http.ResponseWriter, r *http.Request) {
	var in locIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	loc, err := in.parse()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	di, err := s.info(r.Context(), s.Q, userFrom(r.Context()))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.decide(r.Context(), s.Q, di, loc, s.IP.IP(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resultJSON(res))
}

var phaseHelp = map[rules.Phase]string{
	rules.PhaseOut:   "Check in first.",
	rules.PhaseIn:    "You are already checked in.",
	rules.PhaseBreak: "You are on a break. End the break first.",
	rules.PhaseDone:  "You have checked out. Check in again to continue.",
}

// POST /api/attendance/action
func (s *Server) attendanceAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		locIn
		Action string `json:"action"`
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if !slices.Contains([]string{rules.ActCheckIn, rules.ActBreakStart, rules.ActBreakEnd, rules.ActCheckOut}, in.Action) {
		s.fail(w, r, bad("Unknown action."))
		return
	}
	loc, err := in.locIn.parse()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	ip := s.IP.IP(r)
	in.Note = clean(in.Note, 300)

	var blockedRes *rules.Result
	var di dayInfo
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		// One action at a time per person, so a double click cannot check in twice.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", u.ID.String()); err != nil {
			return err
		}
		var err error
		di, err = s.info(ctx, q, u)
		if err != nil {
			return err
		}
		evs, err := q.ListEventsRange(ctx, store.ListEventsRangeParams{UserID: u.ID, FromDay: di.Day, ToDay: di.Day})
		if err != nil {
			return err
		}
		sum := rules.Summarise(toRuleEvents(evs), s.Now())
		if !rules.Allowed(sum.Phase, in.Action) {
			return conflict(phaseHelp[sum.Phase])
		}
		if in.Action == rules.ActBreakStart {
			reasons, err := q.ListBreakReasons(ctx)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(reasons, func(b store.BreakReason) bool { return b.Label == in.Reason }) {
				return bad("Pick a reason for the break.")
			}
			if in.Reason == "Other" && in.Note == "" {
				return bad("Add a short note for an \"Other\" break.")
			}
		} else {
			in.Reason = ""
		}

		res, err := s.decide(ctx, q, di, loc, ip)
		if err != nil {
			return err
		}
		if !res.Allowed {
			blockedRes = &res
			return nil
		}
		p := store.InsertEventParams{
			UserID: u.ID, Day: di.Day, Kind: in.Action, At: s.Now(), Mode: string(di.Mode),
			Reason: in.Reason, Note: in.Note, Ip: ip.String(), DistanceM: res.DistanceM,
		}
		if loc != nil {
			p.Lat, p.Lng, p.AccuracyM = &loc.Lat, &loc.Lng, &loc.AccuracyM
		}
		if res.Office != nil && res.InFence {
			id := res.Office.ID
			p.OfficeID = &id
		}
		_, err = q.InsertEvent(ctx, p)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if blockedRes != nil {
		// Logged outside the transaction above so the record survives.
		p := store.InsertBlockedParams{
			UserID: u.ID, Day: di.Day, Action: in.Action, Code: blockedRes.Code, Message: blockedRes.Message,
			Ip: ip.String(), DistanceM: blockedRes.DistanceM,
		}
		if loc != nil {
			p.Lat, p.Lng, p.AccuracyM = &loc.Lat, &loc.Lng, &loc.AccuracyM
		}
		if _, err := s.Q.InsertBlocked(ctx, p); err != nil {
			s.Log.Error("log blocked attempt", "err", err)
		}
		body := resultJSON(*blockedRes)
		body["error"] = blockedRes.Message
		writeJSON(w, http.StatusForbidden, body)
		return
	}
	s.attendanceToday(w, r)
}

// GET /api/attendance/history?from=&to= (max 62 days)
func (s *Server) attendanceHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	_, loc, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	from, to := today.AddDate(0, 0, -13), today
	if v := r.URL.Query().Get("from"); v != "" {
		if from, err = parseDay(v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if to, err = parseDay(v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if to.Before(from) || to.Sub(from) > 62*24*time.Hour {
		s.fail(w, r, bad("Pick up to 62 days."))
		return
	}
	days, err := s.daySummaries(ctx, u, from, to, loc)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days})
}

type dayRow struct {
	Day     string           `json:"day"`
	Mode    rules.Mode       `json:"mode"`
	Holiday string           `json:"holiday"`
	Summary rules.DaySummary `json:"summary"`
	Late    bool             `json:"late"`
}

// daySummaries returns one row per day from..to for a person, newest first.
func (s *Server) daySummaries(ctx context.Context, u store.User, from, to time.Time, loc *time.Location) ([]dayRow, error) {
	st, err := s.Q.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	evs, err := s.Q.ListEventsRange(ctx, store.ListEventsRangeParams{UserID: u.ID, FromDay: from, ToDay: to})
	if err != nil {
		return nil, err
	}
	hols, err := s.Q.ListHolidays(ctx, store.ListHolidaysParams{FromDay: from, ToDay: to})
	if err != nil {
		return nil, err
	}
	reqs, err := s.Q.ListApprovedRemoteInRange(ctx, store.ListApprovedRemoteInRangeParams{FromDay: from, ToDay: to, UserIds: []uuid.UUID{u.ID}})
	if err != nil {
		return nil, err
	}
	byDay := map[string][]rules.Event{}
	for _, e := range evs {
		k := ymd(e.Day)
		byDay[k] = append(byDay[k], rules.Event{Kind: e.Kind, At: e.At, Mode: e.Mode, Reason: e.Reason, Auto: e.Auto})
	}
	hol := map[string]string{}
	for _, h := range hols {
		hol[ymd(h.Day)] = h.Name
	}
	approved := map[string]string{}
	for _, q := range reqs {
		k := ymd(q.Day)
		if approved[k] != "on_duty" {
			approved[k] = q.Kind
		}
	}
	now := s.Now()
	var out []dayRow
	for d := to; !d.Before(from); d = d.AddDate(0, 0, -1) {
		k := ymd(d)
		sum := rules.Summarise(byDay[k], now)
		out = append(out, dayRow{
			Day: k, Holiday: hol[k], Summary: sum,
			Mode: rules.ModeFor(rules.ModeInput{Policy: string(u.WorkPolicy), RemoteWeekdays: u.RemoteWeekdays, Day: d, ApprovedKind: approved[k], HolidayName: hol[k]}),
			Late: sum.FirstIn != nil && minutesOfDay(*sum.FirstIn, loc) > clockMin(st.LateAfter),
		})
	}
	return out, nil
}
