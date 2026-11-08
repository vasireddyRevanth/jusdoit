package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func doJSON(t *testing.T, client *http.Client, method, url string, body any) (*http.Response, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

func createBoard(t *testing.T, srvURL string, client *http.Client, name string) string {
	t.Helper()
	resp, body := postJSON(t, client, srvURL+"/api/boards", map[string]string{"name": name})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create board = %d, body = %v", resp.StatusCode, body)
	}
	return boardID(t, body)
}

func members(t *testing.T, srvURL string, client *http.Client, id string) []any {
	t.Helper()
	resp, body := getJSON(t, client, srvURL+"/api/boards/"+id+"/members")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list members = %d, body = %v", resp.StatusCode, body)
	}
	list, _ := body["members"].([]any)
	return list
}

func TestInviteLinkJoinFlow(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	bob := registerUser(t, srv, "bob", "correct-horse")

	id := createBoard(t, srv.URL, alice, "shared")

	resp, body := postJSON(t, alice, srv.URL+"/api/boards/"+id+"/invites", map[string]string{"role": "viewer"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create invite = %d, body = %v", resp.StatusCode, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("missing token in %v", body)
	}

	resp, body = getJSON(t, bob, srv.URL+"/api/invites/"+token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview invite = %d, body = %v", resp.StatusCode, body)
	}
	invite, _ := body["invite"].(map[string]any)
	if invite["board_name"] != "shared" || invite["role"] != "viewer" {
		t.Fatalf("invite preview = %v", invite)
	}

	resp, body = postJSON(t, bob, srv.URL+"/api/invites/"+token+"/accept", nil)
	if resp.StatusCode != http.StatusOK || body["board_id"] != id {
		t.Fatalf("accept invite = %d, body = %v", resp.StatusCode, body)
	}

	// Viewer can read but not write.
	if resp, _ := getJSON(t, bob, srv.URL+"/api/boards/"+id); resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer read = %d, want 200", resp.StatusCode)
	}
	if resp, _ := postJSON(t, bob, srv.URL+"/api/boards/"+id+"/tasks", map[string]string{"text": "nope"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer write = %d, want 403", resp.StatusCode)
	}

	if got := members(t, srv.URL, alice, id); len(got) != 2 {
		t.Fatalf("members = %v, want 2", got)
	}
}

func TestAddMemberByUsernameAndRoles(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	bob := registerUser(t, srv, "bob", "correct-horse")

	id := createBoard(t, srv.URL, alice, "team")

	resp, body := postJSON(t, alice, srv.URL+"/api/boards/"+id+"/members", map[string]string{
		"username": "bob", "role": "editor",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member = %d, body = %v", resp.StatusCode, body)
	}

	// Editor can add tasks.
	resp, _ = postJSON(t, bob, srv.URL+"/api/boards/"+id+"/tasks", map[string]string{"text": "chores"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("editor add task = %d, want 201", resp.StatusCode)
	}

	// But only the owner manages members.
	resp, _ = postJSON(t, bob, srv.URL+"/api/boards/"+id+"/members", map[string]string{
		"username": "alice", "role": "viewer",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("editor add member = %d, want 403", resp.StatusCode)
	}

	// Demote bob to viewer, then he can no longer write.
	list := members(t, srv.URL, alice, id)
	var bobID string
	for _, m := range list {
		entry := m.(map[string]any)
		if entry["username"] == "bob" {
			bobID, _ = entry["user_id"].(string)
		}
	}
	if bobID == "" {
		t.Fatalf("bob missing from %v", list)
	}
	resp, _ = doJSON(t, alice, http.MethodPatch, srv.URL+"/api/boards/"+id+"/members/"+bobID, map[string]string{"role": "viewer"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("demote bob = %d, want 204", resp.StatusCode)
	}
	if resp, _ := postJSON(t, bob, srv.URL+"/api/boards/"+id+"/tasks", map[string]string{"text": "nope"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("demoted editor write = %d, want 403", resp.StatusCode)
	}

	// The owner cannot be demoted or removed.
	resp, _ = doJSON(t, alice, http.MethodPatch, srv.URL+"/api/boards/"+id+"/members/"+aliceID(t, srv.URL, alice, id), map[string]string{"role": "viewer"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("demote owner = %d, want 400", resp.StatusCode)
	}

	resp, _ = doJSON(t, alice, http.MethodDelete, srv.URL+"/api/boards/"+id+"/members/"+bobID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove bob = %d, want 204", resp.StatusCode)
	}
	if got := members(t, srv.URL, alice, id); len(got) != 1 {
		t.Fatalf("members after removal = %v, want 1", got)
	}
}

func aliceID(t *testing.T, srvURL string, client *http.Client, id string) string {
	t.Helper()
	for _, m := range members(t, srvURL, client, id) {
		entry := m.(map[string]any)
		if entry["role"] == "owner" {
			userID, _ := entry["user_id"].(string)
			return userID
		}
	}
	t.Fatal("owner not found")
	return ""
}

func TestInviteRejectsUnknownAndInvalidRole(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	id := createBoard(t, srv.URL, alice, "board")

	resp, _ := postJSON(t, alice, srv.URL+"/api/boards/"+id+"/invites", map[string]string{"role": "owner"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("owner invite role = %d, want 400", resp.StatusCode)
	}

	resp, _ = getJSON(t, alice, srv.URL+"/api/invites/not-a-real-token")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("invalid invite = %d, want 404", resp.StatusCode)
	}

	resp, _ = postJSON(t, alice, srv.URL+"/api/boards/"+id+"/members", map[string]string{
		"username": "nobody", "role": "editor",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown username = %d, want 404", resp.StatusCode)
	}
}
