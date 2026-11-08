import React from 'react';
import { Palette, X, Check } from 'lucide-react';

interface ThemeCustomizerProps {
  isOpen: boolean;
  onClose: () => void;
  accentColor: string;
  setAccentColor: (color: string) => void;
  bgTheme: string;
  setBgTheme: (theme: string) => void;
}

export const ACCENT_COLORS = [
  { id: 'indigo', label: 'Indigo', bgClass: 'bg-indigo-600' },
  { id: 'emerald', label: 'Emerald', bgClass: 'bg-emerald-600' },
  { id: 'blue', label: 'Cyber Blue', bgClass: 'bg-blue-600' },
  { id: 'amber', label: 'Amber', bgClass: 'bg-amber-600' },
  { id: 'violet', label: 'Violet', bgClass: 'bg-violet-600' },
];

export const BG_THEMES = [
  { id: 'midnight', label: 'Midnight Dark', desc: 'Deep slate dark mode' },
  { id: 'arctic', label: 'Arctic Clean', desc: 'Crisp minimalist light mode' },
  { id: 'navy', label: 'Corporate Navy', desc: 'Deep professional corporate blue' },
  { id: 'obsidian', label: 'Obsidian OLED', desc: 'Pure black stealth mode' },
];

export const ThemeCustomizer: React.FC<ThemeCustomizerProps> = ({
  isOpen,
  onClose,
  accentColor,
  setAccentColor,
  bgTheme,
  setBgTheme,
}) => {
  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-fadeIn">
      <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-800 max-w-md w-full p-6 shadow-2xl space-y-6">
        <div className="flex items-center justify-between pb-4 border-b border-slate-200 dark:border-slate-800">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-xl bg-brand-600 flex items-center justify-center text-white shadow-sm">
              <Palette className="w-4 h-4" />
            </div>
            <h3 className="text-base font-bold text-slate-900 dark:text-white">Waypoint Workspace Theme</h3>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="space-y-6">
          {/* Accent Color Section */}
          <div className="space-y-3">
            <label className="block text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              UI Accent Color
            </label>
            <div className="grid grid-cols-5 gap-2.5">
              {ACCENT_COLORS.map(color => {
                const isSelected = accentColor === color.id;
                return (
                  <button
                    key={color.id}
                    onClick={() => setAccentColor(color.id)}
                    className={`flex flex-col items-center gap-1.5 p-3 rounded-xl border transition-all ${
                      isSelected
                        ? 'border-brand-600 bg-brand-50 dark:bg-brand-950/40 shadow-sm'
                        : 'border-slate-200 dark:border-slate-800 hover:border-slate-300 dark:hover:border-slate-700'
                    }`}
                  >
                    <div className={`w-6 h-6 rounded-full ${color.bgClass} flex items-center justify-center text-white`}>
                      {isSelected && <Check className="w-3.5 h-3.5" />}
                    </div>
                    <span className="text-[10px] font-medium text-slate-700 dark:text-slate-300">{color.label}</span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Background Theme Presets */}
          <div className="space-y-3">
            <label className="block text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Background Theme Presets
            </label>
            <div className="grid grid-cols-1 gap-2.5">
              {BG_THEMES.map(theme => {
                const isSelected = bgTheme === theme.id;
                return (
                  <div
                    key={theme.id}
                    onClick={() => setBgTheme(theme.id)}
                    className={`p-3.5 rounded-xl border cursor-pointer transition-all flex items-center justify-between ${
                      isSelected
                        ? 'border-brand-600 bg-brand-50/60 dark:bg-brand-950/40 shadow-sm'
                        : 'border-slate-200 dark:border-slate-800 hover:border-slate-300 dark:hover:border-slate-700 bg-slate-50 dark:bg-slate-800/40'
                    }`}
                  >
                    <div>
                      <h4 className="text-xs font-bold text-slate-900 dark:text-white">{theme.label}</h4>
                      <p className="text-[11px] text-slate-500 dark:text-slate-400 mt-0.5">{theme.desc}</p>
                    </div>
                    {isSelected && (
                      <div className="w-5 h-5 rounded-full bg-brand-600 text-white flex items-center justify-center">
                        <Check className="w-3 h-3" />
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        </div>

        <div className="pt-4 border-t border-slate-200 dark:border-slate-800 flex justify-end">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-xl bg-brand-600 text-white text-xs font-semibold hover:bg-brand-500 transition-colors shadow-sm"
          >
            Apply Theme
          </button>
        </div>
      </div>
    </div>
  );
};

