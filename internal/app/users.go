package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type User struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Username  string    `json:"username" gorm:"uniqueIndex"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	KeyHash   string    `json:"-"`
	KeyDigest string    `json:"-" gorm:"uniqueIndex"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ownerContext struct{}
type principalContext struct{}

func owner(db *gorm.DB) string { v, _ := db.Statement.Context.Value(ownerContext{}).(string); return v }
func settingKey(db *gorm.DB, key string) string {
	if id := owner(db); id != "" {
		return "user:" + id + ":" + key
	}
	return key
}
func (a *App) forUser(id string) *App {
	b := *a
	b.DB = a.DB.WithContext(context.WithValue(context.Background(), ownerContext{}, id))
	return &b
}
func (a *App) forRequest(r *http.Request) *App {
	u, _ := r.Context().Value(principalContext{}).(User)
	return a.forUser(u.ID)
}
func principal(r *http.Request) User { u, _ := r.Context().Value(principalContext{}).(User); return u }

// Ownership follows the DB context through every service call and transaction,
// including MCP, import previews, tag moves, and model settings.
func installOwnership(db *gorm.DB) error {
	scope := func(tx *gorm.DB) {
		id := owner(tx)
		if id == "" || tx.Statement.Schema == nil {
			return
		}
		table := tx.Statement.Schema.Table
		if table != "bookmarks" && table != "tags" {
			return
		}
		tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Eq{Column: clause.Column{Table: clause.CurrentTable, Name: "user_id"}, Value: id}}})
	}
	create := func(tx *gorm.DB) {
		id := owner(tx)
		if id == "" || tx.Statement.Schema == nil {
			return
		}
		field := tx.Statement.Schema.LookUpField("UserID")
		if field == nil || (tx.Statement.Schema.Table != "bookmarks" && tx.Statement.Schema.Table != "tags") {
			return
		}
		var assign func(reflect.Value)
		assign = func(v reflect.Value) {
			for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
				v = v.Elem()
			}
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
				for i := 0; i < v.Len(); i++ {
					assign(v.Index(i))
				}
				return
			}
			if v.Kind() == reflect.Struct {
				tx.AddError(field.Set(tx.Statement.Context, v, id))
			}
		}
		assign(tx.Statement.ReflectValue)
	}
	for _, e := range []error{
		db.Callback().Query().Before("gorm:query").Register("app:owner", scope),
		db.Callback().Update().Before("gorm:update").Register("app:owner", scope),
		db.Callback().Delete().Before("gorm:delete").Register("app:owner", scope),
		db.Callback().Create().Before("gorm:create").Register("app:owner", create),
	} {
		if e != nil {
			return e
		}
	}
	return nil
}

func (a *App) migrateUsers() error {
	var n int64
	if e := a.DB.Model(&Migration{}).Where("id = ?", 2).Count(&n).Error; e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	return a.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.AutoMigrate(&User{}); e != nil {
			return e
		}
		// The old single-user credential belongs to a regular legacy account only.
		var old Setting
		err := tx.First(&old, "key = ?", "auth").Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		legacy := err == nil
		key := os.Getenv("INITIAL_ADMIN_KEY")
		if key == "" && !legacy {
			key = os.Getenv("INITIAL_API_KEY")
		}
		if len(key) < 16 {
			return errors.New("INITIAL_ADMIN_KEY must contain at least 16 characters (required when upgrading an existing installation)")
		}
		name := os.Getenv("INITIAL_ADMIN_USERNAME")
		if name == "" {
			name = "admin"
		}
		if e := validateUsername(name); e != nil {
			return e
		}
		admin := User{ID: ID(), Username: name, Role: "superadmin", KeyHash: hashKey(key), KeyDigest: digest(key)}
		if e := tx.Create(&admin).Error; e != nil {
			return e
		}
		// Drop the former global uniqueness before creating per-user indexes.
		for _, item := range []struct {
			model any
			index string
		}{{&Bookmark{}, "idx_bookmarks_digest"}, {&Tag{}, "idx_tags_path"}} {
			if tx.Migrator().HasIndex(item.model, item.index) {
				if e := tx.Migrator().DropIndex(item.model, item.index); e != nil {
					return e
				}
			}
		}
		if e := tx.AutoMigrate(&Bookmark{}, &Tag{}, &Session{}, &Share{}); e != nil {
			return e
		}
		if legacy {
			// Prevent the same credential from authenticating both roles.
			if verify(key, old.Value) {
				return errors.New("INITIAL_ADMIN_KEY must differ from the old personal API key")
			}
			legacyName := "legacy"
			if name == legacyName {
				legacyName = "legacy-user"
			}
			u := User{ID: ID(), Username: legacyName, Role: "user", KeyHash: old.Value, KeyDigest: "legacy-" + ID()}
			if e := tx.Create(&u).Error; e != nil {
				return e
			}
			for _, model := range []any{&Bookmark{}, &Tag{}} {
				if e := tx.Model(model).Where("user_id = '' OR user_id IS NULL").Update("user_id", u.ID).Error; e != nil {
					return e
				}
			}
			var llm Setting
			if e := tx.First(&llm, "key = ?", "llm").Error; e == nil {
				if e = tx.Create(&Setting{Key: "user:" + u.ID + ":llm", Value: llm.Value}).Error; e != nil {
					return e
				}
				if e = tx.Delete(&llm).Error; e != nil {
					return e
				}
			} else if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if e := tx.Delete(&old).Error; e != nil {
				return e
			}
		}
		if e := tx.Where("1=1").Delete(&Session{}).Error; e != nil {
			return e
		}
		return tx.Create(&Migration{ID: 2}).Error
	})
}

