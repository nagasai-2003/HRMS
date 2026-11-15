package mfa

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1, last 6 of the 8 digits.
func TestCodeRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, c := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		if got := Code(secret, Step(time.Unix(c.unix, 0))); got != c.want {
			t.Errorf("T=%d: got %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestVerify(t *testing.T) {
	secret := NewSecret()
	now := time.Unix(1_800_000_000, 0)
	cur := Step(now)

	if st, ok := Verify(secret, Code(secret, cur), now, 0); !ok || st != cur {
		t.Fatalf("current code refused: %d %v", st, ok)
	}
	if _, ok := Verify(secret, Code(secret, cur-1), now, 0); !ok {
		t.Error("previous step should pass (clock skew)")
	}
	if _, ok := Verify(secret, Code(secret, cur+1), now, 0); !ok {
		t.Error("next step should pass (clock skew)")
	}
	if _, ok := Verify(secret, Code(secret, cur-2), now, 0); ok {
		t.Error("two steps old should fail")
	}
	if _, ok := Verify(secret, Code(secret, cur), now, cur); ok {
		t.Error("a used code must not pass again")
	}
	if _, ok := Verify(secret, Code(secret, cur-1), now, cur); ok {
		t.Error("an older code must not pass after a newer one was used")
	}
	spaced := Code(secret, cur)[:3] + " " + Code(secret, cur)[3:]
	if _, ok := Verify(secret, spaced, now, 0); !ok {
		t.Error("a code typed with a space should pass")
	}
	if _, ok := Verify(NewSecret(), Code(secret, cur), now, 0); ok {
		t.Error("another secret's code must fail")
	}
}

func TestCleanCode(t *testing.T) {
	for in, want := range map[string]string{
		"123456": "123456", "123 456": "123456", "123-456": "123456",
		"12345": "", "1234567": "", "12a456": "", "": "",
	} {
		if got := CleanCode(in); got != want {
			t.Errorf("CleanCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes := NewRecoveryCodes(10)
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Fatalf("bad shape %q", c)
		}
		cl := CleanRecovery(c)
		if cl == "" || seen[cl] {
			t.Fatalf("code %q not usable or repeated", c)
		}
		seen[cl] = true
		if CleanRecovery(strings.ToUpper(c)) != cl || CleanRecovery(strings.ReplaceAll(c, "-", " ")) != cl {
			t.Errorf("typed variants of %q should match", c)
		}
	}
	if CleanRecovery("123456") != "" || CleanRecovery("abcde-fghi0") != "" {
		t.Error("a 6-digit code or a code with 0 is not a recovery code")
	}
	if bytes.Equal(HashRecovery("aaaaaaaaaa"), HashRecovery("aaaaaaaaab")) {
		t.Error("hashes collide")
	}
}

func TestBox(t *testing.T) {
	if _, err := NewBox("short"); err == nil {
		t.Fatal("a short key must be refused")
	}
	b, err := NewBox(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	secret := NewSecret()
	sealed := b.Seal(secret, []byte("user-1"))
	got, err := b.Open(sealed, []byte("user-1"))
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("open: %v", err)
	}
	if _, err := b.Open(sealed, []byte("user-2")); err == nil {
		t.Error("a secret moved to another user must not open")
	}
	other, _ := NewBox(strings.Repeat("cd", 32))
	if _, err := other.Open(sealed, []byte("user-1")); err == nil {
		t.Error("another key must not open it")
	}
}

func TestURIAndKey(t *testing.T) {
	secret := []byte("12345678901234567890")
	u := URI("HR Workspace", "a@b.com", secret)
	if !strings.HasPrefix(u, "otpauth://totp/HR%20Workspace:a@b.com?") || !strings.Contains(u, "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ") {
		t.Errorf("uri %s", u)
	}
	if k := Key(secret); k != "GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ" {
		t.Errorf("key %q", k)
	}
	svg, err := QRSVG(u)
	if err != nil || !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "h1v1") {
		t.Errorf("svg: %v", err)
	}
}
