package auth

import (
	"testing"
	"time"
)

// TestOfflineUUID pins the offline-mode convention. Servers derive the same
// value, so a world stays attached to the name across launchers; changing this
// would orphan inventories.
func TestOfflineUUID(t *testing.T) {
	got := OfflineUUID("Gerry")
	if len(got) != 32 {
		t.Fatalf("uuid = %q, want 32 hex characters", got)
	}

	// Version 3 and the RFC 4122 variant must be stamped in.
	if got[12] != '3' {
		t.Errorf("version nibble = %q, want 3", got[12])
	}
	switch got[16] {
	case '8', '9', 'a', 'b':
	default:
		t.Errorf("variant nibble = %q, want one of 8/9/a/b", got[16])
	}

	if OfflineUUID("Gerry") != got {
		t.Error("the same name produced two different uuids")
	}
	if OfflineUUID("Sebi") == got {
		t.Error("two different names produced the same uuid")
	}
}

func TestDashedUUID(t *testing.T) {
	if got := DashedUUID("0123456789abcdef0123456789abcdef"); got != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Errorf("DashedUUID = %q", got)
	}
	// Anything that is not 32 characters is passed through untouched.
	if got := DashedUUID("short"); got != "short" {
		t.Errorf("DashedUUID(%q) = %q", "short", got)
	}
}

func TestValidatePlayerName(t *testing.T) {
	for _, ok := range []string{"Gerry", "a", "_x_", "Player123", "0123456789abcdef"} {
		if err := ValidatePlayerName(ok); err != nil {
			t.Errorf("ValidatePlayerName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "0123456789abcdefg", "has space", "hyphen-ated", "emoji🙂", "semi;colon"} {
		if err := ValidatePlayerName(bad); err == nil {
			t.Errorf("ValidatePlayerName(%q) = nil, want an error", bad)
		}
	}
}

func TestNewOfflineAccount(t *testing.T) {
	a, err := NewOfflineAccount("  Gerry  ")
	if err != nil {
		t.Fatalf("NewOfflineAccount: %v", err)
	}
	if a.Name != "Gerry" {
		t.Errorf("name = %q, want the trimmed name", a.Name)
	}
	if a.Kind != KindOffline {
		t.Errorf("kind = %q", a.Kind)
	}
	if !a.Usable() {
		t.Error("a fresh offline account is not usable")
	}
	if a.AccessToken() != "0" {
		t.Errorf("access token = %q, want 0", a.AccessToken())
	}
	if a.UserType() != "legacy" {
		t.Errorf("user type = %q, want legacy", a.UserType())
	}
	if a.EffectiveXUID() != "0" {
		t.Errorf("xuid = %q, want 0", a.EffectiveXUID())
	}

	if _, err := NewOfflineAccount("bad name"); err == nil {
		t.Error("an invalid name was accepted")
	}
}

func TestMSAAccountUsable(t *testing.T) {
	valid := Account{Kind: KindMSA, MCAccessToken: "t", MCExpiresAt: time.Now().Add(time.Hour)}
	if !valid.Usable() {
		t.Error("a token valid for an hour was reported unusable")
	}

	// The skew must reject a token that expires during the launch.
	soon := Account{Kind: KindMSA, MCAccessToken: "t", MCExpiresAt: time.Now().Add(time.Minute)}
	if soon.Usable() {
		t.Error("a token expiring in a minute was accepted")
	}

	expired := Account{Kind: KindMSA, MCAccessToken: "t", MCExpiresAt: time.Now().Add(-time.Hour)}
	if expired.Usable() {
		t.Error("an expired token was accepted")
	}

	none := Account{Kind: KindMSA, MCExpiresAt: time.Now().Add(time.Hour)}
	if none.Usable() {
		t.Error("an account with no token was accepted")
	}

	flagged := valid
	flagged.NeedsReauth = true
	if flagged.Usable() {
		t.Error("an account flagged for re-authentication was accepted")
	}
}
