package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Shares are immutable snapshots. Possessing a token grants read access only to
// authenticated regular users. The owner's later edits never widen that grant.
type Share struct {
	ID            string    `json:"id" gorm:"primaryKey"`
	UserID        string    `json:"-" gorm:"index"`
	TokenHash     string    `json:"-" gorm:"uniqueIndex"`
	Kind          string    `json:"kind"`
	Label         string    `json:"label"`
	Snapshot      string    `json:"-"`
	BookmarkCount int       `json:"bookmark_count"`
	TagCount      int       `json:"tag_count"`
	Revoked       bool      `json:"revoked"`
	ExpiresAt     time.Time `json:"expires_at"`
	CreatedAt     time.Time `json:"created_at"`
}
type ShareInput struct {
	Kind        string `json:"kind"`
	BookmarkID  string `json:"bookmark_id"`
	TagID       string `json:"tag_id"`
	ExpiresDays int    `json:"expires_days"`
}
type ShareImport struct {
	TargetPath string `json:"target_path"`
	Revision   string `json:"revision,omitempty"`
}

func beneath(path, root string) bool { return path == root || strings.HasPrefix(path, root+"/") }

func (a *App) createShare(in ShareInput) (Share, string, error) {
	s := Share{ID: ID(), UserID: owner(a.DB), Kind: in.Kind}
	token := ID() + ID()
	s.TokenHash = digest(token)
	if in.ExpiresDays == 0 {
		in.ExpiresDays = 7
	}
	if in.ExpiresDays < 1 || in.ExpiresDays > 365 {
		return s, "", errors.New("expiry must be 1 to 365 days")
	}
	s.ExpiresAt = time.Now().Add(time.Duration(in.ExpiresDays) * 24 * time.Hour)
	e := a.DB.Transaction(func(tx *gorm.DB) error {
		bundle := Bundle{Schema: 1, Tags: []string{}, Bookmarks: []Record{}}
		var bs []Bookmark
		paths := map[string]bool{}
		root, prefix := "", ""
		switch in.Kind {
		case "bookmark":
			if in.BookmarkID == "" || in.TagID != "" {
				return errors.New("bookmark_id required")
			}
			var b Bookmark
			if e := tx.First(&b, "id = ?", in.BookmarkID).Error; e != nil {
				return e
			}
			bs = append(bs, b)
			s.Label = "单条收藏"
		case "tag":
			if in.TagID == "" || in.BookmarkID != "" {
				return errors.New("tag_id required")
			}
			var t Tag
			if e := tx.First(&t, "id = ?", in.TagID).Error; e != nil {
				return e
			}
			root = t.Path
			s.Label = t.Name
			if i := strings.LastIndex(root, "/"); i >= 0 {
				prefix = root[:i+1]
			}
			var ts []Tag
			if e := tx.Order("path").Find(&ts).Error; e != nil {
				return e
			}
			for _, t := range ts {
				if beneath(t.Path, root) {
					paths[strings.TrimPrefix(t.Path, prefix)] = true
				}
			}
			var all []Bookmark
			if e := tx.Order("id").Find(&all).Error; e != nil {
				return e
			}
			for _, b := range all {
				for _, p := range b.Tags {
					if beneath(p, root) {
						bs = append(bs, b)
						break
					}
				}
			}
		default:
			return errors.New("kind must be bookmark or tag")
		}
		for _, b := range bs {
			tags := []string{}
			for _, p := range b.Tags {
				if p == Archived || root != "" && !beneath(p, root) {
					continue
				}
				p = strings.TrimPrefix(p, prefix)
				tags = append(tags, p)
				parts := strings.Split(p, "/")
				for i := range parts {
					paths[strings.Join(parts[:i+1], "/")] = true
				}
			}
			score := b.Score
			bundle.Bookmarks = append(bundle.Bookmarks, Record{Raw: base64.StdEncoding.EncodeToString([]byte(b.Raw)), Tags: tags, Description: b.Description, Score: &score})
		}
		for p := range paths {
			bundle.Tags = append(bundle.Tags, p)
		}
		sort.Strings(bundle.Tags)
		data, e := json.Marshal(bundle)
		if e != nil {
			return e
		}
		if len(data) > 16<<20 {
			return errors.New("share snapshot exceeds 16 MiB")
		}
		s.Snapshot = string(data)
		s.BookmarkCount = len(bundle.Bookmarks)
		s.TagCount = len(bundle.Tags)
		return tx.Create(&s).Error
	})
	return s, token, e
}
func (a *App) readShare(token string) (Share, Bundle, error) {
	var s Share
	var b Bundle
	// Disabled/deleted owners immediately invalidate links, even before revocation.
	e := a.DB.Where("token_hash = ? AND revoked = ? AND expires_at > ?", digest(token), false, time.Now()).First(&s).Error
	if e != nil {
		return s, b, e
	}
	var u User
	if e = a.DB.First(&u, "id = ? AND role = ? AND disabled = ?", s.UserID, "user", false).Error; e != nil {
		return s, b, e
	}
	e = json.Unmarshal([]byte(s.Snapshot), &b)
	return s, b, e
}
func mapShared(tx *gorm.DB, b Bundle, target string) (Bundle, error) {
	if target != "" {
		if _, e := normalize([]string{target}, .5, false); e != nil {
			return b, e
		}
		if target == Archived {
			return b, errors.New("reserved target")
		}
		var t Tag
		if e := tx.First(&t, "path = ?", target).Error; e != nil {
			return b, e
		}
	}
	mapped := Bundle{Schema: 1, Tags: []string{}, Bookmarks: []Record{}}
	mapPath := func(p string) string {
		if target == "" {
			return p
		}
		return target + "/" + p
	}
	for _, p := range b.Tags {
		mapped.Tags = append(mapped.Tags, mapPath(p))
	}
	for _, r := range b.Bookmarks {
		tags := []string{}
		for _, p := range r.Tags {
			tags = append(tags, mapPath(p))
		}
		if len(tags) == 0 && target != "" {
			tags = append(tags, target)
		}
		r.Tags = tags
		mapped.Bookmarks = append(mapped.Bookmarks, r)
	}
	return mapped, nil
}
func importSharedInto(tx *gorm.DB, b Bundle) (map[string]int, error) {
	counts := map[string]int{"created": 0, "merged": 0, "unchanged": 0, "tags_created": 0, "tags_reused": 0}
	var before []Tag
	if e := tx.Find(&before).Error; e != nil {
		return counts, e
	}
	existing := map[string]bool{}
	for _, t := range before {
		existing[t.Path] = true
	}
	allPaths := map[string]bool{}
	for _, p := range b.Tags {
		if p == Archived {
			return counts, errors.New("reserved tag")
		}
		if _, e := normalize([]string{p}, .5, false); e != nil {
			return counts, e
		}
		if e := ensureTag(tx, p); e != nil {
			return counts, e
		}
		parts := strings.Split(p, "/")
		for i := range parts {
			allPaths[strings.Join(parts[:i+1], "/")] = true
		}
	}
	for p := range allPaths {
		if existing[p] {
			counts["tags_reused"]++
		} else {
			counts["tags_created"]++
		}
	}
	for _, r := range b.Bookmarks {
		raw, e := base64.StdEncoding.Strict().DecodeString(r.Raw)
		if e != nil {
			return counts, e
		}
		var item Bookmark
		e = tx.First(&item, "digest = ?", digest(string(raw))).Error
		fresh := errors.Is(e, gorm.ErrRecordNotFound)
		if e != nil && !fresh {
			return counts, e
		}
		if fresh {
			item = Bookmark{ID: ID(), Raw: string(raw), Score: .5}
			if r.Score != nil {
				item.Score = *r.Score
			}
		} else if item.Raw != string(raw) {
			return counts, errors.New("digest collision")
		}
		tags, e := normalize(append(append([]string{}, item.Tags...), r.Tags...), item.Score, true)
		if e != nil {
			return counts, e
		}
		description := mergeDescription(item.Description, r.Description)
		if !fresh && strings.Join(tags, "\x00") == strings.Join(item.Tags, "\x00") && description == item.Description {
			counts["unchanged"]++
			continue
		}
		item.Tags = tags
		item.Description = description
		item.Version++
		if e = saveBookmark(tx, &item, true, true); e != nil {
			return counts, e
		}
		if fresh {
			counts["created"]++
		} else {
			counts["merged"]++
		}
	}
	return counts, nil
}
func shareRevision(tx *gorm.DB, s Share, target string) string {
	return digest(s.ID + ":" + digest(s.Snapshot) + ":" + target + ":" + dbRevision(tx))
}
func (a *App) shareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/shares", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		var in ShareInput
		e := decode(w, r, &in)
		var s Share
		var token string
		if e == nil {
			s, token, e = a.createShare(in)
		}
		respond(w, map[string]any{"share": s, "path": "/share/" + token}, e)
	})
	mux.HandleFunc("GET /api/v1/shares", func(w http.ResponseWriter, r *http.Request) {
		var shares = []Share{}
		e := a.DB.Where("user_id = ?", principal(r).ID).Order("created_at desc").Find(&shares).Error
		respond(w, shares, e)
	})
	mux.HandleFunc("DELETE /api/v1/shares/{id}", func(w http.ResponseWriter, r *http.Request) {
		res := a.DB.Model(&Share{}).Where("id = ? AND user_id = ?", r.PathValue("id"), principal(r).ID).Update("revoked", true)
		e := res.Error
		if e == nil && res.RowsAffected == 0 {
			e = gorm.ErrRecordNotFound
		}
		respond(w, map[string]bool{"ok": e == nil}, e)
	})
	mux.HandleFunc("GET /api/v1/shares/{token}", func(w http.ResponseWriter, r *http.Request) {
		s, b, e := a.readShare(r.PathValue("token"))
		respond(w, map[string]any{"share": s, "data": b}, e)
	})
	for _, action := range []string{"preview", "commit"} {
		mux.HandleFunc("POST /api/v1/shares/{token}/"+action, func(w http.ResponseWriter, r *http.Request) {
			a := a.forRequest(r)
			var in ShareImport
			e := decode(w, r, &in)
			var counts map[string]int
			revision := ""
			if e == nil {
				s, b, err := a.readShare(r.PathValue("token"))
				e = err
				if e == nil && s.UserID == principal(r).ID {
					e = errors.New("cannot import your own share")
				}
				if e == nil {
					tx := a.DB.Begin()
					if tx.Error != nil {
						respond(w, nil, tx.Error)
						return
					}
					defer tx.Rollback()
					revision = shareRevision(tx, s, in.TargetPath)
					if action == "commit" && (in.Revision == "" || in.Revision != revision) {
						e = errors.New("database revision conflict; preview again")
					}
					if e == nil {
						var mapped Bundle
						mapped, e = mapShared(tx, b, in.TargetPath)
						if e == nil {
							counts, e = importSharedInto(tx, mapped)
						}
					}
					if e == nil && action == "commit" {
						e = tx.Commit().Error
					}
				}
			}
			respond(w, map[string]any{"counts": counts, "revision": revision}, e)
		})
	}
}
