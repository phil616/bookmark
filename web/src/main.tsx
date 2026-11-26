import React, { useEffect, useState, useRef } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, useNavigate, useLocation } from "react-router-dom";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  App as AntApp,
  Button,
  Card,
  Checkbox,
  ConfigProvider,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Layout,
  Menu,
  Modal,
  Pagination,
  Dropdown,
  Tooltip,
  Slider,
  Space,
  Spin,
  Switch,
  Tag,
  Tree,
  TreeSelect,
  Typography,
  Table,
  Upload,
} from "antd";
import zhCN from "antd/locale/zh_CN";
import { get, set, del, keys } from "idb-keyval";
import {
  Bookmark as BookmarkIcon,
  Library,
  Inbox,
  Archive,
  FilePenLine,
  FolderTree,
  ArrowDownUp,
  Settings as SettingsIcon,
  LogOut,
  Plus,
  Search,
  ArrowUpRight,
  Copy,
  MoreHorizontal,
  Trash2,
  Pencil,
  Sparkles,
  LayoutGrid,
  List,
  SlidersHorizontal,
  Folder,
  Menu as MenuIcon,
  Link as LinkIcon,
  FileText,
  ArrowDownWideNarrow,
} from "lucide-react";
import "@fontsource-variable/noto-sans-sc";
import "./style.css";
import { copyText } from "./clipboard";
function Brand() {
  return (
    <div className="brand">
      <span className="brand-mark">
        <BookmarkIcon size={22} />
      </span>
      <strong>收藏管理</strong>
    </div>
  );
}
function sourceName(raw: string) {
  try {
    const u = new URL(raw.trim());
    if (["http:", "https:"].includes(u.protocol))
      return u.hostname.replace(/^www\./, "");
  } catch {}
  return "文字收藏";
}
const viewTitles: Record<string, string> = {
  bookmarks: "全部收藏",
  unclassified: "未分类",
  archived: "已归档",
  drafts: "本地草稿",
  tags: "分类管理",
  transfer: "数据迁移",
  shares: "分享管理",
  settings: "设置",
};
type Bookmark = {
  id: string;
  raw: string;
  tags: string[];
  description: string;
  score: number;
  version: number;
};
type Account = {
  id: string;
  username: string;
  role: "superadmin" | "user";
  disabled: boolean;
};
type Category = { id: string; path: string; parent_id: string; name: string };
type Draft = Partial<Bookmark> & { draftID?: string; create_tags?: boolean };
const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/+$/, "");
// API 与前端分离部署时，后端相关地址要跟着 VITE_API_BASE_URL 走。
const API_ORIGIN = API_BASE || location.origin;
let pendingRequests = new AbortController();
function cancelPendingRequests() {
  pendingRequests.abort();
  pendingRequests = new AbortController();
}
async function api(path: string, method = "GET", body?: unknown) {
  const r = await fetch(API_BASE + "/api/v1" + path, {
    method,
    signal: pendingRequests.signal,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "online-bookmarks",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!r.ok) {
    const s = await r.text();
    if (r.status === 401 && path !== "/auth/login" && path !== "/auth/session")
      window.dispatchEvent(new Event("session-expired"));
    let m = s;
    let existingID: string | undefined;
    try {
      m = JSON.parse(s).message;
      existingID = JSON.parse(s).existing_id;
    } catch {}
    throw Object.assign(new Error(m || `HTTP ${r.status}`), { existingID });
  }
  return r.json();
}
const qc = new QueryClient();
function treeData(tags: Category[]) {
  const nodes = new Map<string, any>();
  tags.forEach((t) =>
    nodes.set(t.path, {
      title: t.name,
      value: t.path,
      key: t.path,
      children: [],
    }),
  );
  tags.forEach((t) => {
    const parent = t.path.split("/").slice(0, -1).join("/");
    if (nodes.has(parent)) nodes.get(parent).children.push(nodes.get(t.path));
  });
  return tags.filter((t) => !t.parent_id).map((t) => nodes.get(t.path));
}
function CopyButton({
  text,
  label = "复制原文",
}: {
  text: string;
  label?: string;
}) {
  const { message, modal } = AntApp.useApp();
  return (
    <Button
      className="copy-button"
      aria-label={label}
      icon={<Copy size={15} />}
      disabled={!text.length}
      onClick={async () => {
        try {
          await copyText(text);
          message.success(label === "复制原文" ? "原文已复制" : "已复制");
        } catch {
          modal.info({
            title: "请手动复制",
            content: (
              <Input.TextArea
                aria-label="待复制内容"
                readOnly
                value={text}
                autoSize={{ minRows: 3, maxRows: 10 }}
                autoFocus
                onFocus={(e) => e.currentTarget.select()}
                onClick={(e) => e.currentTarget.select()}
              />
            ),
          });
        }
      }}
    >
      {label}
    </Button>
  );
}
function rawText(encoded: string) {
  return new TextDecoder().decode(
    Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0)),
  );
}
function useNarrowScreen() {
  const [narrow, setNarrow] = useState(
    () => window.matchMedia("(width < 640px)").matches,
  );
  useEffect(() => {
    const media = window.matchMedia("(width < 640px)");
    const update = () => setNarrow(media.matches);
    media.addEventListener("change", update);
    update();
    return () => media.removeEventListener("change", update);
  }, []);
  return narrow;
}
function Main() {
  const { message, modal } = AntApp.useApp();
  const query = useQueryClient();
  const nav = useNavigate();
  const loc = useLocation();
  const view = loc.pathname.slice(1) || "bookmarks";
  // Keep this query identical to the sidebar CSS, including fractional widths.
  const [isMobile, setIsMobile] = useState(
    () => window.matchMedia("(width < 992px)").matches,
  );
  const [navOpen, setNavOpen] = useState(false);
  const [openMenuID, setOpenMenuID] = useState<string | null>(null);
  useEffect(() => {
    const query = window.matchMedia("(width < 992px)");
    const update = () => {
      setIsMobile(query.matches);
      setNavOpen(false);
    };
    query.addEventListener("change", update);
    update();
    return () => query.removeEventListener("change", update);
  }, []);
  const [display, setDisplay] = useState<"grid" | "list">("grid");
  const [authed, auth] = useState<boolean | null>(null);
  const [account, accountSet] = useState<Account | null>(null);
  const [username, usernameSet] = useState("");
  const isUser = authed === true && account?.role === "user";
  const draftPrefix = "draft:" + account?.id + ":";
  const activeAccount = useRef(account?.id);
  activeAccount.current = account?.id;
  const [key, keySet] = useState("");
  const [search, searchSet] = useState("");
  const [filter, filterSet] = useState<string[]>([]);
  const [page, pageSet] = useState(1);
  const [selected, select] = useState<string[]>([]);
  const [editor, edit] = useState<Draft | null>(null);
  const [busy, busySet] = useState(false);
  const [analysis, analysisSet] = useState<any>(null);
  const [drafts, draftsSet] = useState<Draft[]>([]);
  const [importData, importSet] = useState<any>(null);
  const [preview, previewSet] = useState<any>(null);
  const clearPrivateState = () => {
    cancelPendingRequests();
    Modal.destroyAll();
    edit(null);
    draftsSet([]);
    select([]);
    filterSet([]);
    searchSet("");
    importSet(null);
    previewSet(null);
    analysisSet(null);
    busySet(false);
    setOpenMenuID(null);
    pageSet(1);
  };
  const reload = () => {
    query.invalidateQueries();
    select([]);
  };
  const run = async (fn: () => Promise<unknown>) => {
    try {
      return await fn();
    } catch (e) {
      if ((e as Error).name !== "AbortError")
        message.error((e as Error).message);
    }
  };
  useEffect(() => {
    let live = true;
    api("/auth/session")
      .then((v) => {
        if (live) {
          accountSet(v.user);
          auth(true);
        }
      })
      .catch(() => {
        if (live) auth(false);
      });
    return () => {
      live = false;
    };
  }, []);
  useEffect(() => {
    const expire = () => {
      query.clear();
      accountSet(null);
      auth(false);
      clearPrivateState();
    };
    window.addEventListener("session-expired", expire);
    return () => window.removeEventListener("session-expired", expire);
  }, [query]);
  const tags = useQuery<Category[]>({
    queryKey: ["tags", account?.id],
    queryFn: () => api("/tags"),
    enabled: isUser,
  });
  const list = useQuery<{ items: Bookmark[]; total: number }>({
    queryKey: ["bookmarks", account?.id, view, search, filter, page],
    queryFn: () =>
      api(
        "/bookmarks?" +
          new URLSearchParams({
            q: search,
            page: String(page),
            archived: String(view === "archived"),
            unclassified: String(view === "unclassified"),
          }) +
          filter.map((t) => "&tag=" + encodeURIComponent(t)).join(""),
      ),
    enabled: isUser,
  });
  const llm = useQuery({
    queryKey: ["llm", account?.id],
    queryFn: () => api("/settings/llm"),
    enabled: isUser,
  });
  useEffect(() => {
    pageSet(1);
    select([]);
    setOpenMenuID(null);
  }, [view, search, filter.join("|")]);
  useEffect(() => {
    if (list.error) message.error(list.error.message);
  }, [list.error]);
  useEffect(() => {
    select([]);
    setOpenMenuID(null);
  }, [page]);
  async function loadDrafts() {
    const loadingAccount = account?.id;
    const ks = await keys();
    const ds = await Promise.all(
      ks
        .filter((k) => String(k).startsWith(draftPrefix))
        .map((k) => get<Draft>(k)),
    );
    if (activeAccount.current === loadingAccount)
      draftsSet(ds.filter(Boolean) as Draft[]);
  }
  useEffect(() => {
    if (isUser && view === "drafts") loadDrafts();
  }, [view, account?.id]);
  useEffect(() => {
    if (!editor) return;
    const timer = window.setTimeout(() => {
      set(draftPrefix + editor.draftID, editor).catch(() =>
        message.error("草稿保存失败，请检查浏览器存储"),
      );
    }, 500);
    return () => clearTimeout(timer);
  }, [editor, account?.id]);
  const update = (v: Partial<Draft>) => edit((p) => ({ ...p, ...v }));
  const open = (b?: Draft) => {
    analysisSet(null);
    edit(
      b
        ? { ...b, draftID: b.draftID || crypto.randomUUID() }
        : {
            raw: "",
            description: "",
            tags: [],
            score: 0.5,
            draftID: crypto.randomUUID(),
          },
    );
  };
  async function save() {
    if (!editor) return;
    busySet(true);
    await run(async () => {
      const payload = {
        raw: editor.raw || "",
        description: editor.description || "",
        tags: editor.tags || [],
        score: editor.score ?? 0.5,
        version: editor.version || 0,
        create_tags: true,
      };
      try {
        await api(
          "/bookmarks" + (editor.id ? "/" + editor.id : ""),
          editor.id ? "PUT" : "POST",
          payload,
        );
      } catch (e) {
        const existingID = (e as Error & { existingID?: string }).existingID;
        if (existingID) {
          modal.confirm({
            title: "此原始内容已经收藏",
            content: "当前内容已保留为本地草稿。是否打开已有收藏？",
            okText: "打开已有收藏",
            onOk: async () => {
              const existing = await api("/bookmarks/" + existingID);
              open(existing);
            },
          });
          return;
        }
        throw e;
      }
      await del(draftPrefix + editor.draftID);
      edit(null);
      reload();
      message.success("收藏已保存");
    });
    busySet(false);
  }
  async function analyze(mode: string) {
    busySet(true);
    await run(async () =>
      analysisSet({
        ...(await api("/analysis", "POST", { raw: editor?.raw, mode })),
        mode,
      }),
    );
    busySet(false);
  }
  async function showShare(source: {
    kind: string;
    bookmark_id?: string;
    tag_id?: string;
  }) {
    await run(async () => {
      const v = await api("/shares", "POST", source);
      const url = location.origin + v.path;
      reload();
      modal.info({
        title: "分享链接已创建",
        content: (
          <>
            <p>有效期 7 天，接收者需登录。</p>
            <Typography.Paragraph copyable>{url}</Typography.Paragraph>
          </>
        ),
      });
    });
  }
  async function download() {
    await run(async () => {
      let data = await api("/exports");
      if (selected.length)
        data.bookmarks = data.bookmarks.filter((r: any) =>
          list.data?.items.some(
            (b) =>
              selected.includes(b.id) &&
              b.raw ===
                new TextDecoder().decode(
                  Uint8Array.from(atob(r.raw_base64), (c) => c.charCodeAt(0)),
                ),
          ),
        );
      const u = URL.createObjectURL(
        new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }),
      );
      const a = document.createElement("a");
      a.href = u;
      a.download = "online-bookmarks.json";
      a.click();
      URL.revokeObjectURL(u);
    });
  }
  function batch(action: string, ids = selected) {
    modal.confirm({
      title:
        action === "delete"
          ? "永久删除所选收藏？"
          : action === "archive"
            ? "归档所选收藏？"
            : "恢复收藏，评分设为 0.5？",
      onOk: async () => {
        await api("/bookmarks/batch", "POST", { ids, action });
        reload();
      },
    });
  }
  async function login() {
    busySet(true);
    try {
      await run(async () => {
        const v = await api("/auth/login", "POST", { api_key: key, username });
        query.clear();
        clearPrivateState();
        accountSet(v.user);
        auth(true);
        keySet("");
        usernameSet("");
      });
    } finally {
      busySet(false);
    }
  }
  if (authed === null)
    return (
      <div className="center">
        <Spin />
      </div>
    );
  if (!authed)
    return (
      <main className="login-shell">
        <section className="login">
          <Brand />
          <h1>登录</h1>
          <Form layout="vertical" onFinish={login}>
            <Form.Item label="用户名（可选）">
              <Input
                aria-label="用户名"
                autoComplete="username"
                value={username}
                onChange={(e) => usernameSet(e.target.value)}
              />
            </Form.Item>
            <Form.Item label="登录密钥">
              <Input.Password
                aria-label="APIKey"
                autoComplete="current-password"
                value={key}
                onChange={(e) => keySet(e.target.value)}
              />
            </Form.Item>
            <Button
              type="primary"
              block
              htmlType="submit"
              loading={busy}
              disabled={!key.length}
            >
              登录
            </Button>
          </Form>
        </section>
      </main>
    );
  const logout = async () => {
    await api("/auth/logout", "POST");
    query.clear();
    accountSet(null);
    auth(false);
    clearPrivateState();
  };
  if (account?.role === "superadmin") return <AdminView logout={logout} />;
  if (loc.pathname.startsWith("/share/"))
    return (
      <SharedView
        token={loc.pathname.slice(7)}
        tags={tags.data || []}
        reload={reload}
      />
    );
  return (
    <Layout className="shell">
      <aside
        id="collection-navigation"
        className={`sidebar ${navOpen ? "is-open" : ""}`}
        inert={isMobile && !navOpen}
        aria-label="收藏导航"
      >
        <div className="sidebar-body">
          <Brand />
          <Menu
            mode="inline"
            selectedKeys={[view]}
            onClick={(e) => {
              nav("/" + e.key);
              setNavOpen(false);
              filterSet([]);
            }}
            items={[
              { key: "bookmarks", label: "全部收藏", icon: <Library /> },
              { key: "unclassified", label: "未分类", icon: <Inbox /> },
              { key: "archived", label: "已归档", icon: <Archive /> },
              { key: "drafts", label: "本地草稿", icon: <FilePenLine /> },
              { type: "divider" },
              { key: "tags", label: "管理分类", icon: <FolderTree /> },
              { key: "transfer", label: "导入 / 导出", icon: <ArrowDownUp /> },
              { key: "shares", label: "分享管理", icon: <LinkIcon /> },
              { key: "settings", label: "设置", icon: <SettingsIcon /> },
            ]}
          />
          <div className="side-tree">
            <div className="tree-heading">
              <span>分类目录</span>
              <Tooltip title="管理分类">
                <Button
                  type="text"
                  size="small"
                  aria-label="管理分类"
                  icon={<Plus size={15} />}
                  onClick={() => nav("/tags")}
                />
              </Tooltip>
            </div>
            {!tags.data?.length && (
              <div className="tree-empty">
                <Folder size={16} />
                无分类
              </div>
            )}
            <Tree
              treeData={treeData(tags.data || [])}
              selectedKeys={filter}
              onSelect={(ks) => {
                filterSet(ks.map(String));
                nav("/bookmarks");
                setNavOpen(false);
              }}
            />
          </div>
        </div>
      </aside>
      {isMobile && navOpen && (
        <button
          className="nav-scrim"
          aria-label="关闭导航"
          onClick={() => setNavOpen(false)}
        />
      )}
      <Layout.Content className="workspace">
        <div className="topbar">
          <div className="breadcrumb">
            <Button
              className="nav-toggle"
              type="text"
              aria-label="打开导航"
              icon={<MenuIcon size={20} />}
              aria-controls="collection-navigation"
              aria-expanded={navOpen}
              onClick={() => setNavOpen((open) => !open)}
            />
            <strong>收藏管理</strong>
          </div>
          <div className="account-actions">
            <span className="account-name" title={account?.username}>
              {account?.username}
            </span>
            <Button icon={<LogOut size={16} />} onClick={() => run(logout)}>
              退出登录
            </Button>
          </div>
        </div>
        <main className="content">
          <header className="page-header">
            <div>
              <h1>{viewTitles[view] || "收藏夹"}</h1>
            </div>
            {["bookmarks", "unclassified", "archived", "drafts"].includes(
              view,
            ) && (
              <Button
                type="primary"
                icon={<Plus size={18} />}
                onClick={() => open()}
              >
                新建收藏
              </Button>
            )}
          </header>
          {["bookmarks", "unclassified", "archived"].includes(view) && (
            <>
              <div className="toolbar">
                <Input.Search
                  prefix={<Search size={17} />}
                  placeholder="搜索内容、网址或标签"
                  allowClear
                  onSearch={(v) => searchSet(v)}
                />
                <TreeSelect
                  multiple
                  treeData={treeData(tags.data || [])}
                  value={filter}
                  onChange={filterSet}
                  placeholder="筛选分类"
                  className="filter-select"
                  suffixIcon={<SlidersHorizontal size={16} />}
                  treeCheckStrictly={false}
                  allowClear
                />
                <div className="view-switch" role="group" aria-label="浏览方式">
                  <Tooltip title="卡片视图">
                    <Button
                      aria-label="卡片视图"
                      aria-pressed={display === "grid"}
                      type="text"
                      className={display === "grid" ? "active" : ""}
                      icon={<LayoutGrid size={17} />}
                      onClick={() => setDisplay("grid")}
                    />
                  </Tooltip>
                  <Tooltip title="列表视图">
                    <Button
                      aria-label="列表视图"
                      aria-pressed={display === "list"}
                      type="text"
                      className={display === "list" ? "active" : ""}
                      icon={<List size={18} />}
                      onClick={() => setDisplay("list")}
                    />
                  </Tooltip>
                </div>
              </div>
              <div className="collection-meta">
                <Space wrap>
                  <Typography.Text type="secondary">
                    {list.data?.total || 0} 条收藏
                  </Typography.Text>
                  {selected.length > 0 && (
                    <>
                      <span>已选 {selected.length}</span>
                      <Button
                        onClick={() =>
                          batch(view === "archived" ? "restore" : "archive")
                        }
                      >
                        {view === "archived" ? "恢复" : "归档"}
                      </Button>
                      <Button danger onClick={() => batch("delete")}>
                        删除
                      </Button>
                      <Button onClick={download}>导出所选</Button>
                      <Button onClick={() => select([])}>取消选择</Button>
                    </>
                  )}
                </Space>
                <span className="sort-label">
                  <ArrowDownWideNarrow size={15} />
                  评分降序
                </span>
              </div>
              {list.isLoading ? (
                <Spin />
              ) : !list.data?.items.length ? (
                <Empty
                  className="empty-state"
                  description={
                    search || filter.length ? "无匹配结果" : "暂无收藏"
                  }
                />
              ) : (
                <div
                  className={`cards ${display === "list" ? "list-view" : ""}`}
                >
                  {list.data.items.map((b) => (
                    <Card
                      key={b.id}
                      className={`bookmark ${selected.includes(b.id) ? "is-selected" : ""}`}
                    >
                      <div className="card-top">
                        <div className="source">
                          <span
                            className={`source-icon ${sourceName(b.raw) === "文字收藏" ? "text-source" : ""}`}
                          >
                            {sourceName(b.raw) === "文字收藏" ? (
                              <FileText size={19} />
                            ) : (
                              <LinkIcon size={19} />
                            )}
                          </span>
                          <span className="source-host">
                            {sourceName(b.raw)}
                          </span>
                        </div>
                        <div className="card-top-actions">
                          <Checkbox
                            aria-label="选择收藏"
                            checked={selected.includes(b.id)}
                            onChange={(e) =>
                              select(
                                e.target.checked
                                  ? [...selected, b.id]
                                  : selected.filter((id) => id !== b.id),
                              )
                            }
                          />
                          <Dropdown
                            placement="bottomRight"
                            classNames={{ root: "bookmark-menu" }}
                            open={openMenuID === b.id}
                            onOpenChange={(open) =>
                              setOpenMenuID((current) =>
                                open ? b.id : current === b.id ? null : current,
                              )
                            }
                            trigger={["click"]}
                            menu={{
                              items: [
                                {
                                  key: "share",
                                  label: "分享收藏",
                                  icon: <LinkIcon size={15} />,
                                },
                                {
                                  key: "copy",
                                  label: "复制原文",
                                  icon: <Copy size={15} />,
                                },
                                {
                                  key: "delete",
                                  label: "删除收藏",
                                  icon: <Trash2 size={15} />,
                                  danger: true,
                                },
                              ],
                              onClick: ({ key }) => {
                                setOpenMenuID(null);
                                if (key === "copy")
                                  run(async () => {
                                    await copyText(b.raw);
                                    message.success("原文已复制");
                                  });
                                else if (key === "share")
                                  showShare({
                                    kind: "bookmark",
                                    bookmark_id: b.id,
                                  });
                                else if (key === "delete")
                                  batch("delete", [b.id]);
                              },
                            }}
                          >
                            <Button
                              type="text"
                              size="small"
                              className="more-button"
                              title="更多操作"
                              aria-label="更多收藏操作"
                              aria-haspopup="menu"
                              aria-expanded={openMenuID === b.id}
                              icon={<MoreHorizontal size={19} />}
                            />
                          </Dropdown>
                        </div>
                      </div>
                      <div className="card-content">
                        <h3>
                          <button
                            className="title-button"
                            onClick={() => open(b)}
                          >
                            {b.description.split("\n")[0] || sourceName(b.raw)}
                          </button>
                        </h3>
                        <p className="raw-content" aria-label="原始内容">
                          {b.raw}
                        </p>
                        {b.description
                          .split("\n")
                          .slice(1)
                          .join("\n")
                          .trim() && (
                          <p className="description">
                            {b.description
                              .split("\n")
                              .slice(1)
                              .join("\n")
                              .trim()}
                          </p>
                        )}
                        <div className="card-tags">
                          {b.tags
                            .filter((t) => t !== "已归档")
                            .map((t) => (
                              <Tag
                                key={t}
                                title={t}
                                onClick={() => filterSet([t])}
                              >
                                {t}
                              </Tag>
                            ))}
                          {!b.tags.filter((t) => t !== "已归档").length && (
                            <span className="untagged">
                              <Folder size={13} />
                              未分类
                            </span>
                          )}
                        </div>
                      </div>
                      <div className="card-footer">
                        <span className="score">
                          {b.score === 0
                            ? "已归档"
                            : `评分 ${b.score.toFixed(2)}`}
                        </span>
                        <div className="card-actions">
                          <CopyButton text={b.raw} />
                          <Tooltip title="编辑收藏">
                            <Button
                              type="text"
                              size="small"
                              aria-label="编辑收藏"
                              icon={<Pencil size={15} />}
                              onClick={() => open(b)}
                            />
                          </Tooltip>
                          <Button
                            type="text"
                            size="small"
                            onClick={() =>
                              batch(b.score === 0 ? "restore" : "archive", [
                                b.id,
                              ])
                            }
                          >
                            {b.score === 0 ? "恢复" : "归档"}
                          </Button>
                          {/^https?:\/\/\S+$/i.test(b.raw.trim()) && (
                            <Tooltip title="打开链接">
                              <Button
                                type="text"
                                size="small"
                                aria-label="打开链接"
                                href={b.raw.trim()}
                                target="_blank"
                                rel="noopener noreferrer"
                                icon={<ArrowUpRight size={17} />}
                              />
                            </Tooltip>
                          )}
                        </div>
                      </div>
                    </Card>
                  ))}
                </div>
              )}
              <Pagination
                style={{ marginTop: 28 }}
                current={page}
                total={list.data?.total || 0}
                pageSize={30}
                showSizeChanger={false}
                onChange={pageSet}
              />
            </>
          )}
          {view === "drafts" && (
            <>
              <Typography.Paragraph type="secondary">
                草稿仅保存在当前浏览器；退出登录不会自动删除。
              </Typography.Paragraph>
              {drafts.length ? (
                drafts.map((d) => (
                  <Card
                    key={d.draftID}
                    style={{ marginBottom: 12 }}
                    title={d.description?.split("\n")[0] || d.raw || "空白草稿"}
                  >
                    <p className="raw-content">{d.raw}</p>
                    <Space wrap>
                      <CopyButton text={d.raw || ""} />
                      <Button onClick={() => open(d)}>继续编辑</Button>
                      <Button
                        danger
                        onClick={() =>
                          modal.confirm({
                            title: "删除本地草稿？",
                            onOk: async () => {
                              await del(draftPrefix + d.draftID);
                              loadDrafts();
                            },
                          })
                        }
                      >
                        删除
                      </Button>
                    </Space>
                  </Card>
                ))
              ) : (
                <Empty description="没有本地草稿" />
              )}
            </>
          )}
          {view === "tags" && (
            <Card title="分类目录">
              <TagManager
                tags={tags.data || []}
                reload={reload}
                share={(tag_id) => showShare({ kind: "tag", tag_id })}
              />
            </Card>
          )}
          {view === "transfer" && (
            <Card title="导入与导出">
              <Typography.Paragraph>
                JSON 格式，原文使用
                Base64。导出包含归档及空分类，不包含密钥与本地草稿。
              </Typography.Paragraph>
              <Space wrap>
                <Button onClick={download}>导出全部收藏</Button>
                <Upload
                  accept=".json,application/json"
                  showUploadList={false}
                  beforeUpload={(f) => {
                    run(async () => {
                      const loadingAccount = activeAccount.current;
                      const data = JSON.parse(await f.text());
                      if (activeAccount.current !== loadingAccount) return;
                      importSet(data);
                      previewSet(null);
                    });
                    return false;
                  }}
                >
                  <Button>选择导入文件</Button>
                </Upload>
              </Space>
              {importData && (
                <div style={{ marginTop: 20 }}>
                  <p>
                    已选择 {importData.bookmarks?.length ?? 0}{" "}
                    条记录，重复收藏采用导入评分，标签保留最细分类。
                  </p>
                  <Space>
                    <Button
                      onClick={() =>
                        run(async () =>
                          previewSet(
                            await api("/imports/preview", "POST", {
                              data: importData,
                            }),
                          ),
                        )
                      }
                    >
                      预检导入
                    </Button>
                    {preview && (
                      <Button
                        type="primary"
                        onClick={() =>
                          run(async () => {
                            await api("/imports/commit", "POST", {
                              data: importData,
                              revision: preview.revision,
                            });
                            importSet(null);
                            previewSet(null);
                            reload();
                            message.success("导入完成");
                          })
                        }
                      >
                        确认提交
                      </Button>
                    )}
                  </Space>
                  {preview && (
                    <p>
                      新增 {preview.counts.created} 条，合并{" "}
                      {preview.counts.merged} 条
                    </p>
                  )}
                </div>
              )}
            </Card>
          )}
          {view === "shares" && <ShareManager />}
          {view === "settings" && (
            <Settings
              cfg={llm.data}
              reload={reload}
              onRotated={() => {
                query.clear();
                clearPrivateState();
                accountSet(null);
                auth(false);
              }}
            />
          )}
        </main>
      </Layout.Content>
      <Drawer
        className="bookmark-editor"
        title={editor?.id ? "编辑收藏" : "新建收藏"}
        open={!!editor}
        onClose={() => {
          if (editor)
            set(draftPrefix + editor.draftID, editor).catch(() =>
              message.error("草稿保存失败"),
            );
          edit(null);
          analysisSet(null);
        }}
        width="min(640px, 100%)"
        extra={
          <Button type="primary" loading={busy} onClick={save}>
            确认保存
          </Button>
        }
      >
        <Form layout="vertical">
          <div className="field-label">
            <label htmlFor="bookmark-raw">
              原始内容 <span className="required-mark">*</span>
            </label>
            <CopyButton text={editor?.raw || ""} />
          </div>
          <Form.Item className="raw-field">
            <Input.TextArea
              id="bookmark-raw"
              aria-label="原始内容"
              placeholder="链接或文本"
              rows={4}
              value={editor?.raw}
              onChange={(e) => update({ raw: e.target.value })}
            />
          </Form.Item>
          <div className="ai-panel">
            <div className="ai-heading">
              <Sparkles size={17} />
              <strong>内容分析</strong>
            </div>
            <Space wrap>
              {[
                ["description", "生成解释"],
                ["tags", "推荐标签"],
                ["both", "一起分析"],
              ].map(([mode, title]) => (
                <Button
                  key={mode}
                  disabled={!llm.data?.enabled || !editor?.raw}
                  loading={busy}
                  onClick={() => analyze(mode)}
                >
                  {title}
                </Button>
              ))}
            </Space>
            {!llm.data?.enabled && (
              <p className="muted">在设置中启用 LLM 后可使用分析。</p>
            )}
          </div>
          <Form.Item label="说明">
            <Input.TextArea
              rows={7}
              value={editor?.description}
              onChange={(e) => update({ description: e.target.value })}
            />
          </Form.Item>
          <Form.Item label="标签">
            <TreeSelect
              multiple
              treeData={treeData(tags.data || [])}
              value={(editor?.tags || []).filter((t) => t !== "已归档")}
              onChange={(values: string[]) => {
                const old = editor?.tags || [];
                const added = values.find((t) => !old.includes(t));
                update({
                  tags: added
                    ? values.filter(
                        (t) =>
                          t === added ||
                          (!t.startsWith(added + "/") &&
                            !added.startsWith(t + "/")),
                      )
                    : values,
                });
              }}
              style={{ width: "100%" }}
              placeholder="选择分类"
            />
          </Form.Item>
          <Form.Item label="新增标签路径">
            <Input.Search
              placeholder="知识/计算机/编程"
              enterButton="添加"
              onSearch={(v) => {
                if (v.trim()) {
                  const p = v.trim();
                  update({
                    tags: [
                      ...(editor?.tags || []).filter(
                        (t) =>
                          t !== p &&
                          !t.startsWith(p + "/") &&
                          !p.startsWith(t + "/"),
                      ),
                      p,
                    ],
                  });
                }
              }}
            />
            {editor?.tags
              ?.filter(
                (t) => t !== "已归档" && !tags.data?.some((c) => c.path === t),
              )
              .map((t) => (
                <Tag
                  color="orange"
                  key={t}
                  closable
                  onClose={() =>
                    update({ tags: editor.tags?.filter((v) => v !== t) })
                  }
                >
                  新增：{t}
                </Tag>
              ))}
          </Form.Item>
          <Form.Item label="评分">
            <Slider
              min={0}
              max={1}
              step={0.01}
              value={editor?.score ?? 0.5}
              onChange={(score) => update({ score })}
            />
            <InputNumber
              min={0}
              max={1}
              step={0.01}
              value={editor?.score ?? 0.5}
              onChange={(s) => update({ score: s ?? 0.5 })}
            />
            {editor?.score === 0 && <Tag>保存后自动归档</Tag>}
          </Form.Item>
          <Typography.Text type="secondary">
            自动保存草稿；确认保存后创建新增标签。
          </Typography.Text>
        </Form>
      </Drawer>
      <Modal
        title="分析结果预览"
        open={!!analysis}
        onCancel={() => analysisSet(null)}
        okText="应用到编辑器"
        onOk={() => {
          const v: Partial<Draft> = {};
          if (analysis.mode !== "tags") v.description = analysis.description;
          if (analysis.mode !== "description") v.tags = analysis.tags;
          update(v);
          analysisSet(null);
        }}
      >
        {analysis?.mode !== "tags" && (
          <Input.TextArea
            rows={5}
            value={analysis?.description}
            onChange={(e) =>
              analysisSet({ ...analysis, description: e.target.value })
            }
          />
        )}
        <div style={{ marginTop: 12 }}>
          {analysis?.tags?.map((t: string) => (
            <Tag
              key={t}
              color={analysis.new_tags?.includes(t) ? "orange" : "blue"}
            >
              {t}
              {analysis.new_tags?.includes(t) ? " · 新增" : ""}
            </Tag>
          ))}
        </div>
      </Modal>
    </Layout>
  );
}
function AdminView({ logout }: { logout: () => Promise<void> }) {
  const { message, modal } = AntApp.useApp();
  const narrow = useNarrowScreen();
  const [page, pageSet] = useState(1);
  const [search, searchSet] = useState("");
  const [editing, editingSet] = useState<Partial<Account> | null>(null);
  const [busy, busySet] = useState(false);
  const [form] = Form.useForm();
  const users = useQuery<Account[]>({
    queryKey: ["admin-users", search],
    queryFn: () => api("/admin/users?q=" + encodeURIComponent(search)),
  });
  const editUser = (u: Partial<Account>) => {
    editingSet(u);
    form.resetFields();
    form.setFieldsValue(u);
  };
  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
    } catch (e) {
      message.error((e as Error).message);
    }
  };
  const currentPage = Math.min(
    page,
    Math.max(1, Math.ceil((users.data?.length || 0) / 10)),
  );
  const actions = (u: Account) => (
    <Space wrap>
      <Button
        onClick={() =>
          run(async () => editUser(await api("/admin/users/" + u.id)))
        }
      >
        编辑
      </Button>
      <Button
        danger
        onClick={() =>
          modal.confirm({
            title: "永久删除用户 " + u.username + "？",
            content: "该用户的收藏、标签、模型配置及分享链接将一并删除。",
            onOk: async () => {
              await api("/admin/users/" + u.id, "DELETE");
              await users.refetch();
            },
          })
        }
      >
        删除
      </Button>
    </Space>
  );
  return (
    <main className="management-page">
      <header className="management-header">
        <h1>用户管理</h1>
        <Button onClick={() => run(logout)}>退出登录</Button>
      </header>
      <div className="management-toolbar">
        <Input.Search
          aria-label="搜索用户"
          placeholder="搜索用户名"
          onSearch={(v) => {
            searchSet(v);
            pageSet(1);
          }}
        />
        <Button type="primary" onClick={() => editUser({ disabled: false })}>
          新增用户
        </Button>
      </div>
      {users.error && (
        <Typography.Paragraph type="danger">
          {users.error.message}
        </Typography.Paragraph>
      )}
      {narrow ? (
        <div className="mobile-records">
          {users.isLoading && <Spin />}
          {users.data
            ?.slice((currentPage - 1) * 10, currentPage * 10)
            .map((u) => (
              <article
                className="mobile-record"
                aria-label={u.username}
                key={u.id}
              >
                <div className="record-heading">
                  <strong>{u.username}</strong>
                  <Tag color={u.disabled ? "default" : "green"}>
                    {u.disabled ? "已停用" : "正常"}
                  </Tag>
                </div>
                {actions(u)}
              </article>
            ))}
          {!users.isLoading && !users.data?.length && (
            <Empty description="无用户" />
          )}
          <Pagination
            size="small"
            current={currentPage}
            pageSize={10}
            total={users.data?.length || 0}
            showSizeChanger={false}
            hideOnSinglePage
            onChange={pageSet}
          />
        </div>
      ) : (
        <Table<Account>
          tableLayout="fixed"
          rowKey="id"
          loading={users.isLoading}
          dataSource={users.data || []}
          pagination={{
            current: currentPage,
            pageSize: 10,
            onChange: pageSet,
            showSizeChanger: false,
            hideOnSinglePage: true,
          }}
          columns={[
            { title: "用户名", dataIndex: "username" },
            {
              title: "状态",
              render: (_, u) => (u.disabled ? "已停用" : "正常"),
            },
            { title: "操作", render: (_, u) => actions(u) },
          ]}
        />
      )}
      <Modal
        title={editing?.id ? "编辑用户" : "新增用户"}
        open={editing !== null}
        onCancel={() => editingSet(null)}
        onOk={() => form.submit()}
        confirmLoading={busy}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={(v) => {
            busySet(true);
            run(async () => {
              await api(
                "/admin/users" + (editing?.id ? "/" + editing.id : ""),
                editing?.id ? "PUT" : "POST",
                { ...v, api_key: v.api_key || "" },
              );
              editingSet(null);
              await users.refetch();
            }).finally(() => busySet(false));
          }}
        >
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true }, { max: 80 }]}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="api_key"
            label={
              editing?.id
                ? "重置登录 APIKey（留空保留）"
                : "登录 APIKey（至少16字符）"
            }
            rules={[{ required: !editing?.id }, { min: 16 }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          {editing?.id && (
            <p className="functional-note">保存后，该用户需重新登录。</p>
          )}
          <Form.Item name="disabled" label="停用账号" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </main>
  );
}
type SharedLink = {
  id: string;
  label: string;
  bookmark_count: number;
  tag_count: number;
  expires_at: string;
  revoked: boolean;
};
function ShareManager() {
  const { modal } = AntApp.useApp();
  const narrow = useNarrowScreen();
  const [page, pageSet] = useState(1);
  const shares = useQuery<SharedLink[]>({
    queryKey: ["shares"],
    queryFn: () => api("/shares"),
  });
  const currentPage = Math.min(
    page,
    Math.max(1, Math.ceil((shares.data?.length || 0) / 10)),
  );
  const status = (s: SharedLink) =>
    s.revoked
      ? "已撤销"
      : Date.parse(s.expires_at) <= Date.now()
        ? "已过期"
        : "有效";
  const revoke = (s: SharedLink) => (
    <Button
      danger
      disabled={s.revoked}
      onClick={() =>
        modal.confirm({
          title: "撤销此分享链接？",
          content: "撤销后链接立即失效，已导入的副本保留。",
          onOk: async () => {
            await api("/shares/" + s.id, "DELETE");
            await shares.refetch();
          },
        })
      }
    >
      撤销
    </Button>
  );
  return (
    <Card title="分享链接">
      <p className="functional-note">
        从收藏菜单或分类管理创建分享。链接仅在创建时显示。
      </p>
      {shares.error && (
        <Typography.Text type="danger">{shares.error.message}</Typography.Text>
      )}
      {narrow ? (
        <div className="mobile-records">
          {shares.isLoading && <Spin />}
          {shares.data
            ?.slice((currentPage - 1) * 10, currentPage * 10)
            .map((s) => (
              <article
                className="mobile-record"
                aria-label={s.label}
                key={s.id}
              >
                <div className="record-heading">
                  <strong>{s.label}</strong>
                  <Tag>{status(s)}</Tag>
                </div>
                <p>
                  {s.bookmark_count} 条收藏 / {s.tag_count} 个标签
                </p>
                <p>到期：{new Date(s.expires_at).toLocaleString()}</p>
                {revoke(s)}
              </article>
            ))}
          {!shares.isLoading && !shares.data?.length && (
            <Empty description="无分享链接" />
          )}
          <Pagination
            size="small"
            current={currentPage}
            pageSize={10}
            total={shares.data?.length || 0}
            showSizeChanger={false}
            hideOnSinglePage
            onChange={pageSet}
          />
        </div>
      ) : (
        <Table<SharedLink>
          tableLayout="fixed"
          rowKey="id"
          loading={shares.isLoading}
          dataSource={shares.data || []}
          pagination={{
            current: currentPage,
            pageSize: 10,
            onChange: pageSet,
            showSizeChanger: false,
            hideOnSinglePage: true,
          }}
          columns={[
            { title: "分享范围", dataIndex: "label" },
            {
              title: "快照",
              render: (_, s) =>
                `${s.bookmark_count} 条收藏 / ${s.tag_count} 个标签`,
            },
            {
              title: "到期时间",
              render: (_, s) => new Date(s.expires_at).toLocaleString(),
            },
            { title: "状态", render: (_, s) => status(s) },
            { title: "操作", render: (_, s) => revoke(s) },
          ]}
        />
      )}
    </Card>
  );
}
function SharedView({
  token,
  tags,
  reload,
}: {
  token: string;
  tags: Category[];
  reload: () => void;
}) {
  const { message } = AntApp.useApp();
  const nav = useNavigate();
  const [target, targetSet] = useState("");
  const [preview, previewSet] = useState<any>(null);
  const [busy, busySet] = useState(false);
  const data = useQuery({
    queryKey: ["shared", token],
    queryFn: () => api("/shares/" + encodeURIComponent(token)),
    retry: false,
  });
  useEffect(() => {
    previewSet(null);
    targetSet("");
  }, [token]);
  async function apply(action: string) {
    busySet(true);
    try {
      const v = await api(
        "/shares/" + encodeURIComponent(token) + "/" + action,
        "POST",
        { target_path: target, revision: preview?.revision || "" },
      );
      if (action === "preview") previewSet(v);
      else {
        message.success("分享内容已导入");
        reload();
        nav("/bookmarks");
      }
    } catch (e) {
      previewSet(null);
      message.error((e as Error).message);
    } finally {
      busySet(false);
    }
  }
  const paths: string[] = data.data?.data.tags || [];
  const nodes: Category[] = paths.map((path) => ({
    id: path,
    path,
    name: path.split("/").at(-1)!,
    parent_id: path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "",
  }));
  return (
    <main className="management-page">
      <header className="management-header">
        <h1>收藏分享</h1>
        <Button onClick={() => nav("/bookmarks")}>返回我的收藏</Button>
      </header>
      {data.isLoading && <Spin />}
      {data.error && (
        <Typography.Paragraph type="danger">
          分享不可用，可能已过期、被撤销或分享者已停用。{data.error.message}
        </Typography.Paragraph>
      )}
      {data.data && (
        <>
          <Card title={data.data.share.label}>
            <p>
              {data.data.share.bookmark_count} 条收藏 · 固定快照 · 有效至{" "}
              {new Date(data.data.share.expires_at).toLocaleString()}
            </p>
            <p>同路径标签复用；相同原文合并，保留已有评分。</p>
            <TreeSelect
              aria-label="导入位置"
              style={{ width: "100%", maxWidth: 480 }}
              treeData={treeData(tags)}
              value={target || undefined}
              placeholder="根目录"
              allowClear
              onChange={(v) => {
                targetSet(v || "");
                previewSet(null);
              }}
            />
            <Space wrap style={{ marginTop: 16 }}>
              <Button loading={busy} onClick={() => apply("preview")}>
                预检合并
              </Button>
              <Button
                type="primary"
                disabled={!preview}
                loading={busy}
                onClick={() => apply("commit")}
              >
                确认导入
              </Button>
            </Space>
            {preview && (
              <p>
                新增 {preview.counts.created} 条，合并 {preview.counts.merged}{" "}
                条，无需修改 {preview.counts.unchanged} 条；新增标签{" "}
                {preview.counts.tags_created} 个，复用标签{" "}
                {preview.counts.tags_reused} 个。
              </p>
            )}
          </Card>
          <Card title="共享标签树" style={{ marginTop: 20 }}>
            <div className="tag-tree">
              <Tree treeData={treeData(nodes)} defaultExpandAll />
            </div>
            {!nodes.length && <Empty description="无标签" />}
          </Card>
          <Card title="共享收藏预览" style={{ marginTop: 20 }}>
            {data.data.data.bookmarks.map((b: any, i: number) => {
              const raw = rawText(b.raw_base64);
              return (
                <article className="shared-record" key={i}>
                  <div className="record-heading">
                    <strong>
                      {b.description.split("\n")[0] || sourceName(raw)}
                    </strong>
                    <CopyButton text={raw} />
                  </div>
                  <p className="shared-raw">{raw}</p>
                  {b.description.split("\n").slice(1).join("\n").trim() && (
                    <p className="shared-description">
                      {b.description.split("\n").slice(1).join("\n")}
                    </p>
                  )}
                  <div className="card-tags">
                    {b.tags.map((t: string) => (
                      <Tag key={t}>{t}</Tag>
                    ))}
                    {b.score === 0 && <Tag>已归档</Tag>}
                  </div>
                </article>
              );
            })}
            {!data.data.data.bookmarks.length && <Empty description="无收藏" />}
          </Card>
        </>
      )}
    </main>
  );
}

