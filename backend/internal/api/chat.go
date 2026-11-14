package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hrms/backend/internal/chattext"
	"hrms/backend/internal/live"
	"hrms/backend/internal/store"
)

// Chat: each team has private channels that only its members can read
// (Admin has no special view). Direct messages work across teams, 1:1 or a
// group of up to 9 people. Any team member can open a channel; the team's
// managers, and whoever opened a channel, rename it; managers archive it.

const (
	maxTeamChannels = 30
	maxGroupOthers  = 8
	messagePage     = 50
)

type chatAccess struct {
	Ch      store.GetChannelRow
	Manager bool // a manager of the channel's team
}

func (a chatAccess) canPost() bool { return a.Ch.ArchivedAt == nil }

func (a chatAccess) canRename(u store.User) bool {
	return a.Ch.Kind == store.ChannelKindTeam && (a.Manager || (a.Ch.CreatedBy != nil && *a.Ch.CreatedBy == u.ID))
}

// loadChannel finds a conversation this person can read. Others look like
// they do not exist.
func (s *Server) loadChannel(ctx context.Context, q *store.Queries, u store.User, id uuid.UUID) (chatAccess, error) {
	missing := notFound("Conversation not found.")
	ch, err := q.GetChannel(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return chatAccess{}, missing
		}
		return chatAccess{}, err
	}
	a := chatAccess{Ch: ch}
	switch ch.Kind {
	case store.ChannelKindTeam:
		isMgr, err := q.GetMembership(ctx, store.GetMembershipParams{TeamID: *ch.TeamID, UserID: u.ID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return chatAccess{}, missing
			}
			return chatAccess{}, err
		}
		a.Manager = isMgr
	default:
		ok, err := q.IsDMMember(ctx, store.IsDMMemberParams{ChannelID: id, UserID: u.ID})
		if err != nil {
			return chatAccess{}, err
		}
		if !ok {
			return chatAccess{}, missing
		}
	}
	return a, nil
}

func (s *Server) channelFromURL(r *http.Request) (chatAccess, error) {
	id, err := urlID(r, "id")
	if err != nil {
		return chatAccess{}, notFound("Conversation not found.")
	}
	return s.loadChannel(r.Context(), s.Q, userFrom(r.Context()), id)
}

func person(id uuid.UUID, name string) map[string]any { return map[string]any{"id": id, "name": name} }

// ---- conversations ----

