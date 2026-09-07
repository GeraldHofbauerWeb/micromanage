package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// msaStub is the whole sign-in chain behind one test server, so a test can
// drive the flow end to end without reaching Microsoft.
type msaStub struct {
	server *httptest.Server

	// pending is how many times the token endpoint answers
	// authorization_pending before handing out a token.
	pending atomic.Int32
	// slowDown makes the token endpoint ask for a longer interval once.
	slowDown atomic.Bool

	xstsStatus int
	xstsBody   string

	profileStatus int
	profileBody   string

	refreshError string

	// polls counts token requests, to prove polling actually happened.
	polls atomic.Int32
}

func newMSAStub(t *testing.T) (*MSA, *msaStub) {
	t.Helper()

	stub := &msaStub{}
	mux := http.NewServeMux()

	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"device_code":      "DEV-CODE",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://microsoft.com/link",
			"message":          "Enter ABCD-EFGH",
			"expires_in":       900,
			"interval":         1,
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		stub.polls.Add(1)
		_ = r.ParseForm()

		if r.Form.Get("grant_type") == "refresh_token" {
			if stub.refreshError != "" {
				writeJSON(w, 400, map[string]any{
					"error":             stub.refreshError,
					"error_description": "AADSTS70000: refresh rejected\r\nTrace ID: x",
				})
				return
			}
			writeJSON(w, 200, map[string]any{
				"access_token":  "MS-ACCESS-2",
				"refresh_token": "MS-REFRESH-2",
			})
			return
		}

		if stub.slowDown.CompareAndSwap(true, false) {
			writeJSON(w, 400, map[string]any{"error": "slow_down"})
			return
		}
		if stub.pending.Load() > 0 {
			stub.pending.Add(-1)
			writeJSON(w, 400, map[string]any{"error": "authorization_pending"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"access_token":  "MS-ACCESS",
			"refresh_token": "MS-REFRESH",
		})
	})

	mux.HandleFunc("/xbl", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Properties struct{ RpsTicket string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.HasPrefix(body.Properties.RpsTicket, "d=") {
			writeJSON(w, 400, map[string]any{"error": "bad ticket"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"Token":         "XBL-TOKEN",
			"DisplayClaims": map[string]any{"xui": []map[string]string{{"uhs": "USERHASH"}}},
		})
	})

	mux.HandleFunc("/xsts", func(w http.ResponseWriter, r *http.Request) {
		if stub.xstsStatus != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stub.xstsStatus)
			_, _ = w.Write([]byte(stub.xstsBody))
			return
		}
		writeJSON(w, 200, map[string]any{
			"Token": "XSTS-TOKEN",
			"DisplayClaims": map[string]any{
				"xui": []map[string]string{{"uhs": "USERHASH", "xid": "2535412345678901"}},
			},
		})
	})

	mux.HandleFunc("/mclogin", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IdentityToken string `json:"identityToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.IdentityToken != "XBL3.0 x=USERHASH;XSTS-TOKEN" {
			writeJSON(w, 401, map[string]any{"error": "bad identity token"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"access_token": "MC-TOKEN",
			"expires_in":   86400,
		})
	})

	mux.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer MC-TOKEN" {
			writeJSON(w, 401, map[string]any{"error": "bad bearer"})
			return
		}
		if stub.profileStatus != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stub.profileStatus)
			_, _ = w.Write([]byte(stub.profileBody))
			return
		}
		writeJSON(w, 200, map[string]any{
			"id":   "069a79f444e94726a5befca90e38aaf5",
			"name": "Notch",
		})
	})

	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)

	base := stub.server.URL
	client := NewMSA("test-client-id")
	client.Endpoints = Endpoints{
		DeviceCode: base + "/devicecode",
		Token:      base + "/token",
		XboxAuth:   base + "/xbl",
		XSTS:       base + "/xsts",
		MCLogin:    base + "/mclogin",
		MCProfile:  base + "/profile",
	}
	return client, stub
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func TestMSASignInEndToEnd(t *testing.T) {
	client, stub := newMSAStub(t)
	stub.pending.Store(2)

	ctx := context.Background()
	code, err := client.StartDeviceCode(ctx)
	if err != nil {
		t.Fatalf("StartDeviceCode: %v", err)
	}
	if code.UserCode != "ABCD-EFGH" || code.VerificationURI != "https://microsoft.com/link" {
		t.Fatalf("device code = %+v", code)
	}
	if code.Interval != time.Second {
		t.Errorf("interval = %v, want 1s", code.Interval)
	}

	tokens, err := client.WaitForToken(ctx, code)
	if err != nil {
		t.Fatalf("WaitForToken: %v", err)
	}
	if tokens.Access != "MS-ACCESS" || tokens.Refresh != "MS-REFRESH" {
		t.Fatalf("tokens = %+v", tokens)
	}
	if polls := stub.polls.Load(); polls != 3 {
		t.Errorf("token endpoint polled %d times, want 3 (two pending, one success)", polls)
	}

	account, err := client.SignIn(ctx, tokens)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if account.Kind != KindMSA {
		t.Errorf("kind = %q, want %q", account.Kind, KindMSA)
	}
	if account.Name != "Notch" {
		t.Errorf("name = %q, want Notch", account.Name)
	}
	if account.UUID != "069a79f444e94726a5befca90e38aaf5" {
		t.Errorf("uuid = %q", account.UUID)
	}
	if account.XUID != "2535412345678901" {
		t.Errorf("xuid = %q", account.XUID)
	}
	if account.MSRefreshToken != "MS-REFRESH" || account.MCAccessToken != "MC-TOKEN" {
		t.Errorf("tokens not stored: %+v", account)
	}
	if !account.Usable() {
		t.Errorf("a freshly signed-in account should be usable, expiry %v", account.MCExpiresAt)
	}
	if account.AccessToken() != "MC-TOKEN" || account.UserType() != "msa" {
		t.Errorf("launch session would be wrong: %+v", account)
	}
}

func TestMSAWaitForTokenBacksOffOnSlowDown(t *testing.T) {
	client, stub := newMSAStub(t)
	stub.slowDown.Store(true)

	code, err := client.StartDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("StartDeviceCode: %v", err)
	}
	// Keep the test quick: the real back-off is five seconds a poll.
	code.Interval = time.Millisecond
	restore := slowDownBackoff
	slowDownBackoff = 50 * time.Millisecond
	defer func() { slowDownBackoff = restore }()

	start := time.Now()
	if _, err := client.WaitForToken(context.Background(), code); err != nil {
		t.Fatalf("WaitForToken: %v", err)
	}
	if waited := time.Since(start); waited < 50*time.Millisecond {
		t.Errorf("slow_down did not lengthen the interval; waited %v", waited)
	}
	if polls := stub.polls.Load(); polls != 2 {
		t.Errorf("token endpoint polled %d times, want 2 (one slow_down, one success)", polls)
	}
}

func TestMSAWaitForTokenReportsRefusals(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  error
	}{
		{"declined", "authorization_declined", ErrDeclined},
		{"expired", "expired_token", ErrCodeExpired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 400, map[string]any{"error": tc.reply})
			}))
			defer server.Close()

			client := NewMSA("id")
			client.Endpoints.Token = server.URL

			_, err := client.WaitForToken(context.Background(), DeviceCode{
				Interval:  time.Millisecond,
				ExpiresAt: time.Now().Add(time.Minute),
				code:      "DEV",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMSAWaitForTokenStopsWhenCodeExpires(t *testing.T) {
	client, _ := newMSAStub(t)

	_, err := client.WaitForToken(context.Background(), DeviceCode{
		Interval:  time.Millisecond,
		ExpiresAt: time.Now().Add(-time.Second),
		code:      "DEV",
	})
	if !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("err = %v, want ErrCodeExpired", err)
	}
}

func TestMSASignInMapsXSTSRefusals(t *testing.T) {
	cases := []struct {
		name string
		xerr int64
		want error
	}{
		{"no xbox account", 2148916233, ErrNoXboxAccount},
		{"child account", 2148916238, ErrChildAccount},
		{"country", 2148916235, ErrXboxUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, stub := newMSAStub(t)
			stub.xstsStatus = http.StatusUnauthorized
			stub.xstsBody = `{"Identity":"0","XErr":` + strconv.FormatInt(tc.xerr, 10) + `,"Message":"","Redirect":"https://start.ui.xboxlive.com"}`

			_, err := client.SignIn(context.Background(), Tokens{Access: "MS-ACCESS"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMSASignInReportsAMissingGame(t *testing.T) {
	client, stub := newMSAStub(t)
	stub.profileStatus = http.StatusNotFound
	stub.profileBody = `{"path":"/minecraft/profile","errorType":"NOT_FOUND"}`

	_, err := client.SignIn(context.Background(), Tokens{Access: "MS-ACCESS"})
	if !errors.Is(err, ErrNoGame) {
		t.Fatalf("err = %v, want ErrNoGame", err)
	}
}

func TestMSARefreshAccount(t *testing.T) {
	client, _ := newMSAStub(t)

	stale := Account{
		Kind:           KindMSA,
		UUID:           "069a79f444e94726a5befca90e38aaf5",
		Name:           "Notch",
		MSRefreshToken: "MS-REFRESH",
		MCAccessToken:  "OLD-TOKEN",
		MCExpiresAt:    time.Now().Add(-time.Hour),
	}
	if stale.Usable() {
		t.Fatal("an expired account should not be usable")
	}

	fresh, err := client.RefreshAccount(context.Background(), stale)
	if err != nil {
		t.Fatalf("RefreshAccount: %v", err)
	}
	if fresh.MCAccessToken != "MC-TOKEN" {
		t.Errorf("access token = %q, want the renewed one", fresh.MCAccessToken)
	}
	if fresh.MSRefreshToken != "MS-REFRESH-2" {
		t.Errorf("refresh token = %q, want the rotated one", fresh.MSRefreshToken)
	}
	if !fresh.Usable() {
		t.Error("a refreshed account should be usable")
	}
}

func TestMSARefreshAccountAsksForReauthWhenRejected(t *testing.T) {
	client, stub := newMSAStub(t)
	stub.refreshError = "invalid_grant"

	account := Account{Kind: KindMSA, MSRefreshToken: "MS-REFRESH"}
	if _, err := client.RefreshAccount(context.Background(), account); !errors.Is(err, ErrReauth) {
		t.Fatalf("err = %v, want ErrReauth", err)
	}
}

func TestMSARefreshAccountLeavesOfflineAccountsAlone(t *testing.T) {
	client, _ := newMSAStub(t)

	offline, err := NewOfflineAccount("Steve")
	if err != nil {
		t.Fatalf("NewOfflineAccount: %v", err)
	}
	got, err := client.RefreshAccount(context.Background(), offline)
	if err != nil {
		t.Fatalf("RefreshAccount: %v", err)
	}
	if got != offline {
		t.Errorf("offline account changed: %+v", got)
	}
}

func TestMSAWithoutClientIDIsNotConfigured(t *testing.T) {
	client := NewMSA("")
	if client.Configured() {
		t.Fatal("a client with no id should not report itself configured")
	}
	if _, err := client.StartDeviceCode(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}
