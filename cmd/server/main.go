package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go-starter/internal/database"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed templates/index.html static/app.css static/app.js static/favicon.svg
var assets embed.FS

type Idea struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Status      string    `json:"status"`
	Author      string    `json:"author"`
	Votes       int       `json:"votes"`
	Voted       bool      `json:"voted"`
	CreatedAt   time.Time `json:"createdAt"`
	Assignee    string    `json:"assignee"`
	Priority    string    `json:"priority"`
	DueDate     string    `json:"dueDate"`
	Archived    bool      `json:"archived"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Sample      bool      `json:"sample"`
}

func main() {
	if password := os.Getenv("APP_PASSWORD"); password != "" && utf8.RuneCountInString(password) < 12 {
		log.Fatal("APP_PASSWORD must contain at least 12 characters")
	}

	db, err := database.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = database.Migrate(ctx, db)
	cancel()
	if err != nil {
		log.Fatal("database migration failed")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	server := &http.Server{Addr: ":" + port, Handler: router(db), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Launchboard ready at http://localhost:%s", port)
	log.Fatal(server.ListenAndServe())
}

func router(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		page, _ := assets.ReadFile("templates/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			fail(w, 503, "Database unavailable")
			return
		}
		respond(w, 200, map[string]string{"status": "ok", "database": "connected"})
	})
	mux.HandleFunc("GET /api/ideas", func(w http.ResponseWriter, r *http.Request) {
		visitor, err := visitorID(w, r)
		if err != nil {
			fail(w, 500, "Could not create visitor session")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		ideas, err := listIdeas(ctx, db, visitor)
		if err != nil {
			fail(w, 503, "データを読み込めませんでした。もう一度お試しください。")
			return
		}
		respond(w, 200, ideas)
	})
	mux.HandleFunc("POST /api/ideas", func(w http.ResponseWriter, r *http.Request) { createTask(db, w, r) })
	mux.HandleFunc("GET /api/export", func(w http.ResponseWriter, r *http.Request) { exportTasks(db, w, r) })
	mux.HandleFunc("POST /api/samples", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := database.AddSamples(ctx, db); err != nil {
			fail(w, 503, "サンプルを読み込めませんでした。")
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/samples/archive", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, err := db.ExecContext(ctx, "UPDATE launchboard_ideas SET archived=TRUE,version=version+1,updated_at=NOW() WHERE seed_key IS NOT NULL AND NOT archived"); err != nil {
			fail(w, 503, "サンプルを整理できませんでした。")
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/ideas/{id}/vote", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id < 1 {
			fail(w, 400, "Invalid idea ID")
			return
		}
		var body struct {
			Voted *bool `json:"voted"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body.Voted == nil {
			fail(w, 400, "voted is required")
			return
		}
		visitor, err := visitorID(w, r)
		if err != nil {
			fail(w, 500, "Could not create visitor session")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var exists bool
		if err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM launchboard_ideas WHERE id=$1)", id).Scan(&exists); err != nil {
			fail(w, 503, "Could not record vote")
			return
		}
		if !exists {
			fail(w, 404, "Idea not found")
			return
		}
		if *body.Voted {
			_, err = db.ExecContext(ctx, "INSERT INTO launchboard_votes(idea_id,visitor) VALUES($1,$2) ON CONFLICT DO NOTHING", id, visitor)
		} else {
			_, err = db.ExecContext(ctx, "DELETE FROM launchboard_votes WHERE idea_id=$1 AND visitor=$2", id, visitor)
		}
		if err != nil {
			fail(w, 503, "投票できませんでした。もう一度お試しください。")
			return
		}
		respond(w, 200, map[string]bool{"voted": *body.Voted})
	})
	mux.HandleFunc("PATCH /api/ideas/{id}", func(w http.ResponseWriter, r *http.Request) { updateTask(db, w, r) })

	protected := accessControl(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != r.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					fail(w, 403, "Cross-origin requests are not allowed")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "Cross-origin requests are not allowed")
				return
			}
		}
		protected.ServeHTTP(w, r)
	})
}

func validCategory(s string) bool { return s == "Product" || s == "Design" || s == "Engineering" }
func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		fail(w, 400, "Invalid request body")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, 400, "Invalid request body")
		return false
	}
	return true
}
func visitorID(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie("launchboard_visitor"); err == nil && len(cookie.Value) == 64 {
		if _, err := hex.DecodeString(cookie.Value); err == nil {
			return cookie.Value, nil
		}
	}
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	id := hex.EncodeToString(value)
	http.SetCookie(w, &http.Cookie{Name: "launchboard_visitor", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", MaxAge: 60 * 60 * 24 * 365})
	return id, nil
}
