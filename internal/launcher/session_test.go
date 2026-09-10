package launcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeraldHofbauerWeb/instant-launcher/internal/auth"
)

// msaAccount is a signed-in Microsoft account whose session runs out at a
// given time.
func msaAccount(expires time.Time) auth.Account {
	return auth.Account{
		Kind:           auth.KindMSA,
		UUID:           "069a79f444e94726a5befca90e38aaf5",
		Name:           "Notch",
		MSRefreshToken: "MS-REFRESH",
		MCAccessToken:  "OLD-TOKEN",
		MCExpiresAt:    expires,
	}
}

// TestBackgroundRenewalGetsAheadOfTheExpiry covers the point of renewing at
// all: a session that is still good right now, but would not survive until
// the player next presses Play, is replaced before they do.
func TestBackgroundRenewalGetsAheadOfTheExpiry(t *testing.T) {
	stub := newSignInStub(t)
	ctrl := newTestController(t, stub)

	account := msaAccount(time.Now().Add(30 * time.Minute))
	if !account.Usable() {
		t.Fatal("the account should still be usable; the test is about the hour after that")
	}
	if err := ctrl.Accounts.Add(account); err != nil {
		t.Fatal(err)
	}

	ctrl.maybeRenewSession(context.Background())

	stored, _ := ctrl.Accounts.Active()
	if stored.MCAccessToken != "MC-TOKEN" {
		t.Errorf("the session was not renewed: %+v", stored)
	}
	if !stored.MCExpiresAt.After(account.MCExpiresAt) {
		t.Errorf("expiry did not move: %s", stored.MCExpiresAt)
	}
	if got := stub.refreshes.Load(); got != 1 {
		t.Errorf("renewals = %d, want exactly one", got)
	}
}

// TestBackgroundRenewalLeavesALiveSessionAlone guards the other side: a
// session with most of its day left is not worth a round trip on every
// refresh.
func TestBackgroundRenewalLeavesALiveSessionAlone(t *testing.T) {
	stub := newSignInStub(t)
	ctrl := newTestController(t, stub)

	if err := ctrl.Accounts.Add(msaAccount(time.Now().Add(10 * time.Hour))); err != nil {
		t.Fatal(err)
	}
	ctrl.maybeRenewSession(context.Background())

	if got := stub.refreshes.Load(); got != 0 {
		t.Errorf("renewals = %d, want none", got)
	}
	if stored, _ := ctrl.Accounts.Active(); stored.MCAccessToken != "OLD-TOKEN" {
		t.Errorf("a live session was replaced: %+v", stored)
	}

	// An offline account has no session to renew and no service to ask.
	offline, err := auth.NewOfflineAccount("Steve")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Accounts.Add(offline); err != nil {
		t.Fatal(err)
	}
	ctrl.maybeRenewSession(context.Background())
	if got := stub.refreshes.Load(); got != 0 {
		t.Errorf("an offline account caused %d renewals", got)
	}
}

// TestBackgroundRenewalReportsARevokedSessionQuietly is why this runs early:
// the account is marked as needing a fresh sign-in, which the account button
// shows in red, and no error is thrown at a player who has not asked for
// anything yet.
func TestBackgroundRenewalReportsARevokedSessionQuietly(t *testing.T) {
	stub := newSignInStub(t)
	stub.refreshRejected.Store(true)
	ctrl := newTestController(t, stub)

	if err := ctrl.Accounts.Add(msaAccount(time.Now().Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}
	ctrl.maybeRenewSession(context.Background())

	stored, _ := ctrl.Accounts.Active()
	if !stored.NeedsReauth {
		t.Error("a revoked session was not marked for a fresh sign-in")
	}
	if stored.MSRefreshToken != "" {
		t.Error("a refused refresh token was kept")
	}
	snap := waitFor(t, ctrl, "the account to reach the screen", func(s Snapshot) bool {
		return s.Active.NeedsReauth
	})
	if snap.Err != nil {
		t.Errorf("a background renewal put an error on screen: %v", snap.Err)
	}

	// And it stays quiet: a second refresh does not keep hammering a token
	// Microsoft has already refused.
	before := stub.refreshes.Load()
	ctrl.maybeRenewSession(context.Background())
	if after := stub.refreshes.Load(); after != before {
		t.Errorf("a rejected account was retried: %d renewals", after-before)
	}
}

// TestRefreshDoesNotWaitForTheSession is the requirement itself: opening the
// window must not get slower because a session is being renewed. The renewal
// is held at Microsoft's door while the refresh is expected to finish anyway.
func TestRefreshDoesNotWaitForTheSession(t *testing.T) {
	stub := newSignInStub(t)
	gate := make(chan struct{})
	stub.refreshGate = gate
	ctrl := newTestController(t, stub)
	m := isolateInstances(t, ctrl)

	if err := os.MkdirAll(filepath.Join(m.InstancesPath, "pack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Accounts.Add(msaAccount(time.Now().Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctrl.doRefresh(context.Background())
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the refresh waited for the session renewal")
	}

	snap := waitFor(t, ctrl, "the instances", func(s Snapshot) bool { return len(s.Instances) == 1 })
	if snap.Instances[0].Name != "pack" {
		t.Errorf("instances = %+v", snap.Instances)
	}

	// Let the renewal finish, and see it land.
	close(gate)
	waitFor(t, ctrl, "the renewed session", func(s Snapshot) bool {
		return s.Active.MCAccessToken == "MC-TOKEN"
	})
}
