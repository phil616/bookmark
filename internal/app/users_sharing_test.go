package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net/url"
)

type apiClient struct {
	t      *testing.T
	h      http.Handler
	cookie string
}

func (c apiClient) call(method, path string, body any, status int) map[string]json.RawMessage {
	c.t.Helper()
	var input string
	if body != nil {
		p, e := json.Marshal(body)
		if e != nil {
			c.t.Fatal(e)
		}
		input = string(p)
	}
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(input))
	r.Header.Set("X-Requested-With", "online-bookmarks")
	if c.cookie != "" {
		r.Header.Set("Cookie", c.cookie)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	if w.Code != status {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body)
	}
	var result map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &result)
	return result
}
func loginClient(t *testing.T, h http.Handler, key string) apiClient {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"api_key":%q}`, key)))
	r.Header.Set("X-Requested-With", "online-bookmarks")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	return apiClient{t, h, w.Header().Get("Set-Cookie")}
}
func jsonAs[T any](t *testing.T, data json.RawMessage) T {
	t.Helper()
	var out T
	if e := json.Unmarshal(data, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func createHTTPUser(c apiClient, name, key string) User {
	data := c.call("POST", "/admin/users", UserInput{Username: name, APIKey: key}, 200)
	return User{ID: jsonAs[string](c.t, data["id"]), Username: name}
}
func createHTTPBookmark(c apiClient, raw string, tags []string, score float64) Bookmark {
	data := c.call("POST", "/bookmarks", BookmarkInput{Raw: raw, Tags: tags, Score: &score, CreateTags: true}, 200)
	return Bookmark{ID: jsonAs[string](c.t, data["id"]), Raw: raw, Version: jsonAs[uint](c.t, data["version"])}
}
func TestRolesAndTenantIsolation(t *testing.T) {
	a := testApp(t)
	h := a.Handler()
	admin := loginClient(t, h, "test-admin-key-123456789")
	for _, p := range []string{"/bookmarks", "/tags", "/exports", "/settings/llm", "/shares"} {
		admin.call("GET", p, nil, 403)
	}
	admin.call("POST", "/bookmarks", BookmarkInput{Raw: "forbidden"}, 403)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-admin-key-123456789")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	alice := createHTTPUser(admin, "alice", "alice-secret-key-123456")
	bob := createHTTPUser(admin, "bob", "bob-secret-key-12345678")
	x := loginClient(t, h, "alice-secret-key-123456")
	y := loginClient(t, h, "bob-secret-key-12345678")
	x.call("GET", "/admin/users", nil, 403)
	x.call("DELETE", "/admin/users/"+bob.ID, nil, 403)
	b := createHTTPBookmark(x, "same raw", []string{"same/child"}, .8)
	createHTTPBookmark(y, "same raw", []string{"same/child"}, .3)
	y.call("GET", "/bookmarks/"+b.ID, nil, 404)
	y.call("PUT", "/bookmarks/"+b.ID, BookmarkInput{Raw: "changed", Version: 1}, 404)
	y.call("DELETE", "/bookmarks/"+b.ID, nil, 200)
	x.call("GET", "/bookmarks/"+b.ID, nil, 200)
	for _, c := range []apiClient{x, y} {
		v := c.call("GET", "/bookmarks", nil, 200)
		if jsonAs[int](t, v["total"]) != 1 {
			t.Fatal(v)
		}
	}
	scoped := a.forUser(alice.ID)
	var tag Tag
	if e := scoped.DB.First(&tag, "path = ?", "same").Error; e != nil {
		t.Fatal(e)
	}
	y.call("PUT", "/tags/"+tag.ID, map[string]string{"path": "other"}, 404)
	y.call("DELETE", "/tags/"+tag.ID, nil, 404)
	x.call("PUT", "/settings/llm", LLMSettings{BaseURL: "https://example.com", Model: "alice-model", APIKey: "alice-private-secret"}, 200)
	v := y.call("GET", "/settings/llm", nil, 200)
	if jsonAs[bool](t, v["has_api_key"]) {
		t.Fatal("settings leaked")
	}
	x.call("POST", "/shares", ShareInput{Kind: "bookmark", BookmarkID: b.ID}, 200)
	y.call("POST", "/shares", ShareInput{Kind: "bookmark", BookmarkID: b.ID}, 404)
	export := y.call("GET", "/exports", nil, 200)
	if len(jsonAs[[]Record](t, export["bookmarks"])) != 1 {
		t.Fatal(export)
	}
	// Neither duplicate credentials nor administrator mutation is permitted.
	admin.call("POST", "/admin/users", UserInput{Username: "duplicate", APIKey: "test-admin-key-123456789"}, 409)
	var su User
	a.DB.First(&su, "role = ?", "superadmin")
	admin.call("DELETE", "/admin/users/"+su.ID, nil, 404)
	admin.call("PUT", "/admin/users/"+alice.ID, UserInput{Username: "alice-renamed", Disabled: true}, 200)
	x.call("GET", "/auth/session", nil, 401)
	admin.call("PUT", "/admin/users/"+alice.ID, UserInput{Username: "alice-renamed", APIKey: "alice-reset-secret-1234"}, 200)
	x = loginClient(t, h, "alice-reset-secret-1234")
	x.call("GET", "/bookmarks/"+b.ID, nil, 200)
	admin.call("DELETE", "/admin/users/"+alice.ID, nil, 200)
	x.call("GET", "/auth/session", nil, 401)
	for _, model := range []any{&Bookmark{}, &Tag{}, &Share{}, &Session{}} {
		var n int64
		db := a.DB.WithContext(req.Context())
		db.Model(model).Where("user_id = ?", alice.ID).Count(&n)
		if n != 0 {
			t.Fatalf("user data retained: %T %d", model, n)
		}
	}
	y.call("GET", "/bookmarks", nil, 200)
}

func TestSharedSubtreeMergeAndLifecycle(t *testing.T) {
	a := testApp(t)
	h := a.Handler()
	admin := loginClient(t, h, "test-admin-key-123456789")
	source := createHTTPUser(admin, "source", "source-secret-key-1234")
	target := createHTTPUser(admin, "target", "target-secret-key-1234")
	src := loginClient(t, h, "source-secret-key-1234")
	dst := loginClient(t, h, "target-secret-key-1234")
	b := createHTTPBookmark(src, "shared raw", []string{"私有/编程/Go", "隐藏/秘密"}, 0)
	src.call("POST", "/tags", map[string]string{"path": "私有/编程/空目录/子级"}, 200)
	createHTTPBookmark(src, "outside", []string{"私有/历史"}, .8)
	var root Tag
	a.forUser(source.ID).DB.First(&root, "path = ?", "私有/编程")
	response := src.call("POST", "/shares", ShareInput{Kind: "tag", TagID: root.ID}, 200)
	path := jsonAs[string](t, response["path"])
	token := strings.TrimPrefix(path, "/share/")
	share := jsonAs[Share](t, response["share"])
	if share.Label != "编程" {
		t.Fatal("ancestor path leaked through share metadata", share.Label)
	}
	apiClient{t, h, ""}.call("GET", "/shares/"+token, nil, 401)
	src.call("POST", "/shares/"+token+"/preview", ShareImport{}, 400)
	// Subsequent source tree changes cannot alter the shared snapshot.
	src.call("PUT", "/tags/"+root.ID, map[string]string{"path": "移动/新位置"}, 200)
	createHTTPBookmark(src, "later", []string{"移动/新位置/Go"}, .5)
	data := dst.call("GET", "/shares/"+token, nil, 200)
	bundle := jsonAs[Bundle](t, data["data"])
	if !reflect.DeepEqual(bundle.Tags, []string{"编程", "编程/Go", "编程/空目录", "编程/空目录/子级"}) || len(bundle.Bookmarks) != 1 || !reflect.DeepEqual(bundle.Bookmarks[0].Tags, []string{"编程/Go"}) {
		t.Fatal(bundle)
	}
	// A duplicate stays active with the recipient's score; equal leaf names in
	// different parents stay distinct. Ancestor associations collapse to leaves.
	existing := createHTTPBookmark(dst, b.Raw, []string{"收件箱/编程", "其他/Go"}, .9)
	dst.call("POST", "/tags", map[string]string{"path": "收件箱/编程/本地"}, 200)
	pre := dst.call("POST", "/shares/"+token+"/preview", ShareImport{TargetPath: "收件箱"}, 200)
	counts := jsonAs[map[string]int](t, pre["counts"])
	if counts["merged"] != 1 || counts["tags_reused"] != 2 || counts["tags_created"] != 3 {
		t.Fatal(counts)
	}
	// Preview performs real validation inside a rolled-back transaction.
	var n int64
	a.forUser(target.ID).DB.Model(&Tag{}).Where("path = ?", "收件箱/编程/Go").Count(&n)
	if n != 0 {
		t.Fatal("preview persisted")
	}
	rev := jsonAs[string](t, pre["revision"])
	dst.call("POST", "/shares/"+token+"/commit", ShareImport{TargetPath: "其他", Revision: rev}, 409)
	dst.call("POST", "/tags", map[string]string{"path": "new mutation"}, 200)
	dst.call("POST", "/shares/"+token+"/commit", ShareImport{TargetPath: "收件箱", Revision: rev}, 409)
	pre = dst.call("POST", "/shares/"+token+"/preview", ShareImport{TargetPath: "收件箱"}, 200)
	src.call("POST", "/tags", map[string]string{"path": "unrelated owner mutation"}, 200)
	dst.call("POST", "/shares/"+token+"/commit", ShareImport{TargetPath: "收件箱", Revision: jsonAs[string](t, pre["revision"])}, 200)
	got := dst.call("GET", "/bookmarks/"+existing.ID, nil, 200)
	if jsonAs[float64](t, got["score"]) != .9 || !reflect.DeepEqual(jsonAs[[]string](t, got["tags"]), []string{"其他/Go", "收件箱/编程/Go"}) {
		t.Fatal(got)
	}
	version := jsonAs[uint](t, got["version"])
	pre = dst.call("POST", "/shares/"+token+"/preview", ShareImport{TargetPath: "收件箱"}, 200)
	if jsonAs[map[string]int](t, pre["counts"])["unchanged"] != 1 {
		t.Fatal(pre)
	}
	dst.call("POST", "/shares/"+token+"/commit", ShareImport{TargetPath: "收件箱", Revision: jsonAs[string](t, pre["revision"])}, 200)
	got = dst.call("GET", "/bookmarks/"+existing.ID, nil, 200)
	if jsonAs[uint](t, got["version"]) != version {
		t.Fatal("repeat import modified bookmark")
	}
	var tags []Tag
	a.forUser(target.ID).DB.Order("path").Find(&tags)
	paths := map[string]Tag{}
	for _, tag := range tags {
		paths[tag.Path] = tag
	}
	if paths["收件箱/编程/空目录/子级"].ParentID != paths["收件箱/编程/空目录"].ID || paths["收件箱/编程/本地"].ID == "" {
		t.Fatal(tags)
	}
	// Owner pause, expiry, and revocation all invalidate read and import.
	admin.call("PUT", "/admin/users/"+source.ID, UserInput{Username: "source", Disabled: true}, 200)
	dst.call("GET", "/shares/"+token, nil, 404)
	admin.call("PUT", "/admin/users/"+source.ID, UserInput{Username: "source"}, 200)
	dst.call("GET", "/shares/"+token, nil, 200)
	a.DB.Model(&Share{}).Where("id = ?", share.ID).Update("expires_at", time.Now().Add(-time.Hour))
	dst.call("GET", "/shares/"+token, nil, 404)
	a.DB.Model(&Share{}).Where("id = ?", share.ID).Update("expires_at", time.Now().Add(time.Hour))
	src = loginClient(t, h, "source-secret-key-1234")
	dst.call("DELETE", "/shares/"+share.ID, nil, 404)
	src.call("DELETE", "/shares/"+share.ID, nil, 200)
	dst.call("GET", "/shares/"+token, nil, 404)
	dst.call("POST", "/shares/"+token+"/commit", ShareImport{Revision: rev}, 404)
}

func TestSingleShareAndRollback(t *testing.T) {
	a := testApp(t)
	h := a.Handler()
	admin := loginClient(t, h, "test-admin-key-123456789")
	target := createHTTPUser(admin, "target", "target-single-secret-123")
	src := loginClient(t, h, "test-api-key-123456789")
	dst := loginClient(t, h, "target-single-secret-123")
	b := createHTTPBookmark(src, "single", []string{"知识/计算机/Go", "兴趣/Go"}, 0)
	src.call("POST", "/tags", map[string]string{"path": "知识/无关空目录"}, 200)
	response := src.call("POST", "/shares", ShareInput{Kind: "bookmark", BookmarkID: b.ID}, 200)
	token := strings.TrimPrefix(jsonAs[string](t, response["path"]), "/share/")
	data := dst.call("GET", "/shares/"+token, nil, 200)
	bundle := jsonAs[Bundle](t, data["data"])
	if len(bundle.Tags) != 5 {
		t.Fatal(bundle.Tags)
	}
	pre := dst.call("POST", "/shares/"+token+"/preview", ShareImport{}, 200)
	dst.call("POST", "/shares/"+token+"/commit", ShareImport{Revision: jsonAs[string](t, pre["revision"])}, 200)
	out, e := a.forUser(target.ID).Export()
	if e != nil || len(out.Bookmarks) != 1 || *out.Bookmarks[0].Score != 0 {
		t.Fatal(out, e)
	}
	longPath := strings.Repeat("长", 340)
	dst.call("POST", "/tags", map[string]string{"path": longPath}, 200)
	before := dbRevision(a.forUser(target.ID).DB)
	dst.call("POST", "/shares/"+token+"/preview", ShareImport{TargetPath: longPath}, 400)
	if before != dbRevision(a.forUser(target.ID).DB) {
		t.Fatal("invalid mapping persisted")
	}
	// Empty subtrees are also transferable.
	var empty Tag
	a.DB.First(&empty, "path = ?", "知识/无关空目录")
	response = src.call("POST", "/shares", ShareInput{Kind: "tag", TagID: empty.ID}, 200)
	token = strings.TrimPrefix(jsonAs[string](t, response["path"]), "/share/")
	pre = dst.call("POST", "/shares/"+token+"/preview", ShareImport{}, 200)
	dst.call("POST", "/shares/"+token+"/commit", ShareImport{Revision: jsonAs[string](t, pre["revision"])}, 200)
	var n int64
	a.forUser(target.ID).DB.Model(&Tag{}).Where("path = ?", "无关空目录").Count(&n)
	if n != 1 {
		t.Fatal(n)
	}
	// Some mapped parents are valid before a deeper node exceeds the path limit.
	createHTTPBookmark(src, "rollback hierarchy", []string{"r/a", "r/zzzzzzzzzz"}, .5)
	var subtree Tag
	a.DB.First(&subtree, "path = ?", "r")
	response = src.call("POST", "/shares", ShareInput{Kind: "tag", TagID: subtree.ID}, 200)
	token = strings.TrimPrefix(jsonAs[string](t, response["path"]), "/share/")
	destination := strings.Repeat("x", 1018)
	dst.call("POST", "/tags", map[string]string{"path": destination}, 200)
	scoped := a.forUser(target.ID)
	before = dbRevision(scoped.DB)
	dst.call("POST", "/shares/"+token+"/preview", ShareImport{TargetPath: destination}, 400)
	shared, _, err := scoped.readShare(token)
	if err != nil {
		t.Fatal(err)
	}
	dst.call("POST", "/shares/"+token+"/commit", ShareImport{TargetPath: destination, Revision: shareRevision(scoped.DB, shared, destination)}, 400)
	if dbRevision(scoped.DB) != before {
		t.Fatal("partial mapped subtree was retained")
	}
}

func TestUpgradeSingleUserDatabase(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("DATABASE_URL", "")
	t.Setenv("INITIAL_ADMIN_KEY", "new-admin-secret-123456")
	var db *gorm.DB
	var e error
	postgresTest := os.Getenv("TEST_DATABASE_URL") != ""
	if postgresTest {
		dsn := os.Getenv("TEST_DATABASE_URL")
		control, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		schema := "upgrade_" + ID()
		if err = control.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { control.Exec("DROP SCHEMA " + schema + " CASCADE"); sql, _ := control.DB(); sql.Close() })
		if parsed, err := url.Parse(dsn); err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
			q := parsed.Query()
			q.Set("search_path", schema)
			parsed.RawQuery = q.Encode()
			dsn = parsed.String()
		} else {
			dsn += " search_path=" + schema
		}
		t.Setenv("DATABASE_URL", dsn)
		db, e = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	} else {
		db, e = gorm.Open(sqlite.Open(os.Getenv("DATA_DIR")+"/bookmarks.db"), &gorm.Config{})
	}
	if e != nil {
		t.Fatal(e)
	}
	for _, sql := range []string{
		`CREATE TABLE migrations (id integer PRIMARY KEY)`, `INSERT INTO migrations VALUES (1)`,
		`CREATE TABLE bookmarks (id text PRIMARY KEY,raw text,digest text,tags_json text,description text,score real,version integer,created_at datetime,updated_at datetime)`,
		`CREATE UNIQUE INDEX idx_bookmarks_digest ON bookmarks(digest)`,
		`CREATE TABLE tags (id text PRIMARY KEY,path text,parent_id text,name text)`,
		`CREATE UNIQUE INDEX idx_tags_path ON tags(path)`,
		`CREATE TABLE settings (key text PRIMARY KEY,value text)`,
		`CREATE TABLE sessions (id text PRIMARY KEY,expires datetime)`,
	} {
		if postgresTest {
			sql = strings.ReplaceAll(sql, "datetime", "timestamptz")
			sql = strings.ReplaceAll(sql, "score real", "score double precision")
		}
		if e = db.Exec(sql).Error; e != nil {
			t.Fatal(e)
		}
	}
	db.Exec(`INSERT INTO bookmarks(id,raw,digest,tags_json,score,version) VALUES(?,?,?,?,?,?)`, "legacy-bookmark", "old raw", digest("old raw"), `["旧/子级"]`, .8, 1)
	db.Exec(`INSERT INTO tags VALUES(?,?,?,?)`, "root-id", "旧", "", "旧")
	db.Exec(`INSERT INTO tags VALUES(?,?,?,?)`, "child-id", "旧/子级", "root-id", "子级")
	db.Create(&Setting{Key: "auth", Value: hashKey("old-personal-key-12345")})
	db.Create(&Setting{Key: "llm", Value: "legacy-llm"})
	db.Exec(`INSERT INTO sessions VALUES (?,?)`, "old-session", time.Now().Add(time.Hour))
	sq, _ := db.DB()
	sq.Close()
	a, e := Open()
	if e != nil {
		t.Fatal(e)
	}
	sq, _ = a.DB.DB()
	defer sq.Close()
	var u User
	if e = a.DB.First(&u, "username = ?", "legacy").Error; e != nil {
		t.Fatal(e)
	}
	if u.Role != "user" || !verify("old-personal-key-12345", u.KeyHash) {
		t.Fatal(u)
	}
	out, e := a.forUser(u.ID).Export()
	if e != nil || len(out.Bookmarks) != 1 || out.Bookmarks[0].Raw != base64.StdEncoding.EncodeToString([]byte("old raw")) {
		t.Fatal(out, e)
	}
	if a.forUser(u.ID).get("llm") != "legacy-llm" {
		t.Fatal("lost settings")
	}
	var tag Tag
	a.forUser(u.ID).DB.First(&tag, "id = ?", "child-id")
	if tag.ParentID != "root-id" {
		t.Fatal(tag)
	}
	var n int64
	a.DB.Model(&Session{}).Count(&n)
	if n != 0 {
		t.Fatal("legacy session retained")
	}
	h := a.Handler()
	client := loginClient(t, h, "old-personal-key-12345")
	client.call("GET", "/bookmarks/legacy-bookmark", nil, 200)
	admin := loginClient(t, h, "new-admin-secret-123456")
	admin.call("GET", "/bookmarks", nil, 403)
}

func TestNormalizeHierarchyTrie(t *testing.T) {
	for _, tc := range []struct{ in, out []string }{
		{[]string{"a", "a!", "a/b", "a/b/c", "a/bb", "a_b/x", "a%/x", "z/b", "z"}, []string{"a!", "a%/x", "a/b/c", "a/bb", "a_b/x", "z/b"}},
		{[]string{"知识/Go", "工作/Go", "知识/Go"}, []string{"工作/Go", "知识/Go"}},
		{[]string{"root/a/b/c", "root/a/b", "root"}, []string{"root/a/b/c"}},
	} {
		for range 10 {
			got, e := normalize(tc.in, .5, true)
			if e != nil || !reflect.DeepEqual(got, tc.out) {
				t.Fatal(tc.in, got, tc.out, e)
			}
		}
		strict, e := normalize(tc.out, .5, false)
		if e != nil || !reflect.DeepEqual(strict, tc.out) {
			t.Fatal(strict, e)
		}
	}
	for _, in := range [][]string{{"a", "a/b"}, {"a/b", "a"}, {"a/b/c", "a", "a/b"}} {
		if _, e := normalize(in, .5, false); e == nil {
			t.Fatal("accepted ancestors", in)
		}
	}
}
