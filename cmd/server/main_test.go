package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go-starter/internal/database"
)

// TEST_DATABASE_URL must point to a disposable, local database.
func TestPostgresWorkflow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration checks")
	}
	if !strings.Contains(dsn, "127.0.0.1:55439/launchboard_test?") {
		t.Fatal("use the dedicated local launchboard_test database on port 55439")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var before, after int
	db.QueryRow("SELECT COUNT(*) FROM launchboard_ideas").Scan(&before)
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	db.QueryRow("SELECT COUNT(*) FROM launchboard_ideas").Scan(&after)
	if before != after || before < 7 {
		t.Fatalf("seed not idempotent: %d -> %d", before, after)
	}
	handler := router(db)
	call := func(method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	check := func(rec *httptest.ResponseRecorder, want int) {
		t.Helper()
		if rec.Code != want {
			t.Fatalf("status %d, want %d: %s", rec.Code, want, rec.Body.String())
		}
	}
	check(call("GET", "/health", "", nil, ""), 200)
	page := call("GET", "/", "", nil, "")
	check(page, 200)
	if !strings.Contains(page.Body.String(), "Launchboard") {
		t.Fatal("missing page")
	}
	list := call("GET", "/api/ideas", "", nil, "")
	check(list, 200)
	cookie := list.Result().Cookies()[0]
	create := call("POST", "/api/ideas", `{"title":"テスト <script>alert(1)</script>","description":"永続化を確認します","category":"Engineering","author":"Test"}`, cookie, "")
	check(create, 201)
	var created struct {
		ID int `json:"id"`
	}
	if err = json.Unmarshal(create.Body.Bytes(), &created); err != nil || created.ID < 1 {
		t.Fatal("invalid created ID")
	}
	defer db.Exec("DELETE FROM launchboard_ideas WHERE id=$1", created.ID)
	voteURL := fmt.Sprintf("/api/ideas/%d/vote", created.ID)
	ideaURL := fmt.Sprintf("/api/ideas/%d", created.ID)
	check(call("POST", voteURL, `{"voted":true}`, cookie, ""), 200)
	check(call("POST", voteURL, `{"voted":true}`, cookie, ""), 200)
	var count int
	db.QueryRow("SELECT COUNT(*) FROM launchboard_votes WHERE idea_id=$1", created.ID).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate vote: %d", count)
	}
	check(call("PATCH", ideaURL, `{"status":"shipped"}`, cookie, ""), 200)
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// A new connection and handler emulate a new process reading existing data.
	second, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/ideas", nil)
	req.AddCookie(cookie)
	router(second).ServeHTTP(rec, req)
	check(rec, 200)
	var ideas []Idea
	if err = json.Unmarshal(rec.Body.Bytes(), &ideas); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, idea := range ideas {
		if idea.ID == created.ID {
			found = true
			if idea.Status != "shipped" || idea.Votes != 1 || !idea.Voted || !strings.Contains(idea.Title, "<script>") {
				t.Fatalf("lost persisted data: %+v", idea)
			}
		}
	}
	if !found {
		t.Fatal("persisted idea missing")
	}
	check(call("POST", voteURL, `{"voted":false}`, cookie, ""), 200)
	db.QueryRow("SELECT COUNT(*) FROM launchboard_votes WHERE idea_id=$1", created.ID).Scan(&count)
	if count != 0 {
		t.Fatal("vote not removed")
	}
	check(call("POST", "/api/ideas", `{"title":"","description":"a","category":"Product"}`, cookie, ""), 400)
	check(call("POST", "/api/ideas", `{"title":"a","description":"b","category":"Invalid"}`, cookie, ""), 400)
	check(call("PATCH", ideaURL, `{"status":"unknown"}`, cookie, ""), 400)
	check(call("POST", voteURL, `{}`, cookie, ""), 400)
	check(call("POST", voteURL, `{"voted":true} {}`, cookie, ""), 400)
	check(call("POST", "/api/ideas", strings.Repeat("x", 9000), cookie, ""), 400)
	check(call("POST", voteURL, `{"voted":true}`, cookie, "https://other.example"), 403)
	check(call("PATCH", "/api/ideas/2147483647", `{"status":"shipped"}`, cookie, ""), 404)
	check(call("POST", "/api/ideas/nope/vote", `{"voted":true}`, cookie, ""), 400)
}
