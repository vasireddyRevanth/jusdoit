package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"jusdoit/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv := httptest.NewServer(New(st, "test", false).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, client *http.Client, url string, body any) (*http.Response, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

func TestRegisterLoginLogoutFlow(t *testing.T) {
	srv := newTestServer(t)
	client := srv.Client()
	jar, _ := cookiejar.New(nil)
	client.Jar = jar

	resp, body := postJSON(t, client, srv.URL+"/api/auth/register", credentials{
		Username: "alice", Password: "correct-horse",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, body = %v", resp.StatusCode, body)
	}
	if len(jar.Cookies(mustURL(t, srv.URL))) == 0 {
		t.Fatal("register did not set a session cookie")
	}

	resp, body = getJSON(t, client, srv.URL+"/api/auth/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status = %d, body = %v", resp.StatusCode, body)
	}
	user, ok := body["user"].(map[string]any)
	if !ok || user["username"] != "alice" {
		t.Fatalf("unexpected me body: %v", body)
	}

	resp, _ = postJSON(t, client, srv.URL+"/api/auth/logout", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}

	resp, _ = getJSON(t, client, srv.URL+"/api/auth/me")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d, want 401", resp.StatusCode)
	}
}

func TestRegisterRejectsDuplicateAndWeakPassword(t *testing.T) {
	srv := newTestServer(t)
	client := srv.Client()
	jar, _ := cookiejar.New(nil)
	client.Jar = jar

	resp, _ := postJSON(t, client, srv.URL+"/api/auth/register", credentials{
		Username: "bob", Password: "long-enough",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first register = %d", resp.StatusCode)
	}

	resp, body := postJSON(t, client, srv.URL+"/api/auth/register", credentials{
		Username: "BOB", Password: "another-password",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate register = %d, body = %v", resp.StatusCode, body)
	}

	resp, _ = postJSON(t, client, srv.URL+"/api/auth/register", credentials{
		Username: "carol", Password: "short",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("weak password = %d, want 400", resp.StatusCode)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	srv := newTestServer(t)
	client := srv.Client()
	jar, _ := cookiejar.New(nil)
	client.Jar = jar

	postJSON(t, client, srv.URL+"/api/auth/register", credentials{
		Username: "dave", Password: "right-password",
	})

	resp, body := postJSON(t, client, srv.URL+"/api/auth/login", credentials{
		Username: "dave", Password: "wrong-password",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, body = %v", resp.StatusCode, body)
	}

	// Drop the cookie left by registration to test an anonymous client.
	client.Jar, _ = cookiejar.New(nil)

	resp, _ = getJSON(t, client, srv.URL+"/api/auth/me")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me with no session = %d, want 401", resp.StatusCode)
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	srv := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/register",
		strings.NewReader(`{"username":"eve","password":"long-enough"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", resp.StatusCode)
	}
}

func getJSON(t *testing.T, client *http.Client, url string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return u
}
