package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The sign-in chain, in order: Microsoft issues an OAuth token, Xbox Live
// exchanges it for a user token, XSTS authorises that token for Minecraft, and
// Minecraft services trade the XSTS token for the session the game launches
// with. Every step is a separate service and each one can refuse for its own
// reasons, which is why the errors below are as specific as they are — "login
// failed" would leave a player with no idea what to do next.
const (
	// The consumers tenant is the one personal Microsoft accounts live in;
	// the common tenant would also accept work accounts, which can never own
	// Minecraft.
	defaultDeviceCodeURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode"
	defaultTokenURL      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	defaultXboxAuthURL   = "https://user.auth.xboxlive.com/user/authenticate"
	defaultXSTSURL       = "https://xsts.auth.xboxlive.com/xsts/authorize"
	defaultMCLoginURL    = "https://api.minecraftservices.com/authentication/login_with_xbox"
	defaultMCProfileURL  = "https://api.minecraftservices.com/minecraft/profile"
)

// msaScope asks for the Xbox Live sign-in permission plus a refresh token, so
// a player signs in once rather than once per session.
const msaScope = "XboxLive.signin offline_access"

// deviceCodeGrant is the OAuth grant type for the device authorisation flow.
const deviceCodeGrant = "urn:ietf:params:oauth:grant-type:device_code"

// slowDownBackoff is how much longer to wait between polls after the service
// asks us to slow down. A variable so tests need not spend it.
var slowDownBackoff = 5 * time.Second

// Sign-in failures a player can act on.
var (
	// ErrNotConfigured means the build has no Azure application id.
	ErrNotConfigured = errors.New("Microsoft sign-in needs an Azure application id, which this build was not given")
	// ErrDeclined means the player rejected the sign-in in the browser.
	ErrDeclined = errors.New("the sign-in was declined")
	// ErrCodeExpired means the device code was never entered in time.
	ErrCodeExpired = errors.New("the sign-in code expired before it was used")
	// ErrNoXboxAccount means the Microsoft account has never signed in to Xbox
	// Live, which Minecraft's authentication requires.
	ErrNoXboxAccount = errors.New("this Microsoft account has no Xbox profile; sign in once at minecraft.net to create one")
	// ErrChildAccount means the account needs an adult to add it to a family.
	ErrChildAccount = errors.New("this is a child account; an adult has to add it to a Microsoft family before it can sign in")
	// ErrXboxUnavailable means Xbox Live does not serve the account's region.
	ErrXboxUnavailable = errors.New("Xbox Live is not available in this account's country")
	// ErrNoGame means the account does not own the game.
	ErrNoGame = errors.New("this account does not own Minecraft: Java Edition")
	// ErrReauth means the stored refresh token is no longer accepted, so the
	// player has to sign in interactively again.
	ErrReauth = errors.New("the Microsoft session expired; sign in again")
)

// Endpoints are the services a sign-in talks to. They are fields rather than
// constants so a test can point the whole chain at one stub server.
type Endpoints struct {
	DeviceCode string
	Token      string
	XboxAuth   string
	XSTS       string
	MCLogin    string
	MCProfile  string
}

// DefaultEndpoints returns the live Microsoft, Xbox and Minecraft services.
func DefaultEndpoints() Endpoints {
	return Endpoints{
		DeviceCode: defaultDeviceCodeURL,
		Token:      defaultTokenURL,
		XboxAuth:   defaultXboxAuthURL,
		XSTS:       defaultXSTSURL,
		MCLogin:    defaultMCLoginURL,
		MCProfile:  defaultMCProfileURL,
	}
}

// MSA performs Microsoft sign-in for one Azure application.
//
// The device code flow is used rather than a redirect: it needs no local HTTP
// server and no registered redirect URI, and it works when the launcher runs
// somewhere the browser cannot reach back into.
type MSA struct {
	// ClientID is the Azure application id. A device code flow is a public
	// client, so there is no secret to go with it.
	ClientID  string
	Endpoints Endpoints
	HTTP      *http.Client

	// Observer, when set, is told which step of the chain is running, so a UI
	// can say more than "signing in".
	Observer func(step string)

	// Now is the clock, injected for tests.
	Now func() time.Time
}

