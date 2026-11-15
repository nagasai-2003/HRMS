// Package mfa is sign-in with an authenticator app: time-based one-time
// codes (TOTP, RFC 6238: SHA-1, 6 digits, 30 seconds, as Google and
// Microsoft Authenticator use), one-time recovery codes, encryption of the
// stored secrets and the QR code people scan.
package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

const (
	period = 30
	digits = 6
	// Accept the code before and after the current one, for phone clocks
	// that are a little off.
	skew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns 20 random bytes, the size RFC 4226 recommends.
func NewSecret() []byte {
	b := make([]byte, 20)
	rand.Read(b)
	return b
}

// Key is the secret as people type it into an app: base32 in groups of 4.
func Key(secret []byte) string {
	s := b32.EncodeToString(secret)
	var out []string
	for len(s) > 4 {
		out = append(out, s[:4])
		s = s[4:]
	}
	return strings.Join(append(out, s), " ")
}

// URI is what the QR code holds. Apps show "issuer (account)".
func URI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", b32.EncodeToString(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(digits))
	q.Set("period", fmt.Sprint(period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Step is the 30-second window a time falls in.
func Step(t time.Time) int64 { return t.Unix() / period }

// Code is the 6-digit code for one step.
func Code(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", digits, n%1_000_000)
}

// CleanCode keeps the digits of what someone typed ("123 456" → "123456").
// It returns "" unless exactly 6 digits remain.
func CleanCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
		default:
			return ""
		}
	}
	if b.Len() != digits {
		return ""
	}
	return b.String()
}

// Verify checks a code against the steps around now. It only accepts a
// step later than lastStep, so a code cannot be used twice. It returns the
// step that matched, to store as the new lastStep.
func Verify(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code = CleanCode(code)
	if code == "" {
		return 0, false
	}
	cur := Step(now)
	for st := cur - skew; st <= cur+skew; st++ {
		if st <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(Code(secret, st)), []byte(code)) == 1 {
			return st, true
		}
	}
	return 0, false
}

// ---- recovery codes ----

// No 0/o, 1/l/i: easy to read off paper.
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

const recoveryLen = 10

// NewRecoveryCodes returns n codes such as "k7m2p-x9qrt".
func NewRecoveryCodes(n int) []string {
	out := make([]string, n)
	buf := make([]byte, recoveryLen)
	for i := range out {
		rand.Read(buf)
		var b strings.Builder
		for j, c := range buf {
			if j == recoveryLen/2 {
				b.WriteByte('-')
			}
			// 256 is not a multiple of 31; the bias is tiny and harmless for
			// 50-bit one-time codes.
			b.WriteByte(recoveryAlphabet[int(c)%len(recoveryAlphabet)])
		}
		out[i] = b.String()
	}
	return out
}

// CleanRecovery lowercases a typed recovery code and drops spaces and
// dashes. It returns "" if it cannot be one.
func CleanRecovery(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ' || r == '-':
		case strings.ContainsRune(recoveryAlphabet, r):
			b.WriteRune(r)
		default:
			return ""
		}
	}
	if b.Len() != recoveryLen {
		return ""
	}
	return b.String()
}

// HashRecovery is what the database keeps. The codes are random enough that
// a plain SHA-256 is safe.
func HashRecovery(cleaned string) []byte {
	h := sha256.Sum256([]byte("hrms-recovery:" + cleaned))
	return h[:]
}

// ---- secrets at rest ----

// Box encrypts authenticator secrets with MFA_KEY (AES-256-GCM), so a copy
// of the database alone is not enough to make codes.
type Box struct{ aead cipher.AEAD }

// NewBox takes the key as 64 hex characters.
func NewBox(hexKey string) (*Box, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil || len(key) != 32 {
		return nil, errors.New("MFA_KEY must be 64 hex characters (openssl rand -hex 32)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal binds the secret to its owner (ad, the user ID), so a secret copied
// to another row does not open.
func (b *Box) Seal(secret, ad []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	rand.Read(nonce)
	return b.aead.Seal(nonce, nonce, secret, ad)
}

func (b *Box) Open(sealed, ad []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("mfa: sealed secret too short")
	}
	return b.aead.Open(nil, sealed[:n], sealed[n:], ad)
}

// ---- QR code ----

// QRSVG draws text as a QR code in SVG, with a quiet zone of 4 modules.
func QRSVG(text string) (string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	size := c.Size + 2*quiet
	var path strings.Builder
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="#fff"/><path fill="#000" d="%s"/></svg>`, size, size, path.String()), nil
}
