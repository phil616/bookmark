package app

import (
	"encoding/base64"
	"errors"
	"gorm.io/gorm"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Record struct {
	Raw         string   `json:"raw_base64"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
	Score       *float64 `json:"score,omitempty"`
}
type Bundle struct {
	Schema    int      `json:"schema_version"`
	Tags      []string `json:"tags"`
	Bookmarks []Record `json:"bookmarks"`
}
type ImportRequest struct {
	Data     Bundle `json:"data"`
	Revision string `json:"revision,omitempty"`
}

func (a *App) Export() (Bundle, error) {
	out := Bundle{Schema: 1, Tags: []string{}, Bookmarks: []Record{}}
	var bs []Bookmark
	var ts []Tag
	if e := a.DB.Order("id").Find(&bs).Error; e != nil {
		return out, e
	}
	a.DB.Order("path").Find(&ts)
	for _, t := range ts {
		out.Tags = append(out.Tags, t.Path)
	}
	for _, b := range bs {
		s := b.Score
		out.Bookmarks = append(out.Bookmarks, Record{base64.StdEncoding.EncodeToString([]byte(b.Raw)), b.Tags, b.Description, &s})
	}
	return out, nil
}
func mergeDescription(old, next string) string {
	if old == next || next == "" {
		return old
	}
	if old == "" {
		return next
	}
	const separator = "\n\n---\n\n"
	parts := strings.Split(old, separator)
	seen := map[string]bool{}
	for _, p := range parts {
		seen[p] = true
	}
	for _, p := range strings.Split(next, separator) {
		if !seen[p] && p != "" {
			parts = append(parts, p)
			seen[p] = true
		}
	}
	return strings.Join(parts, separator)
}

func importInto(tx *gorm.DB, b Bundle) (map[string]int, error) {
	counts := map[string]int{"created": 0, "merged": 0}
	if b.Schema != 1 {
		return counts, errors.New("unsupported schema_version")
	}
	for _, t := range b.Tags {
		if t == Archived {
			continue
		}
		if _, e := normalize([]string{t}, .5, false); e != nil {
			return counts, e
		}
		if e := ensureTag(tx, t); e != nil {
			return counts, e
		}
	}
	for _, r := range b.Bookmarks {
		raw, e := base64.StdEncoding.Strict().DecodeString(r.Raw)
		if e != nil || !utf8.Valid(raw) {
			return counts, errors.New("invalid Base64 or UTF-8")
		}
		var item Bookmark
		e = tx.First(&item, "digest = ?", digest(string(raw))).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			item = Bookmark{ID: ID(), Raw: string(raw)}
			counts["created"]++
		} else if e != nil {
			return counts, e
		} else {
			if item.Raw != string(raw) {
				return counts, errors.New("digest collision")
			}
			counts["merged"]++
		}
		item.Tags = append(item.Tags, r.Tags...)
		item.Description = mergeDescription(item.Description, r.Description)
		item.Score = .5
		if r.Score != nil {
			item.Score = *r.Score
		}
		item.Version++
		if e = saveBookmark(tx, &item, true, true); e != nil {
			return counts, e
		}
	}
	return counts, nil
}
func (a *App) transferRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/exports", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		b, e := a.Export()
		w.Header().Set("Content-Disposition", "attachment; filename=online-bookmarks.json")
		respond(w, b, e)
	})
	mux.HandleFunc("POST /api/v1/imports/preview", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		var in ImportRequest
		e := decode(w, r, &in)
		var counts map[string]int
		revision := ""
		if e == nil {
			tx := a.DB.Begin()
			if tx.Error != nil {
				respond(w, nil, tx.Error)
				return
			}
			defer tx.Rollback()
			revision = dbRevision(tx)
			counts, e = importInto(tx, in.Data)
		}
		respond(w, map[string]any{"counts": counts, "revision": revision}, e)
	})
	mux.HandleFunc("POST /api/v1/imports/commit", func(w http.ResponseWriter, r *http.Request) {
		a := a.forRequest(r)
		var in ImportRequest
		e := decode(w, r, &in)
		var counts map[string]int
		if e == nil {
			e = a.DB.Transaction(func(tx *gorm.DB) error {
				if in.Revision == "" || in.Revision != dbRevision(tx) {
					return errors.New("database revision conflict; preview again")
				}
				var err error
				counts, err = importInto(tx, in.Data)
				return err
			})
		}
		respond(w, counts, e)
	})
}
