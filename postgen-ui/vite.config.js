import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/accounts': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/templates': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/generate': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/stats': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/publish': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/products': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/jobs': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/events': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/analytics': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/worker': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/settings': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/schedules': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      // Covers /deals, /deals/{asin} and /deals/discover. Without it these
      // fall through to the SPA fallback and the screen receives index.html
      // where it expected JSON - which only shows up in dev, since the
      // production build is served from the same origin as the API.
      '/deals': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      }
    }
  },
  build: {
    outDir: '../web',
    emptyOutDir: false,
    rollupOptions: {
      output: {
        entryFileNames: 'app.js',
        assetFileNames: (assetInfo) => {
          if (assetInfo.name && assetInfo.name.endsWith('.css')) {
            return 'styles.css';
          }
          return '[name].[ext]';
        },
        chunkFileNames: '[name].js',
      }
    }
  }
})


