package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/argon2"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func hashKey(k string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	h := argon2.IDKey([]byte(k), salt, 1, 32*1024, 2, 32)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(h)
}
func verify(k, h string) bool {
	parts := strings.Split(h, ":")
	if len(parts) != 2 {
		return false
	}
	salt, e := hex.DecodeString(parts[0])
	if e != nil {
		return false
	}
	sum := argon2.IDKey([]byte(k), salt, 1, 32*1024, 2, 32)
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum)), []byte(parts[1])) == 1
}
func respond(w http.ResponseWriter, v any, e error) {
	w.Header().Set("Content-Type", "application/json")
	if e != nil {
		status := 400
		code := "validation_error"
		if errors.Is(e, gorm.ErrRecordNotFound) {
			status = 404
			code = "not_found"
		}
		if strings.Contains(e.Error(), "conflict") || strings.Contains(strings.ToLower(e.Error()), "duplicate") || strings.Contains(strings.ToLower(e.Error()), "unique constraint") {
			status = 409
			code = "conflict"
		}
		w.WriteHeader(status)
		payload := map[string]any{"code": code, "message": e.Error()}
		var duplicate *DuplicateError
		if errors.As(e, &duplicate) {
			payload["existing_id"] = duplicate.ID
		}
		json.NewEncoder(w).Encode(payload)
		return
	}
	json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e == io.EOF {
		return nil
	} else if e != nil {
		return e
	}
	return errors.New("unexpected trailing JSON")
}
func (a *App) Handler() http.Handler {
	root := *a
	root.DB = a.DB.WithContext(context.Background())
	a = &root
	mux := http.NewServeMux()
	for _, s := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if s != "" {
			a.Origins[s] = true
		}
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		sql, e := a.DB.DB()
		if e == nil {
			e = sql.Ping()
		}
		if e != nil {
			http.Error(w, "unhealthy", 503)
			return
		}
		respond(w, map[string]bool{"ok": true}, nil)
	})
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			APIKey   string `json:"api_key"`
			Username string `json:"username"`
		}
		if e := decode(w, r, &in); e != nil {
			respond(w, nil, e)
			return
		}
		req := r.Clone(r.Context())
		req.Header.Set("Authorization", "Bearer "+in.APIKey)
		u, ok := a.authenticate(req, true)
		if !ok || in.Username != "" && in.Username != u.Username {
			http.Error(w, "用户名或密钥错误", 401)
			return
		}
		token := ID() + ID()
		if e := a.DB.Create(&Session{ID: digest(token), UserID: u.ID, Expires: time.Now().Add(7 * 24 * time.Hour)}).Error; e != nil {
			respond(w, nil, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.Secure, MaxAge: 604800})
		respond(w, map[string]any{"ok": true, "user": u}, nil)
	})
	mux.HandleFunc("GET /api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"ok": true, "user": principal(r)}, nil)
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, e := r.Cookie("session"); e == nil {
			a.DB.Delete(&Session{}, "id = ?", digest(c.Value))
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.Secure})
		respond(w, map[string]bool{"ok": true}, nil)
	})
	mux.HandleFunc("PUT /api/v1/auth/key", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Current string `json:"current"`
			Next    string `json:"next"`
		}
		e := decode(w, r, &in)
		u := principal(r)
		if e == nil && (!verify(in.Current, u.KeyHash) || len(in.Next) < 16) {
			e = errors.New("current key invalid or new key shorter than 16 characters")
		}
		if e == nil {
			e = a.DB.Transaction(func(tx *gorm.DB) error {
				if e := keyAvailable(tx, in.Next, u.ID); e != nil {
					return e
				}
				if e := tx.Model(&User{}).Where("id = ?", u.ID).Updates(map[string]any{"key_hash": hashKey(in.Next), "key_digest": digest(in.Next)}).Error; e != nil {
					return e
				}
				return tx.Where("user_id = ?", u.ID).Delete(&Session{}).Error
			})
		}
		respond(w, map[string]bool{"ok": e == nil}, e)
	})
	mux.HandleFunc("GET /api/v1/bookmarks", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		p, _ := strconv.Atoi(r.URL.Query().Get("page"))
		n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		v, e := a.List(Query{Keyword: r.URL.Query().Get("q"), Tags: r.URL.Query()["tag"], Archived: r.URL.Query().Get("archived") == "true", Unclassified: r.URL.Query().Get("unclassified") == "true", Page: p, Limit: n})
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/v1/bookmarks/{id}", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		var b Bookmark
		e := a.DB.First(&b, "id = ?", r.PathValue("id")).Error
		respond(w, b, e)
	})
	for _, method := range []string{"POST /api/v1/bookmarks", "PUT /api/v1/bookmarks/{id}"} {
		mux.HandleFunc(method, func(w http.ResponseWriter, r *http.Request) {
			a := a.forRequest(r)
			var in BookmarkInput
			e := decode(w, r, &in)
			var b Bookmark
			if e == nil {
				b, e = a.Write(r.PathValue("id"), in)
			}
			respond(w, b, e)
		})
	}
	mux.HandleFunc("DELETE /api/v1/bookmarks/{id}", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		respond(w, map[string]bool{"ok": true}, a.DB.Delete(&Bookmark{}, "id = ?", r.PathValue("id")).Error)
	})
	mux.HandleFunc("POST /api/v1/bookmarks/batch", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		var in struct {
			IDs    []string `json:"ids"`
			Action string   `json:"action"`
		}
		e := decode(w, r, &in)
		if e == nil {
			e = a.Batch(in.IDs, in.Action)
		}
		respond(w, map[string]bool{"ok": e == nil}, e)
	})
	mux.HandleFunc("GET /api/v1/tags", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		var ts []Tag
		e := a.DB.Order("path").Find(&ts).Error
		respond(w, ts, e)
	})
	mux.HandleFunc("POST /api/v1/tags", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		var in struct {
			Path string `json:"path"`
		}
		e := decode(w, r, &in)
		if e == nil {
			_, e = normalize([]string{in.Path}, .5, false)
			if in.Path == Archived {
				e = errors.New("reserved tag")
			}
		}
		if e == nil {
			e = a.DB.Transaction(func(tx *gorm.DB) error { return ensureTag(tx, in.Path) })
		}
		respond(w, map[string]bool{"ok": e == nil}, e)
	})
	mux.HandleFunc("PUT /api/v1/tags/{id}", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		var in struct {
			Path string `json:"path"`
		}
		e := decode(w, r, &in)
		if e == nil {
			e = a.TagChange(r.PathValue("id"), in.Path, false)
		}
		respond(w, map[string]bool{"ok": e == nil}, e)
	})
	mux.HandleFunc("DELETE /api/v1/tags/{id}", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		respond(w, map[string]bool{"ok": true}, a.TagChange(r.PathValue("id"), "", true))
	})
	a.analysisRoutes(mux)
	a.transferRoutes(mux)
	a.userRoutes(mux)
	a.shareRoutes(mux)
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { a.forRequest(r).MCP().ServeHTTP(w, r) }))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		dir := os.Getenv("WEB_DIR")
		if dir == "" {
			dir = "web/dist"
		}
		// Revalidate every SPA entry so deployments pick up the new asset hash.
		w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
		p := dir + r.URL.Path
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.FileServer(http.Dir(dir)).ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, dir+"/index.html")
	}))
	authLimit := rate.NewLimiter(10, 20)
	var gate sync.Mutex // Serialize mutations with import revision checks, across REST and MCP.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		if (r.URL.Path == "/api/v1/auth/login" || r.Header.Get("Authorization") != "") && !authLimit.Allow() {
			http.Error(w, "authentication rate limit", 429)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" {
			u, e := url.Parse(origin)
			same := e == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
			if !same && !a.Origins[origin] {
				http.Error(w, "origin denied", 403)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, MCP-Protocol-Version, Mcp-Method, Mcp-Name")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.WriteHeader(204)
			return
		}
		public := r.URL.Path == "/healthz" || r.URL.Path == "/api/v1/auth/login"
		protected := strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp"
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != "GET" && r.Header.Get("Authorization") == "" && r.Header.Get("X-Requested-With") != "online-bookmarks" {
			http.Error(w, "CSRF header required", 403)
			return
		}
		if protected && r.Method != "GET" && r.URL.Path != "/api/v1/analysis" && r.URL.Path != "/api/v1/settings/llm/test" {
			gate.Lock()
			defer gate.Unlock()
		}
		if protected && !public {
			u, ok := a.authenticate(r, r.URL.Path == "/mcp")
			if !ok {
				http.Error(w, "unauthorized", 401)
				return
			}
			adminPath := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
			authPath := r.URL.Path == "/api/v1/auth/session" || r.URL.Path == "/api/v1/auth/logout"
			if (u.Role == "superadmin" && !adminPath && !authPath) || (u.Role != "superadmin" && adminPath) {
				http.Error(w, "forbidden", 403)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), principalContext{}, u))
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}
func (a *App) Batch(ids []string, action string) error {
	if len(ids) == 0 || len(ids) > 1000 {
		return errors.New("select 1 to 1000 bookmarks")
	}
	return a.DB.Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			var b Bookmark
			if e := tx.First(&b, "id = ?", id).Error; e != nil {
				return e
			}
			switch action {
			case "delete":
				if e := tx.Delete(&b).Error; e != nil {
					return e
				}
				continue
			case "archive":
				b.Score = 0
			case "restore":
				b.Score = .5
			default:
				return errors.New("unknown action")
			}
			b.Version++
			if e := saveBookmark(tx, &b, false, true); e != nil {
				return e
			}
		}
		return nil
	})
}
