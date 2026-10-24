/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // 主色调 - 明亮鹅黄色；深色阶用于小号文字和高对比交互态
        primary: {
          50: '#fffbe8',
          100: '#fff3bd',
          200: '#ffe78a',
          300: '#f8d65d',
          400: '#f0c43d',
          500: '#e9b824',
          600: '#976800',
          700: '#7e5600',
          800: '#65460b',
          900: '#523a0d',
          950: '#302204'
        },
        // 辅助色 - 更有生命力的莫奈花园绿，用于成长与成功语义
        accent: {
          50: '#f2f8ef',
          100: '#e4f0df',
          200: '#c7dfc0',
          300: '#a5cc9f',
          400: '#79b37b',
          500: '#5c9865',
          600: '#477b54',
          700: '#386344',
          800: '#2e5038',
          900: '#284331',
          950: '#14251a'
        },
        // 将默认冷灰统一为暖中性色，让现有页面无需逐个重写也能融入主题
        gray: {
          50: '#fffdf6',
          100: '#faf6e8',
          200: '#eee5c9',
          300: '#d7caa2',
          400: '#9b8c62',
          500: '#716744',
          600: '#5b5238',
          700: '#48402d',
          800: '#342f24',
          900: '#27231b',
          950: '#17140f'
        },
        // 兼容旧有 dark-* 工具类，但仍保持浅色画布
        dark: {
          50: '#fffef8',
          100: '#fffbe8',
          200: '#fff3bd',
          300: '#f8d65d',
          400: '#6b6045',
          500: '#716443',
          600: '#d7c16a',
          700: '#e8d98f',
          800: '#fff3bd',
          900: '#fffef8',
          950: '#fffbe8'
        }
      },
      fontFamily: {
        sans: [
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 1px 2px rgba(63, 52, 32, 0.08)',
        'glass-sm': '0 1px 2px rgba(63, 52, 32, 0.06)',
        glow: '0 0 0 transparent',
        'glow-lg': '0 0 0 transparent',
        card: '0 1px 2px rgba(63, 52, 32, 0.04), 0 8px 24px -12px rgba(63, 52, 32, 0.16)',
        'card-hover': '0 2px 6px -2px rgba(63, 52, 32, 0.08), 0 18px 36px -16px rgba(63, 52, 32, 0.26)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': '#e9b824',
        'gradient-dark': '#fff3bd',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 20%, rgba(233, 184, 36, 0.18) 0px, transparent 50%), radial-gradient(at 80% 0%, rgba(92, 152, 101, 0.13) 0px, transparent 50%), radial-gradient(at 0% 50%, rgba(185, 78, 55, 0.1) 0px, transparent 50%)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 20px rgba(233, 184, 36, 0.25)' },
          '100%': { boxShadow: '0 0 30px rgba(233, 184, 36, 0.4)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '2rem',
        '5xl': '2.5rem'
      }
    }
  },
  plugins: []
}
