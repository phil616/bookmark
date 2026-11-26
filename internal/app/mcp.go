package app

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
	"net/http"
)

type IDInput struct {
	ID string `json:"id"`
}
type UpdateInput struct {
	ID string `json:"id"`
	BookmarkInput
}
type TagInput struct {
	Path string `json:"path"`
}
type TagUpdate struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func (a *App) MCP() http.Handler {
	s := mcp.NewServer(&mcp.Implementation{Name: "online-bookmarks", Version: Version}, &mcp.ServerOptions{Instructions: "先读取 bookmarks://analysis-spec/v1 或 analyze_bookmark_v1 prompt。" + AnalysisPrompt})
	read := &mcp.ToolAnnotations{ReadOnlyHint: true}
	destructive := true
	closed := false
	additive := false
	write := &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed}
	add := &mcp.ToolAnnotations{DestructiveHint: &additive, OpenWorldHint: &closed}
	read.OpenWorldHint = &closed
	mcp.AddTool(s, &mcp.Tool{Name: "bookmarks_list", Description: "分页查询收藏，默认排除归档，按 score 降序；tags 为 AND，包含后代。", Annotations: read}, func(ctx context.Context, r *mcp.CallToolRequest, in Query) (*mcp.CallToolResult, map[string]any, error) {
		v, e := a.List(in)
		return nil, v, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "bookmarks_get", Description: "读取四元组及更新所需 version。", Annotations: read}, func(ctx context.Context, r *mcp.CallToolRequest, in IDInput) (*mcp.CallToolResult, Bookmark, error) {
		var b Bookmark
		e := a.DB.First(&b, "id = ?", in.ID).Error
		return nil, b, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "bookmarks_create", Annotations: add, Description: AnalysisPrompt + " create_tags 默认 false；新分类先调用 tags_create。"}, func(ctx context.Context, r *mcp.CallToolRequest, in BookmarkInput) (*mcp.CallToolResult, Bookmark, error) {
		in.CreateTags = false
		b, e := a.Write("", in)
		return nil, b, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "bookmarks_update", Description: "完整替换四元组，必须携带当前 version；" + AnalysisPrompt, Annotations: write}, func(ctx context.Context, r *mcp.CallToolRequest, in UpdateInput) (*mcp.CallToolResult, Bookmark, error) {
		in.CreateTags = false
		b, e := a.Write(in.ID, in.BookmarkInput)
		return nil, b, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "bookmarks_delete", Description: "永久删除收藏。", Annotations: write}, func(ctx context.Context, r *mcp.CallToolRequest, in IDInput) (*mcp.CallToolResult, map[string]bool, error) {
		e := a.DB.Delete(&Bookmark{}, "id = ?", in.ID).Error
		return nil, map[string]bool{"ok": e == nil}, e
	})
	for _, action := range []string{"archive", "restore"} {
		action := action
		mcp.AddTool(s, &mcp.Tool{Name: "bookmarks_" + action, Description: "archive 设置评分为 0；restore 设置为 0.5。", Annotations: write}, func(ctx context.Context, r *mcp.CallToolRequest, in IDInput) (*mcp.CallToolResult, map[string]bool, error) {
			e := a.Batch([]string{in.ID}, action)
			return nil, map[string]bool{"ok": e == nil}, e
		})
	}
	mcp.AddTool(s, &mcp.Tool{Name: "tags_list", Description: "读取分类树，path 使用 / 分层。", Annotations: read}, func(ctx context.Context, r *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, map[string]any, error) {
		var ts []Tag
		e := a.DB.Order("path").Find(&ts).Error
		return nil, map[string]any{"items": ts}, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tags_create", Annotations: add, Description: "创建已获用户认可的新分类路径及缺失的父节点。"}, func(ctx context.Context, r *mcp.CallToolRequest, in TagInput) (*mcp.CallToolResult, map[string]bool, error) {
		_, e := normalize([]string{in.Path}, .5, false)
		if e == nil {
			e = a.DB.Transaction(func(tx *gorm.DB) error { return ensureTag(tx, in.Path) })
		}
		return nil, map[string]bool{"ok": e == nil}, e
	})
	for _, name := range []string{"tags_rename", "tags_move"} {
		mcp.AddTool(s, &mcp.Tool{Name: name, Description: "提供目标完整 path，更新子树和收藏标签。", Annotations: write}, func(ctx context.Context, r *mcp.CallToolRequest, in TagUpdate) (*mcp.CallToolResult, map[string]bool, error) {
			e := a.TagChange(in.ID, in.Path, false)
			return nil, map[string]bool{"ok": e == nil}, e
		})
	}
	mcp.AddTool(s, &mcp.Tool{Name: "tags_delete", Description: "删除分类子树及收藏标签关联，保留收藏。", Annotations: write}, func(ctx context.Context, r *mcp.CallToolRequest, in IDInput) (*mcp.CallToolResult, map[string]bool, error) {
		e := a.TagChange(in.ID, "", true)
		return nil, map[string]bool{"ok": e == nil}, e
	})
	s.AddResource(&mcp.Resource{URI: "bookmarks://analysis-spec/v1", Name: "收藏分析规范 v1", MIMEType: "text/plain"}, func(ctx context.Context, r *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "bookmarks://analysis-spec/v1", MIMEType: "text/plain", Text: AnalysisPrompt}}}, nil
	})
	s.AddPrompt(&mcp.Prompt{Name: "analyze_bookmark_v1", Description: "收藏字段定义与分类分析提示词"}, func(ctx context.Context, r *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: AnalysisPrompt}}}}, nil
	})
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{Stateless: true})
}
