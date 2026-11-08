package httpapi

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

func registerUser(t *testing.T, srv *httptest.Server, username, password string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	resp, body := postJSON(t, client, srv.URL+"/api/auth/register", credentials{
		Username: username, Password: password,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %s = %d, body = %v", username, resp.StatusCode, body)
	}
	return client
}

func boardID(t *testing.T, body map[string]any) string {
	t.Helper()
	board, ok := body["board"].(map[string]any)
	if !ok {
		t.Fatalf("missing board in %v", body)
	}
	id, _ := board["id"].(string)
	if id == "" {
		t.Fatalf("missing board id in %v", board)
	}
	return id
}

func TestBoardAndTaskFlow(t *testing.T) {
	srv := newTestServer(t)
	client := registerUser(t, srv, "alice", "correct-horse")

	resp, body := postJSON(t, client, srv.URL+"/api/boards", map[string]string{"name": "home"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create board = %d, body = %v", resp.StatusCode, body)
	}
	id := boardID(t, body)

	resp, body = postJSON(t, client, srv.URL+"/api/boards/"+id+"/tasks", map[string]string{"text": "dishes"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add task = %d, body = %v", resp.StatusCode, body)
	}

	resp, body = getJSON(t, client, srv.URL+"/api/boards/"+id)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get board = %d, body = %v", resp.StatusCode, body)
	}
	active, ok := body["active"].([]any)
	if !ok || len(active) != 1 {
		t.Fatalf("active = %v, want one task", body["active"])
	}

	resp, body = postJSON(t, client, srv.URL+"/api/boards/"+id+"/spin", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("spin = %d, body = %v", resp.StatusCode, body)
	}
	if task, ok := body["task"].(map[string]any); !ok || task["text"] != "dishes" {
		t.Fatalf("spin task = %v", body["task"])
	}

	resp, body = postJSON(t, client, srv.URL+"/api/boards/"+id+"/spin", nil)
	if resp.StatusCode != http.StatusOK || body["task"] != nil {
		t.Fatalf("empty spin = %d %v, want null task", resp.StatusCode, body)
	}

	resp, body = getJSON(t, client, srv.URL+"/api/boards")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list boards = %d", resp.StatusCode)
	}
	if boards, ok := body["boards"].([]any); !ok || len(boards) != 1 {
		t.Fatalf("boards = %v, want one", body["boards"])
	}
}

func TestBoardAccessIsIsolated(t *testing.T) {
	srv := newTestServer(t)
	alice := registerUser(t, srv, "alice", "correct-horse")
	bob := registerUser(t, srv, "bob", "correct-horse")

	_, body := postJSON(t, alice, srv.URL+"/api/boards", map[string]string{"name": "private"})
	id := boardID(t, body)

	resp, _ := getJSON(t, bob, srv.URL+"/api/boards/"+id)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("bob get board = %d, want 404", resp.StatusCode)
	}

	resp, _ = postJSON(t, bob, srv.URL+"/api/boards/"+id+"/tasks", map[string]string{"text": "sneaky"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("bob add task = %d, want 404", resp.StatusCode)
	}

	resp, _ = getJSON(t, bob, srv.URL+"/api/boards")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob list boards = %d", resp.StatusCode)
	}
}

func TestBoardsRequireAuth(t *testing.T) {
	srv := newTestServer(t)
	resp, _ := getJSON(t, srv.Client(), srv.URL+"/api/boards")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous boards = %d, want 401", resp.StatusCode)
	}
}

func TestBoardNameValidation(t *testing.T) {
	srv := newTestServer(t)
	client := registerUser(t, srv, "alice", "correct-horse")

	resp, _ := postJSON(t, client, srv.URL+"/api/boards", map[string]string{"name": "   "})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank name = %d, want 400", resp.StatusCode)
	}
}
