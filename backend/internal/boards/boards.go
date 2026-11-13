// Package boards holds the plain rules behind team task boards: status group
// templates, team keys (the FE in FE-142), task references and labels.
package boards

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"hrms/backend/internal/store"
)

type StatusDef struct {
	Name     string
	Color    string
	Category store.StatusCategory
}

const (
	grey   = "#8A909C"
	blue   = "#2F5BD3"
	orange = "#D97706"
	red    = "#B42318"
	green  = "#15803D"
	purple = "#7C3AED"
	teal   = "#0E7490"
)

var (
	notStarted = store.StatusCategoryNotStarted
	active     = store.StatusCategoryActive
	done       = store.StatusCategoryDone
)

// Templates a team can start from. "default" is what a new team gets.
var Templates = map[string][]StatusDef{
	"default": {
		{"To do", grey, notStarted}, {"In progress", blue, active}, {"In review", orange, active},
		{"Blocked", red, active}, {"Done", green, done},
	},
	"software": {
		{"To do", grey, notStarted}, {"In progress", blue, active}, {"In review", orange, active},
		{"QA", purple, active}, {"Done", green, done},
	},
	"design": {
		{"Brief", grey, notStarted}, {"Exploring", blue, active}, {"Feedback", orange, active},
		{"Final", purple, active}, {"Delivered", green, done},
	},
	"sales": {
		{"Lead", grey, notStarted}, {"Contacted", blue, active}, {"Demo", teal, active},
		{"Negotiation", orange, active}, {"Won", green, done}, {"Lost", red, done},
	},
}

var (
	keyRe   = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,5}$`)
	colorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	refRe   = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,5})-([0-9]{1,7})$`)
)

// ValidKey: 2 to 6 capital letters or digits, starting with a letter.
func ValidKey(k string) bool { return keyRe.MatchString(k) }

// DeriveKey suggests a key from a team name: initials for several words
// ("Release squad" → RS), else the first three letters ("Frontend" → FRO).
// It returns "" when the name has nothing usable.
func DeriveKey(name string) string {
	var words []string
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return !isASCIIAlnum(r) }) {
		words = append(words, strings.ToUpper(w))
	}
	var k string
	if len(words) >= 2 {
		for _, w := range words {
			if len(k) < 4 {
				k += w[:1]
			}
		}
	} else if len(words) == 1 {
		k = words[0]
		if len(k) > 3 {
			k = k[:3]
		}
	}
	if !ValidKey(k) {
		return ""
	}
	return k
}

func isASCIIAlnum(r rune) bool { return r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

// ValidColor: a #RRGGBB colour.
func ValidColor(c string) bool { return colorRe.MatchString(c) }

// ParseRef reads a task reference such as "fe-142" as ("FE", 142).
func ParseRef(s string) (string, int32, bool) {
	m := refRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return "", 0, false
	}
	return strings.ToUpper(m[1]), int32(n), true
}

const (
	MaxLabels   = 10
	MaxLabelLen = 24
)

// NormLabels trims labels, squeezes spaces, drops empties and repeats
// (ignoring case) and keeps the first spelling. ok is false when there are
// too many or one is too long.
func NormLabels(in []string) (out []string, ok bool) {
	out = []string{}
	seen := map[string]bool{}
	for _, l := range in {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			continue
		}
		if utf8.RuneCountInString(l) > MaxLabelLen {
			return nil, false
		}
		k := strings.ToLower(l)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, l)
	}
	if len(out) > MaxLabels {
		return nil, false
	}
	return out, true
}

// Insert puts id into ids at index (clamped), for drag and drop.
func Insert[T any](ids []T, index int, id T) []T {
	if index < 0 {
		index = 0
	}
	if index > len(ids) {
		index = len(ids)
	}
	out := make([]T, 0, len(ids)+1)
	out = append(out, ids[:index]...)
	out = append(out, id)
	return append(out, ids[index:]...)
}
