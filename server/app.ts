import express, { Express, Request, Response, NextFunction } from "express";
import cookieParser from "cookie-parser";
import { db, initDb, type TaskRow, type DocRow, type ChatRow } from "./db";
import { broadcast } from "./ws";
import {
  hashPassword,
  comparePassword,
  signToken,
  verifyToken,
  findUserByEmail,
  findUserById,
  createUser,
  type TokenPayload,
} from "./auth";

export interface AppOptions {
  storePath?: string;
}

interface AuthRequest extends Request {
  user?: TokenPayload;
}

const requireAuth = (req: AuthRequest, res: Response, next: NextFunction) => {
  const token = req.cookies?.token;
  if (!token) {
    res.status(401).json({ error: "Not authenticated" });
    return;
  }
  const payload = verifyToken(token);
  if (!payload) {
    res.status(401).json({ error: "Invalid or expired token" });
    return;
  }
  req.user = payload;
  next();
};

function uid(req: AuthRequest): string {
  return req.user!.userId;
}

function taskRowToTask(r: TaskRow) {
  return {
    id: r.id,
    title: r.title,
    description: r.description,
    status: r.status as "backlog" | "in_progress" | "review" | "done",
    priority: r.priority as "low" | "medium" | "high" | "urgent",
    assignee: { name: r.assignee_name ?? "", role: r.assignee_role ?? "" },
    dueDate: r.due_date ?? "",
    vpnRequired: Boolean(r.vpn_required),
    tags: JSON.parse(r.tags || "[]"),
  };
}

function docRowToDoc(r: DocRow) {
  return {
    id: r.id,
    title: r.title,
    content: r.content,
    lastEditedBy: r.last_edited_by ?? "",
    updatedAt: r.updated_at,
    category: r.category,
  };
}

function chatRowToChat(r: ChatRow) {
  return {
    id: r.id,
    senderName: r.sender_name,
    text: r.text,
    timestamp: r.timestamp,
    vpnNode: r.vpn_node ?? "",
  };
}

