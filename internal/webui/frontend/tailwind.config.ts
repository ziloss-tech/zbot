import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        surface: {
          950: '#060608',
          900: '#0c0d11',
          800: '#13141a',
          700: '#1c1d26',
          600: '#252631',
          500: '#323340',
        },
        // Real model brand colors
        openai: {
          DEFAULT: '#10a37f',
          dim: '#0d8a6b',
          glow: '#1dc9a0',
          bg: 'rgba(16,163,127,0.08)',
        },
        anthropic: {
          DEFAULT: '#d97757',
          dim: '#c4623e',
          glow: '#f0977a',
          bg: 'rgba(217,119,87,0.08)',
        },
        gemini: {
          DEFAULT: '#8ab4f8',
          dim: '#6d9ef5',
          glow: '#aecbfb',
          bg: 'rgba(138,180,248,0.08)',
        },
        observer: {
          DEFAULT: '#a78bfa',
          dim: '#8b5cf6',
          glow: '#c4b5fd',
          bg: 'rgba(167,139,250,0.08)',
        },
        // Keep legacy aliases for existing components
        planner: {
          DEFAULT: '#10a37f',
          dim: '#0d8a6b',
          glow: '#1dc9a0',
        },
        executor: {
          DEFAULT: '#d97757',
          dim: '#c4623e',
          glow: '#f0977a',
        },
      },
      fontFamily: {
        mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace'],
        display: ['"Inter"', 'system-ui', 'sans-serif'],
        sans: ['"Inter"', 'system-ui', 'sans-serif'],
      },
      backgroundImage: {
        'glass': 'linear-gradient(135deg, rgba(255,255,255,0.03) 0%, rgba(255,255,255,0.01) 100%)',
        'glass-hover': 'linear-gradient(135deg, rgba(255,255,255,0.05) 0%, rgba(255,255,255,0.02) 100%)',
      },
      boxShadow: {
        'glass': '0 1px 0 0 rgba(255,255,255,0.04) inset, 0 -1px 0 0 rgba(0,0,0,0.2) inset',
        'openai': '0 0 20px rgba(16,163,127,0.12)',
        'anthropic': '0 0 20px rgba(217,119,87,0.12)',
        'gemini': '0 0 20px rgba(138,180,248,0.12)',
        'observer': '0 0 20px rgba(167,139,250,0.12)',
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'cursor-blink': 'blink 0.9s step-end infinite',
        'glow-openai': 'glowOpenai 2s ease-in-out infinite alternate',
        'glow-anthropic': 'glowAnthropic 2s ease-in-out infinite alternate',
      },
      keyframes: {
        blink: {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0' },
        },
        glowOpenai: {
          '0%': { boxShadow: '0 0 8px rgba(16,163,127,0.15)' },
          '100%': { boxShadow: '0 0 24px rgba(16,163,127,0.35)' },
        },
        glowAnthropic: {
          '0%': { boxShadow: '0 0 8px rgba(217,119,87,0.15)' },
          '100%': { boxShadow: '0 0 24px rgba(217,119,87,0.35)' },
        },
      },
    },
  },
  plugins: [],
} satisfies Config
