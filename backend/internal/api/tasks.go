package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hrms/backend/internal/boards"
	"hrms/backend/internal/store"
)

// Tasks live on team boards. A team's board is private: only its members and
// Admin can see or change its tasks. The team's managers and Admin also edit
// its status groups and may delete any of its tasks; anyone else may delete
// only tasks they reported.

const (
	maxTitle       = 200
	maxDescription = 10000
	maxComment     = 4000
	maxChecklist   = 100
	maxStatuses    = 20
)

type teamAccess struct {
	Team    store.Team
	Member  bool
	Manager bool
	Admin   bool
}

func (a teamAccess) canView() bool   { return a.Member || a.Admin }
func (a teamAccess) canManage() bool { return a.Manager || a.Admin }

func (s *Server) accessTeam(ctx context.Context, q *store.Queries, u store.User, teamID uuid.UUID) (teamAccess, error) {
	t, err := q.GetTeam(ctx, teamID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return teamAccess{}, notFound("Team not found.")
		}
		return teamAccess{}, err
	}
	a := teamAccess{Team: t, Admin: u.Role == store.AppRoleAdmin}
	isMgr, err := q.GetMembership(ctx, store.GetMembershipParams{TeamID: teamID, UserID: u.ID})
	switch {
	case err == nil:
		a.Member, a.Manager = true, isMgr
	case !errors.Is(err, pgx.ErrNoRows):
		return teamAccess{}, err
	}
	if !a.canView() {
		return teamAccess{}, forbidden("This board is private to the " + t.Name + " team.")
	}
	return a, nil
}

// loadTask finds a task the person may see. Tasks of other teams look like
// they do not exist.
func (s *Server) loadTask(ctx context.Context, q *store.Queries, u store.User, r *http.Request) (store.GetTaskRow, teamAccess, error) {
	id, err := urlID(r, "id")
	if err != nil {
		return store.GetTaskRow{}, teamAccess{}, notFound("Task not found.")
	}
	t, err := q.GetTask(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return t, teamAccess{}, notFound("Task not found.")
		}
		return t, teamAccess{}, err
	}
	a, err := s.accessTeam(ctx, q, u, t.TeamID)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			return t, a, notFound("Task not found.")
		}
		return t, a, err
	}
	return t, a, nil
}

// lockTask serialises changes to one task.
func lockTask(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT 1 FROM tasks WHERE id = $1 FOR UPDATE", id)
	return err
}

func taskKey(key string, n int32) string { return fmt.Sprintf("%s-%d", key, n) }

func dayPtrJSON(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ymd(*t)
}

// optDay reads "" as no date.
func optDay(s string) (*time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	d, err := parseDay(strings.TrimSpace(s))
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func parsePriority(p string) (store.TaskPriority, error) {
	switch v := store.TaskPriority(p); v {
	case store.TaskPriorityLow, store.TaskPriorityMedium, store.TaskPriorityHigh, store.TaskPriorityUrgent:
		return v, nil
	}
	return "", bad("Priority must be low, medium, high or urgent.")
}

func parseCategory(c string) (store.StatusCategory, error) {
	switch v := store.StatusCategory(c); v {
	case store.StatusCategoryNotStarted, store.StatusCategoryActive, store.StatusCategoryDone:
		return v, nil
	}
	return "", bad("A group counts as not_started, active or done.")
}

func checkTitle(t string) (string, error) {
	t = clean(strings.Join(strings.Fields(t), " "), maxTitle+1)
	if t == "" {
		return "", bad("Give the task a title.")
	}
	if utf8.RuneCountInString(t) > maxTitle {
		return "", bad(fmt.Sprintf("Keep the title under %d characters.", maxTitle))
	}
	return t, nil
}

func checkDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if utf8.RuneCountInString(d) > maxDescription {
		return "", bad(fmt.Sprintf("Keep the description under %d characters.", maxDescription))
	}
	return d, nil
}

func checkLabels(in []string) ([]string, error) {
	out, ok := boards.NormLabels(in)
	if !ok {
		return nil, bad(fmt.Sprintf("Use up to %d labels of %d characters or less.", boards.MaxLabels, boards.MaxLabelLen))
	}
	return out, nil
}

func checkEstimate(m *int32) (*int32, error) {
	if m == nil || *m == 0 {
		return nil, nil
	}
	if *m < 1 || *m > 60000 {
		return nil, bad("The estimate must be between 1 minute and 1000 hours.")
	}
	return m, nil
}

func checkDates(start, due *time.Time) error {
	if start != nil && due != nil && due.Before(*start) {
		return bad("The due date cannot be before the start date.")
	}
	return nil
}

