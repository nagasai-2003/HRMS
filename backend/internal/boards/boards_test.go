package boards

import (
	"slices"
	"strings"
	"testing"

	"hrms/backend/internal/store"
)

func TestDeriveKey(t *testing.T) {
	for name, want := range map[string]string{
		"Frontend":         "FRO",
		"Release squad":    "RS",
		"HR":               "HR",
		"Q":                "",
		"42 crew":          "",
		"Data & ML team":   "DMT",
		"a b c d e f":      "ABCD",
		"Ünïcode only ü":   "NCO",
		"":                 "",
		"Platform-Backend": "PB",
	} {
		if got := DeriveKey(name); got != want {
			t.Errorf("DeriveKey(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestValidKey(t *testing.T) {
	for k, want := range map[string]bool{"FE": true, "QA2": true, "ABCDEF": true, "ABCDEFG": false, "F": false, "fe": false, "2FE": false, "F-E": false} {
		if ValidKey(k) != want {
			t.Errorf("ValidKey(%q) = %v", k, !want)
		}
	}
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		in   string
		key  string
		n    int32
		okay bool
	}{
		{"FE-142", "FE", 142, true},
		{" fe-7 ", "FE", 7, true},
		{"QA2-1", "QA2", 1, true},
		{"FE-0", "", 0, false},
		{"FE142", "", 0, false},
		{"Fix login", "", 0, false},
		{"F-1", "", 0, false},
		{"FE-12345678", "", 0, false},
	}
	for _, c := range cases {
		k, n, ok := ParseRef(c.in)
		if k != c.key || n != c.n || ok != c.okay {
			t.Errorf("ParseRef(%q) = %q %d %v", c.in, k, n, ok)
		}
	}
}

func TestNormLabels(t *testing.T) {
	got, ok := NormLabels([]string{" Bug ", "bug", "", "  Design   system ", "UI"})
	if !ok || !slices.Equal(got, []string{"Bug", "Design system", "UI"}) {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := NormLabels([]string{strings.Repeat("x", 25)}); ok {
		t.Error("a 25-character label should be refused")
	}
	many := make([]string, 11)
	for i := range many {
		many[i] = string(rune('a' + i))
	}
	if _, ok := NormLabels(many); ok {
		t.Error("11 labels should be refused")
	}
	if got, ok := NormLabels(nil); !ok || got == nil || len(got) != 0 {
		t.Error("no labels should give an empty list")
	}
}

func TestTemplates(t *testing.T) {
	for name, defs := range Templates {
		hasDone := false
		seen := map[string]bool{}
		for _, d := range defs {
			if !ValidColor(d.Color) {
				t.Errorf("%s: bad colour %s", name, d.Color)
			}
			if seen[strings.ToLower(d.Name)] {
				t.Errorf("%s: %s twice", name, d.Name)
			}
			seen[strings.ToLower(d.Name)] = true
			hasDone = hasDone || d.Category == store.StatusCategoryDone
		}
		if !hasDone {
			t.Errorf("%s has no Done group", name)
		}
	}
}

func TestInsert(t *testing.T) {
	ids := []string{"a", "b", "c"}
	for idx, want := range map[int]string{-3: "xabc", 0: "xabc", 1: "axbc", 3: "abcx", 9: "abcx"} {
		if got := strings.Join(Insert(ids, idx, "x"), ""); got != want {
			t.Errorf("Insert at %d = %s, want %s", idx, got, want)
		}
	}
	if strings.Join(ids, "") != "abc" {
		t.Error("Insert changed its input")
	}
}
