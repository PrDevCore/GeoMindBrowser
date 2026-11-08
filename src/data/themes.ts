export const BRAND_SHADES = [50, 100, 200, 400, 500, 600, 800, 900, 950] as const;

export type BrandShade = (typeof BRAND_SHADES)[number];

// Values copied from the Tailwind palette scale so the brand tokens match the
// shades they replaced. Applied at runtime as --brand-<shade> custom properties.
export const ACCENT_PALETTES: Record<string, Record<BrandShade, string>> = {
  indigo: {
    50: 'oklch(96.2% 0.018 272.314)',
    100: 'oklch(93% 0.034 272.788)',
    200: 'oklch(87% 0.065 274.039)',
    400: 'oklch(67.3% 0.182 276.935)',
    500: 'oklch(58.5% 0.233 277.117)',
    600: 'oklch(51.1% 0.262 276.966)',
    800: 'oklch(39.8% 0.195 277.366)',
    900: 'oklch(35.9% 0.144 278.697)',
    950: 'oklch(25.7% 0.09 281.288)',
  },
  emerald: {
    50: 'oklch(97.9% 0.021 166.113)',
    100: 'oklch(95% 0.052 163.051)',
    200: 'oklch(90.5% 0.093 164.15)',
    400: 'oklch(76.5% 0.177 163.223)',
    500: 'oklch(69.6% 0.17 162.48)',
    600: 'oklch(59.6% 0.145 163.225)',
    800: 'oklch(43.2% 0.095 166.913)',
    900: 'oklch(37.8% 0.077 168.94)',
    950: 'oklch(26.2% 0.051 172.552)',
  },
  blue: {
    50: 'oklch(97% 0.014 254.604)',
    100: 'oklch(93.2% 0.032 255.585)',
    200: 'oklch(88.2% 0.059 254.128)',
    400: 'oklch(70.7% 0.165 254.624)',
    500: 'oklch(62.3% 0.214 259.815)',
    600: 'oklch(54.6% 0.245 262.881)',
    800: 'oklch(42.4% 0.199 265.638)',
    900: 'oklch(37.9% 0.146 265.522)',
    950: 'oklch(28.2% 0.091 267.935)',
  },
  amber: {
    50: 'oklch(98.7% 0.022 95.277)',
    100: 'oklch(96.2% 0.059 95.617)',
    200: 'oklch(92.4% 0.12 95.746)',
    400: 'oklch(82.8% 0.189 84.429)',
    500: 'oklch(76.9% 0.188 70.08)',
    600: 'oklch(66.6% 0.179 58.318)',
    800: 'oklch(47.3% 0.137 46.201)',
    900: 'oklch(41.4% 0.112 45.904)',
    950: 'oklch(27.9% 0.077 45.635)',
  },
  violet: {
    50: 'oklch(96.9% 0.016 293.756)',
    100: 'oklch(94.3% 0.029 294.588)',
    200: 'oklch(89.4% 0.057 293.283)',
    400: 'oklch(70.2% 0.183 293.541)',
    500: 'oklch(60.6% 0.25 292.717)',
    600: 'oklch(54.1% 0.281 293.009)',
    800: 'oklch(43.2% 0.232 292.759)',
    900: 'oklch(38% 0.189 293.745)',
    950: 'oklch(28.3% 0.141 291.089)',
  },
};

export type AccentColor = keyof typeof ACCENT_PALETTES;

export const applyAccentPalette = (accent: string) => {
  const palette = ACCENT_PALETTES[accent] ?? ACCENT_PALETTES.indigo;
  const root = document.documentElement;
  for (const shade of BRAND_SHADES) {
    root.style.setProperty(`--brand-${shade}`, palette[shade]);
  }
};

const THEME_STORAGE_KEY = 'waypoint.theme';
const BG_THEMES = ['midnight', 'arctic', 'navy', 'obsidian'];
const DEFAULT_THEME = { accentColor: 'indigo', bgTheme: 'midnight' };

export interface StoredTheme {
  accentColor: string;
  bgTheme: string;
}

export const readStoredTheme = (): StoredTheme => {
  try {
    const raw = localStorage.getItem(THEME_STORAGE_KEY);
    if (!raw) return DEFAULT_THEME;
    const stored = JSON.parse(raw) as Partial<StoredTheme>;
    return {
      accentColor: stored.accentColor && ACCENT_PALETTES[stored.accentColor] ? stored.accentColor : DEFAULT_THEME.accentColor,
      bgTheme: stored.bgTheme && BG_THEMES.includes(stored.bgTheme) ? stored.bgTheme : DEFAULT_THEME.bgTheme,
    };
  } catch {
    return DEFAULT_THEME;
  }
};

export const writeStoredTheme = (theme: StoredTheme) => {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify(theme));
  } catch {
    // Private-mode or quota failures only affect persistence, not the running theme.
  }
};