// activeMembers of a team, for the assignee picker.
func activeMembers(ctx context.Context, q *store.Queries, teamID uuid.UUID) ([]map[string]any, map[uuid.UUID]string, error) {
	ms, err := q.ListTeamMembers(ctx, teamID)
	if err != nil {
		return nil, nil, err
	}
	out := []map[string]any{}
	names := map[uuid.UUID]string{}
	for _, m := range ms {
		if !m.Active {
			continue
		}
		names[m.ID] = m.Name
		out = append(out, map[string]any{"id": m.ID, "name": m.Name, "title": m.Title, "is_manager": m.IsManager})
	}
	return out, names, nil
}

func statusJSON(st store.ListStatusesRow) map[string]any {
	return map[string]any{
		"id": st.ID, "name": st.Name, "color": st.Color, "category": st.Category,
		"position": st.Position, "task_count": st.TaskCount,
	}
}

func statusesJSON(rows []store.ListStatusesRow) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, st := range rows {
		out[i] = statusJSON(st)
	}
	return out
}

func assigneeMap(ctx context.Context, q *store.Queries, ids []uuid.UUID) (map[uuid.UUID][]map[string]any, error) {
	rows, err := q.ListAssigneesFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]map[string]any{}
	for _, a := range rows {
		out[a.TaskID] = append(out[a.TaskID], map[string]any{"id": a.UserID, "name": a.Name})
	}
	return out, nil
}

func orEmpty(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}

// GET /api/tasks/teams: boards this person can open.
func (s *Server) taskTeams(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	rows, err := s.Q.ListTaskTeams(r.Context(), store.ListTaskTeamsParams{UserID: u.ID, AllTeams: u.Role == store.AppRoleAdmin})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": rows})
}

