package app

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

const Archived = "已归档"

type DuplicateError struct{ ID string }

func (e *DuplicateError) Error() string { return "duplicate raw" }

func normalize(tags []string, score float64, merge bool) ([]string, error) {
	set := map[string]bool{}
	for _, t := range tags {
		if t == Archived {
			continue
		}
		if !utf8.ValidString(t) || len(t) > 1024 || strings.ContainsAny(t, "\x00\r\n") {
			return nil, errors.New("invalid tag")
		}
		for _, p := range strings.Split(t, "/") {
			if p == "" || strings.TrimSpace(p) != p || p == Archived {
				return nil, errors.New("invalid tag path")
			}
		}
		set[t] = true
	}
	// A segment trie detects ancestor conflicts without comparing tag pairs.
	// Names are interpreted only within their parent; punctuation is literal.
	type branch struct {
		selected bool
		children map[string]*branch
	}
	root := &branch{children: map[string]*branch{}}
	for t := range set {
		node := root
		for _, part := range strings.Split(t, "/") {
			if node.selected && !merge {
				return nil, errors.New("ancestor and descendant tags conflict")
			}
			next := node.children[part]
			if next == nil {
				next = &branch{children: map[string]*branch{}}
				node.children[part] = next
			}
			node = next
		}
		if len(node.children) > 0 && !merge {
			return nil, errors.New("ancestor and descendant tags conflict")
		}
		node.selected = true
	}
	out := []string{}
	var visit func(*branch, []string)
	visit = func(node *branch, path []string) {
		if len(node.children) == 0 && node.selected {
			out = append(out, strings.Join(path, "/"))
			return
		}
		for name, child := range node.children {
			visit(child, append(path, name))
		}
	}
	visit(root, nil)
	sort.Strings(out)
	if score == 0 {
		out = append(out, Archived)
	}
	return out, nil
}
func ensureTag(tx *gorm.DB, path string) error {
	if path == Archived {
		return errors.New("reserved system tag")
	}
	parent := ""
	parts := strings.Split(path, "/")
	for i, name := range parts {
		p := strings.Join(parts[:i+1], "/")
		var t Tag
		e := tx.First(&t, "path = ?", p).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			t = Tag{ID: ID(), Path: p, Name: name, ParentID: parent}
			if e = tx.Create(&t).Error; e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		parent = t.ID
	}
	return nil
}
func saveBookmark(tx *gorm.DB, b *Bookmark, createTags, merge bool) error {
	if strings.TrimSpace(b.Raw) == "" || !utf8.ValidString(b.Raw) || len(b.Raw) > 1048576 {
		return errors.New("raw must be nonempty UTF-8, at most 1 MiB")
	}
	if math.IsNaN(b.Score) || math.IsInf(b.Score, 0) || b.Score < 0 || b.Score > 1 {
		return errors.New("score must be between 0 and 1")
	}
	var e error
	b.Tags, e = normalize(b.Tags, b.Score, merge)
	if e != nil {
		return e
	}
	for _, t := range b.Tags {
		if t == Archived {
			continue
		}
		if createTags {
			if e = ensureTag(tx, t); e != nil {
				return e
			}
		} else {
			var n int64
			tx.Model(&Tag{}).Where("path = ?", t).Count(&n)
			if n == 0 {
				return fmt.Errorf("unknown tag: %s", t)
			}
		}
	}
	b.Digest = digest(b.Raw)
	var duplicate Bookmark
	e = tx.First(&duplicate, "digest = ? AND id <> ?", b.Digest, b.ID).Error
	if e == nil {
		if duplicate.Raw != b.Raw {
			return errors.New("digest collision")
		}
		return &DuplicateError{ID: duplicate.ID}
	}
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return e
	}
	return tx.Save(b).Error
}

type BookmarkInput struct {
	Raw         string   `json:"raw"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description,omitempty"`
	Score       *float64 `json:"score,omitempty"`
	Version     uint     `json:"version,omitempty"`
	CreateTags  bool     `json:"create_tags,omitempty"`
}

func (a *App) Write(id string, in BookmarkInput) (Bookmark, error) {
	b := Bookmark{ID: id}
	e := a.DB.Transaction(func(tx *gorm.DB) error {
		if id != "" {
			if e := tx.First(&b, "id = ?", id).Error; e != nil {
				return e
			}
			if in.Version != b.Version {
				return errors.New("version conflict")
			}
		} else {
			b.ID = ID()
			b.Score = .5
		}
		b.Raw = in.Raw
		b.Tags = in.Tags
		b.Description = in.Description
		if in.Score != nil {
			b.Score = *in.Score
		}
		b.Version++
		if id != "" {
			r := tx.Model(&Bookmark{}).Where("id = ? AND version = ?", id, in.Version).Update("version", b.Version)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return errors.New("version conflict")
			}
		}
		return saveBookmark(tx, &b, in.CreateTags, false)
	})
	return b, e
}

