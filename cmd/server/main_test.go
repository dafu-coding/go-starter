package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"go-starter/internal/database"
)

// Each run gets an isolated schema in the explicitly named disposable local DB.
func integrationDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration checks")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host != "127.0.0.1:55439" || parsed.Path != "/launchboard_test" {
		t.Fatal("use the dedicated local launchboard_test database on port 55439")
	}
	admin, err := sql.Open("postgres", raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("run_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
	query := parsed.Query()
	query.Set("options", "-csearch_path="+schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, parsed.String()
}

func TestPostgresWorkflow(t *testing.T) {
	t.Setenv("APP_PASSWORD", "")
	db, dsn := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	migrate()
	migrate()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM launchboard_ideas").Scan(&count); err != nil || count != 0 {
		t.Fatalf("new workspace must be empty: %d %v", count, err)
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
	getTasks := func(rec *httptest.ResponseRecorder) []Idea {
		t.Helper()
		check(rec, 200)
		var tasks []Idea
		if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
			t.Fatal(err)
		}
		return tasks
	}
	check(call("GET", "/health", "", nil, ""), 200)
	check(call("GET", "/", "", nil, ""), 200)
	list := call("GET", "/api/ideas", "", nil, "")
	check(list, 200)
	cookie := list.Result().Cookies()[0]
	create := call("POST", "/api/ideas", `{"title":"=SUM(1,2)","description":"","category":"Engineering","author":"Test","assignee":"田中","dueDate":"2026-11-03","priority":"high"}`, cookie, "")
	check(create, 201)
	var created struct {
		ID      int `json:"id"`
		Version int `json:"version"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 {
		t.Fatal("missing initial version")
	}
	path := fmt.Sprintf("/api/ideas/%d", created.ID)
	vote := path + "/vote"
	check(call("POST", vote, `{"voted":true}`, cookie, ""), 200)
	check(call("POST", vote, `{"voted":true}`, cookie, ""), 200)
	tasks := getTasks(call("GET", "/api/ideas", "", cookie, ""))
	if len(tasks) != 1 || tasks[0].Votes != 1 || !tasks[0].Voted || tasks[0].DueDate != "2026-11-03" || tasks[0].Assignee != "田中" {
		t.Fatalf("incorrect task: %+v", tasks)
	}
	check(call("PATCH", path, `{"version":1,"title":"更新したタスク <script>","description":"メモを更新","assignee":"佐藤","priority":"low","dueDate":"","status":"shipped"}`, cookie, ""), 200)
	check(call("PATCH", path, `{"version":1,"status":"idea"}`, cookie, ""), 409)
	var completed sql.NullTime
	if err := db.QueryRow("SELECT completed_at FROM launchboard_ideas WHERE id=$1", created.ID).Scan(&completed); err != nil || !completed.Valid {
		t.Fatalf("completion timestamp missing: %v", err)
	}
	migrate()
	second, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/ideas", nil)
	req.AddCookie(cookie)
	router(second).ServeHTTP(rec, req)
	tasks = getTasks(rec)
	task := tasks[0]
	if task.Title != "更新したタスク <script>" || task.Status != "shipped" || task.Priority != "low" || task.Assignee != "佐藤" || task.DueDate != "" || task.Version != 2 || task.Votes != 1 {
		t.Fatalf("lost persisted update: %+v", task)
	}
	check(call("PATCH", path, `{"version":2,"archived":true}`, cookie, ""), 200)
	tasks = getTasks(call("GET", "/api/ideas", "", cookie, ""))
	if !tasks[0].Archived {
		t.Fatal("archive not persisted")
	}
	check(call("PATCH", path, `{"version":3,"archived":false,"status":"idea"}`, cookie, ""), 200)
	if err := db.QueryRow("SELECT completed_at FROM launchboard_ideas WHERE id=$1", created.ID).Scan(&completed); err != nil || completed.Valid {
		t.Fatal("completion timestamp not cleared")
	}
	check(call("POST", vote, `{"voted":false}`, cookie, ""), 200)
	tasks = getTasks(call("GET", "/api/ideas", "", cookie, ""))
	if tasks[0].Archived || tasks[0].Votes != 0 {
		t.Fatal("restore or vote undo failed")
	}
	check(call("POST", "/api/samples", "", cookie, ""), 200)
	check(call("POST", "/api/samples", "", cookie, ""), 200)
	migrate()
	tasks = getTasks(call("GET", "/api/ideas", "", cookie, ""))
	if len(tasks) != 7 {
		t.Fatalf("samples must be explicit and idempotent, got %d", len(tasks))
	}
	// Legacy fake vote counters never appear in current totals.
	if _, err := db.Exec("UPDATE launchboard_ideas SET seed_votes=999 WHERE seed_key IS NOT NULL"); err != nil {
		t.Fatal(err)
	}
	tasks = getTasks(call("GET", "/api/ideas", "", cookie, ""))
	for _, task := range tasks {
		if task.Sample && task.Votes != 0 {
			t.Fatal("fake votes exposed")
		}
	}
	check(call("POST", "/api/samples/archive", "", cookie, ""), 200)
	migrate()
	tasks = getTasks(call("GET", "/api/ideas", "", cookie, ""))
	for _, task := range tasks {
		if task.Sample && !task.Archived {
			t.Fatal("sample archive reset by migration")
		}
	}
	check(call("POST", "/api/ideas", `{"title":"=SUM(1,2)","category":"Product"}`, cookie, ""), 201)
	exported := call("GET", "/api/export", "", cookie, "")
	check(exported, 200)
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(exported.Body.String(), "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 9 || rows[1][1] != "'=SUM(1,2)" {
		t.Fatalf("CSV export missing rows or formula protection: %v", rows)
	}
	for _, input := range []string{
		`{"title":"","category":"Product"}`, `{"title":"a","category":"Invalid"}`, `{"title":"a","category":"Product","dueDate":"2026-02-30"}`, `{"title":"a","category":"Product","priority":"urgent"}`, `{"title":"a","category":"Product","assignee":"` + strings.Repeat("あ", 61) + `"}`,
	} {
		check(call("POST", "/api/ideas", input, cookie, ""), 400)
	}
	check(call("PATCH", path, `{"version":4,"dueDate":"invalid"}`, cookie, ""), 400)
	check(call("PATCH", path, `{"status":"shipped"}`, cookie, ""), 400)
	check(call("POST", vote, `{}`, cookie, ""), 400)
	check(call("POST", vote, `{"voted":true} {}`, cookie, ""), 400)
	check(call("POST", "/api/ideas", strings.Repeat("x", 9000), cookie, ""), 400)
	check(call("POST", vote, `{"voted":true}`, cookie, "https://other.example"), 403)
	check(call("PATCH", "/api/ideas/2147483647", `{"version":1,"status":"shipped"}`, cookie, ""), 404)
	check(call("PATCH", "/api/ideas/2147483648", `{"version":1,"status":"shipped"}`, cookie, ""), 400)
}

func TestAccessControl(t *testing.T) {
	t.Setenv("APP_PASSWORD", "test-workspace-password")
	underlying := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	handler := accessControl(underlying)
	call := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Forwarded-Proto", "https")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{"/api/ideas", "/api/export", "/api/samples", "/api/samples/archive"} {
		if rec := call("GET", path, "", nil); rec.Code != 401 {
			t.Fatalf("unprotected: %s", path)
		}
	}
	if call("GET", "/health", "", nil).Code != 204 {
		t.Fatal("health check blocked")
	}
	if call("POST", "/api/session", `{"password":"wrong"}`, nil).Code != 401 {
		t.Fatal("bad password accepted")
	}
	login := call("POST", "/api/session", `{"password":"test-workspace-password"}`, nil)
	if login.Code != 200 || len(login.Result().Cookies()) != 1 {
		t.Fatal("login failed")
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie protections missing")
	}
	if call("GET", "/api/ideas", "", cookie).Code != 204 {
		t.Fatal("valid session rejected")
	}
	forged := *cookie
	forged.Value += "0"
	if call("GET", "/api/ideas", "", &forged).Code != 401 {
		t.Fatal("forged session accepted")
	}
	logout := call("DELETE", "/api/session", "", cookie)
	if logout.Code != 200 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout does not clear session")
	}
	t.Setenv("APP_PASSWORD", "rotated-workspace-password")
	newHandler := accessControl(underlying)
	req := httptest.NewRequest("GET", "/api/ideas", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	newHandler.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("password change must invalidate sessions")
	}
	for i := 0; i < 10; i++ {
		if call("POST", "/api/session", `{"password":"wrong"}`, nil).Code != 401 {
			t.Fatal("unexpected login result")
		}
	}
	if call("POST", "/api/session", `{"password":"wrong"}`, nil).Code != 429 {
		t.Fatal("login retries not limited")
	}
}
