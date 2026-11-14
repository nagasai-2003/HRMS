// Package chattext reads chat and comment text: mentions written <@user-id>,
// task keys such as FE-142, and channel names.
package chattext

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const (
	MaxBody       = 4000
	MaxTaskRefs   = 5
	MaxChannelLen = 40
	MaxTopic      = 250
)

var (
	mentionRe = regexp.MustCompile(`<@([0-9a-fA-F-]{36})>`)
	taskRe    = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,5})-([1-9][0-9]{0,6})\b`)
	nameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	nonSlug   = regexp.MustCompile(`[^a-z0-9]+`)
)

// Mentions lists the people mentioned, each once, in order.
func Mentions(body string) []uuid.UUID {
	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(body, -1) {
		id, err := uuid.Parse(m[1])
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// Plain replaces mentions with @Name (or "@someone") for previews.
func Plain(body string, names map[uuid.UUID]string) string {
	return mentionRe.ReplaceAllStringFunc(body, func(tok string) string {
		id, err := uuid.Parse(tok[2 : len(tok)-1])
		if err == nil {
			if n, ok := names[id]; ok {
				return "@" + n
			}
		}
		return "@someone"
	})
}

// TaskRefs finds task keys (FE-142), each once, at most MaxTaskRefs.
func TaskRefs(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range taskRe.FindAllString(mentionRe.ReplaceAllString(body, " "), -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
		if len(out) == MaxTaskRefs {
			break
		}
	}
	return out
}

// ChannelName turns "Release Notes!" into "release-notes". It returns "" when
// nothing usable is left.
func ChannelName(s string) string {
	s = nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	s = strings.Trim(s, "-")
	if len(s) > MaxChannelLen {
		s = strings.TrimRight(s[:MaxChannelLen], "-")
	}
	if !nameRe.MatchString(s) {
		return ""
	}
	return s
}

// DefaultChannel is a new team's first channel: "frontend-general".
func DefaultChannel(team string) string {
	base := ChannelName(team)
	if base == "" {
		base = "team"
	}
	if len(base) > 31 {
		base = strings.TrimRight(base[:31], "-")
	}
	return base + "-general"
}

// Snippet cuts text for a notification.
func Snippet(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return s
}