function TagManager({
  tags,
  reload,
  share,
}: {
  tags: Category[];
  reload: () => void;
  share: (id: string) => void;
}) {
  const { message, modal } = AntApp.useApp();
  const [path, pathSet] = useState("");
  const [selected, selectedSet] = useState<string>("");
  const [actionPath, actionSet] = useState("");
  async function call(fn: () => Promise<unknown>) {
    try {
      await fn();
      reload();
    } catch (e) {
      message.error((e as Error).message);
    }
  }
  const node = tags.find((t) => t.path === selected);
  return (
    <>
      <Space.Compact style={{ width: "100%" }}>
        <Input
          placeholder="新分类完整路径"
          value={path}
          onChange={(e) => pathSet(e.target.value)}
        />
        <Button
          onClick={() =>
            call(async () => {
              await api("/tags", "POST", { path });
              pathSet("");
            })
          }
        >
          新增
        </Button>
      </Space.Compact>
      <div className="tag-tree">
        <Tree
          treeData={treeData(tags)}
          onSelect={(ks) => {
            const p = String(ks[0] || "");
            selectedSet(p);
            actionSet(p);
          }}
        />
      </div>
      {node && (
        <>
          <Input
            aria-label="目标分类路径"
            value={actionPath}
            onChange={(e) => actionSet(e.target.value)}
          />
          <Space wrap style={{ marginTop: 12 }}>
            <Button onClick={() => share(node.id)}>分享此分类及子树</Button>
            <Button
              onClick={() =>
                call(() => api("/tags/" + node.id, "PUT", { path: actionPath }))
              }
            >
              重命名 / 移动
            </Button>
            <Button
              danger
              onClick={() =>
                modal.confirm({
                  title: "删除 " + node.path + " 及其子分类？",
                  content: "所有相关收藏将移除此分类关联，收藏本身保留。",
                  onOk: () => call(() => api("/tags/" + node.id, "DELETE")),
                })
              }
            >
              删除子树
            </Button>
          </Space>
        </>
      )}
    </>
  );
}
function Settings({
  cfg,
  reload,
  onRotated,
}: {
  cfg: any;
  reload: () => void;
  onRotated: () => void;
}) {
  const { message, modal } = AntApp.useApp();
  const [form] = Form.useForm();
  const [keyForm] = Form.useForm();
  useEffect(() => {
    form.setFieldsValue({ ...cfg, api_key: "" });
  }, [cfg]);
  async function run(fn: () => Promise<unknown>) {
    try {
      await fn();
      message.success("操作成功");
    } catch (e) {
      message.error((e as Error).message);
    }
  }
  return (
    <div className="settings">
      <Card title="Anthropic 模型设置">
        <Form
          form={form}
          layout="vertical"
          onFinish={(v) =>
            run(async () => {
              await api("/settings/llm", "PUT", v);
              reload();
            })
          }
        >
          <Form.Item name="enabled" label="启用分析" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item
            name="base_url"
            label="Base URL"
            rules={[{ required: true }]}
          >
            <Input placeholder="https://api.anthropic.com" />
          </Form.Item>
          <Form.Item
            name="model_id"
            label="Model ID"
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="api_key"
            label={cfg?.has_api_key ? "APIKey（已保存，留空保留）" : "APIKey"}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Space wrap>
            <Button type="primary" htmlType="submit">
              保存
            </Button>
            <Button
              onClick={() => run(() => api("/settings/llm/test", "POST", {}))}
            >
              测试已保存配置
            </Button>
            <Button
              danger
              onClick={() =>
                modal.confirm({
                  title: "清除模型配置？",
                  onOk: () =>
                    run(async () => {
                      await api("/settings/llm", "DELETE");
                      reload();
                    }),
                })
              }
            >
              清除配置
            </Button>
          </Space>
        </Form>
      </Card>
      <Card title="更改登录 APIKey">
        <Form
          form={keyForm}
          layout="vertical"
          onFinish={(v) =>
            run(async () => {
              await api("/auth/key", "PUT", v);
              onRotated();
            })
          }
        >
          <Form.Item
            name="current"
            label="当前 APIKey"
            rules={[{ required: true }]}
          >
            <Input.Password />
          </Form.Item>
          <Form.Item
            name="next"
            label="新 APIKey（至少16字符）"
            rules={[{ required: true, min: 16 }]}
          >
            <Input.Password />
          </Form.Item>
          <p>修改后旧会话和 MCP 密钥立即失效，需要重新登录。</p>
          <Button htmlType="submit">更改密钥</Button>
        </Form>
      </Card>
      <Card title="MCP 接入">
        <p>
          端点：
          <Typography.Text copyable>{API_ORIGIN}/mcp</Typography.Text>
        </p>
        <p>请求头：Authorization: Bearer &lt;APIKey&gt;</p>
        <p>协议：Streamable HTTP。分析规范：bookmarks://analysis-spec/v1。</p>
      </Card>
    </div>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ConfigProvider
      locale={zhCN}
      button={{ autoInsertSpace: false }}
      theme={{
        token: {
          colorPrimary: "#3056c9",
          colorText: "#25324a",
          colorTextSecondary: "#667085",
          colorBorder: "#e2e6ee",
          colorBgLayout: "#f7f8fc",
          controlHeight: 40,
          fontSize: 14,
          borderRadius: 6,
          fontFamily:
            '"Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans SC Variable", "PingFang SC", "Microsoft YaHei", sans-serif',
        },
        components: {
          Button: { primaryShadow: "none", fontWeight: 500 },
          Menu: {
            itemSelectedBg: "#eef0ff",
            itemSelectedColor: "#5363db",
            itemHoverBg: "#f4f5fa",
            itemHeight: 42,
          },
          Card: { headerFontSize: 16 },
          Input: { activeShadow: "0 0 0 3px #5363db12" },
          Drawer: { footerPaddingBlock: 20 },
          Tree: { nodeSelectedBg: "#eef0ff", nodeHoverBg: "#f4f5fa" },
        },
      }}
    >
      <QueryClientProvider client={qc}>
        <BrowserRouter>
          <AntApp>
            <Main />
          </AntApp>
        </BrowserRouter>
      </QueryClientProvider>
    </ConfigProvider>
  </React.StrictMode>,
);
