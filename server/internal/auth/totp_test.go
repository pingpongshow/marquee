package auth

import (
	"encoding/base32"
	"testing"
	"time"
)

// RFC 6238 test vector (SHA-1, secret "12345678901234567890"), truncated to 6 digits.
func TestTOTPVector(t *testing.T) {
	secret := []byte("12345678901234567890")
	cases := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for unix, want := range cases {
		if got := totpCode(secret, unix/totpStep); got != want {
			t.Errorf("t=%d: %s, want %s", unix, got, want)
		}
	}
	b := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	now := time.Unix(1234567890, 0)
	if totpMatch(b, "005924", now) == 0 || totpMatch(b, "005924", now.Add(31*time.Second)) == 0 {
		t.Error("current and previous step accepted")
	}
	if totpMatch(b, "005924", now.Add(95*time.Second)) != 0 {
		t.Error("old codes rejected")
	}
}
