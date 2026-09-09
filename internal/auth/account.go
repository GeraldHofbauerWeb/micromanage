// Package auth manages the accounts a launch runs as.
package auth

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Kind distinguishes a real Microsoft account from a local one.
type Kind string

const (
	// KindMSA is a Microsoft account, able to play online.
	KindMSA Kind = "msa"
	// KindOffline is a local account with no authentication. Single-player
	// works fully; servers running in online mode reject it.
	KindOffline Kind = "offline"
)

// Account is a player identity.
type Account struct {
	Kind Kind   `json:"kind"`
	UUID string `json:"uuid"` // undashed
	Name string `json:"name"`

	// XUID comes from the XSTS token and is templated into modern versions'
	// arguments. Offline accounts use "0".
	XUID string `json:"xuid,omitempty"`

	// MSRefreshToken renews the Microsoft session. Present only for KindMSA.
	MSRefreshToken string `json:"ms_refresh_token,omitempty"`
	// MCAccessToken is the Minecraft services token used at launch.
	MCAccessToken string    `json:"mc_access_token,omitempty"`
	MCExpiresAt   time.Time `json:"mc_expires_at,omitzero"`

	// NeedsReauth is set when a refresh token was rejected, so the UI can ask
	// for a fresh sign-in instead of failing at launch.
	NeedsReauth bool `json:"needs_reauth,omitempty"`

	LastUsed time.Time `json:"last_used,omitzero"`
}

// expirySkew treats a token as expired slightly early, so it cannot lapse
// between the check and the launch.
const expirySkew = 5 * time.Minute

// Usable reports whether the account can launch right now without a refresh.
func (a Account) Usable() bool {
	if a.Kind == KindOffline {
		return true
	}
	if a.NeedsReauth || a.MCAccessToken == "" {
		return false
	}
	return time.Now().Add(expirySkew).Before(a.MCExpiresAt)
}

// RenewableWithin reports whether a Microsoft session is worth renewing now
// because it lapses inside d.
//
// This is what the launcher checks in the background, ahead of any launch:
// Usable answers "can this play right this second", which is the wrong
// question an hour before a session runs out.
func (a Account) RenewableWithin(d time.Duration) bool {
	if a.Kind != KindMSA || a.NeedsReauth || a.MSRefreshToken == "" {
		return false
	}
	return !time.Now().Add(d).Before(a.MCExpiresAt)
}

// AccessToken returns the token to launch with. Offline accounts use "0",
// which is what the game expects when there is no session.
func (a Account) AccessToken() string {
	if a.Kind == KindOffline {
		return "0"
	}
	return a.MCAccessToken
}

// UserType is the value templated into ${user_type}.
func (a Account) UserType() string {
	if a.Kind == KindOffline {
		return "legacy"
	}
	return "msa"
}

// EffectiveXUID returns the XUID to template, defaulting to "0" — modern
// versions misbehave when it is empty rather than absent.
func (a Account) EffectiveXUID() string {
	if a.XUID == "" {
		return "0"
	}
	return a.XUID
}

// NewOfflineAccount creates a local account.
//
// The UUID follows the same convention servers use for offline players — a
// version 3 UUID over "OfflinePlayer:<name>" — so worlds and inventories stay
// attached to the name across launchers.
func NewOfflineAccount(name string) (Account, error) {
	name = strings.TrimSpace(name)
	if err := ValidatePlayerName(name); err != nil {
		return Account{}, err
	}
	return Account{
		Kind: KindOffline,
		Name: name,
		UUID: OfflineUUID(name),
		XUID: "0",
	}, nil
}

// OfflineUUID computes the offline-mode UUID for a player name.
func OfflineUUID(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))

	// Stamp version 3 and the RFC 4122 variant, as the servers do.
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80

	return hex.EncodeToString(sum[:])
}

// DashedUUID renders an undashed UUID in canonical 8-4-4-4-12 form.
func DashedUUID(undashed string) string {
	if len(undashed) != 32 {
		return undashed
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		undashed[0:8], undashed[8:12], undashed[12:16], undashed[16:20], undashed[20:32])
}

// ValidatePlayerName applies Minecraft's own rules for a username.
func ValidatePlayerName(name string) error {
	if name == "" {
		return fmt.Errorf("player name cannot be empty")
	}
	if len(name) > 16 {
		return fmt.Errorf("player name %q is longer than 16 characters", name)
	}
	for _, r := range name {
		isAllowed := r == '_' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
		if !isAllowed {
			return fmt.Errorf("player name %q contains %q; only letters, digits and _ are allowed", name, r)
		}
	}
	return nil
}
