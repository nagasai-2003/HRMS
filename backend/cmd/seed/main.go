// Command seed fills an empty database with a demo company: offices, office
// networks, teams, people in every role, projects, two weeks of history and
// a few tasks on the Frontend and Backend boards. It does nothing if anyone
// already exists.
//
// With -demo-tasks it only adds the demo tasks, to a database seeded before
// task boards existed, and only to demo teams whose boards are empty.
// With -demo-chat it only adds demo channels and messages where they are empty.
//
// For a real company use -first-admin -email you@company.com -name "Your Name"
// instead: it creates only one Admin, the Owner (password: SEED_PASSWORD),
// who sets up an authenticator at the first sign-in and then adds offices,
// networks, teams and people in the app.
//
// -reset-mfa -email someone@company.com clears a person's authenticator
// (for a lost phone, including the Owner's). They set it up again at the
// next sign-in. It needs no SEED_PASSWORD.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/mail"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"hrms/backend/internal/api"
	"hrms/backend/internal/boards"
	"hrms/backend/internal/rules"
	"hrms/backend/internal/store"
)

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func main() {
	demoTasks := flag.Bool("demo-tasks", false, "only add demo tasks to empty Frontend and Backend boards")
	demoChat := flag.Bool("demo-chat", false, "only add demo channels and messages where they are empty")
	firstAdmin := flag.Bool("first-admin", false, "create only the first Admin account (no demo data)")
	adminEmail := flag.String("email", "", "with -first-admin: the Admin's email")
	adminName := flag.String("name", "", "with -first-admin: the Admin's name")
	resetMFA := flag.Bool("reset-mfa", false, "clear the authenticator of the person with -email")
	flag.Parse()
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://hrms:hrms-dev@localhost:5442/hrms?sslmode=disable"
	}
	if *resetMFA {
		pool, err := pgxpool.New(ctx, dbURL)
		must(err)
		defer pool.Close()
		resetAuthenticator(ctx, store.New(pool), *adminEmail)
		return
	}
	password := os.Getenv("SEED_PASSWORD")
	if len(password) < 10 {
		fmt.Fprintln(os.Stderr, "seed: set SEED_PASSWORD (10+ characters) in .env")
		os.Exit(1)
	}
	pool, err := pgxpool.New(ctx, dbURL)
	must(err)
	defer pool.Close()
	q := store.New(pool)
	ist, _ := time.LoadLocation("Asia/Kolkata")
	today := rules.DayOf(time.Now(), ist)

	if *demoTasks {
		seedTasks(ctx, q, today)
		return
	}
	if *demoChat {
		seedChat(ctx, pool, q, today, ist)
		return
	}

	users, err := q.ListUsers(ctx)
	must(err)
	if len(users) > 0 {
		fmt.Println("seed: people already exist, nothing to do")
		return
	}
	if *firstAdmin {
		createFirstAdmin(ctx, q, *adminEmail, *adminName, password)
		return
	}
	hash, err := api.HashPassword(password)
	must(err)

	hq, err := q.CreateOffice(ctx, store.CreateOfficeParams{Name: "HQ Hyderabad", Lat: 17.4483, Lng: 78.3915, RadiusM: 150})
	must(err)
	vizag, err := q.CreateOffice(ctx, store.CreateOfficeParams{Name: "Branch Vizag", Lat: 17.7231, Lng: 83.3013, RadiusM: 120})
	must(err)
	for _, n := range []struct{ cidr, label string }{
		{"49.205.112.18/32", "HQ fibre (example)"},
		{"183.82.96.0/28", "HQ backup line (example)"},
		{"127.0.0.0/8", "This computer: development only, remove in production"},
		{"::1/128", "This computer (IPv6): development only, remove in production"},
	} {
		_, err := q.CreateNetwork(ctx, store.CreateNetworkParams{Cidr: netip.MustParsePrefix(n.cidr), Label: n.label})
		must(err)
	}
	for _, p := range [][2]string{{"INT", "Internal and meetings"}, {"HRMS", "HRMS build"}, {"PORTAL", "Client portal"}, {"LEARN", "Training"}} {
		_, err := q.CreateProject(ctx, store.CreateProjectParams{Code: p[0], Name: p[1]})
		must(err)
	}

	type person struct {
		key, name, title, email string
		role                    store.AppRole
		policy                  store.WorkPolicy
		remote                  []int16
		office                  *uuid.UUID
	}
	people := []person{
		{"rahul", "Rahul Verma", "Operations Head", "admin@company.test", store.AppRoleAdmin, store.WorkPolicyOffice, nil, &hq.ID},
		{"priya", "Priya Menon", "HR Manager", "hr@company.test", store.AppRoleHr, store.WorkPolicyOffice, nil, &hq.ID},
		{"sneha", "Sneha N", "Engineering Lead", "manager@company.test", store.AppRoleManager, store.WorkPolicyOffice, nil, &hq.ID},
		{"ananya", "Ananya Rao", "Frontend Developer", "employee@company.test", store.AppRoleEmployee, store.WorkPolicyHybrid, []int16{3}, &hq.ID},
		{"vikram", "Vikram K", "Frontend Developer", "vikram@company.test", store.AppRoleEmployee, store.WorkPolicyOffice, nil, &hq.ID},
		{"meera", "Meera J", "UI Engineer", "meera@company.test", store.AppRoleEmployee, store.WorkPolicyOffice, nil, &hq.ID},
		{"arjun", "Arjun B", "Backend Lead", "arjun@company.test", store.AppRoleManager, store.WorkPolicyOffice, nil, &hq.ID},
		{"rohit", "Rohit P", "Backend Developer", "rohit@company.test", store.AppRoleEmployee, store.WorkPolicyOffice, nil, &hq.ID},
		{"lakshmi", "Lakshmi K", "DevOps Engineer", "lakshmi@company.test", store.AppRoleEmployee, store.WorkPolicyRemote, nil, nil},
		{"kiran", "Kiran S", "Design Lead", "kiran@company.test", store.AppRoleManager, store.WorkPolicyHybrid, []int16{3, 5}, &hq.ID},
		{"pooja", "Pooja S", "Product Designer", "pooja@company.test", store.AppRoleEmployee, store.WorkPolicyOffice, nil, &vizag.ID},
	}
	ids := map[string]store.User{}
	for _, p := range people {
		if p.remote == nil {
			p.remote = []int16{}
		}
		u, err := q.CreateUser(ctx, store.CreateUserParams{
			Email: p.email, Name: p.name, Title: p.title, PasswordHash: hash, Role: p.role,
			WorkPolicy: p.policy, RemoteWeekdays: p.remote, OfficeID: p.office,
		})
		must(err)
		ids[p.key] = u
	}
	// The demo Admin is the Owner.
	_, err = q.MakeOwner(ctx, ids["rahul"].ID)
	must(err)
	type member struct {
		key     string
		manager bool
	}
	teams := []struct {
		name, key string
		members   []member
	}{
		{"Frontend", "FE", []member{{"sneha", true}, {"ananya", false}, {"vikram", false}, {"meera", false}}},
		{"Backend", "BE", []member{{"arjun", true}, {"rohit", false}, {"lakshmi", false}}},
		{"Design", "DS", []member{{"kiran", true}, {"pooja", false}}},
		{"HR", "HR", []member{{"priya", true}, {"rahul", false}}},
	}
	for _, tm := range teams {
		t, err := q.CreateTeam(ctx, store.CreateTeamParams{Name: tm.name, Key: tm.key})
		must(err)
		for i, d := range boards.Templates["default"] {
			_, err := q.CreateStatus(ctx, store.CreateStatusParams{TeamID: t.ID, Name: d.Name, Color: d.Color, Category: d.Category, Position: int32(i)})
			must(err)
		}
		for _, m := range tm.members {
			must(q.UpsertTeamMember(ctx, store.UpsertTeamMemberParams{TeamID: t.ID, UserID: ids[m.key].ID, IsManager: m.manager}))
		}
	}

	// Two weeks of past working days (Mon to Fri), so dashboards and
	// timesheets have something to show.
	r := rand.New(rand.NewPCG(7, 13))
	for _, p := range people {
		u := ids[p.key]
		for d := today.AddDate(0, 0, -14); d.Before(today); d = d.AddDate(0, 0, 1) {
			if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
				continue
			}
			if r.IntN(12) == 0 {
				continue // an absent day
			}
			mode := string(rules.ModeFor(rules.ModeInput{Policy: string(u.WorkPolicy), RemoteWeekdays: u.RemoteWeekdays, Day: d}))
			in := 9*60 + r.IntN(50)
			lunch := 13*60 + r.IntN(30)
			lunchLen := 30 + r.IntN(25)
			out := 18*60 + r.IntN(60)
			var office *uuid.UUID
			ip := "49.205.112.18"
			if mode == "office" {
				office = p.office
			} else {
				ip = "157.48.20.6"
			}
			ev := func(kind string, min int, reason string) {
				_, err := q.InsertEvent(ctx, store.InsertEventParams{
					UserID: u.ID, Day: d, Kind: kind, At: rules.At(d, min, ist), Mode: mode,
					Reason: reason, Ip: ip, OfficeID: office,
				})
				must(err)
			}
			ev(rules.ActCheckIn, in, "")
			ev(rules.ActBreakStart, lunch, "Lunch")
			ev(rules.ActBreakEnd, lunch+lunchLen, "")
			ev(rules.ActCheckOut, out, "")
		}
	}
	seedTasks(ctx, q, today)
	seedChat(ctx, pool, q, today, ist)
	fmt.Println("seed: demo company created. Sign-in emails are in README.md; the password is SEED_PASSWORD in .env.")
}

