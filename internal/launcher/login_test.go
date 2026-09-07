package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/auth"
	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
)

// signInStub stands in for Microsoft, Xbox Live and Minecraft services, so the
// controller's sign-in can be driven end to end without a network.
type signInStub struct {
	*httptest.Server

	// authorised gates the token endpoint the way the player does: until it is
	// set, the flow reports that it is still waiting on the browser.
	authorised atomic.Bool
	// refreshRejected makes a token refresh fail the way a revoked one does.
	refreshRejected atomic.Bool
}

func newSignInStub(t *testing.T) *signInStub {
	t.Helper()

	stub := &signInStub{}
	mux := http.NewServeMux()

	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, 200, map[string]any{
			"device_code":      "DEV-CODE",
			"user_code":        "WXYZ-1234",
			"verification_uri": "https://microsoft.com/link",
			"expires_in":       900,
			"interval":         1,
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			if stub.refreshRejected.Load() {
				writeTestJSON(w, 400, map[string]any{"error": "invalid_grant"})
				return
			}
			writeTestJSON(w, 200, map[string]any{
				"access_token": "MS-ACCESS", "refresh_token": "MS-REFRESH-2",
			})
			return
		}
		if !stub.authorised.Load() {
			writeTestJSON(w, 400, map[string]any{"error": "authorization_pending"})
			return
		}
		writeTestJSON(w, 200, map[string]any{
			"access_token": "MS-ACCESS", "refresh_token": "MS-REFRESH",
		})
	})

	mux.HandleFunc("/xbl", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, 200, map[string]any{
			"Token":         "XBL-TOKEN",
			"DisplayClaims": map[string]any{"xui": []map[string]string{{"uhs": "USERHASH"}}},
		})
	})

	mux.HandleFunc("/xsts", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, 200, map[string]any{
			"Token": "XSTS-TOKEN",
			"DisplayClaims": map[string]any{
				"xui": []map[string]string{{"uhs": "USERHASH", "xid": "2535499999"}},
			},
		})
	})

	mux.HandleFunc("/mclogin", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, 200, map[string]any{"access_token": "MC-TOKEN", "expires_in": 86400})
	})

	mux.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, 200, map[string]any{
			"id": "069a79f444e94726a5befca90e38aaf5", "name": "Notch",
		})
	})

	stub.Server = httptest.NewServer(mux)
	t.Cleanup(stub.Close)
	return stub
}

func (s *signInStub) endpoints() auth.Endpoints {
	return auth.Endpoints{
		DeviceCode: s.URL + "/devicecode",
		Token:      s.URL + "/token",
		XboxAuth:   s.URL + "/xbl",
		XSTS:       s.URL + "/xsts",
		MCLogin:    s.URL + "/mclogin",
		MCProfile:  s.URL + "/profile",
	}
}

func writeTestJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// newTestController wires a controller against a temporary app directory and
// drains its events into the store, which is what the render loop's pump does
// in the running launcher.
func newTestController(t *testing.T, stub *signInStub) *Controller {
	t.Helper()

	dir := t.TempDir()
	accounts, err := NewAccountStore(dir)
	if err != nil {
		t.Fatalf("NewAccountStore: %v", err)
	}

	manager := &instance.Manager{AppDir: dir, InstancesPath: dir, MinecraftPath: dir, BackupPath: dir}
	ctrl := NewController(manager, NewStore(), accounts, "test")
	ctrl.MSAClientID = "test-client-id"
	ctrl.MSAEndpoints = stub.endpoints()

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case e := <-ctrl.Events():
				if e.Apply != nil {
					e.Apply(ctrl.Store())
				}
			}
		}
	}()

	return ctrl
}

