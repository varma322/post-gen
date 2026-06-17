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


