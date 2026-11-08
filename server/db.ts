import Database from "better-sqlite3";
import path from "path";
import fs from "fs";

type SqlDB = InstanceType<typeof Database>;

const DB_PATH_DEFAULT = path.resolve(__dirname, "..", "data", "app.db");

let _dbInstance: SqlDB | null = null;
const resolveDbPath = () => process.env.DB_PATH ?? DB_PATH_DEFAULT;

const ensureDb = (): SqlDB => {
  if (!_dbInstance) {
    const dp = resolveDbPath();
    fs.mkdirSync(path.dirname(dp), { recursive: true });
    _dbInstance = new Database(dp);
    _dbInstance.pragma("journal_mode = WAL");
    _dbInstance.pragma("foreign_keys = ON");
  }
  return _dbInstance;
};

export const db = new Proxy({} as SqlDB, {
  get(_, prop: keyof SqlDB) {
    return ensureDb()[prop];
  },
}) as SqlDB;

export function initDb() {
  ensureDb();
  fs.mkdirSync(path.dirname(resolveDbPath()), { recursive: true });

  db.exec(`
    CREATE TABLE IF NOT EXISTS users (
      id TEXT PRIMARY KEY,
      email TEXT UNIQUE NOT NULL,
      password_hash TEXT NOT NULL,
      name TEXT NOT NULL,
      role TEXT NOT NULL DEFAULT 'Member',
      avatar TEXT,
      created_at INTEGER NOT NULL
    );

    CREATE TABLE IF NOT EXISTS tasks (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      title TEXT NOT NULL,
      description TEXT NOT NULL DEFAULT '',
      status TEXT NOT NULL DEFAULT 'backlog',
      priority TEXT NOT NULL DEFAULT 'medium',
      assignee_name TEXT,
      assignee_role TEXT,
      due_date TEXT,
      vpn_required INTEGER NOT NULL DEFAULT 0,
      tags TEXT NOT NULL DEFAULT '[]',
      created_at INTEGER NOT NULL,
      updated_at INTEGER NOT NULL
    );

    CREATE TABLE IF NOT EXISTS docs (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      title TEXT NOT NULL,
      content TEXT NOT NULL DEFAULT '',
      last_edited_by TEXT,
      updated_at INTEGER NOT NULL,
      category TEXT NOT NULL DEFAULT 'General',
      created_at INTEGER NOT NULL
    );

    CREATE TABLE IF NOT EXISTS chat_messages (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      sender_name TEXT NOT NULL,
      text TEXT NOT NULL,
      timestamp INTEGER NOT NULL,
      vpn_node TEXT,
      created_at INTEGER NOT NULL
    );

    CREATE INDEX IF NOT EXISTS idx_tasks_user ON tasks(user_id);
    CREATE INDEX IF NOT EXISTS idx_docs_user ON docs(user_id);
    CREATE INDEX IF NOT EXISTS idx_chat_user ON chat_messages(user_id);
  `);
}

// Row mappers
export interface UserRow {
  id: string;
  email: string;
  password_hash: string;
  name: string;
  role: string;
  avatar: string | null;
  created_at: number;
}

export interface TaskRow {
  id: string;
  user_id: string;
  title: string;
  description: string;
  status: string;
  priority: string;
  assignee_name: string | null;
  assignee_role: string | null;
  due_date: string | null;
  vpn_required: number;
  tags: string;
  created_at: number;
  updated_at: number;
}

export interface DocRow {
  id: string;
  user_id: string;
  title: string;
  content: string;
  last_edited_by: string | null;
  updated_at: number;
  category: string;
  created_at: number;
}

export interface ChatRow {
  id: string;
  user_id: string;
  sender_name: string;
  text: string;
  timestamp: number;
  vpn_node: string | null;
  created_at: number;
}