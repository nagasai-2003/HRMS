package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"hrms/backend/internal/chattext"
	"hrms/backend/internal/store"
)

type demoMsg struct {
	from string // email
	at   string // "09:40" today, IST
	text string // "@manager@company.test" becomes a mention
}

// seedChat makes sure every team has its general channel, then adds demo
// channels and messages where they are still empty.
func seedChat(ctx context.Context, pool *pgxpool.Pool, q *store.Queries, today time.Time, ist *time.Location) {
	user := func(email string) store.User {
		u, err := q.GetUserByEmail(ctx, email)
		must(err)
		return u
	}
	teams, err := q.ListTaskTeams(ctx, store.ListTaskTeamsParams{AllTeams: true})
	must(err)
	channelID := map[string]uuid.UUID{} // "Frontend/frontend-general"
	for _, t := range teams {
		n, err := q.CountTeamChannels(ctx, &t.ID)
		must(err)
		if n == 0 {
			_, err := q.CreateTeamChannel(ctx, store.CreateTeamChannelParams{TeamID: &t.ID, Name: chattext.DefaultChannel(t.Name), IsDefault: true})
			must(err)
		}
		extra := map[string][][2]string{
			"Frontend": {{"frontend-standup", "Daily standup notes, before 10:00"}, {"fe-releases", "Release plans and go/no-go"}},
			"Backend":  {{"backend-oncall", "Alerts and who is on call"}},
		}[t.Name]
		for _, c := range extra {
			_, err := pool.Exec(ctx, `INSERT INTO channels (kind, team_id, name, topic) VALUES ('team', $1, $2, $3) ON CONFLICT DO NOTHING`, t.ID, c[0], c[1])
			must(err)
		}
		rows, err := pool.Query(ctx, `SELECT id, name FROM channels WHERE team_id = $1`, t.ID)
		must(err)
		for rows.Next() {
			var id uuid.UUID
			var name string
			must(rows.Scan(&id, &name))
			channelID[t.Name+"/"+name] = id
		}
		must(rows.Err())
	}

	at := func(clock string) time.Time {
		var h, m int
		fmt.Sscanf(clock, "%d:%d", &h, &m)
		return time.Date(today.Year(), today.Month(), today.Day(), h, m, 0, 0, ist)
	}
	put := func(ch uuid.UUID, msgs []demoMsg) bool {
		var n int
		must(pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE channel_id = $1`, ch).Scan(&n))
		if n > 0 {
			return false
		}
		// Demo times are "today"; if that is still ahead, move the whole
		// conversation back so it ended a few minutes ago.
		shift := time.Duration(0)
		if end := at(msgs[len(msgs)-1].at); end.After(time.Now().Add(-5 * time.Minute)) {
			shift = time.Now().Add(-5 * time.Minute).Sub(end)
		}
		var last time.Time
		for _, d := range msgs {
			body, mentions := d.text, []uuid.UUID{}
			for _, w := range strings.Fields(d.text) {
				if strings.HasPrefix(w, "@") && strings.Contains(w, "@company.test") {
					email := strings.TrimRight(w[1:], ".,:;!?")
					u := user(email)
					body = strings.Replace(body, "@"+email, "<@"+u.ID.String()+">", 1)
					mentions = append(mentions, u.ID)
				}
			}
			last = at(d.at).Add(shift)
			_, err := pool.Exec(ctx, `INSERT INTO messages (channel_id, author_id, body, mentions, created_at) VALUES ($1, $2, $3, $4, $5)`,
				ch, user(d.from).ID, body, mentions, last)
			must(err)
		}
		_, err := pool.Exec(ctx, `UPDATE channels SET last_message_at = $2 WHERE id = $1`, ch, last)
		must(err)
		return true
	}
	// Everyone has read everything up to this point, except where noted.
	readAll := func(ch uuid.UUID, except ...string) {
		_, err := pool.Exec(ctx, `
			INSERT INTO chat_reads (channel_id, user_id, last_read_id)
			SELECT $1, a.user_id, (SELECT coalesce(max(id), 0) FROM messages WHERE channel_id = $1)
			FROM (SELECT tm.user_id FROM channels c JOIN team_members tm ON tm.team_id = c.team_id WHERE c.id = $1
			      UNION SELECT user_id FROM dm_members WHERE channel_id = $1) a
			JOIN users u ON u.id = a.user_id
			WHERE NOT (u.email = ANY($2::text[]))
			ON CONFLICT (channel_id, user_id) DO UPDATE SET last_read_id = excluded.last_read_id`, ch, except)
		must(err)
	}

	if ch, ok := channelID["Frontend/frontend-general"]; ok && put(ch, []demoMsg{
		{"manager@company.test", "09:40", "Standup in 5 minutes. Please update your task status before that."},
		{"vikram@company.test", "09:46", "Date picker fix is almost there. Safari still shows the wrong week start."},
		{"employee@company.test", "10:04", "Break modal is ready for review: FE-6"},
		{"meera@company.test", "10:12", "Looks good. One small thing: the reason list should scroll on small screens."},
		{"manager@company.test", "10:15", "Release call at 16:00 today. @employee@company.test can you share the build notes before that?"},
	}) {
		readAll(ch, "employee@company.test")
		fmt.Println("seed: added demo messages to #frontend-general")
	}
	if ch, ok := channelID["Frontend/frontend-standup"]; ok && put(ch, []demoMsg{
		{"vikram@company.test", "09:31", "Yesterday: WFH date picker. Today: Safari week start. No blockers."},
		{"meera@company.test", "09:33", "Yesterday: geofence messages. Today: same, plus copy review with Kiran."},
		{"manager@company.test", "09:35", "Thanks. I am in interviews 11:00 to 13:00."},
	}) {
		readAll(ch, "employee@company.test")
	}
	if ch, ok := channelID["Backend/backend-general"]; ok && put(ch, []demoMsg{
		{"arjun@company.test", "10:02", "Backups ran fine overnight. Lakshmi, thanks for BE-4."},
		{"lakshmi@company.test", "10:05", "Welcome. I will add restore checks next week."},
	}) {
		readAll(ch)
	}

	// A direct message between the employee and HR.
	ananya, priya := user("employee@company.test"), user("hr@company.test")
	a, b := ananya.ID.String(), priya.ID.String()
	if a > b {
		a, b = b, a
	}
	key := a + ":" + b
	var dm uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM channels WHERE dm_key = $1`, key).Scan(&dm); err != nil {
		must(pool.QueryRow(ctx, `INSERT INTO channels (kind, dm_key, created_by) VALUES ('dm', $1, $2) RETURNING id`, key, priya.ID).Scan(&dm))
		_, err := pool.Exec(ctx, `INSERT INTO dm_members (channel_id, user_id) VALUES ($1, $2), ($1, $3)`, dm, ananya.ID, priya.ID)
		must(err)
	}
	if put(dm, []demoMsg{
		{"hr@company.test", "11:20", "Hi Ananya, your extra WFH request for Friday is with me. Is it for the release?"},
		{"employee@company.test", "11:24", "Yes, the release call runs late. I can come in on Monday instead."},
		{"hr@company.test", "11:26", "That works. I will approve it today."},
	}) {
		readAll(dm, "employee@company.test")
	}
}