// GET /api/chat/conversations: my teams' channels and my direct messages.
func (s *Server) chatConversations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	chans, err := s.Q.ListMyTeamChannels(ctx, u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	total := 0
	var teams []map[string]any
	index := map[uuid.UUID]int{}
	for _, c := range chans {
		i, ok := index[*c.TeamID]
		if !ok {
			i = len(teams)
			index[*c.TeamID] = i
			teams = append(teams, map[string]any{
				"id": c.TeamID, "name": c.TeamName, "key": c.TeamKey, "is_manager": c.IsManager, "channels": []map[string]any{},
			})
		}
		if c.ArchivedAt == nil {
			total += int(c.Unread)
		}
		teams[i]["channels"] = append(teams[i]["channels"].([]map[string]any), map[string]any{
			"id": c.ID, "name": c.Name, "topic": c.Topic, "is_default": c.IsDefault, "archived": c.ArchivedAt != nil,
			"unread": c.Unread, "mentioned": c.Mentioned, "last_message_at": c.LastMessageAt,
		})
	}
	dms, err := s.dmList(ctx, u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, d := range dms {
		total += int(d["unread"].(int32))
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": orEmpty(teams), "dms": orEmpty(dms), "unread": total})
}

func (s *Server) dmList(ctx context.Context, u store.User) ([]map[string]any, error) {
	rows, err := s.Q.ListMyDMs(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, d := range rows {
		ids[i] = d.ID
	}
	members, err := s.Q.ListDMMembersFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	var people []uuid.UUID
	byChan := map[uuid.UUID][]store.ListDMMembersForRow{}
	for _, m := range members {
		if m.ID == u.ID {
			continue
		}
		byChan[m.ChannelID] = append(byChan[m.ChannelID], m)
		people = append(people, m.ID)
	}
	online := s.Live.Online(ctx, people)
	out := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		ms := make([]map[string]any, 0, len(byChan[d.ID]))
		for _, m := range byChan[d.ID] {
			ms = append(ms, map[string]any{
				"id": m.ID, "name": m.Name, "title": m.Title, "teams": m.Teams, "active": m.Active, "online": online[m.ID],
			})
		}
		out = append(out, map[string]any{
			"id": d.ID, "members": ms, "unread": d.Unread, "last_message_at": d.LastMessageAt,
		})
	}
	return out, nil
}

// GET /api/chat/unread: the number on the Chat menu item.
func (s *Server) chatUnread(w http.ResponseWriter, r *http.Request) {
	n, err := s.Q.CountUnread(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int32{"unread": n})
}

// GET /api/chat/people?q=: anyone in the company, to start a conversation.
func (s *Server) chatPeople(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	q := clean(r.URL.Query().Get("q"), 60)
	rows, err := s.Q.SearchPeople(ctx, store.SearchPeopleParams{UserID: u.ID, Q: likeEscape(q)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids := make([]uuid.UUID, len(rows))
	for i, p := range rows {
		ids[i] = p.ID
	}
	online := s.Live.Online(ctx, ids)
	out := make([]map[string]any, len(rows))
	for i, p := range rows {
		out[i] = map[string]any{"id": p.ID, "name": p.Name, "title": p.Title, "teams": p.Teams, "online": online[p.ID]}
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": out})
}

// POST /api/chat/dms {user_ids}: opens (or finds) a conversation with them.
func (s *Server) openDM(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserIDs []uuid.UUID `json:"user_ids"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	var others []uuid.UUID
	for _, id := range in.UserIDs {
		if id != u.ID && !slices.Contains(others, id) {
			others = append(others, id)
		}
	}
	if len(others) == 0 {
		s.fail(w, r, bad("Pick at least one person."))
		return
	}
	if len(others) > maxGroupOthers {
		s.fail(w, r, bad(fmt.Sprintf("A group message can include up to %d other people.", maxGroupOthers)))
		return
	}
	found, err := s.Q.ActiveUsersByID(ctx, others)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(found) != len(others) {
		s.fail(w, r, bad("You can message only people with an active account."))
		return
	}
	everyone := append(slices.Clone(others), u.ID)
	slices.SortFunc(everyone, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })

	var id uuid.UUID
	created := false
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		// One conversation per set of people, even if two tabs race.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", "dm:"+uuidList(everyone)); err != nil {
			return err
		}
		var key *string
		if len(others) == 1 {
			k := everyone[0].String() + ":" + everyone[1].String()
			key = &k
			if got, err := q.FindDMByKey(ctx, &k); err == nil {
				id = got
				return nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		} else {
			if got, err := q.FindGroupDM(ctx, everyone); err == nil {
				id = got
				return nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		ch, err := q.CreateDM(ctx, store.CreateDMParams{DmKey: key, CreatedBy: &u.ID})
		if err != nil {
			return err
		}
		id, created = ch.ID, true
		for _, m := range everyone {
			if err := q.AddDMMember(ctx, store.AddDMMemberParams{ChannelID: id, UserID: m}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if created {
		s.Live.Send(ctx, everyone, live.Event{Type: "channel.changed", Data: map[string]any{"channel_id": id}})
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "created": created})
}

func uuidList(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return strings.Join(parts, ",")
}

// ---- team channels ----

type channelIn struct {
	Name     *string `json:"name"`
	Topic    *string `json:"topic"`
	Archived *bool   `json:"archived"`
}

func checkChannel(name, topic string) (string, string, error) {
	n := chattext.ChannelName(name)
	if n == "" {
		return "", "", bad("Give the channel a name with letters or numbers, like release-notes.")
	}
	topic = strings.Join(strings.Fields(topic), " ")
	if utf8.RuneCountInString(topic) > chattext.MaxTopic {
		return "", "", bad(fmt.Sprintf("Keep the topic under %d characters.", chattext.MaxTopic))
	}
	return n, topic, nil
}

func channelTaken(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return conflict("This team already has a channel with that name.")
	}
	return err
}

// POST /api/chat/teams/{teamID}/channels {name, topic}: any team member.
func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var in channelIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	teamID, err := urlID(r, "teamID")
	if err != nil {
		s.fail(w, r, notFound("Team not found."))
		return
	}
	if _, err := s.Q.GetMembership(ctx, store.GetMembershipParams{TeamID: teamID, UserID: u.ID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = forbidden("Only members of this team can open its channels.")
		}
		s.fail(w, r, err)
		return
	}
	name, topic, err := checkChannel(deref(in.Name), deref(in.Topic))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var ch store.Channel
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM teams WHERE id = $1 FOR UPDATE", teamID); err != nil {
			return err
		}
		n, err := q.CountTeamChannels(ctx, &teamID)
		if err != nil {
			return err
		}
		if n >= maxTeamChannels {
			return conflict(fmt.Sprintf("A team can have up to %d channels. Archive one you no longer use.", maxTeamChannels))
		}
		ch, err = q.CreateTeamChannel(ctx, store.CreateTeamChannelParams{TeamID: &teamID, Name: name, Topic: topic, CreatedBy: &u.ID})
		return channelTaken(err)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.channelChanged(ctx, ch.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"id": ch.ID, "name": ch.Name})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PATCH /api/chat/channels/{id} {name, topic, archived}
func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	var in channelIn
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	a, err := s.channelFromURL(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ch := a.Ch
	if ch.Kind != store.ChannelKindTeam {
		s.fail(w, r, bad("Direct messages have no name or topic to change."))
		return
	}
	name, topic, archived := ch.Name, ch.Topic, ch.ArchivedAt
	if in.Name != nil || in.Topic != nil {
		if !a.canRename(u) {
			s.fail(w, r, forbidden("Only the team's managers or whoever opened the channel can rename it."))
			return
		}
		if in.Name != nil {
			name = *in.Name
		}
		if in.Topic != nil {
			topic = *in.Topic
		}
		if name, topic, err = checkChannel(name, topic); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.Archived != nil {
		if !a.Manager {
			s.fail(w, r, forbidden("Only the team's managers can archive a channel."))
			return
		}
		if *in.Archived && ch.IsDefault {
			s.fail(w, r, bad("The team's general channel cannot be archived."))
			return
		}
		if *in.Archived && archived == nil {
			now := s.Now()
			archived = &now
		} else if !*in.Archived {
			archived = nil
		}
	}
	if err := s.Q.UpdateChannel(ctx, store.UpdateChannelParams{ID: ch.ID, Name: name, Topic: topic, ArchivedAt: archived}); err != nil {
		s.fail(w, r, channelTaken(err))
		return
	}
	s.channelChanged(ctx, ch.ID)
	s.writeChannel(w, r, ch.ID)
}

// channelChanged tells everyone in a conversation to reload its details.
func (s *Server) channelChanged(ctx context.Context, id uuid.UUID) {
	aud, err := s.Q.ChannelAudience(ctx, id)
	if err != nil {
		s.Log.Error("channel audience", "err", err)
		return
	}
	s.Live.Send(ctx, aud, live.Event{Type: "channel.changed", Data: map[string]any{"channel_id": id}})
}

// GET /api/chat/channels/{id}
func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	a, err := s.channelFromURL(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeChannel(w, r, a.Ch.ID)
}

func (s *Server) writeChannel(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	ctx := r.Context()
	u := userFrom(ctx)
	a, err := s.loadChannel(ctx, s.Q, u, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ch := a.Ch
	people, err := s.Q.ListChannelPeople(ctx, store.ListChannelPeopleParams{TeamID: ch.TeamID, ChannelID: ch.ID})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids := make([]uuid.UUID, len(people))
	for i, p := range people {
		ids[i] = p.ID
	}
	online := s.Live.Online(ctx, ids)
	members := make([]map[string]any, len(people))
	for i, p := range people {
		members[i] = map[string]any{
			"id": p.ID, "name": p.Name, "title": p.Title, "active": p.Active, "is_manager": p.IsManager, "online": online[p.ID] || p.ID == u.ID,
		}
	}
	lastRead, err := s.Q.GetLastRead(ctx, store.GetLastReadParams{ChannelID: ch.ID, UserID: u.ID})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var team any
	if ch.TeamID != nil {
		team = map[string]any{"id": ch.TeamID, "name": ch.TeamName, "key": ch.TeamKey}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": map[string]any{
			"id": ch.ID, "kind": ch.Kind, "name": ch.Name, "topic": ch.Topic, "team": team,
			"is_default": ch.IsDefault, "archived": ch.ArchivedAt != nil, "created_at": ch.CreatedAt,
		},
		"members": members, "last_read_id": lastRead,
		"can_post": a.canPost(), "can_rename": a.canRename(u),
		"can_archive": ch.Kind == store.ChannelKindTeam && a.Manager && !ch.IsDefault,
		"can_moderate": a.Manager,
	})
}

// ---- messages ----

type msgRow = store.ListMessagesRow

// messagesJSON shapes messages for this viewer: mention names, and cards for
// task keys on boards the viewer can open.
func (s *Server) messagesJSON(ctx context.Context, u store.User, rows []msgRow, withTasks bool) ([]map[string]any, error) {
	var ids []uuid.UUID
	var refs []string
	for _, m := range rows {
		ids = append(ids, m.Mentions...)
		if withTasks && m.DeletedAt == nil {
			refs = append(refs, chattext.TaskRefs(m.Body)...)
		}
	}
	names, err := s.userNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	cards := map[string]map[string]any{}
	if len(refs) > 0 {
		ts, err := s.Q.TasksByRef(ctx, store.TasksByRefParams{Refs: refs, AllTeams: u.Role == store.AppRoleAdmin, UserID: u.ID})
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			cards[t.Ref] = map[string]any{
				"id": t.ID, "key": t.Ref, "title": t.Title,
				"status_name": t.StatusName, "status_color": t.StatusColor, "status_category": t.StatusCategory,
			}
		}
	}
	out := make([]map[string]any, len(rows))
	for i, m := range rows {
		ms := []map[string]any{}
		for _, id := range m.Mentions {
			if n, ok := names[id]; ok {
				ms = append(ms, person(id, n))
			}
		}
		refs := []string{}
		var tasks []map[string]any
		if m.DeletedAt == nil {
			refs = chattext.TaskRefs(m.Body)
			if withTasks {
				tasks = []map[string]any{}
				for _, ref := range refs {
					if c, ok := cards[ref]; ok {
						tasks = append(tasks, c)
					}
				}
			}
		}
		out[i] = map[string]any{
			"id": strconv.FormatInt(m.ID, 10), "channel_id": m.ChannelID, "author": person(m.AuthorID, m.AuthorName),
			"body": m.Body, "mentions": ms, "task_refs": refs, "tasks": tasks,
			"edited": m.EditedAt != nil, "deleted": m.DeletedAt != nil, "created_at": m.CreatedAt,
		}
	}
	return out, nil
}

func (s *Server) userNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Q.UserNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}

func queryInt(r *http.Request, k string) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get(k), 10, 64)
	return max(n, 0)
}

// GET /api/chat/channels/{id}/messages?before=ID or ?after=ID
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	a, err := s.channelFromURL(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var rows []msgRow
	more := false
	if after := queryInt(r, "after"); after > 0 {
		got, err := s.Q.ListMessagesAfter(ctx, store.ListMessagesAfterParams{ChannelID: a.Ch.ID, After: after})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		for _, m := range got {
			rows = append(rows, msgRow(m))
		}
	} else {
		got, err := s.Q.ListMessages(ctx, store.ListMessagesParams{ChannelID: a.Ch.ID, Before: queryInt(r, "before"), Lim: messagePage + 1})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if len(got) > messagePage {
			more, got = true, got[:messagePage]
		}
		slices.Reverse(got)
		rows = got
	}
	out, err := s.messagesJSON(ctx, u, rows, true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "has_more": more})
}

// loadMessage finds a message in a conversation this person can read.
func (s *Server) loadMessage(r *http.Request) (msgRow, chatAccess, error) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "msgID"), 10, 64)
	if err != nil {
		return msgRow{}, chatAccess{}, notFound("Message not found.")
	}
	m, err := s.Q.GetMessage(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return msgRow{}, chatAccess{}, notFound("Message not found.")
		}
		return msgRow{}, chatAccess{}, err
	}
	a, err := s.loadChannel(ctx, s.Q, userFrom(ctx), m.ChannelID)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			return msgRow{}, a, notFound("Message not found.")
		}
		return msgRow{}, a, err
	}
	return msgRow(m), a, nil
}

// GET /api/chat/messages/{msgID}
func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	m, _, err := s.loadMessage(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeMessage(w, r, m, http.StatusOK)
}

func (s *Server) writeMessage(w http.ResponseWriter, r *http.Request, m msgRow, status int) {
	out, err := s.messagesJSON(r.Context(), userFrom(r.Context()), []msgRow{m}, true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, status, out[0])
}

func checkBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", bad("Write a message first.")
	}
	if utf8.RuneCountInString(body) > chattext.MaxBody {
		return "", bad(fmt.Sprintf("Keep a message under %d characters.", chattext.MaxBody))
	}
	return body, nil
}

// mentionable keeps the mentioned people who can read this conversation.
func mentionable(body string, audience []uuid.UUID, author uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{}
	for _, id := range chattext.Mentions(body) {
		if id != author && slices.Contains(audience, id) {
			out = append(out, id)
		}
	}
	return out
}

// POST /api/chat/channels/{id}/messages {body}
func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	body, err := checkBody(in.Body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.channelFromURL(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !a.canPost() {
		s.fail(w, r, conflict("This channel is archived. A team manager can bring it back."))
		return
	}
	if !s.allowMessage(ctx, u.ID) {
		writeErr(w, http.StatusTooManyRequests, "You are sending messages very fast. Wait a few seconds.")
		return
	}
	audience, err := s.Q.ChannelAudience(ctx, a.Ch.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	mentions := mentionable(body, audience, u.ID)
	var m store.Message
	err = s.tx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		var err error
		if m, err = q.CreateMessage(ctx, store.CreateMessageParams{ChannelID: a.Ch.ID, AuthorID: u.ID, Body: body, Mentions: mentions}); err != nil {
			return err
		}
		if err := q.TouchChannel(ctx, a.Ch.ID); err != nil {
			return err
		}
		_, err = q.MarkRead(ctx, store.MarkReadParams{ChannelID: a.Ch.ID, UserID: u.ID, LastReadID: m.ID})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	row := msgRow{ID: m.ID, ChannelID: m.ChannelID, AuthorID: u.ID, Body: m.Body, Mentions: m.Mentions, CreatedAt: m.CreatedAt, AuthorName: u.Name}
	s.pushMessage(ctx, u, "message.new", row, audience)
	s.notifyChatMentions(ctx, u, a.Ch, row, mentions)
	s.writeMessage(w, r, row, http.StatusCreated)
}

// allowMessage: at most 30 messages in 10 seconds per person.
func (s *Server) allowMessage(ctx context.Context, userID uuid.UUID) bool {
	if s.Redis == nil {
		return true
	}
	key := "chat:rate:" + userID.String()
	n, err := s.Redis.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		s.Redis.Expire(ctx, key, 10*time.Second)
	}
	return n <= 30
}

// pushMessage sends a message to everyone in the conversation. Task cards
// depend on who reads, so the browser asks for them when task_refs is set.
func (s *Server) pushMessage(ctx context.Context, u store.User, kind string, m msgRow, audience []uuid.UUID) {
	out, err := s.messagesJSON(ctx, u, []msgRow{m}, false)
	if err != nil {
		s.Log.Error("push message", "err", err)
		return
	}
	s.Live.Send(ctx, audience, live.Event{Type: kind, Data: map[string]any{"channel_id": m.ChannelID, "message": out[0]}})
}

func (s *Server) notifyChatMentions(ctx context.Context, u store.User, ch store.GetChannelRow, m msgRow, to []uuid.UUID) {
	if len(to) == 0 {
		return
	}
	where := "a direct message"
	if ch.Kind == store.ChannelKindTeam {
		where = "#" + ch.Name
	}
	names, err := s.userNames(ctx, chattext.Mentions(m.Body))
	if err != nil {
		s.Log.Error("mention names", "err", err)
	}
	body := chattext.Snippet(chattext.Plain(m.Body, names), 140)
	link := fmt.Sprintf("/chat?c=%s&m=%d", ch.ID, m.ID)
	var list []notice
	for _, id := range to {
		list = append(list, notice{To: id, Kind: "chat_mention", Title: u.Name + " mentioned you in " + where, Body: body, Link: link})
	}
	s.notify(ctx, u, list)
}

// PATCH /api/chat/messages/{msgID} {body}: your own messages.
func (s *Server) editMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	body, err := checkBody(in.Body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	m, a, err := s.loadMessage(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if m.AuthorID != u.ID {
		s.fail(w, r, forbidden("You can edit only your own messages."))
		return
	}
	if m.DeletedAt != nil {
		s.fail(w, r, conflict("This message was deleted."))
		return
	}
	if !a.canPost() {
		s.fail(w, r, conflict("This channel is archived."))
		return
	}
	audience, err := s.Q.ChannelAudience(ctx, m.ChannelID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	mentions := mentionable(body, audience, u.ID)
	if err := s.Q.EditMessage(ctx, store.EditMessageParams{ID: m.ID, Body: body, Mentions: mentions}); err != nil {
		s.fail(w, r, err)
		return
	}
	var fresh []uuid.UUID
	for _, id := range mentions {
		if !slices.Contains(m.Mentions, id) {
			fresh = append(fresh, id)
		}
	}
	now := s.Now()
	m.Body, m.Mentions, m.EditedAt = body, mentions, &now
	s.pushMessage(ctx, u, "message.edit", m, audience)
	s.notifyChatMentions(ctx, u, a.Ch, m, fresh)
	s.writeMessage(w, r, m, http.StatusOK)
}

// DELETE /api/chat/messages/{msgID}: your own, or any in your team's
// channels if you manage the team.
func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	m, a, err := s.loadMessage(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if m.AuthorID != u.ID && !a.Manager {
		s.fail(w, r, forbidden("You can delete only your own messages."))
		return
	}
	if m.DeletedAt == nil {
		if err := s.Q.DeleteMessage(ctx, m.ID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	audience, err := s.Q.ChannelAudience(ctx, m.ChannelID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := s.Now()
	m.Body, m.Mentions, m.DeletedAt = "", []uuid.UUID{}, &now
	s.pushMessage(ctx, u, "message.delete", m, audience)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/chat/channels/{id}/read {last_read_id}
func (s *Server) markRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LastReadID string `json:"last_read_id"`
	}
	if err := readJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	u := userFrom(ctx)
	upTo, err := strconv.ParseInt(in.LastReadID, 10, 64)
	if err != nil || upTo < 0 {
		s.fail(w, r, bad("last_read_id must be a message id."))
		return
	}
	a, err := s.channelFromURL(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	newest, err := s.Q.MaxMessageID(ctx, a.Ch.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	got, err := s.Q.MarkRead(ctx, store.MarkReadParams{ChannelID: a.Ch.ID, UserID: u.ID, LastReadID: min(upTo, newest)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.Live.Send(ctx, []uuid.UUID{u.ID}, live.Event{Type: "read", Data: map[string]any{"channel_id": a.Ch.ID, "last_read_id": strconv.FormatInt(got, 10)}})
	writeJSON(w, http.StatusOK, map[string]string{"last_read_id": strconv.FormatInt(got, 10)})
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// GET /api/chat/search?q=: messages in conversations I can read.
func (s *Server) searchMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	q := clean(r.URL.Query().Get("q"), 100)
	if utf8.RuneCountInString(q) < 2 {
		s.fail(w, r, bad("Type at least 2 characters to search."))
		return
	}
	rows, err := s.Q.SearchMessages(ctx, store.SearchMessagesParams{Q: likeEscape(q), UserID: u.ID})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var ids, dmIDs []uuid.UUID
	for _, m := range rows {
		ids = append(ids, m.Mentions...)
		if m.Kind == store.ChannelKindDm {
			dmIDs = append(dmIDs, m.ChannelID)
		}
	}
	names, err := s.userNames(ctx, ids)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	dmNames := map[uuid.UUID][]string{}
	if len(dmIDs) > 0 {
		ms, err := s.Q.ListDMMembersFor(ctx, dmIDs)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		for _, m := range ms {
			if m.ID != u.ID {
				dmNames[m.ChannelID] = append(dmNames[m.ChannelID], m.Name)
			}
		}
	}
	out := make([]map[string]any, len(rows))
	for i, m := range rows {
		where := "#" + m.ChannelName + " · " + m.TeamName
		if m.Kind == store.ChannelKindDm {
			where = strings.Join(dmNames[m.ChannelID], ", ")
		}
		out[i] = map[string]any{
			"id": strconv.FormatInt(m.ID, 10), "channel_id": m.ChannelID, "where": where, "author_name": m.AuthorName,
			"text": chattext.Snippet(chattext.Plain(m.Body, names), 220), "created_at": m.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}
