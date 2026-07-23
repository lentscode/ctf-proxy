import { APIError, parseObserveEvent, type ObserveEvent } from './api'
import { authenticatedFetch } from './auth'
import { z } from 'zod'

export const streamStatusSchema = z.enum(['connecting', 'live', 'reconnecting', 'disconnected'])
export type StreamStatus = z.infer<typeof streamStatusSchema>

interface EventStreamOptions {
  onEvent: (event: ObserveEvent) => void
  onConnected: () => void
  onStatus: (status: StreamStatus) => void
  onUnauthorized: () => void
}

export function subscribeToEventStream(options: EventStreamOptions): () => void {
  let active = true
  let retryCount = 0
  let controller: AbortController | undefined
  let retryTimer: ReturnType<typeof setTimeout> | undefined

  const scheduleReconnect = () => {
    if (!active) return
    retryCount += 1
    options.onStatus('reconnecting')
    const delay = Math.min(1_000 * 2 ** (retryCount - 1), 30_000)
    retryTimer = setTimeout(() => void connect(), delay)
  }

  const connect = async () => {
    controller = new AbortController()
    options.onStatus(retryCount === 0 ? 'connecting' : 'reconnecting')
    try {
      const response = await authenticatedFetch('/api/v1/events/stream', {
        headers: { Accept: 'text/event-stream' },
        signal: controller.signal,
      })
      if (response.status === 401) {
        if (active) options.onUnauthorized()
        return
      }
      if (!response.ok || !response.body) {
        throw new APIError('Event stream unavailable.', response.status)
      }
      retryCount = 0
      options.onStatus('live')
      options.onConnected()
      await readSSE(response.body, options.onEvent)
      if (active) scheduleReconnect()
    } catch {
      if (active && !controller.signal.aborted) scheduleReconnect()
    }
  }

  void connect()
  return () => {
    active = false
    controller?.abort()
    if (retryTimer) clearTimeout(retryTimer)
  }
}

async function readSSE(body: ReadableStream<Uint8Array>, onEvent: (event: ObserveEvent) => void): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let eventType = ''
  let data: string[] = []

  const dispatch = () => {
    if (eventType === 'observe' && data.length > 0) {
      try {
        const event = parseObserveEvent(JSON.parse(data.join('\n')))
        if (event) onEvent(event)
      } catch {
        // Invalid SSE payloads are deliberately ignored.
      }
    }
    eventType = ''
    data = []
  }

  while (true) {
    const chunk = await reader.read()
    if (chunk.done) return
    buffer += decoder.decode(chunk.value, { stream: true })
    const lines = buffer.split(/\r?\n/)
    buffer = lines.pop() ?? ''
    for (const line of lines) {
      if (line === '') {
        dispatch()
      } else if (line.startsWith('event:')) {
        eventType = line.slice('event:'.length).trim()
      } else if (line.startsWith('data:')) {
        data.push(line.slice('data:'.length).trimStart())
      }
    }
  }
}
