import React, { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { apiJson } from "../lib/api";

interface User {
  id: string;
  email: string;
  name: string;
  role: string;
  avatar?: string | null;
}

interface AuthContextType {
  user: User | null;
  loading: boolean;
  register: (email: string, password: string, name: string) => Promise<void>;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within an AuthProvider");
  return ctx;
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await apiJson("/api/auth/me");
        if (!cancelled && data?.user) setUser(data.user);
      } catch {
        if (!cancelled) setUser(null);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const register = async (email: string, password: string, name: string) => {
    const data = await apiJson("/api/auth/register", "POST", { email, password, name });
    setUser({ id: data.user.id, email: data.user.email, name: data.user.name, role: data.user.role ?? "Member" });
  };

  const login = async (email: string, password: string) => {
    const data = await apiJson("/api/auth/login", "POST", { email, password });
    setUser({ id: data.user.id, email: data.user.email, name: data.user.name, role: data.user.role ?? "Member" });
  };

  const logout = async () => {
    try {
      await apiJson("/api/auth/logout", "POST");
    } catch {}
    setUser(null);
  };

  return (
    <AuthContext.Provider value={{ user, loading, register, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}