func validateUsername(s string) error {
	if s == "" || len(s) > 80 || strings.TrimSpace(s) != s || !utf8.ValidString(s) || strings.ContainsAny(s, "\n\r\t") {
		return errors.New("username must be 1 to 80 UTF-8 bytes without surrounding whitespace")
	}
	return nil
}
func (a *App) authenticate(r *http.Request, bearerOnly bool) (User, bool) {
	var u User
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		key := strings.TrimPrefix(h, "Bearer ")
		if a.DB.First(&u, "key_digest = ? AND disabled = ?", digest(key), false).Error == nil {
			return u, verify(key, u.KeyHash)
		}
		// A migrated Argon2 credential has no recoverable digest until first login.
		var candidates []User
		if a.DB.Where("key_digest LIKE ? AND disabled = ?", "legacy-%", false).Find(&candidates).Error != nil {
			return u, false
		}
		for _, candidate := range candidates {
			if verify(key, candidate.KeyHash) {
				return candidate, true
			}
		}
		return u, false
	}
	if bearerOnly {
		return u, false
	}
	c, e := r.Cookie("session")
	if e != nil {
		return u, false
	}
	var s Session
	if a.DB.First(&s, "id = ? AND expires > ?", digest(c.Value), time.Now()).Error != nil {
		return u, false
	}
	e = a.DB.First(&u, "id = ? AND disabled = ?", s.UserID, false).Error
	return u, e == nil
}

// Indexed digests decide uniqueness except for migrated Argon2-only keys.
func keyAvailable(tx *gorm.DB, key, exceptID string) error {
	var count int64
	if e := tx.Model(&User{}).Where("key_digest = ? AND id <> ?", digest(key), exceptID).Count(&count).Error; e != nil {
		return e
	}
	if count > 0 {
		return errors.New("duplicate API key")
	}
	var legacy []User
	if e := tx.Where("key_digest LIKE ? AND id <> ?", "legacy-%", exceptID).Find(&legacy).Error; e != nil {
		return e
	}
	for _, u := range legacy {
		if verify(key, u.KeyHash) {
			return errors.New("duplicate API key")
		}
	}
	return nil
}

type UserInput struct {
	Username string `json:"username"`
	APIKey   string `json:"api_key"`
	Disabled bool   `json:"disabled"`
}

func (a *App) userRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		var users = []User{}
		db := a.DB.Where("role = ?", "user").Order("username")
		if q := r.URL.Query().Get("q"); q != "" {
			db = db.Where("username LIKE ?", "%"+q+"%")
		}
		e := db.Find(&users).Error
		respond(w, users, e)
	})
	mux.HandleFunc("GET /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		var u User
		e := a.DB.First(&u, "id = ? AND role = ?", r.PathValue("id"), "user").Error
		respond(w, u, e)
	})
	for _, method := range []string{"POST /api/v1/admin/users", "PUT /api/v1/admin/users/{id}"} {
		mux.HandleFunc(method, func(w http.ResponseWriter, r *http.Request) {
			var in UserInput
			e := decode(w, r, &in)
			var u User
			if e == nil {
				e = validateUsername(in.Username)
			}
			if e == nil && (len(in.APIKey) > 0 && len(in.APIKey) < 16 || r.Method == "POST" && len(in.APIKey) < 16) {
				e = errors.New("API key must contain at least 16 characters")
			}
			if e == nil {
				e = a.DB.Transaction(func(tx *gorm.DB) error {
					if r.Method == "PUT" {
						if e := tx.First(&u, "id = ? AND role = ?", r.PathValue("id"), "user").Error; e != nil {
							return e
						}
					} else {
						u = User{ID: ID(), Role: "user"}
					}
					u.Username = in.Username
					u.Disabled = in.Disabled
					if in.APIKey != "" {
						if e := keyAvailable(tx, in.APIKey, u.ID); e != nil {
							return e
						}
						u.KeyHash = hashKey(in.APIKey)
						u.KeyDigest = digest(in.APIKey)
					}
					if e := tx.Save(&u).Error; e != nil {
						return e
					}
					return tx.Where("user_id = ?", u.ID).Delete(&Session{}).Error
				})
			}
			respond(w, u, e)
		})
	}
	mux.HandleFunc("DELETE /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		e := a.DB.Transaction(func(tx *gorm.DB) error {
			var u User
			if e := tx.First(&u, "id = ? AND role = ?", r.PathValue("id"), "user").Error; e != nil {
				return e
			}
			for _, model := range []any{&Bookmark{}, &Tag{}, &Session{}, &Share{}} {
				if e := tx.Where("user_id = ?", u.ID).Delete(model).Error; e != nil {
					return e
				}
			}
			if e := tx.Where("key LIKE ?", "user:"+u.ID+":%").Delete(&Setting{}).Error; e != nil {
				return e
			}
			return tx.Delete(&u).Error
		})
		respond(w, map[string]bool{"ok": e == nil}, e)
	})
}
