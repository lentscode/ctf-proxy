import { spawn } from 'node:child_process'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const controlAddress = '127.0.0.1:18081'
const controlURL = `http://${controlAddress}`
const dashboardPort = 4173
const token = 'e2e-token'
const temporaryDirectory = await mkdtemp(join(tmpdir(), 'ctf-proxy-e2e-'))
const configPath = join(temporaryDirectory, 'ctf-proxy.yaml')
const tokensPath = join(temporaryDirectory, '.tokens')
const binaryPath = join(temporaryDirectory, 'ctf-proxy')

let controlProcess
let viteProcess

function command(name, args, options = {}) {
  return spawn(name, args, { stdio: 'inherit', ...options })
}

function waitForExit(child) {
  return new Promise((resolve) => child.once('exit', resolve))
}

async function stop(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return
  child.kill('SIGTERM')
  await Promise.race([waitForExit(child), delay(5_000)])
  if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL')
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

async function waitForControl() {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    if (controlProcess.exitCode !== null || controlProcess.signalCode !== null) {
      throw new Error('ctf-proxy stopped before its control API became ready')
    }
    try {
      const response = await fetch(`${controlURL}/healthz`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (response.ok) return
    } catch {
      // The listener is not ready yet.
    }
    await delay(100)
  }
  throw new Error('timed out waiting for the ctf-proxy control API')
}

async function cleanup() {
  await Promise.all([stop(viteProcess), stop(controlProcess)])
  await rm(temporaryDirectory, { recursive: true, force: true })
}

try {
  await writeFile(configPath, 'version: 1\nproxies: []\n', { mode: 0o600 })
  await writeFile(tokensPath, `${token}\n`, { mode: 0o600 })

  const build = command('go', ['build', '-o', binaryPath, './cmd/ctf-proxy'])
  if (await waitForExit(build) !== 0) throw new Error('could not build ctf-proxy for E2E tests')

  controlProcess = command(binaryPath, [], {
    env: {
      ...process.env,
      CTF_PROXY_CONFIG: configPath,
      CTF_PROXY_CONTROL_ADDR: controlAddress,
      CTF_PROXY_TOKENS_FILE: tokensPath,
    },
  })
  await waitForControl()

  viteProcess = command(process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm', [
    'exec', 'vite', '--host', '127.0.0.1', '--port', String(dashboardPort), '--strictPort',
  ], {
    env: { ...process.env, VITE_CONTROL_ORIGIN: controlURL },
  })

  const signal = await new Promise((resolve) => {
    process.once('SIGINT', () => resolve('SIGINT'))
    process.once('SIGTERM', () => resolve('SIGTERM'))
    viteProcess.once('exit', () => resolve('vite exited'))
    controlProcess.once('exit', () => resolve('ctf-proxy exited'))
  })
  if (signal === 'vite exited' || signal === 'ctf-proxy exited') {
    throw new Error(`E2E service stopped unexpectedly: ${signal}`)
  }
} finally {
  await cleanup()
}
