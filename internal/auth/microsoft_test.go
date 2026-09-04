package auth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jhyoong/KumaApprove/internal/credstore"
)

func TestMSCallbackRedirectURIIsFixedLoopback(t *testing.T) {
	// Microsoft matches the redirect URI exactly and requires the literal
	// host "localhost" for desktop apps; an ephemeral port or loopback IP
	// fails consent with AADSTS50011.
	got := MicrosoftCallbackRedirectURI()
	want := "http://localhost:8400/callback"
	if got != want {
		t.Fatalf("redirect URI = %q, want %q", got, want)
	}
	if strings.HasPrefix(got, "http://127.0.0.1") {
		t.Fatal("redirect URI must use host localhost, not 127.0.0.1")
	}
}

func TestMSListenLoopbackServesIPv4andIPv6(t *testing.T) {
	l, err := listenLoopback(msCallbackPort)
	if err != nil {
		if strings.Contains(err.Error(), "address already in use") {
			t.Skipf("callback port %d busy: %v", msCallbackPort, err)
		}
		t.Fatalf("listenLoopback: %v", err)
	}
	defer l.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})}
	go srv.Serve(l)
	defer srv.Shutdown(context.Background())

	time.Sleep(100 * time.Millisecond)

	tcpAddr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type %T", l.Addr())
	}

	// The advertised URI is "localhost", which resolves to ::1 or 127.0.0.1
	// depending on the host. Whichever it picks must reach the server, so a
	// dual-stack bind is required unless IPv6 is unavailable.
	hosts := []string{"127.0.0.1"}
	if tcpAddr.IP.To4() == nil {
		hosts = append(hosts, "::1")
	}

	for _, host := range hosts {
		u := "http://" + host + ":" + strconv.Itoa(msCallbackPort) + "/callback"
		if strings.Contains(host, ":") {
			u = "http://[" + host + "]:" + strconv.Itoa(msCallbackPort) + "/callback"
		}
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(u)
		if err != nil {
			t.Errorf("callback unreachable via %s: %v", host, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func TestHandleCallback(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
		wantMsg string
	}{
		{
			name:  "authorization code returned",
			query: "?code=auth-code-123&state=abc",
		},
		{
			name:    "microsoft error surfaces description",
			query:   "?error=invalid_request&error_description=redirect_uri+mismatch",
			wantErr: true,
			wantMsg: "redirect_uri mismatch",
		},
		{
			name:    "missing code and error",
			query:   "?state=abc",
			wantErr: true,
			wantMsg: "no authorization code",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan callbackResult, 1)
			req := httptest.NewRequest(http.MethodGet, "/callback"+tc.query, nil)
			rec := httptest.NewRecorder()

			handleCallback(rec, req, ch)

			select {
			case res := <-ch:
				if tc.wantErr {
					if res.err == nil {
						t.Fatal("expected error, got nil")
					}
					if !strings.Contains(res.err.Error(), tc.wantMsg) {
						t.Errorf("error = %q, want substring %q", res.err, tc.wantMsg)
					}
					return
				}
				if res.err != nil {
					t.Fatalf("unexpected error: %v", res.err)
				}
				if res.code != "auth-code-123" {
					t.Errorf("code = %q, want auth-code-123", res.code)
				}
				if res.state != "abc" {
					t.Errorf("state = %q, want abc", res.state)
				}
			case <-time.After(time.Second):
				t.Fatal("handleCallback did not report a result")
			}
		})
	}
}

func TestMSAuthURLIncludesRegisteredRedirectAndState(t *testing.T) {
	auth := &MicrosoftAuth{ClientID: "ms-client-id", TenantID: "consumers"}

	state, err := oauthState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 32 {
		t.Fatalf("state length = %d, want 32 hex chars", len(state))
	}

	authURL := auth.authorizeURL() + "?redirect_uri=" + url.QueryEscape(MicrosoftCallbackRedirectURI()) +
		"&state=" + url.QueryEscape(state)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("redirect_uri"); got != MicrosoftCallbackRedirectURI() {
		t.Errorf("redirect_uri = %q, want %q", got, MicrosoftCallbackRedirectURI())
	}
	if got := parsed.Query().Get("state"); got != state {
		t.Errorf("state = %q, want %q", got, state)
	}
}

func TestOAuthStateIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		s, err := oauthState()
		if err != nil {
			t.Fatal(err)
		}
		if seen[s] {
			t.Fatalf("duplicate state generated: %s", s)
		}
		seen[s] = true
	}
}

func TestMicrosoftGetTokenStillValid(t *testing.T) {
	store := newFakeStore()
	store.Put("outlook:test@outlook.com", credstore.Credential{
		AccessToken:  "valid-ms-token",
		RefreshToken: "refresh-123",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	provider := &MicrosoftAuth{
		Store: store,
	}

	token, err := provider.GetToken("outlook", "test@outlook.com")
	if err != nil {
		t.Fatal(err)
	}
	if token != "valid-ms-token" {
		t.Fatalf("expected valid-ms-token, got %s", token)
	}
}

func TestMicrosoftRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing form: %v", err)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("expected grant_type=refresh_token, got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "old-refresh-token" {
			t.Errorf("expected old refresh token, got %s", r.FormValue("refresh_token"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-ms-access-token",
			"refresh_token": "new-ms-refresh-token",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("outlook:test@outlook.com", credstore.Credential{
		AccessToken:  "expired-token",
		RefreshToken: "old-refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &MicrosoftAuth{
		ClientID:     "ms-client-id",
		TenantID:     "consumers",
		TokenURL:     server.URL,
		Store:        store,
	}

	token, err := provider.GetToken("outlook", "test@outlook.com")
	if err != nil {
		t.Fatal(err)
	}
	if token != "new-ms-access-token" {
		t.Fatalf("expected new-ms-access-token, got %s", token)
	}

	// Verify refresh token was rotated in the store.
	updated, _ := store.Get("outlook:test@outlook.com")
	if updated.AccessToken != "new-ms-access-token" {
		t.Fatal("access token should have been updated in store")
	}
	if updated.RefreshToken != "new-ms-refresh-token" {
		t.Fatal("refresh token should have been rotated in store")
	}
}

func TestMicrosoftGetTokenMissing(t *testing.T) {
	store := newFakeStore()
	provider := &MicrosoftAuth{Store: store}

	_, err := provider.GetToken("outlook", "nobody@outlook.com")
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

func TestMicrosoftRefreshFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "token has been revoked",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("outlook:test@outlook.com", credstore.Credential{
		AccessToken:  "expired-token",
		RefreshToken: "revoked-refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &MicrosoftAuth{
		ClientID:     "ms-client-id",
		TenantID:     "consumers",
		TokenURL:     server.URL,
		Store:        store,
	}

	_, err := provider.GetToken("outlook", "test@outlook.com")
	if err == nil {
		t.Fatal("expected AuthExpiredError")
	}
	if _, ok := err.(*AuthExpiredError); !ok {
		t.Fatalf("expected *AuthExpiredError, got %T: %v", err, err)
	}
}