// NewMSA returns a client for an Azure application id.
func NewMSA(clientID string) *MSA {
	return &MSA{
		ClientID:  clientID,
		Endpoints: DefaultEndpoints(),
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		Now:       time.Now,
	}
}

// Configured reports whether sign-in can be attempted at all.
func (m *MSA) Configured() bool { return m != nil && m.ClientID != "" }

func (m *MSA) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *MSA) http() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	return http.DefaultClient
}

func (m *MSA) step(name string) {
	if m.Observer != nil {
		m.Observer(name)
	}
}

// DeviceCode is a pending sign-in: the player types UserCode at
// VerificationURI, and the launcher polls until they have.
type DeviceCode struct {
	UserCode        string
	VerificationURI string
	// Message is Microsoft's own wording of the instruction, kept so the UI
	// can show exactly what the service says.
	Message   string
	Interval  time.Duration
	ExpiresAt time.Time

	// code is the secret half of the pair, sent when polling. It is not
	// exported because it is of no use to a UI and should not be displayed.
	code string
}

// StartDeviceCode asks Microsoft for a code for the player to enter.
func (m *MSA) StartDeviceCode(ctx context.Context) (DeviceCode, error) {
	if !m.Configured() {
		return DeviceCode{}, ErrNotConfigured
	}

	var resp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Message         string `json:"message"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	form := url.Values{"client_id": {m.ClientID}, "scope": {msaScope}}
	if err := m.postForm(ctx, m.Endpoints.DeviceCode, form, &resp); err != nil {
		return DeviceCode{}, err
	}
	if resp.DeviceCode == "" || resp.UserCode == "" {
		return DeviceCode{}, fmt.Errorf("microsoft returned no device code")
	}

	// Microsoft always sends both, but a missing interval that defaulted to
	// zero would turn polling into a denial-of-service against ourselves.
	interval := time.Duration(resp.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expires := time.Duration(resp.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = 15 * time.Minute
	}

	return DeviceCode{
		UserCode:        resp.UserCode,
		VerificationURI: resp.VerificationURI,
		Message:         resp.Message,
		Interval:        interval,
		ExpiresAt:       m.now().Add(expires),
		code:            resp.DeviceCode,
	}, nil
}

// Tokens is a Microsoft OAuth token pair.
type Tokens struct {
	Access  string
	Refresh string
}

// WaitForToken polls until the player has entered the code, the code expires,
// or the context is cancelled.
func (m *MSA) WaitForToken(ctx context.Context, dc DeviceCode) (Tokens, error) {
	if !m.Configured() {
		return Tokens{}, ErrNotConfigured
	}

	interval := dc.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return Tokens{}, ctx.Err()
		case <-timer.C:
		}

		if !dc.ExpiresAt.IsZero() && m.now().After(dc.ExpiresAt) {
			return Tokens{}, ErrCodeExpired
		}

		tokens, err := m.exchange(ctx, url.Values{
			"client_id":   {m.ClientID},
			"grant_type":  {deviceCodeGrant},
			"device_code": {dc.code},
		})

		var oauthErr *oauthError
		if errors.As(err, &oauthErr) {
			switch oauthErr.Code {
			case "authorization_pending":
				// The player has not finished in the browser yet.
			case "slow_down":
				// Polling faster than the service likes; back off for good,
				// as the flow requires the new interval to persist.
				interval += slowDownBackoff
			case "expired_token", "code_expired":
				return Tokens{}, ErrCodeExpired
			case "authorization_declined", "access_denied":
				return Tokens{}, ErrDeclined
			default:
				return Tokens{}, err
			}
			timer.Reset(interval)
			continue
		}
		if err != nil {
			return Tokens{}, err
		}
		return tokens, nil
	}
}

// Refresh renews a Microsoft session without asking the player for anything.
func (m *MSA) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if !m.Configured() {
		return Tokens{}, ErrNotConfigured
	}
	if refreshToken == "" {
		return Tokens{}, ErrReauth
	}

	tokens, err := m.exchange(ctx, url.Values{
		"client_id":     {m.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {msaScope},
	})

	var oauthErr *oauthError
	if errors.As(err, &oauthErr) && oauthErr.Code == "invalid_grant" {
		// The token was revoked, or the password changed. Nothing to retry.
		return Tokens{}, ErrReauth
	}
	return tokens, err
}

// exchange posts to the token endpoint and reads the token pair back.
func (m *MSA) exchange(ctx context.Context, form url.Values) (Tokens, error) {
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := m.postForm(ctx, m.Endpoints.Token, form, &resp); err != nil {
		return Tokens{}, err
	}
	if resp.AccessToken == "" {
		return Tokens{}, fmt.Errorf("microsoft returned no access token")
	}
	return Tokens{Access: resp.AccessToken, Refresh: resp.RefreshToken}, nil
}

// SignIn turns a Microsoft token pair into an account that can launch.
func (m *MSA) SignIn(ctx context.Context, tokens Tokens) (Account, error) {
	m.step("Authenticating with Xbox Live")
	xblToken, userHash, err := m.xboxLive(ctx, tokens.Access)
	if err != nil {
		return Account{}, err
	}

	m.step("Authorising for Minecraft")
	xstsToken, xstsHash, xuid, err := m.xsts(ctx, xblToken)
	if err != nil {
		return Account{}, err
	}
	// XSTS echoes the user hash back; prefer its copy but fall back to the
	// Xbox Live one, which is the same value in every case seen so far.
	if xstsHash != "" {
		userHash = xstsHash
	}

	m.step("Signing in to Minecraft services")
	mcToken, expiresIn, err := m.minecraftLogin(ctx, userHash, xstsToken)
	if err != nil {
		return Account{}, err
	}

	m.step("Fetching the player profile")
	id, name, err := m.profile(ctx, mcToken)
	if err != nil {
		return Account{}, err
	}

	return Account{
		Kind:           KindMSA,
		UUID:           id,
		Name:           name,
		XUID:           xuid,
		MSRefreshToken: tokens.Refresh,
		MCAccessToken:  mcToken,
		MCExpiresAt:    m.now().Add(expiresIn),
	}, nil
}

// RefreshAccount renews a stored account, keeping its refresh token when the
// service does not issue a new one.
func (m *MSA) RefreshAccount(ctx context.Context, account Account) (Account, error) {
	if account.Kind != KindMSA {
		return account, nil
	}

	tokens, err := m.Refresh(ctx, account.MSRefreshToken)
	if err != nil {
		return account, err
	}
	if tokens.Refresh == "" {
		tokens.Refresh = account.MSRefreshToken
	}

	fresh, err := m.SignIn(ctx, tokens)
	if err != nil {
		return account, err
	}
	return fresh, nil
}

// xboxLive exchanges the Microsoft token for an Xbox Live user token.
func (m *MSA) xboxLive(ctx context.Context, accessToken string) (token, userHash string, err error) {
	body := map[string]any{
		"Properties": map[string]any{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			// The "d=" prefix marks the ticket as a delegation token; without
			// it Xbox Live rejects the request as malformed.
			"RpsTicket": "d=" + accessToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}

	var resp xboxResponse
	if err := m.postJSON(ctx, m.Endpoints.XboxAuth, "", body, &resp); err != nil {
		return "", "", err
	}
	if resp.Token == "" {
		return "", "", fmt.Errorf("xbox live returned no token")
	}
	return resp.Token, resp.userHash(), nil
}

// xsts authorises the Xbox Live token for Minecraft's relying party.
func (m *MSA) xsts(ctx context.Context, xblToken string) (token, userHash, xuid string, err error) {
	body := map[string]any{
		"Properties": map[string]any{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xblToken},
		},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}

	var resp xboxResponse
	err = m.postJSON(ctx, m.Endpoints.XSTS, "", body, &resp)
	if err != nil {
		// XSTS reports why an account cannot play through a numeric code in
		// the 401 body, and those reasons each need a different fix.
		var httpErr *httpError
		if errors.As(err, &httpErr) {
			if reason := xstsReason(httpErr.Body); reason != nil {
				return "", "", "", reason
			}
		}
		return "", "", "", err
	}
	if resp.Token == "" {
		return "", "", "", fmt.Errorf("xsts returned no token")
	}
	return resp.Token, resp.userHash(), resp.xuid(), nil
}

// minecraftLogin trades the XSTS token for a Minecraft session token.
func (m *MSA) minecraftLogin(ctx context.Context, userHash, xstsToken string) (string, time.Duration, error) {
	body := map[string]any{
		"identityToken": fmt.Sprintf("XBL3.0 x=%s;%s", userHash, xstsToken),
	}

	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := m.postJSON(ctx, m.Endpoints.MCLogin, "", body, &resp); err != nil {
		return "", 0, err
	}
	if resp.AccessToken == "" {
		return "", 0, fmt.Errorf("minecraft services returned no access token")
	}

	expires := time.Duration(resp.ExpiresIn) * time.Second
	if expires <= 0 {
		// The service documents 24 hours; assume the short end rather than
		// treating an unknown token as long-lived.
		expires = time.Hour
	}
	return resp.AccessToken, expires, nil
}

// profile reads the player's name and uuid, which is also the entitlement
// check: an account without the game has no profile.
func (m *MSA) profile(ctx context.Context, mcToken string) (id, name string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.Endpoints.MCProfile, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+mcToken)
	req.Header.Set("Accept", "application/json")

	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := m.do(req, &resp); err != nil {
		var httpErr *httpError
		if errors.As(err, &httpErr) && (httpErr.Status == http.StatusNotFound ||
			strings.Contains(httpErr.Body, "NOT_FOUND")) {
			return "", "", ErrNoGame
		}
		return "", "", err
	}
	if resp.ID == "" || resp.Name == "" {
		return "", "", ErrNoGame
	}
	return resp.ID, resp.Name, nil
}

// xboxResponse is the shape both Xbox services answer with.
type xboxResponse struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		XUI []struct {
			UHS string `json:"uhs"`
			XID string `json:"xid"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
}

