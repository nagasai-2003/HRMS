package chattext

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestMentions(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	body := "hi <@" + a.String() + "> and <@" + b.String() + "> and again <@" + a.String() + "> <@nope>"
	got := Mentions(body)
	if !slices.Equal(got, []uuid.UUID{a, b}) {
		t.Fatalf("got %v", got)
	}
	if p := Plain(body, map[uuid.UUID]string{a: "Ananya Rao"}); p != "hi @Ananya Rao and @someone and again @Ananya Rao <@nope>" {
		t.Fatalf("plain %q", p)
	}
}

func TestTaskRefs(t *testing.T) {
	got := TaskRefs("FE-6 is done, see BE-12 and FE-6 again; not fe-7, X-1, FE-0 or ABCDEFG-1. WEB2-99")
	if !slices.Equal(got, []string{"FE-6", "BE-12", "WEB2-99"}) {
		t.Fatalf("got %v", got)
	}
	if n := len(TaskRefs("A1-1 A1-2 A1-3 A1-4 A1-5 A1-6")); n != MaxTaskRefs {
		t.Fatalf("cap %d", n)
	}
}

func TestChannelName(t *testing.T) {
	cases := map[string]string{
		"Release Notes!":   "release-notes",
		"  --fe releases ": "fe-releases",
		"###":              "",
		"Ünïcode":          "n-code",
	}
	for in, want := range cases {
		if got := ChannelName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if got := DefaultChannel("Frontend"); got != "frontend-general" {
		t.Fatal(got)
	}
	if got := DefaultChannel("தமிழ்"); got != "team-general" {
		t.Fatal(got)
	}
	if got := DefaultChannel("A very long team name that keeps going on"); len(got) > MaxChannelLen {
		t.Fatal(got)
	}
	if Snippet("a  b\n c", 10) != "a b c" || Snippet("abcdefghijkl", 5) != "abcd…" {
		t.Fatal("snippet")
	}
}
