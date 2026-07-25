import { defineConfig } from 'vite'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import babel from '@rolldown/plugin-babel'
import tailwindcss from '@tailwindcss/vite'

const controlTarget = process.env.VITE_CONTROL_ORIGIN ?? 'http://127.0.0.1:8081'
const dashboardAddress = process.env.CTF_PROXY_DASHBOARD_ADDR

function dashboardServerOptions() {
  if (!dashboardAddress) return {}

  let address: URL
  try {
    address = new URL(`http://${dashboardAddress}`)
  } catch {
    throw new Error('CTF_PROXY_DASHBOARD_ADDR must use host:port form')
  }
  const host = address.hostname.replace(/^\[(.*)\]$/, '$1')
  const port = Number(address.port)
  if (!host || !address.port || address.username || address.password || address.pathname !== '/' || address.search || address.hash) {
    throw new Error('CTF_PROXY_DASHBOARD_ADDR must use host:port form')
  }
  if (host === '0.0.0.0' || host === '::') {
    throw new Error('CTF_PROXY_DASHBOARD_ADDR must name a specific host or IP')
  }
  if (!Number.isInteger(port) || port < 1 || port > 65_535) {
    throw new Error('CTF_PROXY_DASHBOARD_ADDR must use a port from 1 through 65535')
  }
  return { host, port, strictPort: true }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    babel({ presets: [reactCompilerPreset()] }),
    tailwindcss(),
  ],
  build: {
    outDir: 'cmd/ctf-proxy/dashboard/dist',
    emptyOutDir: true,
  },
  server: {
	...dashboardServerOptions(),
    proxy: {
      '/api': controlTarget,
      '/healthz': controlTarget,
    },
  },
})
