import { spawn } from 'node:child_process'
import { chmod, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const controlAddress = '127.0.0.1:18081'
const controlURL = `http://${controlAddress}`
const token = 'e2e-token'
const temporaryDirectory = await mkdtemp(join(tmpdir(), 'ctf-proxy-e2e-'))
const configPath = join(temporaryDirectory, 'ctf-proxy.yaml')
const tokensPath = join(temporaryDirectory, '.tokens')
const binaryPath = join(temporaryDirectory, 'ctf-proxy')
const composeRoot = join(temporaryDirectory, 'services')
const fakeBinDirectory = join(temporaryDirectory, 'bin')
const fakeDockerPath = join(fakeBinDirectory, 'docker')
const fakeDockerLog = join(temporaryDirectory, 'docker.log')

let controlProcess

// command starts a child process with inherited terminal output.
function command(name, args, options = {}) {
  return spawn(name, args, { stdio: 'inherit', ...options })
}

// waitForExit resolves with a child process's exit code.
function waitForExit(child) {
  return new Promise((resolve) => child.once('exit', resolve))
}

// stop asks a child to exit cleanly, then applies a hard timeout fallback.
async function stop(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return
  child.kill('SIGTERM')
  await Promise.race([waitForExit(child), delay(5_000)])
  if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL')
}

// delay creates a timer promise for polling and shutdown waits.
function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

// waitForControl polls the authenticated health endpoint until startup completes.
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

// cleanup stops the embedded-dashboard binary and removes its temporary state.
async function cleanup() {
  await stop(controlProcess)
  await rm(temporaryDirectory, { recursive: true, force: true })
}

try {
  await writeFile(configPath, 'version: 1\nmetrics:\n  competition_start: "2026-01-01T00:00:00Z"\n  round_duration: 2m\n  retention_rounds: 720\nproxies: []\n', { mode: 0o600 })
  await writeFile(tokensPath, `${token}\n`, { mode: 0o600 })
  await mkdir(join(composeRoot, 'demo'), { recursive: true })
  await mkdir(fakeBinDirectory, { recursive: true })
  await writeFile(join(composeRoot, 'demo', 'compose.yaml'), 'services:\n  web:\n    image: example\n    ports:\n      - "18080:80"\n', { mode: 0o600 })
  await writeFile(fakeDockerPath, `#!/bin/sh
set -eu
printf '%s\\n' "$*" >> "$CTF_PROXY_FAKE_DOCKER_LOG"
[ "$1" = "compose" ] || exit 2
exit 0
`)
  await chmod(fakeDockerPath, 0o755)

  const frontendBuild = command(process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm', ['run', 'build:frontend'])
  if (await waitForExit(frontendBuild) !== 0) throw new Error('could not build dashboard for E2E tests')

  const build = command('go', ['build', '-tags', 'production', '-o', binaryPath, './cmd/ctf-proxy'])
  if (await waitForExit(build) !== 0) throw new Error('could not build ctf-proxy for E2E tests')

  controlProcess = command(binaryPath, [], {
    env: {
      ...process.env,
      CTF_PROXY_CONFIG: configPath,
      CTF_PROXY_CONTROL_ADDR: controlAddress,
      CTF_PROXY_TOKENS_FILE: tokensPath,
      CTF_PROXY_COMPOSE_ROOT: composeRoot,
      CTF_PROXY_FAKE_DOCKER_LOG: fakeDockerLog,
      PATH: `${fakeBinDirectory}:${process.env.PATH}`,
    },
  })
  await waitForControl()

  const signal = await new Promise((resolve) => {
    process.once('SIGINT', () => resolve('SIGINT'))
    process.once('SIGTERM', () => resolve('SIGTERM'))
    controlProcess.once('exit', () => resolve('ctf-proxy exited'))
  })
  if (signal === 'ctf-proxy exited') {
    throw new Error(`E2E service stopped unexpectedly: ${signal}`)
  }
} finally {
  await cleanup()
}
