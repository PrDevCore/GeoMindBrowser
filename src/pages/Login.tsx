import React, { useState } from "react";
import { useAuth } from "../contexts/AuthContext";
import { LogIn, Mail, Lock, User, ArrowLeft } from "lucide-react";

interface AuthFormProps {
  onSwitch: () => void;
}

function LoginForm({ onSwitch }: AuthFormProps) {
  const { login } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      await login(email, password);
    } catch (err: any) {
      setError(err.message || "Login failed.");
    }
  };

  return (
    <div className="w-full max-w-md space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold text-slate-900 dark:text-white">Welcome back</h2>
        <button onClick={onSwitch} className="text-xs text-brand-600 dark:text-brand-400 underline">
          Create an account
        </button>
      </div>
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-2">
          <div className="flex items-center gap-3 pl-2 border-l border-brand-500">
            <Mail className="w-5 h-5 text-slate-400" />
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="email@example.com"
              className="flex-1 bg-transparent text-slate-900 dark:text-white placeholder-slate-500 focus:outline-none"
            />
          </div>
          <div className="flex items-center gap-3 pl-2 border-l border-brand-500">
            <Lock className="w-5 h-5 text-slate-400" />
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="â€¢â€¢â€¢â€¢â€¢â€¢â€¢â€¢"
              className="flex-1 bg-transparent text-slate-900 dark:text-white placeholder-slate-500 focus:outline-none"
            />
          </div>
        </div>
        {error && <p className="text-xs text-red-500 dark:text-red-400">{error}</p>}
        <button
          type="submit"
          className="w-full px-4 py-2 rounded-xl bg-brand-600 text-white text-sm font-semibold hover:bg-brand-500 transition-colors"
        >
          Sign in
        </button>
      </form>
    </div>
  );
}

function RegisterForm({ onSwitch }: AuthFormProps) {
  const { register } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      await register(email, password, name);
    } catch (err: any) {
      setError(err.message || "Registration failed.");
    }
  };

  return (
    <div className="w-full max-w-md space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold text-slate-900 dark:text-white">Create account</h2>
        <button onClick={onSwitch} className="text-xs text-brand-600 dark:text-brand-400 underline">
          Already have an account?
        </button>
      </div>
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-2">
          <div className="flex items-center gap-3 pl-2 border-l border-brand-500">
            <User className="w-5 h-5 text-slate-400" />
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Full name"
              className="flex-1 bg-transparent text-slate-900 dark:text-white placeholder-slate-500 focus:outline-none"
            />
          </div>
          <div className="flex items-center gap-3 pl-2 border-l border-brand-500">
            <Mail className="w-5 h-5 text-slate-400" />
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="email@example.com"
              className="flex-1 bg-transparent text-slate-900 dark:text-white placeholder-slate-500 focus:outline-none"
            />
          </div>
          <div className="flex items-center gap-3 pl-2 border-l border-brand-500">
            <Lock className="w-5 h-5 text-slate-400" />
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="â€¢â€¢â€¢â€¢â€¢â€¢â€¢â€¢"
              className="flex-1 bg-transparent text-slate-900 dark:text-white placeholder-slate-500 focus:outline-none"
            />
          </div>
        </div>
        {error && <p className="text-xs text-red-500 dark:text-red-400">{error}</p>}
        <button
          type="submit"
          className="w-full px-4 py-2 rounded-xl bg-brand-600 text-white text-sm font-semibold hover:bg-brand-500 transition-colors"
        >
          Create account
        </button>
      </form>
    </div>
  );
}

export default function Login() {
  const [registerMode, setRegisterMode] = useState(false);

  return (
    <div className="min-h-screen flex items-center justify-center bg-slate-50 dark:bg-slate-950 p-4">
      <div className="w-full max-w-md bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-800 p-8 shadow-xl">
        <div className="flex items-center gap-2 mb-8">
          <LogIn className="w-6 h-6 text-brand-600 dark:text-brand-400" />
          <span className="text-xl font-bold text-slate-900 dark:text-white">Waypoint</span>
        </div>
        {registerMode ? <RegisterForm onSwitch={() => setRegisterMode(false)} /> : <LoginForm onSwitch={() => setRegisterMode(true)} />}
      </div>
    </div>
  );
}
