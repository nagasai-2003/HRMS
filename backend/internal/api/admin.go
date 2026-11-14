package api

import (
	"errors"
	"net/http"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hrms/backend/internal/boards"
	"hrms/backend/internal/chattext"
	"hrms/backend/internal/clientip"
	"hrms/backend/internal/live"
	"hrms/backend/internal/store"
)

func uniqueAs(err error, as error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return as
	}
	return err
}

func urlID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return id, notFound("Not found.")
	}
	return id, nil
}

func personJSON(u store.User) map[string]any {
	return map[string]any{
		"id": u.ID, "email": u.Email, "name": u.Name, "title": u.Title, "role": u.Role,
		"work_policy": u.WorkPolicy, "remote_weekdays": u.RemoteWeekdays, "office_id": u.OfficeID,
		"active": u.Active, "created_at": u.CreatedAt,
	}
}

// GET /api/admin/people: people, teams with members, offices.
func (s *Server) adminPeople(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	users, err := s.Q.ListUsers(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	teams, err := s.Q.ListTeams(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	mems, err := s.Q.ListAllMemberships(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	offs, err := s.Q.ListOffices(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	people := make([]map[string]any, len(users))
	for i, u := range users {
		people[i] = personJSON(u)
	}
	members := map[string][]map[string]any{}
	for _, m := range mems {
		k := m.TeamID.String()
		members[k] = append(members[k], map[string]any{"user_id": m.UserID, "is_manager": m.IsManager})
	}
	ts := make([]map[string]any, len(teams))
	for i, t := range teams {
		ms := members[t.ID.String()]
		if ms == nil {
			ms = []map[string]any{}
		}
		ts[i] = map[string]any{"id": t.ID, "name": t.Name, "key": t.Key, "members": ms, "managers": t.Managers}
	}
	offices := make([]map[string]any, len(offs))
	for i, o := range offs {
		offices[i] = map[string]any{"id": o.ID, "name": o.Name}
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people, "teams": ts, "offices": offices})
}

type personIn struct {
	Email          string     `json:"email"`
	Name           string     `json:"name"`
	Title          string     `json:"title"`
	Role           string     `json:"role"`
	WorkPolicy     string     `json:"work_policy"`
	RemoteWeekdays []int16    `json:"remote_weekdays"`
	OfficeID       *uuid.UUID `json:"office_id"`
	Active         *bool      `json:"active"`
	Password       string     `json:"password"`
}

func (p *personIn) check() error {
	p.Name = clean(p.Name, 120)
	p.Title = clean(p.Title, 120)
	if p.Name == "" {
		return bad("Add a name.")
	}
	if !slices.Contains([]string{"employee", "manager", "hr", "admin"}, p.Role) {
		return bad("Pick a role.")
	}
	if !slices.Contains([]string{"office", "hybrid", "remote"}, p.WorkPolicy) {
		return bad("Pick a work policy.")
	}
	if p.RemoteWeekdays == nil {
		p.RemoteWeekdays = []int16{}
	}
	for _, d := range p.RemoteWeekdays {
		if d < 1 || d > 7 {
			return bad("Remote days must be weekdays 1 to 7.")
		}
	}
	slices.Sort(p.RemoteWeekdays)
	p.RemoteWeekdays = slices.Compact(p.RemoteWeekdays)
	if p.WorkPolicy == "hybrid" && len(p.RemoteWeekdays) == 0 {
		return bad("Pick at least one remote day for a hybrid policy.")
	}
	if p.WorkPolicy != "hybrid" {
		p.RemoteWeekdays = []int16{}
	}
	if p.Password != "" && len(p.Password) < 10 {
		return bad("Passwords need at least 10 characters.")
	}
	return nil
}

// POST /api/admin/people
func (s *Server) adminCreatePerson(w http.ResponseWriter, r *http.Request) {
	var in personIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.check(); err != nil {
		s.fail(w, r, err)
		return
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(in.Email))
	if err != nil {
		s.fail(w, r, bad("The email is not valid."))
		return
	}
	if in.Password == "" {
		s.fail(w, r, bad("Set a first password (at least 10 characters)."))
		return
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.Q.CreateUser(r.Context(), store.CreateUserParams{
		Email: strings.ToLower(addr.Address), Name: in.Name, Title: in.Title, PasswordHash: hash,
		Role: store.AppRole(in.Role), WorkPolicy: store.WorkPolicy(in.WorkPolicy),
		RemoteWeekdays: in.RemoteWeekdays, OfficeID: in.OfficeID,
	})
	if err != nil {
		s.fail(w, r, uniqueAs(err, conflict("Someone already has that email.")))
		return
	}
	writeJSON(w, http.StatusCreated, personJSON(u))
}

// PATCH /api/admin/people/{id}
func (s *Server) adminUpdatePerson(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in personIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.check(); err != nil {
		s.fail(w, r, err)
		return
	}
	me := userFrom(ctx)
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	if id == me.ID && (in.Role != "admin" || !active) {
		s.fail(w, r, bad("You cannot remove your own admin access."))
		return
	}
	var out store.User
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		var err error
		out, err = q.UpdateUser(ctx, store.UpdateUserParams{
			ID: id, Name: in.Name, Title: in.Title, Role: store.AppRole(in.Role),
			WorkPolicy: store.WorkPolicy(in.WorkPolicy), RemoteWeekdays: in.RemoteWeekdays,
			OfficeID: in.OfficeID, Active: active,
		})
		if err != nil {
			return err
		}
		if in.Password != "" {
			hash, err := HashPassword(in.Password)
			if err != nil {
				return err
			}
			if err := q.SetPassword(ctx, store.SetPasswordParams{ID: id, PasswordHash: hash}); err != nil {
				return err
			}
		}
		if !active || in.Password != "" {
			return q.DeleteUserSessions(ctx, id)
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, personJSON(out))
}

type teamIn struct {
	Name    string `json:"name"`
	Key     string `json:"key"`
	derived bool
}

// readTeam: a name, and a key for task numbers (suggested from the name
// when left empty).
func readTeam(r *http.Request) (teamIn, error) {
	var in teamIn
	if err := readJSON(r, &in); err != nil {
		return in, err
	}
	in.Name = clean(in.Name, 80)
	if in.Name == "" {
		return in, bad("Add a team name.")
	}
	in.Key = strings.ToUpper(strings.TrimSpace(in.Key))
	if in.Key == "" {
		in.Key, in.derived = boards.DeriveKey(in.Name), true
	}
	if !boards.ValidKey(in.Key) {
		return in, bad("Add a short key for task numbers: 2 to 6 capital letters or digits, starting with a letter (like FE).")
	}
	return in, nil
}

// teamTaken says which of the team's unique values is already used.
func teamTaken(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		if pe.ConstraintName == "teams_key_unique" {
			return conflict("Another team already uses that key.")
		}
		return conflict("A team with that name exists.")
	}
	return err
}

// A new team starts with the default status groups.
func (s *Server) adminCreateTeam(w http.ResponseWriter, r *http.Request) {
	in, err := readTeam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	if in.derived {
		// A suggested key that is taken becomes RS2, RS3 and so on.
		base := in.Key
		for n := 2; n <= 99; n++ {
			taken, err := s.Q.TeamKeyTaken(ctx, in.Key)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			if !taken {
				break
			}
			suffix := strconv.Itoa(n)
			in.Key = base[:min(len(base), 6-len(suffix))] + suffix
		}
	}
	var t store.Team
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		if t, err = q.CreateTeam(ctx, store.CreateTeamParams{Name: in.Name, Key: in.Key}); err != nil {
			return teamTaken(err)
		}
		if _, err := q.CreateTeamChannel(ctx, store.CreateTeamChannelParams{
			TeamID: &t.ID, Name: chattext.DefaultChannel(t.Name), IsDefault: true,
		}); err != nil {
			return err
		}
		return createStatuses(ctx, q, t.ID, boards.Templates["default"])
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// PATCH /api/admin/teams/{id} {name, key}. Changing the key renumbers how
// every task shows (FE-142 → WEB-142); the numbers stay.
func (s *Server) adminRenameTeam(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	in, err := readTeam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Q.UpdateTeam(r.Context(), store.UpdateTeamParams{ID: id, Name: in.Name, Key: in.Key}); err != nil {
		s.fail(w, r, teamTaken(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminDeleteTeam(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Q.DeleteTeam(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PUT /api/admin/teams/{id}/members/{userID} {is_manager}
// Making an employee a team manager also gives them the manager role.
func (s *Server) adminPutMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	teamID, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	userID, err := urlID(r, "userID")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		IsManager bool `json:"is_manager"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	joined := false
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		u, err := q.GetUser(ctx, userID)
		if err != nil {
			return notFound("Person not found.")
		}
		if _, err := q.GetMembership(ctx, store.GetMembershipParams{TeamID: teamID, UserID: userID}); errors.Is(err, pgx.ErrNoRows) {
			// New members start with the team's chat history marked read.
			joined = true
			if err := q.MarkTeamChannelsRead(ctx, store.MarkTeamChannelsReadParams{UserID: userID, TeamID: &teamID}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := q.UpsertTeamMember(ctx, store.UpsertTeamMemberParams{TeamID: teamID, UserID: userID, IsManager: in.IsManager}); err != nil {
			return err
		}
		if in.IsManager && u.Role == store.AppRoleEmployee {
			return q.SetUserRole(ctx, store.SetUserRoleParams{ID: userID, Role: store.AppRoleManager})
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if joined {
		s.Live.Send(ctx, []uuid.UUID{userID}, live.Event{Type: "channel.changed", Data: map[string]any{"team_id": teamID}})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminRemoveMember(w http.ResponseWriter, r *http.Request) {
	teamID, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	userID, err := urlID(r, "userID")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Q.RemoveTeamMember(r.Context(), store.RemoveTeamMemberParams{TeamID: teamID, UserID: userID}); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/admin/rules: settings, offices and networks in one go.
func (s *Server) adminRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, err := s.Q.GetSettings(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	offs, err := s.Q.ListOffices(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	nets, err := s.Q.ListNetworks(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": map[string]any{
			"rule": st.Rule, "max_accuracy_m": st.MaxAccuracyM, "shift_start": clockStr(st.ShiftStart),
			"late_after": clockStr(st.LateAfter), "auto_checkout": clockStr(st.AutoCheckout),
			"wfh_quota": st.WfhQuota, "timezone": st.Timezone,
		},
		"offices": offs, "networks": nets, "your_ip": s.IP.IP(r).String(),
	})
}

// PUT /api/admin/rules
func (s *Server) adminSaveRules(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rule         string `json:"rule"`
		MaxAccuracyM int32  `json:"max_accuracy_m"`
		ShiftStart   string `json:"shift_start"`
		LateAfter    string `json:"late_after"`
		AutoCheckout string `json:"auto_checkout"`
		WfhQuota     int32  `json:"wfh_quota"`
		Timezone     string `json:"timezone"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Rule != "both" && in.Rule != "either" {
		s.fail(w, r, bad("Pick both or either."))
		return
	}
	if in.MaxAccuracyM < 10 || in.MaxAccuracyM > 2000 {
		s.fail(w, r, bad("Accuracy must be 10 to 2000 metres."))
		return
	}
	if in.WfhQuota < 0 || in.WfhQuota > 31 {
		s.fail(w, r, bad("The WFH quota must be 0 to 31 days."))
		return
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil || in.Timezone == "" {
		s.fail(w, r, bad("Unknown time zone."))
		return
	}
	ss, err1 := parseClock(in.ShiftStart)
	la, err2 := parseClock(in.LateAfter)
	ac, err3 := parseClock(in.AutoCheckout)
	if err := errors.Join(err1, err2, err3); err != nil {
		s.fail(w, r, bad("Times must look like 09:30."))
		return
	}
	if _, err := s.Q.UpdateSettings(r.Context(), store.UpdateSettingsParams{
		Rule: in.Rule, MaxAccuracyM: in.MaxAccuracyM, ShiftStart: ss, LateAfter: la,
		AutoCheckout: ac, WfhQuota: in.WfhQuota, Timezone: in.Timezone,
	}); err != nil {
		s.fail(w, r, err)
		return
	}
	s.adminRules(w, r)
}

type officeIn struct {
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	RadiusM int32   `json:"radius_m"`
	Active  *bool   `json:"active"`
}

func (o *officeIn) check() error {
	o.Name = clean(o.Name, 80)
	if o.Name == "" {
		return bad("Add an office name.")
	}
	if o.Lat < -90 || o.Lat > 90 || o.Lng < -180 || o.Lng > 180 {
		return bad("Latitude or longitude is out of range.")
	}
	if o.RadiusM < 20 || o.RadiusM > 5000 {
		return bad("The radius must be 20 to 5000 metres.")
	}
	return nil
}

func (s *Server) adminCreateOffice(w http.ResponseWriter, r *http.Request) {
	var in officeIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.check(); err != nil {
		s.fail(w, r, err)
		return
	}
	o, err := s.Q.CreateOffice(r.Context(), store.CreateOfficeParams{Name: in.Name, Lat: in.Lat, Lng: in.Lng, RadiusM: in.RadiusM})
	if err != nil {
		s.fail(w, r, uniqueAs(err, conflict("An office with that name exists.")))
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) adminUpdateOffice(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in officeIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.check(); err != nil {
		s.fail(w, r, err)
		return
	}
	active := in.Active == nil || *in.Active
	o, err := s.Q.UpdateOffice(r.Context(), store.UpdateOfficeParams{ID: id, Name: in.Name, Lat: in.Lat, Lng: in.Lng, RadiusM: in.RadiusM, Active: active})
	if err != nil {
		s.fail(w, r, uniqueAs(err, conflict("An office with that name exists.")))
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) adminDeleteOffice(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Q.DeleteOffice(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminCreateNetwork(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Cidr  string `json:"cidr"`
		Label string `json:"label"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ps, err := clientip.Parse(in.Cidr)
	if err != nil || len(ps) != 1 {
		s.fail(w, r, bad("Enter one IP address (49.205.112.18) or range (183.82.96.0/28)."))
		return
	}
	p := ps[0]
	if p.Bits() < 8 && p.Addr().Is4() || p.Bits() < 32 && p.Addr().Is6() {
		s.fail(w, r, bad("That range is too wide to be an office network."))
		return
	}
	n, err := s.Q.CreateNetwork(r.Context(), store.CreateNetworkParams{Cidr: p, Label: clean(in.Label, 80)})
	if err != nil {
		s.fail(w, r, uniqueAs(err, conflict("That network is already allowed.")))
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) adminDeleteNetwork(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Q.DeleteNetwork(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- holidays and projects (HR and Admin) ----

func (s *Server) listHolidays(w http.ResponseWriter, r *http.Request) {
	_, _, today, err := s.today(r.Context(), s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	from := today.AddDate(0, -1, 0)
	hs, err := s.Q.ListHolidays(r.Context(), store.ListHolidaysParams{FromDay: from, ToDay: from.AddDate(1, 1, 0)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]map[string]string, len(hs))
	for i, h := range hs {
		out[i] = map[string]string{"day": ymd(h.Day), "name": h.Name}
	}
	writeJSON(w, http.StatusOK, map[string]any{"holidays": out})
}

func (s *Server) putHoliday(w http.ResponseWriter, r *http.Request) {
	day, err := parseDay(chi.URLParam(r, "day"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Name = clean(in.Name, 80); in.Name == "" {
		s.fail(w, r, bad("Add the holiday name."))
		return
	}
	if err := s.Q.UpsertHoliday(r.Context(), store.UpsertHolidayParams{Day: day, Name: in.Name}); err != nil {
		s.fail(w, r, err)
		return
	}
	s.listHolidays(w, r)
}

func (s *Server) deleteHoliday(w http.ResponseWriter, r *http.Request) {
	day, err := parseDay(chi.URLParam(r, "day"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Q.DeleteHoliday(r.Context(), day); err != nil {
		s.fail(w, r, err)
		return
	}
	s.listHolidays(w, r)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Q.ListProjects(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

type projectIn struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Active *bool  `json:"active"`
}

func (p *projectIn) check() error {
	p.Code = strings.ToUpper(clean(p.Code, 20))
	p.Name = clean(p.Name, 120)
	if p.Code == "" || p.Name == "" {
		return bad("Add a code and a name.")
	}
	return nil
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in projectIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.check(); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.Q.CreateProject(r.Context(), store.CreateProjectParams{Code: in.Code, Name: in.Name})
	if err != nil {
		s.fail(w, r, uniqueAs(err, conflict("A project with that code exists.")))
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in projectIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.check(); err != nil {
		s.fail(w, r, err)
		return
	}
	active := in.Active == nil || *in.Active
	p, err := s.Q.UpdateProject(r.Context(), store.UpdateProjectParams{ID: id, Code: in.Code, Name: in.Name, Active: active})
	if err != nil {
		s.fail(w, r, uniqueAs(err, conflict("A project with that code exists.")))
		return
	}
	writeJSON(w, http.StatusOK, p)
}
