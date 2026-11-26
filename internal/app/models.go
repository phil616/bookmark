package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"time"
)

type Bookmark struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	UserID      string    `json:"-" gorm:"uniqueIndex:owner_digest;index"`
	Raw         string    `json:"raw"`
	Digest      string    `json:"-" gorm:"uniqueIndex:owner_digest"`
	TagsJSON    string    `json:"-"`
	Tags        []string  `json:"tags" gorm:"-"`
	Description string    `json:"description"`
	Score       float64   `json:"score"`
	Version     uint      `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (b *Bookmark) AfterFind(*gorm.DB) error { return json.Unmarshal([]byte(b.TagsJSON), &b.Tags) }
func (b *Bookmark) BeforeSave(*gorm.DB) error {
	p, e := json.Marshal(b.Tags)
	b.TagsJSON = string(p)
	return e
}

type Tag struct {
	ID       string `json:"id" gorm:"primaryKey"`
	UserID   string `json:"-" gorm:"uniqueIndex:owner_path;index"`
	Path     string `json:"path" gorm:"uniqueIndex:owner_path"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
}
type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value string
}
type Session struct {
	ID      string `gorm:"primaryKey"`
	UserID  string `gorm:"index"`
	Expires time.Time
}
type Migration struct {
	ID uint `gorm:"primaryKey"`
}
type App struct {
	DB      *gorm.DB
	Secret  []byte
	Origins map[string]bool
	Secure  bool
}

func ID() string {
	p := make([]byte, 16)
	if _, e := rand.Read(p); e != nil {
		panic(e)
	}
	return hex.EncodeToString(p)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func Open() (*App, error) {
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir = "data"
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	var db *gorm.DB
	var e error
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		db, e = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	} else {
		db, e = gorm.Open(sqlite.Open(filepath.Join(dir, "bookmarks.db")+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
		if e == nil {
			s, _ := db.DB()
			s.SetMaxOpenConns(1)
		}
	}
	if e != nil {
		return nil, e
	}
	// Ordered, transactional initial migration. Future migrations append new IDs.
	if e = db.AutoMigrate(&Migration{}); e != nil {
		return nil, e
	}
	var n int64
	db.Model(&Migration{}).Where("id = ?", 1).Count(&n)
	if n == 0 {
		e = db.Transaction(func(tx *gorm.DB) error {
			if e := tx.AutoMigrate(&Bookmark{}, &Tag{}, &Setting{}, &Session{}); e != nil {
				return e
			}
			return tx.Create(&Migration{ID: 1}).Error
		})
		if e != nil {
			return nil, e
		}
	}
	keyPath := filepath.Join(dir, "master.key")
	key, e := os.ReadFile(keyPath)
	if os.IsNotExist(e) {
		key = make([]byte, 32)
		if _, e = rand.Read(key); e != nil {
			return nil, e
		}
		e = os.WriteFile(keyPath, key, 0600)
	}
	if e != nil {
		return nil, e
	}
	if len(key) != 32 {
		return nil, errors.New("master.key must contain 32 bytes")
	}
	a := &App{DB: db, Secret: key, Origins: map[string]bool{}, Secure: os.Getenv("COOKIE_SECURE") == "true"}
	if e = a.migrateUsers(); e != nil {
		return nil, e
	}
	if e = installOwnership(db); e != nil {
		return nil, e
	}
	return a, e
}
func (a *App) get(key string) string {
	var s Setting
	a.DB.First(&s, "key = ?", settingKey(a.DB, key))
	return s.Value
}
func put(tx *gorm.DB, key, value string) error {
	return tx.Save(&Setting{Key: settingKey(tx, key), Value: value}).Error
}
func (a *App) encrypt(s string) (string, error) {
	b, e := aes.NewCipher(a.Secret)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return "", e
	}
	n := make([]byte, g.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(g.Seal(n, n, []byte(s), nil)), nil
}
func (a *App) decrypt(s string) (string, error) {
	p, e := base64.StdEncoding.DecodeString(s)
	if e != nil {
		return "", e
	}
	b, _ := aes.NewCipher(a.Secret)
	g, _ := cipher.NewGCM(b)
	if len(p) < g.NonceSize() {
		return "", fmt.Errorf("invalid encrypted secret")
	}
	v, e := g.Open(nil, p[:g.NonceSize()], p[g.NonceSize():], nil)
	return string(v), e
}