func (r xboxResponse) userHash() string {
	if len(r.DisplayClaims.XUI) == 0 {
		return ""
	}
	return r.DisplayClaims.XUI[0].UHS
}

func (r xboxResponse) xuid() string {
	if len(r.DisplayClaims.XUI) == 0 {
		return ""
	}
	return r.DisplayClaims.XUI[0].XID
}

// xstsReason maps an XSTS refusal to the fix the player has to apply. It
// returns nil for a body that carries no known code.
func xstsReason(body string) error {
	var resp struct {
		XErr int64 `json:"XErr"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil
	}
	switch resp.XErr {
	case 2148916233:
		return ErrNoXboxAccount
	case 2148916235:
		return ErrXboxUnavailable
	case 2148916236, 2148916237:
		return fmt.Errorf("this account needs adult verification before it can sign in")
	case 2148916238:
		return ErrChildAccount
	default:
		return nil
	}
}

// oauthError is the error body the Microsoft OAuth endpoints return.
type oauthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *oauthError) Error() string {
	if e.Description != "" {
		// Microsoft's descriptions carry a trace id and correlation id on
		// their own lines; the first line is the part a player can read.
		if line, _, found := strings.Cut(e.Description, "\r\n"); found {
			return line
		}
		return e.Description
	}
	return e.Code
}

// httpError is an unexpected response from any of the services.
type httpError struct {
	URL    string
	Status int
	Body   string
}

func (e *httpError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	if body == "" {
		return fmt.Sprintf("%s: %s", e.URL, http.StatusText(e.Status))
	}
	return fmt.Sprintf("%s: %s: %s", e.URL, http.StatusText(e.Status), body)
}

// postForm posts a form-encoded request, as the OAuth endpoints expect.
func (m *MSA) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return m.do(req, out)
}

// postJSON posts a JSON request, as the Xbox and Minecraft services expect.
func (m *MSA) postJSON(ctx context.Context, endpoint, bearer string, body, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return m.do(req, out)
}

// do sends a request and decodes the response, turning both OAuth error
// bodies and unexpected statuses into errors callers can match on.
func (m *MSA) do(req *http.Request, out any) error {
	resp, err := m.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// The bodies here are small; reading them whole is what lets an error be
	// reported with the service's own explanation in it.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var oauthErr oauthError
		if json.Unmarshal(data, &oauthErr) == nil && oauthErr.Code != "" {
			return &oauthErr
		}
		return &httpError{URL: req.URL.String(), Status: resp.StatusCode, Body: string(data)}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: unreadable response: %w", req.URL, err)
	}
	return nil
}
