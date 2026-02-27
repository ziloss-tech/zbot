import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        surface: {
          900: '#0a0b0d',
          800: '#111218',
          700: '#1a1b23',
          600: '#24252f',
        },
        planner: {
          DEFAULT: '#6366f1',
          dim: '#4f46e5',
          glow: '#818cf8',
        },
        executor: {
          DEFAULT: '#f59e0b',
          dim: '#d97706',
          glow: '#fbbf24',
        },
      },
      fontFamily: {
        mono: ['"JetBrains Mono"', '"Geist Mono"', 'ui-monospace', 'monospace'],
        display: ['"Syne"', '"DM Serif Display"', 'serif'],
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'cursor-blink': 'blink 1s step-end infinite',
      },
      keyframes: {
        blink: {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0' },
        },
      },
    },
  },
  plugins: [],
} satisfies Config
