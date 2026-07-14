/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './index.html',
    './index.tsx',
    './App.tsx',
    './pages/**/*.{ts,tsx}',
    './components/**/*.{ts,tsx}',
    './lib/**/*.{ts,tsx}',
  ],
  theme: {
    extend: {
      fontFamily: {
        // Geist gives the UI character without shouting; mono carries data + logs.
        sans: [
          "'Geist Variable'", 'ui-sans-serif', 'system-ui', '-apple-system',
          'Segoe UI', 'Roboto', 'Helvetica Neue', 'Arial', 'sans-serif',
        ],
        mono: [
          "'Geist Mono Variable'", 'ui-monospace', 'SFMono-Regular', 'Menlo',
          'Monaco', 'Consolas', 'monospace',
        ],
      },
      colors: {
        // ── Dark operator-console palette (the overhaul). Near-black surfaces,
        // hairline dividers, mono-forward data, teal interactive accent, semantic
        // status colors. Referenced as bg-bg / text-ink / border-line / text-acc …
        bg: '#0a0b0f',
        panel: '#0e1016',
        panel2: '#0c0e13',
        tree: '#0b0c11',
        line: 'rgba(255,255,255,0.055)',
        line2: 'rgba(255,255,255,0.10)',
        ink: '#eef1f6',
        ink2: '#c8cdd8',
        mut: '#949cad',
        dim: '#606978',
        faint: '#3a4150',
        acc: '#4de0c8',
        acc2: '#7fe6d4',
        ok: '#3ad07f',
        changed: '#e0b23a',
        amber: '#e0b23a',
        err: '#f2685f',
        run: '#5aa2ff',
        unreach: '#b06bff',
        violet: '#b06bff',
        grp: '#8fb4ff',
        cut: '#081210',

        // Single accent: a deep, desaturated cobalt ("Prussian") — authoritative,
        // premium, and safe for white-on-accent buttons + accent-on-white links.
        brand: {
          50: '#eef2ff',
          100: '#dfe6ff',
          200: '#c3d0fe',
          300: '#9db2fb',
          400: '#6f8bf5',
          500: '#4763e6',
          600: '#3049c9',
          700: '#2739a3',
          800: '#243284',
          900: '#232f6b',
        },
      },
      boxShadow: {
        // Tinted to the cool neutral hue rather than pure black — softer, less sterile.
        xs: '0 1px 2px 0 rgb(24 27 40 / 0.06)',
        sm: '0 1px 2px 0 rgb(24 27 40 / 0.06), 0 1px 3px 0 rgb(24 27 40 / 0.08)',
        DEFAULT: '0 2px 4px -1px rgb(24 27 40 / 0.07), 0 4px 8px -2px rgb(24 27 40 / 0.08)',
        md: '0 4px 8px -2px rgb(24 27 40 / 0.08), 0 8px 16px -4px rgb(24 27 40 / 0.08)',
        lg: '0 12px 24px -6px rgb(24 27 40 / 0.12), 0 4px 8px -4px rgb(24 27 40 / 0.08)',
        xl: '0 24px 48px -12px rgb(24 27 40 / 0.18)',
        // Accent-tinted glow for primary emphasis, used sparingly.
        brand: '0 6px 16px -4px rgb(48 73 201 / 0.35)',
      },
    },
  },
  plugins: [],
};
