package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"gorm.io/gorm"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("INITIAL_ADMIN_KEY", "test-admin-key-123456789")
	t.Setenv("INITIAL_API_KEY", "test-admin-key-123456789")
	if d := os.Getenv("TEST_DATABASE_URL"); d != "" {
		t.Setenv("DATABASE_URL", d)
	} else {
		t.Setenv("DATABASE_URL", "")
	}
	a, e := Open()
	if e != nil {
		t.Fatal(e)
	}
	if os.Getenv("TEST_DATABASE_URL") != "" {
		a.DB.Where("1=1").Delete(&Bookmark{})
		a.DB.Where("1=1").Delete(&Tag{})
		a.DB.Where("1=1").Delete(&Session{})
		a.DB.Delete(&Setting{}, "key = ?", "llm")
		a.DB.Where("role = ?", "user").Delete(&User{})
	}
	t.Cleanup(func() { s, _ := a.DB.DB(); s.Close() })
	user := User{ID: ID(), Username: "test-user", Role: "user", KeyHash: hashKey("test-api-key-123456789"), KeyDigest: digest("test-api-key-123456789")}
	if e := a.DB.Create(&user).Error; e != nil {
		t.Fatal(e)
	}
	return a.forUser(user.ID)
}
func ptr(v float64) *float64 { return &v }
func TestRules(t *testing.T) {
	a := testApp(t)
	b, e := a.Write("", BookmarkInput{Raw: "https://example.com/?a=1", Tags: []string{"知识/计算机", "知识/历史"}, CreateTags: true})
	if e != nil {
		t.Fatal(e)
	}
	if b.Score != .5 || b.Version != 1 {
		t.Fatal(b)
	}
	_, e = a.Write("", BookmarkInput{Raw: b.Raw})
	if e == nil {
		t.Fatal("duplicate accepted")
	}
	_, e = a.Write("", BookmarkInput{Raw: "x", Tags: []string{"知识", "知识/计算机"}, CreateTags: true})
	if e == nil {
		t.Fatal("ancestor accepted")
	}
	b, e = a.Write(b.ID, BookmarkInput{Raw: b.Raw, Tags: b.Tags, Score: ptr(0), Version: 1})
	if e != nil || b.Tags[len(b.Tags)-1] != Archived {
		t.Fatal(b, e)
	}
	v, _ := a.List(Query{})
	if v["total"] != 0 {
		t.Fatal(v)
	}
	v, _ = a.List(Query{Archived: true})
	if v["total"] != 1 {
		t.Fatal(v)
	}
	_, e = a.Write(b.ID, BookmarkInput{Raw: b.Raw, Version: 1})
	if e == nil {
		t.Fatal("stale accepted")
	}
	if e = a.Batch([]string{b.ID}, "restore"); e != nil {
		t.Fatal(e)
	}
}
func TestImport(t *testing.T) {
	a := testApp(t)
	b, e := a.Write("", BookmarkInput{Raw: "中文\nhttps://example.com", Description: "旧解释", Tags: []string{"知识/计算机"}, CreateTags: true})
	if e != nil {
		t.Fatal(e)
	}
	bundle := Bundle{Schema: 1, Bookmarks: []Record{{Raw: base64.StdEncoding.EncodeToString([]byte(b.Raw)), Description: "新解释", Tags: []string{"知识/计算机/编程", "知识/历史"}, Score: ptr(0)}}}
	for range 2 {
		if e = a.DB.Transaction(func(tx *gorm.DB) error { _, e := importInto(tx, bundle); return e }); e != nil {
			t.Fatal(e)
		}
	}
	a.DB.First(&b, "id = ?", b.ID)
	if b.Score != 0 || len(b.Tags) != 3 || strings.Count(b.Description, "新解释") != 1 {
		t.Fatal(b)
	}
	bundle.Bookmarks = append(bundle.Bookmarks, Record{Raw: "invalid!"})
	if e = a.DB.Transaction(func(tx *gorm.DB) error { _, e := importInto(tx, bundle); return e }); e == nil {
		t.Fatal("invalid accepted")
	}
	out, e := a.Export()
	if e != nil || out.Bookmarks[0].Raw != base64.StdEncoding.EncodeToString([]byte(b.Raw)) {
		t.Fatal(out, e)
	}
}
func TestMove(t *testing.T) {
	a := testApp(t)
	b, e := a.Write("", BookmarkInput{Raw: "move", Tags: []string{"知识/计算机/编程"}, CreateTags: true})
	if e != nil {
		t.Fatal(e)
	}
	var tag Tag
	a.DB.First(&tag, "path = ?", "知识/计算机")
	id := tag.ID
	if e = a.TagChange(id, "技能/软件", false); e != nil {
		t.Fatal(e)
	}
	a.DB.First(&b, "id = ?", b.ID)
	if b.Tags[0] != "技能/软件/编程" {
		t.Fatal(b)
	}
	a.DB.First(&tag, "id = ?", id)
	if tag.Path != "技能/软件" {
		t.Fatal(tag)
	}
	if e = a.TagChange(id, "技能/软件/循环", false); e == nil {
		t.Fatal("cycle accepted")
	}
	if e = a.TagChange(id, "", true); e != nil {
		t.Fatal(e)
	}
	a.DB.First(&b, "id = ?", b.ID)
	if len(b.Tags) != 0 {
		t.Fatal(b)
	}
}
func TestHTTPAuth(t *testing.T) {
	a := testApp(t)
	h := a.Handler()
	request := func(method, path, body, key, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Requested-With", "online-bookmarks")
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if request("GET", "/api/v1/bookmarks", "", "", "").Code != 401 {
		t.Fatal("unprotected")
	}
	login := request("POST", "/api/v1/auth/login", `{"api_key":"test-api-key-123456789"}`, "", "")
	if login.Code != 200 {
		t.Fatal(login.Body)
	}
	cookie := login.Header().Get("Set-Cookie")
	if request("GET", "/api/v1/auth/session", "", "", cookie).Code != 200 {
		t.Fatal("session")
	}
	r := httptest.NewRequest("POST", "/api/v1/bookmarks", strings.NewReader(`{}`))
	r.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("csrf")
	}
	r = httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(`{}`))
	r.Header.Set("Authorization", "Bearer test-api-key-123456789")
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("origin")
	}
	if request("POST", "/mcp", "{}", "", cookie).Code != 401 {
		t.Fatal("MCP cookie accepted")
	}
	rot := request("PUT", "/api/v1/auth/key", `{"current":"test-api-key-123456789","next":"new-key-123456789012345"}`, "", cookie)
	if rot.Code != 200 {
		t.Fatal(rot.Body)
	}
	if request("GET", "/api/v1/auth/session", "", "", cookie).Code != 401 {
		t.Fatal("old session")
	}
	if request("GET", "/api/v1/bookmarks", "", "test-api-key-123456789", "").Code != 401 {
		t.Fatal("old key")
	}
}
func TestFetchPrivate(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.0.0.1", "::1", "169.254.169.254", "100.64.0.1", "::ffff:192.168.1.1"} {
		if allowedIP(net.ParseIP(s)) {
			t.Fatal(s)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("private request reached server") }))
	defer srv.Close()
	if _, e := fetchHTML(context.Background(), srv.URL); e == nil {
		t.Fatal("private fetch accepted")
	}
}
func TestLLMMock(t *testing.T) {
	a := testApp(t)
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.URL.Path != "/v1/messages" {
			t.Error(r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "secret" {
			t.Error("missing key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","model":"mock","content":[{"type":"text","text":"{\"description\":\"测试\",\"tags\":[\"知识/计算机\"]}"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer s.Close()
	secret, _ := a.encrypt("secret")
	p, _ := json.Marshal(LLMSettings{BaseURL: s.URL, Model: "mock", APIKey: secret, Enabled: true})
	put(a.DB, "llm", string(p))
	v, e := a.Analyze(context.Background(), "普通文字", "both")
	if e != nil || v["description"] != "测试" || count != 1 {
		t.Fatal(v, e, count)
	}
	var n int64
	a.DB.Model(&Bookmark{}).Count(&n)
	if n != 0 {
		t.Fatal("analysis persisted")
	}
}

func TestDescriptionIdempotence(t *testing.T) {
	v := mergeDescription("one", "two")
	if mergeDescription(v, v) != v || mergeDescription(v, "one") != v {
		t.Fatal("reimport duplicated explanation")
	}
}
func TestInitializationIgnoresConfig(t *testing.T) {
	a := testApp(t)
	t.Setenv("INITIAL_ADMIN_KEY", "ignored-admin-key-12345")
	b, e := Open()
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := b.DB.DB()
	defer sql.Close()
	var u User
	if e = a.DB.First(&u, "role = ?", "superadmin").Error; e != nil {
		t.Fatal(e)
	}
	if !verify("test-admin-key-123456789", u.KeyHash) || verify("ignored-admin-key-12345", u.KeyHash) {
		t.Fatal("initialization key reused")
	}
}

func TestHTMLResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		ct, body  string
		wantError bool
	}{{"html", 200, "text/html; charset=utf-8", `<html><head><title>标题</title><meta name="description" content="SEO"></head><body>正文<script>badscript</script><p hidden>invisible</p></body></html>`, false}, {"redirect", 302, "text/html", "", true}, {"media", 200, "video/mp4", "", true}, {"error", 403, "text/html", "", true}, {"oversize", 200, "text/html", strings.Repeat("x", (2<<20)+1), true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ct)
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer s.Close()
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			v, e := readHTML(context.Background(), s.URL, client)
			if (e != nil) != tc.wantError {
				t.Fatal(v, e)
			}
			if !tc.wantError && (!strings.Contains(v, "SEO") || !strings.Contains(v, "标题") || strings.Contains(v, "badscript") || strings.Contains(v, "invisible")) {
				t.Fatal(v)
			}
		})
	}
}
func TestHTMLTimeout(t *testing.T) {
	var count atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1); <-r.Context().Done() }))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := readHTML(ctx, s.URL, &http.Client{}); e == nil || count.Load() != 1 {
		t.Fatal(e, count.Load())
	}
}

func TestFrontendDeploymentCaching(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	t.Setenv("WEB_DIR", dir)
	if e := os.Mkdir(filepath.Join(dir, "assets"), 0700); e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"index.html": `<html><script src="/assets/current-123.js"></script></html>`, "assets/current-123.js": `console.log("current")`} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	h := a.Handler()
	for _, tc := range []struct {
		path, cache string
		status      int
	}{
		{"/", "no-cache, max-age=0, must-revalidate", 200},
		{"/bookmarks", "no-cache, max-age=0, must-revalidate", 200},
		{"/share/token", "no-cache, max-age=0, must-revalidate", 200},
		{"/assets/current-123.js", "public, max-age=31536000, immutable", 200},
		{"/assets/old-456.js", "no-store", 404},
		{"/api/v1/auth/session", "private, no-store", 401},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status || w.Header().Get("Cache-Control") != tc.cache {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Header().Get("Cache-Control"))
		}
		if tc.status == 404 && strings.Contains(w.Body.String(), "<html>") {
			t.Fatal("missing bundle returned SPA HTML")
		}
	}
}