// GET /api/tasks/board?team_id=
func (s *Server) taskBoard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	teamID, err := uuid.Parse(r.URL.Query().Get("team_id"))
	if err != nil {
		s.fail(w, r, notFound("Team not found."))
		return
	}
	a, err := s.accessTeam(ctx, s.Q, u, teamID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	statuses, err := s.Q.ListStatuses(ctx, teamID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rows, err := s.Q.ListBoardTasks(ctx, teamID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids := make([]uuid.UUID, len(rows))
	for i, t := range rows {
		ids[i] = t.ID
	}
	people, err := assigneeMap(ctx, s.Q, ids)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	members, _, err := activeMembers(ctx, s.Q, teamID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, _, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tasks := make([]map[string]any, len(rows))
	for i, t := range rows {
		tasks[i] = map[string]any{
			"id": t.ID, "key": taskKey(a.Team.Key, t.Number), "number": t.Number, "title": t.Title,
			"status_id": t.StatusID, "priority": t.Priority, "labels": t.Labels,
			"start_date": dayPtrJSON(t.StartDate), "due_date": dayPtrJSON(t.DueDate),
			"estimate_minutes": t.EstimateMinutes, "logged_minutes": t.LoggedMinutes,
			"completed_at": t.CompletedAt, "updated_at": t.UpdatedAt,
			"checklist_total": t.ChecklistTotal, "checklist_done": t.ChecklistDone,
			"comment_count": t.CommentCount, "assignees": orEmpty(people[t.ID]),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"team":       map[string]any{"id": a.Team.ID, "name": a.Team.Name, "key": a.Team.Key},
		"can_manage": a.canManage(), "is_member": a.Member,
		"statuses": statusesJSON(statuses), "tasks": tasks, "members": members, "today": ymd(today),
	})
}

// GET /api/tasks/mine: tasks assigned to me across my teams.
func (s *Server) myTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	rows, err := s.Q.ListMyTasks(ctx, u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids := make([]uuid.UUID, len(rows))
	for i, t := range rows {
		ids[i] = t.ID
	}
	people, err := assigneeMap(ctx, s.Q, ids)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, _, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tasks := make([]map[string]any, len(rows))
	for i, t := range rows {
		tasks[i] = map[string]any{
			"id": t.ID, "key": taskKey(t.TeamKey, t.Number), "title": t.Title,
			"team": map[string]any{"id": t.TeamID, "name": t.TeamName, "key": t.TeamKey},
			"status_id": t.StatusID, "status_name": t.StatusName, "status_color": t.StatusColor,
			"status_category": t.StatusCategory, "priority": t.Priority, "labels": t.Labels,
			"start_date": dayPtrJSON(t.StartDate), "due_date": dayPtrJSON(t.DueDate),
			"estimate_minutes": t.EstimateMinutes, "logged_minutes": t.LoggedMinutes,
			"completed_at": t.CompletedAt, "assignees": orEmpty(people[t.ID]),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "today": ymd(today)})
}

type newTaskIn struct {
	TeamID          uuid.UUID   `json:"team_id"`
	Title           string      `json:"title"`
	Description     string      `json:"description"`
	StatusID        *uuid.UUID  `json:"status_id"`
	Priority        string      `json:"priority"`
	Labels          []string    `json:"labels"`
	StartDate       string      `json:"start_date"`
	DueDate         string      `json:"due_date"`
	EstimateMinutes *int32      `json:"estimate_minutes"`
	AssigneeIDs     []uuid.UUID `json:"assignee_ids"`
}

// POST /api/tasks
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in newTaskIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	title, err := checkTitle(in.Title)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	desc, err := checkDescription(in.Description)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Priority == "" {
		in.Priority = string(store.TaskPriorityMedium)
	}
	prio, err := parsePriority(in.Priority)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	labels, err := checkLabels(in.Labels)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	start, err := optDay(in.StartDate)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	due, err := optDay(in.DueDate)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := checkDates(start, due); err != nil {
		s.fail(w, r, err)
		return
	}
	est, err := checkEstimate(in.EstimateMinutes)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var id uuid.UUID
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		if _, err := s.accessTeam(ctx, q, u, in.TeamID); err != nil {
			return err
		}
		statuses, err := q.ListStatuses(ctx, in.TeamID)
		if err != nil {
			return err
		}
		if len(statuses) == 0 {
			return conflict("This team has no status groups. Ask the team manager to add one.")
		}
		st := statuses[0]
		for _, x := range statuses {
			if x.Category == store.StatusCategoryNotStarted {
				st = x
				break
			}
		}
		if in.StatusID != nil {
			i := slices.IndexFunc(statuses, func(x store.ListStatusesRow) bool { return x.ID == *in.StatusID })
			if i < 0 {
				return bad("That status group is not on this board.")
			}
			st = statuses[i]
		}
		_, names, err := activeMembers(ctx, q, in.TeamID)
		if err != nil {
			return err
		}
		for _, a := range in.AssigneeIDs {
			if _, ok := names[a]; !ok {
				return bad("Only active members of this team can be assigned.")
			}
		}
		n, err := q.NextTaskNumber(ctx, in.TeamID)
		if err != nil {
			return err
		}
		t, err := q.CreateTask(ctx, store.CreateTaskParams{
			TeamID: in.TeamID, Number: n, Title: title, Description: desc, StatusID: st.ID,
			Priority: prio, Labels: labels, StartDate: start, DueDate: due, EstimateMinutes: est,
			ReporterID: u.ID, Done: st.Category == store.StatusCategoryDone,
		})
		if err != nil {
			return err
		}
		id = t.ID
		for _, a := range in.AssigneeIDs {
			if err := q.AddAssignee(ctx, store.AddAssigneeParams{TaskID: t.ID, UserID: a}); err != nil {
				return err
			}
		}
		return q.AddActivity(ctx, store.AddActivityParams{TaskID: t.ID, ActorID: u.ID, Text: "created the task in " + st.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeTask(w, r, id, http.StatusCreated)
}

// GET /api/tasks/{id}
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, _, err := s.loadTask(r.Context(), s.Q, userFrom(r.Context()), r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeTask(w, r, t.ID, http.StatusOK)
}

func (s *Server) writeTask(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	ctx := r.Context()
	u := userFrom(ctx)
	t, err := s.Q.GetTask(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.accessTeam(ctx, s.Q, u, t.TeamID)
	if err != nil {
		s.fail(w, r, notFound("Task not found."))
		return
	}
	statuses, err := s.Q.ListStatuses(ctx, t.TeamID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	people, err := assigneeMap(ctx, s.Q, []uuid.UUID{t.ID})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	members, _, err := activeMembers(ctx, s.Q, t.TeamID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	checklist, err := s.Q.ListChecklist(ctx, t.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	comments, err := s.Q.ListComments(ctx, t.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	activity, err := s.Q.ListActivity(ctx, t.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	byPerson, err := s.Q.TaskTimeByPerson(ctx, &t.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, _, today, err := s.today(ctx, s.Q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var logged int32
	for _, p := range byPerson {
		logged += p.Minutes
	}
	cl := make([]map[string]any, len(checklist))
	for i, c := range checklist {
		cl[i] = map[string]any{"id": c.ID, "title": c.Title, "done": c.Done}
	}
	cm := make([]map[string]any, len(comments))
	for i, c := range comments {
		cm[i] = map[string]any{
			"id": c.ID, "author_id": c.AuthorID, "author_name": c.AuthorName, "body": c.Body, "created_at": c.CreatedAt,
			"can_delete": c.AuthorID == u.ID || a.Admin,
		}
	}
	ac := make([]map[string]any, len(activity))
	for i, x := range activity {
		ac[i] = map[string]any{"id": x.ID, "actor_name": x.ActorName, "text": x.Text, "created_at": x.CreatedAt}
	}
	writeJSON(w, status, map[string]any{
		"task": map[string]any{
			"id": t.ID, "key": taskKey(t.TeamKey, t.Number), "number": t.Number, "title": t.Title,
			"description": t.Description, "status_id": t.StatusID, "status_name": t.StatusName,
			"status_color": t.StatusColor, "status_category": t.StatusCategory, "priority": t.Priority,
			"labels": t.Labels, "start_date": dayPtrJSON(t.StartDate), "due_date": dayPtrJSON(t.DueDate),
			"estimate_minutes": t.EstimateMinutes, "completed_at": t.CompletedAt,
			"created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
			"reporter": map[string]any{"id": t.ReporterID, "name": t.ReporterName},
			"team":     map[string]any{"id": t.TeamID, "name": t.TeamName, "key": t.TeamKey},
			"assignees": orEmpty(people[t.ID]),
		},
		"statuses": statusesJSON(statuses), "members": members, "checklist": cl, "comments": cm,
		"activity": ac, "time": map[string]any{"logged_minutes": logged, "by_person": byPerson},
		"can_delete": t.ReporterID == u.ID || a.canManage(), "can_manage": a.canManage(),
		"today": ymd(today),
	})
}

// PATCH /api/tasks/{id}: any of the fields; dates "" and estimate 0 clear them.
func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title           *string      `json:"title"`
		Description     *string      `json:"description"`
		Priority        *string      `json:"priority"`
		Labels          *[]string    `json:"labels"`
		StartDate       *string      `json:"start_date"`
		DueDate         *string      `json:"due_date"`
		EstimateMinutes *int32       `json:"estimate_minutes"`
		StatusID        *uuid.UUID   `json:"status_id"`
		AssigneeIDs     *[]uuid.UUID `json:"assignee_ids"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	var id uuid.UUID
	err := s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		t, _, err := s.loadTask(ctx, q, u, r)
		if err != nil {
			return err
		}
		id = t.ID
		if err := lockTask(ctx, tx, t.ID); err != nil {
			return err
		}
		if t, err = q.GetTask(ctx, t.ID); err != nil {
			return err
		}
		var log []string
		p := store.UpdateTaskParams{
			ID: t.ID, Title: t.Title, Description: t.Description, Priority: t.Priority, Labels: t.Labels,
			StartDate: t.StartDate, DueDate: t.DueDate, EstimateMinutes: t.EstimateMinutes,
		}
		if in.Title != nil {
			if p.Title, err = checkTitle(*in.Title); err != nil {
				return err
			}
			if p.Title != t.Title {
				log = append(log, "renamed the task")
			}
		}
		if in.Description != nil {
			if p.Description, err = checkDescription(*in.Description); err != nil {
				return err
			}
			if p.Description != t.Description {
				log = append(log, "edited the description")
			}
		}
		if in.Priority != nil {
			if p.Priority, err = parsePriority(*in.Priority); err != nil {
				return err
			}
			if p.Priority != t.Priority {
				log = append(log, "set priority to "+string(p.Priority))
			}
		}
		if in.Labels != nil {
			if p.Labels, err = checkLabels(*in.Labels); err != nil {
				return err
			}
			if !slices.Equal(p.Labels, t.Labels) {
				log = append(log, "changed labels")
			}
		}
		if in.StartDate != nil {
			if p.StartDate, err = optDay(*in.StartDate); err != nil {
				return err
			}
			if d := dayChange("start date", t.StartDate, p.StartDate); d != "" {
				log = append(log, d)
			}
		}
		if in.DueDate != nil {
			if p.DueDate, err = optDay(*in.DueDate); err != nil {
				return err
			}
			if d := dayChange("due date", t.DueDate, p.DueDate); d != "" {
				log = append(log, d)
			}
		}
		if err := checkDates(p.StartDate, p.DueDate); err != nil {
			return err
		}
		if in.EstimateMinutes != nil {
			if p.EstimateMinutes, err = checkEstimate(in.EstimateMinutes); err != nil {
				return err
			}
			if !eqInt(p.EstimateMinutes, t.EstimateMinutes) {
				if p.EstimateMinutes == nil {
					log = append(log, "cleared the estimate")
				} else {
					log = append(log, "set the estimate to "+minutesText(*p.EstimateMinutes))
				}
			}
		}
		if err := q.UpdateTask(ctx, p); err != nil {
			return err
		}
		if in.AssigneeIDs != nil {
			lines, err := s.setAssignees(ctx, q, t, *in.AssigneeIDs)
			if err != nil {
				return err
			}
			log = append(log, lines...)
		}
		if in.StatusID != nil && *in.StatusID != t.StatusID {
			line, err := moveTask(ctx, q, t, *in.StatusID, -1)
			if err != nil {
				return err
			}
			log = append(log, line)
		}
		for _, l := range log {
			if err := q.AddActivity(ctx, store.AddActivityParams{TaskID: t.ID, ActorID: u.ID, Text: l}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeTask(w, r, id, http.StatusOK)
}

func eqInt(a, b *int32) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func minutesText(m int32) string {
	h, mm := m/60, m%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", mm)
	case mm == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %02dm", h, mm)
}

func dayChange(what string, from, to *time.Time) string {
	switch {
	case from == nil && to == nil:
		return ""
	case to == nil:
		return "cleared the " + what
	case from != nil && from.Equal(*to):
		return ""
	}
	return "set the " + what + " to " + to.Format("2 Jan 2006")
}

func (s *Server) setAssignees(ctx context.Context, q *store.Queries, t store.GetTaskRow, want []uuid.UUID) ([]string, error) {
	_, names, err := activeMembers(ctx, q, t.TeamID)
	if err != nil {
		return nil, err
	}
	cur, err := q.ListAssigneesFor(ctx, []uuid.UUID{t.ID})
	if err != nil {
		return nil, err
	}
	have := map[uuid.UUID]string{}
	for _, a := range cur {
		have[a.UserID] = a.Name
	}
	wantSet := map[uuid.UUID]bool{}
	var log []string
	for _, id := range want {
		if wantSet[id] {
			continue
		}
		wantSet[id] = true
		if _, ok := have[id]; ok {
			continue
		}
		name, ok := names[id]
		if !ok {
			return nil, bad("Only active members of this team can be assigned.")
		}
		if err := q.AddAssignee(ctx, store.AddAssigneeParams{TaskID: t.ID, UserID: id}); err != nil {
			return nil, err
		}
		log = append(log, "assigned "+name)
	}
	for id, name := range have {
		if wantSet[id] {
			continue
		}
		if err := q.RemoveAssignee(ctx, store.RemoveAssigneeParams{TaskID: t.ID, UserID: id}); err != nil {
			return nil, err
		}
		log = append(log, "unassigned "+name)
	}
	slices.Sort(log)
	return log, nil
}

// moveTask puts a task into a status group at index (-1: at the end) and
// renumbers that column. It returns the activity line, or "" when the group
// did not change.
func moveTask(ctx context.Context, q *store.Queries, t store.GetTaskRow, statusID uuid.UUID, index int) (string, error) {
	st, err := q.GetStatus(ctx, statusID)
	if err != nil || st.TeamID != t.TeamID {
		return "", bad("That status group is not on this board.")
	}
	if err := q.SetTaskStatus(ctx, store.SetTaskStatusParams{ID: t.ID, StatusID: st.ID, Done: st.Category == store.StatusCategoryDone}); err != nil {
		return "", err
	}
	ids, err := q.ColumnTaskIDs(ctx, store.ColumnTaskIDsParams{StatusID: st.ID, ExcludeID: t.ID})
	if err != nil {
		return "", err
	}
	if index < 0 {
		index = len(ids)
	}
	for i, id := range boards.Insert(ids, index, t.ID) {
		if err := q.SetTaskPosition(ctx, store.SetTaskPositionParams{ID: id, Position: float64(i + 1)}); err != nil {
			return "", err
		}
	}
	if st.ID == t.StatusID {
		return "", nil
	}
	return "moved " + t.StatusName + " → " + st.Name, nil
}

// POST /api/tasks/{id}/move {status_id, index}: drag and drop on the board.
func (s *Server) moveTaskHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StatusID uuid.UUID `json:"status_id"`
		Index    int       `json:"index"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	err := s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		t, _, err := s.loadTask(ctx, q, u, r)
		if err != nil {
			return err
		}
		if err := lockTask(ctx, tx, t.ID); err != nil {
			return err
		}
		if t, err = q.GetTask(ctx, t.ID); err != nil {
			return err
		}
		line, err := moveTask(ctx, q, t, in.StatusID, max(in.Index, 0))
		if err != nil || line == "" {
			return err
		}
		return q.AddActivity(ctx, store.AddActivityParams{TaskID: t.ID, ActorID: u.ID, Text: line})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/tasks/{id}: the reporter, the team's managers or Admin.
// Hours logged against it stay on the timesheets, unlinked.
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	t, a, err := s.loadTask(ctx, s.Q, u, r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if t.ReporterID != u.ID && !a.canManage() {
		s.fail(w, r, forbidden("Only the person who created it, the team manager or Admin can delete a task."))
		return
	}
	if err := s.Q.DeleteTask(ctx, t.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- checklist ----

func checkItemTitle(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	if t == "" {
		return "", bad("Write the subtask.")
	}
	if utf8.RuneCountInString(t) > 200 {
		return "", bad("Keep a subtask under 200 characters.")
	}
	return t, nil
}

// POST /api/tasks/{id}/checklist {title}
func (s *Server) addChecklistItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	title, err := checkItemTitle(in.Title)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		t, _, err := s.loadTask(ctx, q, userFrom(ctx), r)
		if err != nil {
			return err
		}
		id = t.ID
		if err := lockTask(ctx, tx, t.ID); err != nil {
			return err
		}
		n, err := q.CountChecklist(ctx, t.ID)
		if err != nil {
			return err
		}
		if n >= maxChecklist {
			return bad(fmt.Sprintf("A task can have up to %d subtasks.", maxChecklist))
		}
		if _, err := q.AddChecklistItem(ctx, store.AddChecklistItemParams{TaskID: t.ID, Title: title}); err != nil {
			return err
		}
		return q.TouchTask(ctx, t.ID)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeTask(w, r, id, http.StatusCreated)
}

// PATCH /api/tasks/{id}/checklist/{itemID} {title?, done?}
func (s *Server) updateChecklistItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title *string `json:"title"`
		Done  *bool   `json:"done"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	var id uuid.UUID
	err := s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		t, _, err := s.loadTask(ctx, q, u, r)
		if err != nil {
			return err
		}
		id = t.ID
		itemID, err := urlID(r, "itemID")
		if err != nil {
			return notFound("Subtask not found.")
		}
		it, err := q.GetChecklistItem(ctx, store.GetChecklistItemParams{ID: itemID, TaskID: t.ID})
		if err != nil {
			return notFound("Subtask not found.")
		}
		title, done := it.Title, it.Done
		if in.Title != nil {
			if title, err = checkItemTitle(*in.Title); err != nil {
				return err
			}
		}
		if in.Done != nil {
			done = *in.Done
		}
		if err := q.UpdateChecklistItem(ctx, store.UpdateChecklistItemParams{ID: it.ID, Title: title, Done: done}); err != nil {
			return err
		}
		if done && !it.Done {
			if err := q.AddActivity(ctx, store.AddActivityParams{TaskID: t.ID, ActorID: u.ID, Text: `completed subtask "` + title + `"`}); err != nil {
				return err
			}
		}
		return q.TouchTask(ctx, t.ID)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeTask(w, r, id, http.StatusOK)
}

// DELETE /api/tasks/{id}/checklist/{itemID}
func (s *Server) deleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, _, err := s.loadTask(ctx, s.Q, userFrom(ctx), r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	itemID, err := urlID(r, "itemID")
	if err != nil {
		s.fail(w, r, notFound("Subtask not found."))
		return
	}
	if _, err := s.Q.GetChecklistItem(ctx, store.GetChecklistItemParams{ID: itemID, TaskID: t.ID}); err != nil {
		s.fail(w, r, notFound("Subtask not found."))
		return
	}
	if err := s.Q.DeleteChecklistItem(ctx, itemID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeTask(w, r, t.ID, http.StatusOK)
}

// ---- comments ----

// POST /api/tasks/{id}/comments {body}
func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		s.fail(w, r, bad("Write a comment first."))
		return
	}
	if utf8.RuneCountInString(body) > maxComment {
		s.fail(w, r, bad(fmt.Sprintf("Keep a comment under %d characters.", maxComment)))
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	t, _, err := s.loadTask(ctx, s.Q, u, r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.Q.AddComment(ctx, store.AddCommentParams{TaskID: t.ID, AuthorID: u.ID, Body: body}); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeTask(w, r, t.ID, http.StatusCreated)
}

// DELETE /api/tasks/{id}/comments/{commentID}: the author or Admin.
func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	t, a, err := s.loadTask(ctx, s.Q, u, r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cid, err := urlID(r, "commentID")
	if err != nil {
		s.fail(w, r, notFound("Comment not found."))
		return
	}
	c, err := s.Q.GetComment(ctx, store.GetCommentParams{ID: cid, TaskID: t.ID})
	if err != nil {
		s.fail(w, r, notFound("Comment not found."))
		return
	}
	if c.AuthorID != u.ID && !a.Admin {
		s.fail(w, r, forbidden("You can delete only your own comments."))
		return
	}
	if err := s.Q.DeleteComment(ctx, c.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeTask(w, r, t.ID, http.StatusOK)
}

// ---- status groups (team managers and Admin) ----

func (s *Server) manageTeam(ctx context.Context, q *store.Queries, r *http.Request) (teamAccess, error) {
	teamID, err := urlID(r, "teamID")
	if err != nil {
		return teamAccess{}, notFound("Team not found.")
	}
	a, err := s.accessTeam(ctx, q, userFrom(ctx), teamID)
	if err != nil {
		return a, err
	}
	if !a.canManage() {
		return a, forbidden("Only the team manager or Admin can change status groups.")
	}
	return a, nil
}

type statusIn struct {
	Name     string `json:"name"`
	Color    string `json:"color"`
	Category string `json:"category"`
}

func (in statusIn) check() (string, string, store.StatusCategory, error) {
	name := strings.Join(strings.Fields(in.Name), " ")
	if name == "" {
		return "", "", "", bad("Name the group.")
	}
	if utf8.RuneCountInString(name) > 40 {
		return "", "", "", bad("Keep the name under 40 characters.")
	}
	if !boards.ValidColor(in.Color) {
		return "", "", "", bad("Pick a colour.")
	}
	cat, err := parseCategory(in.Category)
	return name, strings.ToUpper(in.Color), cat, err
}

func renumberStatuses(ctx context.Context, q *store.Queries, ids []uuid.UUID) error {
	for i, id := range ids {
		if err := q.SetStatusPosition(ctx, store.SetStatusPositionParams{ID: id, Position: int32(i)}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) writeStatuses(w http.ResponseWriter, r *http.Request, teamID uuid.UUID, status int) {
	rows, err := s.Q.ListStatuses(r.Context(), teamID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, status, map[string]any{"statuses": statusesJSON(rows)})
}

var nameTaken = conflict("Another group on this board has that name.")

// POST /api/tasks/teams/{teamID}/statuses: a new group goes before the
// first Done group, or last when it is a Done group.
func (s *Server) createStatus(w http.ResponseWriter, r *http.Request) {
	var in statusIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	name, color, cat, err := in.check()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	var teamID uuid.UUID
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		a, err := s.manageTeam(ctx, q, r)
		if err != nil {
			return err
		}
		teamID = a.Team.ID
		rows, err := q.ListStatuses(ctx, teamID)
		if err != nil {
			return err
		}
		if len(rows) >= maxStatuses {
			return bad(fmt.Sprintf("A board can have up to %d groups.", maxStatuses))
		}
		st, err := q.CreateStatus(ctx, store.CreateStatusParams{TeamID: teamID, Name: name, Color: color, Category: cat, Position: int32(len(rows))})
		if err != nil {
			return uniqueAs(err, nameTaken)
		}
		ids := make([]uuid.UUID, 0, len(rows))
		at := len(rows)
		for i, x := range rows {
			if at == len(rows) && cat != store.StatusCategoryDone && x.Category == store.StatusCategoryDone {
				at = i
			}
			ids = append(ids, x.ID)
		}
		return renumberStatuses(ctx, q, boards.Insert(ids, at, st.ID))
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeStatuses(w, r, teamID, http.StatusCreated)
}

// PATCH /api/tasks/teams/{teamID}/statuses/{statusID}
func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	var in statusIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	name, color, cat, err := in.check()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	var teamID uuid.UUID
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		a, err := s.manageTeam(ctx, q, r)
		if err != nil {
			return err
		}
		teamID = a.Team.ID
		st, err := s.teamStatus(ctx, q, r, teamID)
		if err != nil {
			return err
		}
		if st.Category == store.StatusCategoryDone && cat != store.StatusCategoryDone {
			if err := keepOneDone(ctx, q, teamID, st.ID); err != nil {
				return err
			}
		}
		if _, err := q.UpdateStatus(ctx, store.UpdateStatusParams{ID: st.ID, Name: name, Color: color, Category: cat}); err != nil {
			return uniqueAs(err, nameTaken)
		}
		if (st.Category == store.StatusCategoryDone) != (cat == store.StatusCategoryDone) {
			return q.SyncCompleted(ctx, store.SyncCompletedParams{StatusID: st.ID, Done: cat == store.StatusCategoryDone})
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeStatuses(w, r, teamID, http.StatusOK)
}

func (s *Server) teamStatus(ctx context.Context, q *store.Queries, r *http.Request, teamID uuid.UUID) (store.TaskStatus, error) {
	id, err := urlID(r, "statusID")
	if err != nil {
		return store.TaskStatus{}, notFound("Status group not found.")
	}
	st, err := q.GetStatus(ctx, id)
	if err != nil || st.TeamID != teamID {
		return store.TaskStatus{}, notFound("Status group not found.")
	}
	return st, nil
}

// keepOneDone refuses to leave a board with no Done group, since tasks could
// then never be finished.
func keepOneDone(ctx context.Context, q *store.Queries, teamID, leaving uuid.UUID) error {
	rows, err := q.ListStatuses(ctx, teamID)
	if err != nil {
		return err
	}
	for _, x := range rows {
		if x.ID != leaving && x.Category == store.StatusCategoryDone {
			return nil
		}
	}
	return bad("Keep at least one group that counts as Done, so tasks can be finished.")
}

// PUT /api/tasks/teams/{teamID}/statuses/order {ids}
func (s *Server) orderStatuses(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	var teamID uuid.UUID
	err := s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		a, err := s.manageTeam(ctx, q, r)
		if err != nil {
			return err
		}
		teamID = a.Team.ID
		rows, err := q.ListStatuses(ctx, teamID)
		if err != nil {
			return err
		}
		have := make([]uuid.UUID, len(rows))
		for i, x := range rows {
			have[i] = x.ID
		}
		got := slices.Clone(in.IDs)
		slices.SortFunc(have, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		slices.SortFunc(got, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		if !slices.Equal(have, got) {
			return bad("Send every group on the board once, in the new order.")
		}
		return renumberStatuses(ctx, q, in.IDs)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeStatuses(w, r, teamID, http.StatusOK)
}

// DELETE /api/tasks/teams/{teamID}/statuses/{statusID}?move_to=
// A group that still has tasks needs a group to move them to.
func (s *Server) deleteStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var teamID uuid.UUID
	err := s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		a, err := s.manageTeam(ctx, q, r)
		if err != nil {
			return err
		}
		teamID = a.Team.ID
		st, err := s.teamStatus(ctx, q, r, teamID)
		if err != nil {
			return err
		}
		rows, err := q.ListStatuses(ctx, teamID)
		if err != nil {
			return err
		}
		if len(rows) <= 1 {
			return bad("A board needs at least one group.")
		}
		if st.Category == store.StatusCategoryDone {
			if err := keepOneDone(ctx, q, teamID, st.ID); err != nil {
				return err
			}
		}
		count := int32(0)
		for _, x := range rows {
			if x.ID == st.ID {
				count = x.TaskCount
			}
		}
		if count > 0 {
			to, err := uuid.Parse(r.URL.Query().Get("move_to"))
			if err != nil {
				return &apiError{Status: http.StatusConflict, Code: "has_tasks",
					Message: fmt.Sprintf("%q still has %d task(s). Pick a group to move them to.", st.Name, count)}
			}
			i := slices.IndexFunc(rows, func(x store.ListStatusesRow) bool { return x.ID == to })
			if i < 0 || to == st.ID {
				return bad("Pick another group on this board to move the tasks to.")
			}
			if err := q.MoveAllTasks(ctx, store.MoveAllTasksParams{FromID: st.ID, ToID: to, ToDone: rows[i].Category == store.StatusCategoryDone}); err != nil {
				return err
			}
		}
		return q.DeleteStatus(ctx, st.ID)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeStatuses(w, r, teamID, http.StatusOK)
}

// POST /api/tasks/teams/{teamID}/statuses/template {template}
// Replaces every group, so only on a board with no tasks yet.
func (s *Server) applyTemplate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Template string `json:"template"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	defs, ok := boards.Templates[in.Template]
	if !ok {
		s.fail(w, r, bad("Unknown template."))
		return
	}
	ctx := r.Context()
	var teamID uuid.UUID
	err := s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		a, err := s.manageTeam(ctx, q, r)
		if err != nil {
			return err
		}
		teamID = a.Team.ID
		// Stop tasks being added while the groups are swapped.
		if _, err := tx.Exec(ctx, "SELECT 1 FROM teams WHERE id = $1 FOR UPDATE", teamID); err != nil {
			return err
		}
		n, err := q.CountTeamTasks(ctx, teamID)
		if err != nil {
			return err
		}
		if n > 0 {
			return conflict("A template replaces every group, so it only works on a board with no tasks. Edit the groups one by one instead.")
		}
		if err := q.DeleteTeamStatuses(ctx, teamID); err != nil {
			return err
		}
		return createStatuses(ctx, q, teamID, defs)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeStatuses(w, r, teamID, http.StatusOK)
}

func createStatuses(ctx context.Context, q *store.Queries, teamID uuid.UUID, defs []boards.StatusDef) error {
	for i, d := range defs {
		if _, err := q.CreateStatus(ctx, store.CreateStatusParams{TeamID: teamID, Name: d.Name, Color: d.Color, Category: d.Category, Position: int32(i)}); err != nil {
			return err
		}
	}
	return nil
}
