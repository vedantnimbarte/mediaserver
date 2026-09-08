/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        // Netflix's shell: a near-black that is warmer than pure #000, so artwork and
        // white text both sit on it without the harsh edge true black gives.
        ink: {
          950: '#000000',
          900: '#141414', // the canonical Netflix background
          850: '#181818', // cards and rows
          800: '#1f1f1f',
          700: '#2a2a2a',
          600: '#333333',
          500: '#4d4d4d',
          400: '#808080',
          300: '#b3b3b3', // Netflix's secondary text
          200: '#e5e5e5', // Netflix's primary text
          100: '#f5f5f5',
        },
        // The accent is driven by a CSS variable so a user can recolour the whole app
        // from Appearance settings without a rebuild.
        accent: {
          DEFAULT: 'rgb(var(--accent) / <alpha-value>)',
          soft: 'rgb(var(--accent-soft) / <alpha-value>)',
          dim: 'rgb(var(--accent-dim) / <alpha-value>)',
        },
        netflix: {
          red: '#e50914',
          dark: '#b20710',
        },
      },
      fontFamily: {
        sans: [
          'Inter',
          'Netflix Sans',
          'Helvetica Neue',
          'Segoe UI',
          'system-ui',
          '-apple-system',
          'sans-serif',
        ],
      },
      letterSpacing: {
        wordmark: '-0.04em',
      },
      boxShadow: {
        // The lift under an expanded card, which is what sells the hover interaction.
        card: '0 8px 24px rgba(0, 0, 0, 0.7)',
        hero: '0 24px 60px rgba(0, 0, 0, 0.85)',
        menu: '0 12px 32px rgba(0, 0, 0, 0.8)',
      },
      transitionTimingFunction: {
        // Netflix's cards ease out fast then settle, rather than moving linearly.
        card: 'cubic-bezier(0.2, 0.6, 0.3, 1)',
      },
      animation: {
        'fade-in': 'fadeIn 200ms ease-out',
        'slide-up': 'slideUp 220ms cubic-bezier(0.2, 0.6, 0.3, 1)',
        'scale-in': 'scaleIn 160ms cubic-bezier(0.2, 0.6, 0.3, 1)',
        shimmer: 'shimmer 1.6s ease-in-out infinite',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(12px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.96)' },
          '100%': { opacity: '1', transform: 'scale(1)' },
        },
        shimmer: {
          '0%, 100%': { opacity: '0.4' },
          '50%': { opacity: '0.75' },
        },
      },
      zIndex: {
        hovercard: '40',
        bar: '50',
        modal: '60',
        toast: '70',
      },
    },
  },
  plugins: [],
}
