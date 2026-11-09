package httpapi

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
)

func guestClient(srvURL string) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func createGuestLink(t *testing.T, srvURL string, owner *http.Client, boardID, role string) (string, string) {
	t.Helper()
	resp, body := postJSON(t, owner, srvURL+"/api/boards/"+boardID+"/guest-links", map[string]string{"role": role})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create guest link = %d, body = %v", resp.StatusCode, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("missing token in %v", body)
	}
	link, _ := body["link"].(map[string]any)
	id, _ := link["id"].(string)
	return token, id
}

func TestGuestLinkEditorFlow(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	id := createBoard(t, srv.URL, alice, "shared")

	token, _ := createGuestLink(t, srv.URL, alice, id, "editor")

	guest := guestClient(srv.URL)
	resp, body := getJSON(t, guest, srv.URL+"/api/guest/"+token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview = %d, body = %v", resp.StatusCode, body)
	}
	invite, _ := body["invite"].(map[string]any)
	if invite["board_name"] != "shared" || invite["role"] != "editor" {
		t.Fatalf("preview = %v", invite)
	}

	resp, body = postJSON(t, guest, srv.URL+"/api/guest/"+token, nil)
	if resp.StatusCode != http.StatusOK || body["guest"] != true || body["board_id"] != id {
		t.Fatalf("join = %d, body = %v", resp.StatusCode, body)
	}

	resp, body = getJSON(t, guest, srv.URL+"/api/auth/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("guest me = %d, body = %v", resp.StatusCode, body)
	}
	user, _ := body["user"].(map[string]any)
	username, _ := user["username"].(string)
	if len(username) < 7 || username[:6] != "guest-" {
		t.Fatalf("guest username = %q", username)
	}

	resp, _ = postJSON(t, guest, srv.URL+"/api/boards/"+id+"/tasks", map[string]string{"text": "chores"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("guest editor add task = %d, want 201", resp.StatusCode)
	}
	if got := members(t, srv.URL, alice, id); len(got) != 2 {
		t.Fatalf("members = %v, want owner + guest", got)
	}
}

func TestGuestLinkViewerCannotWrite(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	id := createBoard(t, srv.URL, alice, "readonly")
	token, _ := createGuestLink(t, srv.URL, alice, id, "viewer")

	guest := guestClient(srv.URL)
	if resp, _ := postJSON(t, guest, srv.URL+"/api/guest/"+token, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("join = %d", resp.StatusCode)
	}
	if resp, _ := getJSON(t, guest, srv.URL+"/api/boards/"+id); resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer read = %d, want 200", resp.StatusCode)
	}
	if resp, _ := postJSON(t, guest, srv.URL+"/api/boards/"+id+"/tasks", map[string]string{"text": "nope"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer write = %d, want 403", resp.StatusCode)
	}
}

func TestRevokingGuestLinkKicksGuests(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	id := createBoard(t, srv.URL, alice, "temp")
	token, linkID := createGuestLink(t, srv.URL, alice, id, "editor")

	guest := guestClient(srv.URL)
	if resp, _ := postJSON(t, guest, srv.URL+"/api/guest/"+token, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("join = %d", resp.StatusCode)
	}

	resp, _ := doJSON(t, alice, http.MethodDelete, srv.URL+"/api/boards/"+id+"/guest-links/"+linkID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204", resp.StatusCode)
	}

	if resp, _ := getJSON(t, guest, srv.URL+"/api/auth/me"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("guest me after revoke = %d, want 401", resp.StatusCode)
	}
	if got := members(t, srv.URL, alice, id); len(got) != 1 {
		t.Fatalf("members after revoke = %v, want owner only", got)
	}

	// The dead link no longer mints new guests.
	fresh := guestClient(srv.URL)
	if resp, _ := postJSON(t, fresh, srv.URL+"/api/guest/"+token, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("dead link join = %d, want 404", resp.StatusCode)
	}
}

func TestSignedInUserJoinsThroughGuestLink(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	bob := registerUser(t, srv, "bob", "correct-horse")
	id := createBoard(t, srv.URL, alice, "mix")
	token, _ := createGuestLink(t, srv.URL, alice, id, "viewer")

	resp, body := postJSON(t, bob, srv.URL+"/api/guest/"+token, nil)
	if resp.StatusCode != http.StatusOK || body["guest"] != false {
		t.Fatalf("signed-in join = %d, body = %v", resp.StatusCode, body)
	}

	resp, body = getJSON(t, bob, srv.URL+"/api/auth/me")
	user, _ := body["user"].(map[string]any)
	if user["username"] != "bob" {
		t.Fatalf("bob lost his account: %v", user)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob me = %d", resp.StatusCode)
	}
}

func TestGuestLinksAreOwnerOnly(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	bob := registerUser(t, srv, "bob", "correct-horse")
	id := createBoard(t, srv.URL, alice, "team")
	postJSON(t, alice, srv.URL+"/api/boards/"+id+"/members", map[string]string{"username": "bob", "role": "editor"})

	resp, _ := postJSON(t, bob, srv.URL+"/api/boards/"+id+"/guest-links", map[string]string{"role": "viewer"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("editor create guest link = %d, want 403", resp.StatusCode)
	}
}