export const createApp = ({ storePath }: AppOptions = {}): Express => {
  initDb();
  const app = express();
  app.use(express.json({ limit: "1mb" }));
  app.use(cookieParser());

  app.get("/api/health", (_req, res) => {
    res.json({ ok: true, db: "sqlite" });
  });

  app.post("/api/auth/register", async (req: Request, res: Response) => {
    const { email, password, name } = req.body as { email?: string; password?: string; name?: string };
    if (!email || !password || !name) {
      res.status(400).json({ error: "Email, password and name are required." });
      return;
    }
    if (findUserByEmail(email)) {
      res.status(409).json({ error: "Email already registered." });
      return;
    }
    const id = crypto.randomUUID();
    const passwordHash = await hashPassword(password);
    createUser(id, email, passwordHash, name);
    const token = signToken({ userId: id, email });
    res.cookie("token", token, { httpOnly: true, sameSite: "lax", secure: process.env.NODE_ENV === "production", maxAge: 7 * 24 * 60 * 60 * 1000, path: "/" });
    res.status(201).json({ ok: true, user: { id, email, name } });
  });

  app.post("/api/auth/login", async (req: Request, res: Response) => {
    const { email, password } = req.body as { email?: string; password?: string };
    const user = findUserByEmail(email ?? "");
    if (!user) { res.status(401).json({ error: "Invalid credentials." }); return; }
    const valid = await comparePassword(password ?? "", user.password_hash);
    if (!valid) { res.status(401).json({ error: "Invalid credentials." }); return; }
    const token = signToken({ userId: user.id, email: user.email });
    res.cookie("token", token, { httpOnly: true, sameSite: "lax", secure: process.env.NODE_ENV === "production", maxAge: 7 * 24 * 60 * 60 * 1000, path: "/" });
    res.json({ ok: true, user: { id: user.id, email: user.email, name: user.name, role: user.role } });
  });

  app.post("/api/auth/logout", (_req: Request, res: Response) => {
    res.clearCookie("token", { path: "/" });
    res.json({ ok: true });
  });

  app.get("/api/auth/me", requireAuth, (req: AuthRequest, res: Response) => {
    const user = findUserById(req.user!.userId);
    if (!user) { res.status(404).json({ error: "User not found." }); return; }
    res.json({ user: { id: user.id, email: user.email, name: user.name, role: user.role, avatar: user.avatar } });
  });

  app.get("/api/tasks", requireAuth, (_req: AuthRequest, res: Response) => {
    const rows = db.prepare("SELECT * FROM tasks WHERE user_id = ? ORDER BY updated_at DESC").all(uid(_req)) as TaskRow[];
    res.json(rows.map(taskRowToTask));
  });

  app.post("/api/tasks", requireAuth, (req: AuthRequest, res: Response) => {
    const { title, description, status, priority, assignee, dueDate, vpnRequired, tags } = req.body as Partial<{
      title: string; description: string; status: string; priority: string;
      assignee: { name?: string; role?: string }; dueDate: string; vpnRequired: boolean; tags: string[];
    }>;
    if (!title) { res.status(400).json({ error: "Title is required." }); return; }
    const id = crypto.randomUUID(); const now = Date.now();
    db.prepare(`INSERT INTO tasks (id, user_id, title, description, status, priority, assignee_name, assignee_role, due_date, vpn_required, tags, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`).run(id, uid(req), title, description ?? "", status ?? "backlog", priority ?? "medium", assignee?.name ?? null, assignee?.role ?? null, dueDate ?? null, vpnRequired ? 1 : 0, JSON.stringify(tags ?? []), now, now);
    const row = db.prepare("SELECT * FROM tasks WHERE id = ?").get(id) as TaskRow;
    res.status(201).json(taskRowToTask(row));
  });

  app.put("/api/tasks/:id", requireAuth, (req: AuthRequest, res: Response) => {
    const { id } = req.params;
    const fields = req.body as Partial<{
      title: string; description: string; status: string; priority: string;
      assignee: { name?: string; role?: string }; dueDate: string; vpnRequired: boolean; tags: string[];
    }>;
    const existing = db.prepare("SELECT * FROM tasks WHERE id = ? AND user_id = ?").get(id, uid(req)) as TaskRow | undefined;
    if (!existing) { res.status(404).json({ error: "Task not found." }); return; }
    const now = Date.now();
    db.prepare(`UPDATE tasks SET title = COALESCE(?, title), description = COALESCE(?, description), status = COALESCE(?, status), priority = COALESCE(?, priority), assignee_name = COALESCE(?, assignee_name), assignee_role = COALESCE(?, assignee_role), due_date = COALESCE(?, due_date), vpn_required = CASE WHEN ? IS NOT NULL THEN ? ELSE vpn_required END, tags = COALESCE(?, tags), updated_at = ? WHERE id = ? AND user_id = ?`).run(fields.title, fields.description, fields.status, fields.priority, fields.assignee?.name ?? null, fields.assignee?.role ?? null, fields.dueDate, fields.vpnRequired !== undefined ? 1 : null, fields.vpnRequired ? 1 : 0, fields.tags !== undefined ? JSON.stringify(fields.tags) : null, now, id, uid(req));
    const row = db.prepare("SELECT * FROM tasks WHERE id = ? AND user_id = ?").get(id, uid(req)) as TaskRow;
    res.json(taskRowToTask(row));
  });

  app.delete("/api/tasks/:id", requireAuth, (req: AuthRequest, res: Response) => {
    const { id } = req.params;
    const info = db.prepare("DELETE FROM tasks WHERE id = ? AND user_id = ?").run(id, uid(req));
    if (info.changes === 0) { res.status(404).json({ error: "Task not found." }); return; }
    res.json({ ok: true });
  });

  app.get("/api/docs", requireAuth, (_req: AuthRequest, res: Response) => {
    const rows = db.prepare("SELECT * FROM docs WHERE user_id = ? ORDER BY updated_at DESC").all(uid(_req)) as DocRow[];
    res.json(rows.map(docRowToDoc));
  });

  app.post("/api/docs", requireAuth, (req: AuthRequest, res: Response) => {
    const { title, content, category } = req.body as Partial<{ title: string; content: string; category: string }>;
    if (!title) { res.status(400).json({ error: "Title is required." }); return; }
    const id = crypto.randomUUID(); const now = Date.now();
    db.prepare(`INSERT INTO docs (id, user_id, title, content, last_edited_by, updated_at, category, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`).run(id, uid(req), title, content ?? "", "", now, category ?? "General", now);
    const row = db.prepare("SELECT * FROM docs WHERE id = ?").get(id) as DocRow;
    res.status(201).json(docRowToDoc(row));
  });

  app.put("/api/docs/:id", requireAuth, (req: AuthRequest, res: Response) => {
    const { id } = req.params;
    const { title, content, category, lastEditedBy } = req.body as Partial<DocRow & { lastEditedBy: string }>;
    const existing = db.prepare("SELECT * FROM docs WHERE id = ? AND user_id = ?").get(id, uid(req)) as DocRow | undefined;
    if (!existing) { res.status(404).json({ error: "Doc not found." }); return; }
    const now = Date.now();
    db.prepare(`UPDATE docs SET title = COALESCE(?, title), content = COALESCE(?, content), last_edited_by = COALESCE(?, last_edited_by), updated_at = ?, category = COALESCE(?, category) WHERE id = ? AND user_id = ?`).run(title, content, lastEditedBy, now, category, id, uid(req));
    const row = db.prepare("SELECT * FROM docs WHERE id = ? AND user_id = ?").get(id, uid(req)) as DocRow;
    res.json(docRowToDoc(row));
  });

  app.delete("/api/docs/:id", requireAuth, (req: AuthRequest, res: Response) => {
    const { id } = req.params;
    const info = db.prepare("DELETE FROM docs WHERE id = ? AND user_id = ?").run(id, uid(req));
    if (info.changes === 0) { res.status(404).json({ error: "Doc not found." }); return; }
    res.json({ ok: true });
  });

  app.get("/api/chat", requireAuth, (_req: AuthRequest, res: Response) => {
    const rows = db.prepare("SELECT * FROM chat_messages WHERE user_id = ? ORDER BY timestamp ASC").all(uid(_req)) as ChatRow[];
    res.json(rows.map(chatRowToChat));
  });

  app.post("/api/chat", requireAuth, (req: AuthRequest, res: Response) => {
    const { text, vpnNode } = req.body as Partial<{ text: string; vpnNode: string }>;
    if (!text) { res.status(400).json({ error: "Text is required." }); return; }
    const user = findUserById(uid(req));
    const id = crypto.randomUUID(); const now = Date.now();
    db.prepare(`INSERT INTO chat_messages (id, user_id, sender_name, text, timestamp, vpn_node, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`).run(id, uid(req), user?.name ?? "User", text, now, vpnNode ?? null, now);
    const row = db.prepare("SELECT * FROM chat_messages WHERE id = ?").get(id) as ChatRow;
    const message = chatRowToChat(row);
    broadcast(uid(req), { type: "chat:new", message });
    res.status(201).json(message);
  });

  return app;
};

