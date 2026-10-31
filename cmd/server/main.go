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
}

func main() {
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
		rows, err := db.QueryContext(ctx, `SELECT i.id,i.title,i.description,i.category,i.status,i.author,i.seed_votes+(SELECT COUNT(*) FROM launchboard_votes v WHERE v.idea_id=i.id),EXISTS(SELECT 1 FROM launchboard_votes v WHERE v.idea_id=i.id AND v.visitor=$1),i.created_at FROM launchboard_ideas i ORDER BY i.created_at DESC,i.id DESC LIMIT 500`, visitor)
		if err != nil {
			fail(w, 503, "データを読み込めませんでした。もう一度お試しください。")
			return
		}
		defer rows.Close()
		ideas := []Idea{}
		for rows.Next() {
			var idea Idea
			if err := rows.Scan(&idea.ID, &idea.Title, &idea.Description, &idea.Category, &idea.Status, &idea.Author, &idea.Votes, &idea.Voted, &idea.CreatedAt); err != nil {
				fail(w, 500, "Could not read idea")
				return
			}
			ideas = append(ideas, idea)
		}
		if rows.Err() != nil {
			fail(w, 503, "Could not read ideas")
			return
		}
		respond(w, 200, ideas)
	})
	mux.HandleFunc("POST /api/ideas", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Author      string `json:"author"`
			Category    string `json:"category"`
		}
		if !decode(w, r, &body) {
			return
		}
		body.Title = strings.TrimSpace(body.Title)
		body.Description = strings.TrimSpace(body.Description)
		body.Author = strings.TrimSpace(body.Author)
		if body.Author == "" {
			body.Author = "Guest"
		}
		if utf8.RuneCountInString(body.Title) < 1 || utf8.RuneCountInString(body.Title) > 100 || utf8.RuneCountInString(body.Description) < 1 || utf8.RuneCountInString(body.Description) > 600 || utf8.RuneCountInString(body.Author) > 30 || !validCategory(body.Category) {
			fail(w, 400, "タイトル・説明・カテゴリを確認してください。")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int
		err := db.QueryRowContext(ctx, `INSERT INTO launchboard_ideas(title,description,category,author) VALUES ($1,$2,$3,$4) RETURNING id`, body.Title, body.Description, body.Category, body.Author).Scan(&id)
		if err != nil {
			fail(w, 503, "保存できませんでした。もう一度お試しください。")
			return
		}
		respond(w, 201, map[string]int{"id": id})
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
	mux.HandleFunc("PATCH /api/ideas/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id < 1 {
			fail(w, 400, "Invalid idea ID")
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body.Status != "idea" && body.Status != "building" && body.Status != "shipped" {
			fail(w, 400, "Invalid status")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		result, err := db.ExecContext(ctx, "UPDATE launchboard_ideas SET status=$1 WHERE id=$2", body.Status, id)
		if err != nil {
			fail(w, 503, "更新できませんでした。もう一度お試しください。")
			return
		}
		count, err := result.RowsAffected()
		if err != nil {
			fail(w, 500, "Could not update idea")
			return
		}
		if count == 0 {
			fail(w, 404, "Idea not found")
			return
		}
		respond(w, 200, map[string]string{"status": body.Status})
	})
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
		mux.ServeHTTP(w, r)
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