// waitFor polls the store until a condition holds, which is how a test watches
// state that several goroutines produce.
func waitFor(t *testing.T, ctrl *Controller, what string, cond func(Snapshot) bool) Snapshot {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snap := ctrl.Store().Snapshot()
		if cond(snap) {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; last state: %+v", what, ctrl.Store().Snapshot())
	return Snapshot{}
}

func TestMicrosoftSignInPublishesTheCodeThenTheAccount(t *testing.T) {
	stub := newSignInStub(t)
	ctrl := newTestController(t, stub)

	ctrl.Dispatch(ActionLoginMicrosoft{})

	// The code has to reach the screen while the flow is still waiting, or
	// there is nothing for the player to enter.
	snap := waitFor(t, ctrl, "the device code", func(s Snapshot) bool {
		return s.Login.Active && s.Login.UserCode != ""
	})
	if snap.Login.UserCode != "WXYZ-1234" {
		t.Errorf("user code = %q", snap.Login.UserCode)
	}
	if snap.Login.VerificationURI != "https://microsoft.com/link" {
		t.Errorf("verification uri = %q", snap.Login.VerificationURI)
	}
	if snap.Screen != ScreenLogin {
		t.Errorf("screen = %v, want the login screen while a code is pending", snap.Screen)
	}
	if snap.Login.ExpiresAt.IsZero() {
		t.Error("no expiry published; the countdown would have nothing to show")
	}

	// The player finishes in the browser.
	stub.authorised.Store(true)

	snap = waitFor(t, ctrl, "the signed-in account", func(s Snapshot) bool {
		return s.HasAccount
	})
	if snap.Login.Active {
		t.Error("the sign-in panel is still showing after it succeeded")
	}
	if snap.Active.Name != "Notch" || snap.Active.Kind != auth.KindMSA {
		t.Errorf("active account = %+v", snap.Active)
	}
	if snap.Active.XUID != "2535499999" {
		t.Errorf("xuid = %q, want the one XSTS issued", snap.Active.XUID)
	}
	if !snap.Active.Usable() {
		t.Error("a freshly signed-in account should be usable")
	}
	if snap.Screen != ScreenInstances {
		t.Errorf("screen = %v, want the instance list after signing in", snap.Screen)
	}
	if !strings.Contains(snap.Status, "Notch") {
		t.Errorf("status = %q, want it to name the account", snap.Status)
	}

	// It has to survive a restart, which is the whole point of storing it.
	reloaded, err := NewAccountStore(ctrl.Manager.AppDir)
	if err != nil {
		t.Fatalf("NewAccountStore: %v", err)
	}
	stored, ok := reloaded.Active()
	if !ok || stored.MCAccessToken != "MC-TOKEN" || stored.MSRefreshToken != "MS-REFRESH" {
		t.Errorf("stored account = %+v, ok = %v", stored, ok)
	}
}

func TestMicrosoftSignInCancelledIsNotAFailure(t *testing.T) {
	stub := newSignInStub(t)
	ctrl := newTestController(t, stub)

	id := ctrl.Dispatch(ActionLoginMicrosoft{})
	waitFor(t, ctrl, "the device code", func(s Snapshot) bool { return s.Login.Active })

	ctrl.Cancel(id)

	snap := waitFor(t, ctrl, "the cancelled sign-in", func(s Snapshot) bool { return !s.Login.Active })
	if snap.Err != nil {
		t.Errorf("cancelling reported an error: %v", snap.Err)
	}
	if snap.HasAccount {
		t.Error("a cancelled sign-in left an account behind")
	}
	if snap.Status != "Sign-in cancelled" {
		t.Errorf("status = %q", snap.Status)
	}
}

func TestEnsureSessionRenewsAnExpiredAccount(t *testing.T) {
	stub := newSignInStub(t)
	ctrl := newTestController(t, stub)

	stale := auth.Account{
		Kind:           auth.KindMSA,
		UUID:           "069a79f444e94726a5befca90e38aaf5",
		Name:           "Notch",
		MSRefreshToken: "MS-REFRESH",
		MCAccessToken:  "OLD-TOKEN",
		MCExpiresAt:    time.Now().Add(-time.Hour),
	}
	if err := ctrl.Accounts.Add(stale); err != nil {
		t.Fatalf("Add: %v", err)
	}

	fresh, err := ctrl.ensureSession(context.Background(), 1, stale)
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if fresh.MCAccessToken != "MC-TOKEN" || !fresh.Usable() {
		t.Errorf("account not renewed: %+v", fresh)
	}

	stored, _ := ctrl.Accounts.Active()
	if stored.MCAccessToken != "MC-TOKEN" {
		t.Errorf("the renewed token was not stored: %+v", stored)
	}
}

func TestEnsureSessionMarksARejectedAccountForReauth(t *testing.T) {
	stub := newSignInStub(t)
	stub.refreshRejected.Store(true)
	ctrl := newTestController(t, stub)

	stale := auth.Account{
		Kind:           auth.KindMSA,
		UUID:           "069a79f444e94726a5befca90e38aaf5",
		Name:           "Notch",
		MSRefreshToken: "REVOKED",
		MCExpiresAt:    time.Now().Add(-time.Hour),
	}
	if err := ctrl.Accounts.Add(stale); err != nil {
		t.Fatalf("Add: %v", err)
	}

	_, err := ctrl.ensureSession(context.Background(), 1, stale)
	if err == nil {
		t.Fatal("ensureSession accepted a revoked session")
	}
	if !strings.Contains(err.Error(), "Notch") {
		t.Errorf("err = %v, want it to name the account that has to sign in again", err)
	}

	stored, _ := ctrl.Accounts.Active()
	if !stored.NeedsReauth {
		t.Error("the account was not marked as needing a fresh sign-in")
	}
	if stored.MSRefreshToken != "" {
		t.Error("a refused refresh token was kept, so the next launch would retry it")
	}
}

func TestEnsureSessionLeavesUsableAndOfflineAccountsAlone(t *testing.T) {
	stub := newSignInStub(t)
	ctrl := newTestController(t, stub)

	offline, err := auth.NewOfflineAccount("Steve")
	if err != nil {
		t.Fatalf("NewOfflineAccount: %v", err)
	}
	got, err := ctrl.ensureSession(context.Background(), 1, offline)
	if err != nil || got != offline {
		t.Errorf("offline account changed: %+v, %v", got, err)
	}

	live := auth.Account{Kind: auth.KindMSA, MCAccessToken: "MC-TOKEN", MCExpiresAt: time.Now().Add(time.Hour)}
	got, err = ctrl.ensureSession(context.Background(), 1, live)
	if err != nil || got != live {
		t.Errorf("a usable account was refreshed needlessly: %+v, %v", got, err)
	}
}

func TestSignOutForgetsTheAccount(t *testing.T) {
	stub := newSignInStub(t)
	ctrl := newTestController(t, stub)

	account, err := auth.NewOfflineAccount("Steve")
	if err != nil {
		t.Fatalf("NewOfflineAccount: %v", err)
	}
	if err := ctrl.Accounts.Add(account); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Publish it the way a refresh would, so the wait below sees it go rather
	// than starting from an already-empty list.
	list, active := ctrl.Accounts.List()
	ctrl.Store().SetAccounts(list, active)

	ctrl.Dispatch(ActionSignOut{UUID: account.UUID})

	snap := waitFor(t, ctrl, "the account to be gone", func(s Snapshot) bool {
		return len(s.Accounts) == 0
	})
	if snap.Screen != ScreenLogin {
		t.Errorf("screen = %v, want the login screen once no account is left", snap.Screen)
	}
}
