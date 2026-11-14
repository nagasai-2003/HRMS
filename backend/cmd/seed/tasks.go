package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hrms/backend/internal/store"
)

type demoTask struct {
	status, title, desc string
	labels              []string
	people              []string // emails
	prio                store.TaskPriority
	due                 int // days from today; 0 means no due date unless dueToday
	dueToday            bool
	estimate            int32
	checklist           []string
	checked             int
	comments            [][2]string // email, text
}

// seedTasks fills the Frontend and Backend boards when they are empty.
func seedTasks(ctx context.Context, q *store.Queries, today time.Time) {
	boards := map[string]struct {
		reporter string
		tasks    []demoTask
	}{
		"Frontend": {"manager@company.test", []demoTask{
			{status: "To do", title: "Chat unread badge count", labels: []string{"Chat"}, people: []string{"employee@company.test"}, prio: store.TaskPriorityMedium, due: 5},
			{status: "To do", title: "Dark mode tokens", labels: []string{"Design system"}, people: []string{"meera@company.test"}, prio: store.TaskPriorityLow, due: 11},
			{status: "To do", title: "Empty states for the task list", labels: []string{"Tasks"}, people: []string{"manager@company.test"}, prio: store.TaskPriorityLow, due: 13},
			{status: "In progress", title: "Fix date picker on the WFH form", labels: []string{"Requests", "Bug"}, people: []string{"employee@company.test", "vikram@company.test"}, prio: store.TaskPriorityHigh, due: 3, estimate: 240,
				desc: "The picker lets people choose a Sunday and a day more than 3 months ahead. Both are refused by the API, so block them in the picker too."},
			{status: "In progress", title: "Geofence error messages", labels: []string{"Attendance"}, people: []string{"meera@company.test"}, prio: store.TaskPriorityMedium, due: 4},
			{status: "In review", title: "Attendance widget: break reason modal", labels: []string{"Attendance"}, people: []string{"employee@company.test"}, prio: store.TaskPriorityHigh, dueToday: true, estimate: 360,
				desc: "When someone presses Start break, show the reason list from Attendance rules. A reason is required; \"Other\" needs a note of up to 140 characters. Block the break if the location or network checks fail on an office day.",
				checklist: []string{"Modal layout and reason list", "Required reason validation", "Run the checks before the break", "Tests for the break flow"}, checked: 2,
				comments: [][2]string{
					{"manager@company.test", "Please use the reason list from settings, not a fixed one, so admin edits show up."},
					{"employee@company.test", "Done and moved to review. Vikram, can you wire the checks call?"},
				}},
			{status: "Blocked", title: "Dashboard export to PDF", labels: []string{"Dashboard"}, people: []string{"employee@company.test", "manager@company.test"}, prio: store.TaskPriorityMedium, due: -2,
				comments: [][2]string{{"employee@company.test", "Waiting on the file storage step (MinIO) before the export can be saved."}}},
			{status: "Done", title: "Login page responsive layout", labels: []string{"Auth"}, people: []string{"meera@company.test"}, prio: store.TaskPriorityLow},
			{status: "Done", title: "Sidebar sections by role", labels: []string{"Navigation"}, people: []string{"employee@company.test"}, prio: store.TaskPriorityMedium},
		}},
		"Backend": {"arjun@company.test", []demoTask{
			{status: "To do", title: "Rate-limit task comments", labels: []string{"Tasks"}, people: []string{"rohit@company.test"}, prio: store.TaskPriorityLow, due: 9},
			{status: "In progress", title: "River job for e-mail alerts", labels: []string{"Mail"}, people: []string{"rohit@company.test"}, prio: store.TaskPriorityHigh, due: 6, estimate: 600},
			{status: "In review", title: "Geofence accuracy setting", labels: []string{"Attendance"}, people: []string{"lakshmi@company.test"}, prio: store.TaskPriorityMedium, due: 1},
			{status: "Done", title: "Nightly Postgres backups", labels: []string{"Ops"}, people: []string{"lakshmi@company.test"}, prio: store.TaskPriorityHigh},
		}},
	}
	teams, err := q.ListTaskTeams(ctx, store.ListTaskTeamsParams{AllTeams: true})
	must(err)
	user := func(email string) uuid.UUID {
		u, err := q.GetUserByEmail(ctx, email)
		must(err)
		return u.ID
	}
	for _, t := range teams {
		b, ok := boards[t.Name]
		if !ok {
			continue
		}
		n, err := q.CountTeamTasks(ctx, t.ID)
		must(err)
		if n > 0 {
			fmt.Printf("seed: %s already has tasks, skipped\n", t.Name)
			continue
		}
		statuses, err := q.ListStatuses(ctx, t.ID)
		must(err)
		status := map[string]store.ListStatusesRow{}
		for _, s := range statuses {
			status[s.Name] = s
		}
		reporter := user(b.reporter)
		for _, d := range b.tasks {
			st, ok := status[d.status]
			if !ok {
				st = statuses[0]
			}
			var due *time.Time
			if d.due != 0 || d.dueToday {
				x := today.AddDate(0, 0, d.due)
				due = &x
			}
			var est *int32
			if d.estimate > 0 {
				est = &d.estimate
			}
			num, err := q.NextTaskNumber(ctx, t.ID)
			must(err)
			task, err := q.CreateTask(ctx, store.CreateTaskParams{
				TeamID: t.ID, Number: num, Title: d.title, Description: d.desc, StatusID: st.ID,
				Priority: d.prio, Labels: d.labels, DueDate: due, EstimateMinutes: est,
				ReporterID: reporter, Done: st.Category == store.StatusCategoryDone,
			})
			must(err)
			for _, p := range d.people {
				must(q.AddAssignee(ctx, store.AddAssigneeParams{TaskID: task.ID, UserID: user(p)}))
			}
			for i, c := range d.checklist {
				it, err := q.AddChecklistItem(ctx, store.AddChecklistItemParams{TaskID: task.ID, Title: c})
				must(err)
				if i < d.checked {
					must(q.UpdateChecklistItem(ctx, store.UpdateChecklistItemParams{ID: it.ID, Title: c, Done: true}))
				}
			}
			for _, c := range d.comments {
				_, err := q.AddComment(ctx, store.AddCommentParams{TaskID: task.ID, AuthorID: user(c[0]), Body: c[1], Mentions: []uuid.UUID{}})
				must(err)
			}
			must(q.AddActivity(ctx, store.AddActivityParams{TaskID: task.ID, ActorID: reporter, Text: "created the task in " + st.Name}))
		}
		fmt.Printf("seed: added %d demo tasks to %s\n", len(b.tasks), t.Name)
	}
}