type Query struct {
	Keyword      string   `json:"keyword,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Archived     bool     `json:"archived,omitempty"`
	Unclassified bool     `json:"unclassified,omitempty"`
	Page         int      `json:"page,omitempty"`
	Limit        int      `json:"limit,omitempty"`
}

func (a *App) List(q Query) (map[string]any, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 30
	}
	var all []Bookmark
	db := a.DB.Order("score desc, updated_at desc, id asc")
	if q.Archived {
		db = db.Where("score = 0")
	} else {
		db = db.Where("score > 0")
	}
	if e := db.Find(&all).Error; e != nil {
		return nil, e
	}
	filtered := []Bookmark{}
	for _, b := range all {
		match := true
		if q.Keyword != "" {
			match = strings.Contains(strings.ToLower(b.Raw+"\n"+b.Description+"\n"+strings.Join(b.Tags, "\n")), strings.ToLower(q.Keyword))
		}
		if q.Unclassified {
			count := 0
			for _, t := range b.Tags {
				if t != Archived {
					count++
				}
			}
			match = match && count == 0
		}
		for _, t := range q.Tags {
			found := false
			for _, v := range b.Tags {
				if v == t || strings.HasPrefix(v, t+"/") {
					found = true
				}
			}
			match = match && found
		}
		if match {
			filtered = append(filtered, b)
		}
	}
	total := len(filtered)
	start := (q.Page - 1) * q.Limit
	if start > total {
		start = total
	}
	end := start + q.Limit
	if end > total {
		end = total
	}
	return map[string]any{"items": filtered[start:end], "total": total, "page": q.Page}, nil
}
func (a *App) TagChange(id, path string, remove bool) error {
	return a.DB.Transaction(func(tx *gorm.DB) error {
		var old Tag
		if e := tx.First(&old, "id = ?", id).Error; e != nil {
			return e
		}
		if path == old.Path && !remove {
			return nil
		}
		if strings.HasPrefix(path, old.Path+"/") {
			return errors.New("cannot move a node below itself")
		}
		if !remove {
			if path == Archived {
				return errors.New("reserved system tag")
			}
			if _, e := normalize([]string{path}, .5, false); e != nil {
				return e
			}
			var n int64
			tx.Model(&Tag{}).Where("path = ?", path).Count(&n)
			if n > 0 {
				return errors.New("target already exists")
			}
		}
		var tags []Tag
		tx.Find(&tags)
		for _, t := range tags {
			if t.Path == old.Path || strings.HasPrefix(t.Path, old.Path+"/") {
				if e := tx.Delete(&t).Error; e != nil {
					return e
				}
			}
		}
		if !remove {
			sort.Slice(tags, func(i, j int) bool {
				return len(strings.Split(tags[i].Path, "/")) < len(strings.Split(tags[j].Path, "/"))
			})
			parentPath := strings.Join(strings.Split(path, "/")[:len(strings.Split(path, "/"))-1], "/")
			if parentPath != "" {
				if e := ensureTag(tx, parentPath); e != nil {
					return e
				}
			}
			for _, t := range tags {
				if t.Path == old.Path || strings.HasPrefix(t.Path, old.Path+"/") {
					t.Path = path + strings.TrimPrefix(t.Path, old.Path)
					parts := strings.Split(t.Path, "/")
					t.Name = parts[len(parts)-1]
					t.ParentID = ""
					if len(parts) > 1 {
						var parent Tag
						if e := tx.First(&parent, "path = ?", strings.Join(parts[:len(parts)-1], "/")).Error; e != nil {
							return e
						}
						t.ParentID = parent.ID
					}
					if e := tx.Create(&t).Error; e != nil {
						return e
					}
				}
			}
		}
		var bs []Bookmark
		tx.Find(&bs)
		for _, b := range bs {
			next := []string{}
			changed := false
			for _, t := range b.Tags {
				if t == old.Path || strings.HasPrefix(t, old.Path+"/") {
					changed = true
					if !remove {
						next = append(next, path+strings.TrimPrefix(t, old.Path))
					}
				} else {
					next = append(next, t)
				}
			}
			if changed {
				b.Tags = next
				b.Version++
				if e := saveBookmark(tx, &b, false, true); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func dbRevision(tx *gorm.DB) string {
	var bs []Bookmark
	var ts []Tag
	tx.Order("id").Find(&bs)
	tx.Order("id").Find(&ts)
	s := ""
	for _, b := range bs {
		s += fmt.Sprintf("%s:%d:%d;", b.ID, b.Version, b.UpdatedAt.UnixNano())
	}
	for _, t := range ts {
		s += t.ID + ":" + t.Path + ";"
	}
	return digest(s)
}