func createFirstAdmin(ctx context.Context, q *store.Queries, email, name, password string) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if _, err := mail.ParseAddress(email); err != nil || !strings.Contains(email, "@") {
		fmt.Fprintln(os.Stderr, "seed: give a valid -email for the Admin")
		os.Exit(1)
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "seed: give the Admin's -name")
		os.Exit(1)
	}
	if len(password) < 12 {
		fmt.Fprintln(os.Stderr, "seed: SEED_PASSWORD must be 12 characters or more for a real Admin")
		os.Exit(1)
	}
	hash, err := api.HashPassword(password)
	must(err)
	u, err := q.CreateUser(ctx, store.CreateUserParams{
		Email: email, Name: name, Title: "Administrator", PasswordHash: hash, Role: store.AppRoleAdmin,
		WorkPolicy: store.WorkPolicyOffice, RemoteWeekdays: []int16{},
	})
	must(err)
	n, err := q.MakeOwner(ctx, u.ID)
	must(err)
	if n != 1 {
		must(fmt.Errorf("could not make %s the Owner", email))
	}
	fmt.Printf("seed: Admin %s created as the Owner. Sign in, set up the authenticator app, then add offices, office networks, teams and people.\n", email)
}

func resetAuthenticator(ctx context.Context, q *store.Queries, email string) {
	u, err := q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed: no one has the email", email)
		os.Exit(1)
	}
	must(q.ResetMFA(ctx, u.ID))
	must(q.DeleteRecoveryCodes(ctx, u.ID))
	must(q.DeleteUserSessions(ctx, u.ID))
	fmt.Printf("seed: authenticator cleared for %s. They set it up again at the next sign-in.\n", u.Email)
}
