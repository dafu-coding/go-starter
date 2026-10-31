package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type loginAttempt struct {
	count int
	until time.Time
}

// Optional shared workspace access; changing APP_PASSWORD invalidates all sessions.
func accessControl(next http.Handler) http.Handler {
	password := os.Getenv("APP_PASSWORD")
	key := sha256.Sum256([]byte(password))
	var mu sync.Mutex
	attempts := map[string]loginAttempt{}
	authenticated := func(r *http.Request) bool {
		cookie, err := r.Cookie("launchboard_session")
		if err != nil {
			return false
		}
		pieces := strings.Split(cookie.Value, ".")
		if len(pieces) != 3 {
			return false
		}
		expiry, err := strconv.ParseInt(pieces[0], 10, 64)
		if err != nil || time.Now().Unix() >= expiry {
			return false
		}
		signature, err := hex.DecodeString(pieces[2])
		if err != nil {
			return false
		}
		mac := hmac.New(sha256.New, key[:])
		mac.Write([]byte(pieces[0] + "." + pieces[1]))
		return hmac.Equal(signature, mac.Sum(nil))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" {
			w.Header().Set("Cache-Control", "no-store")
			switch r.Method {
			case "GET":
				respond(w, 200, map[string]any{"protected": password != "", "authenticated": password == "" || authenticated(r)})
				return
			case "DELETE":
				http.SetCookie(w, &http.Cookie{Name: "launchboard_session", Path: "/", Value: "", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secureRequest(r)})
				respond(w, 200, map[string]bool{"ok": true})
				return
			case "POST":
				if password == "" {
					respond(w, 200, map[string]bool{"ok": true})
					return
				}
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}
				now := time.Now()
				mu.Lock()
				for address, attempt := range attempts {
					if now.After(attempt.until) {
						delete(attempts, address)
					}
				}
				attempt := attempts[host]
				blocked := attempt.count >= 10 || len(attempts) > 10000
				mu.Unlock()
				if blocked {
					w.Header().Set("Retry-After", "600")
					fail(w, 429, "試行回数が多すぎます。10分後にお試しください。")
					return
				}
				var body struct {
					Password string `json:"password"`
				}
				if !decode(w, r, &body) {
					return
				}
				supplied := sha256.Sum256([]byte(body.Password))
				if subtle.ConstantTimeCompare(supplied[:], key[:]) != 1 {
					mu.Lock()
					attempt = attempts[host]
					attempt.count++
					if attempt.until.IsZero() {
						attempt.until = now.Add(10 * time.Minute)
					}
					attempts[host] = attempt
					mu.Unlock()
					fail(w, 401, "パスワードが違います。")
					return
				}
				mu.Lock()
				delete(attempts, host)
				mu.Unlock()
				nonce := make([]byte, 32)
				if _, err := rand.Read(nonce); err != nil {
					fail(w, 500, "ログインできませんでした。")
					return
				}
				payload := strconv.FormatInt(now.Add(7*24*time.Hour).Unix(), 10) + "." + hex.EncodeToString(nonce)
				mac := hmac.New(sha256.New, key[:])
				mac.Write([]byte(payload))
				value := payload + "." + hex.EncodeToString(mac.Sum(nil))
				http.SetCookie(w, &http.Cookie{Name: "launchboard_session", Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secureRequest(r), MaxAge: 7 * 24 * 60 * 60})
				respond(w, 200, map[string]bool{"ok": true})
				return
			default:
				w.Header().Set("Allow", "GET, POST, DELETE")
				fail(w, 405, "Method not allowed")
				return
			}
		}
		if password != "" && strings.HasPrefix(r.URL.Path, "/api/") && !authenticated(r) {
			fail(w, 401, "ワークスペースへのログインが必要です。")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
