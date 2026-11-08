import { sign, verify, type JwtPayload } from "jsonwebtoken";
import { compare, hash } from "bcryptjs";
import { db, type UserRow } from "./db";

const JWT_SECRET = process.env.JWT_SECRET ?? "dev-secret-change-me";
const JWT_EXPIRES_IN = "7d";

export interface TokenPayload {
  userId: string;
  email: string;
}

export function hashPassword(password: string): Promise<string> {
  return hash(password, 10);
}

export function comparePassword(plain: string, hashValue: string): Promise<boolean> {
  return compare(plain, hashValue);
}

export function signToken(payload: TokenPayload): string {
  return sign(payload, JWT_SECRET, { expiresIn: JWT_EXPIRES_IN, subject: payload.userId });
}

export function verifyToken(token: string): TokenPayload | null {
  try {
    const decoded = verify(token, JWT_SECRET) as JwtPayload & TokenPayload;
    return { userId: decoded.userId, email: decoded.email };
  } catch {
    return null;
  }
}

export function findUserByEmail(email: string): UserRow | undefined {
  const row = db
    .prepare("SELECT * FROM users WHERE email = ?")
    .get(email) as UserRow | undefined;
  return row;
}

export function findUserById(id: string): UserRow | undefined {
  const row = db.prepare("SELECT * FROM users WHERE id = ?").get(id) as UserRow | undefined;
  return row;
}

export function createUser(id: string, email: string, passwordHash: string, name: string): UserRow {
  const now = Date.now();
  db.prepare(
    "INSERT INTO users (id, email, password_hash, name, role, avatar, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
  ).run(id, email, passwordHash, name, "Member", null, now);
  return findUserById(email) ?? findUserById(id)!;
}