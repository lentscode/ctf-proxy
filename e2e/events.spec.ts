import { expect, test } from '@playwright/test'
import { authenticate, controlToken } from './helpers'

test('operator sees a new control event from the live event stream', async ({ page }) => {
  await authenticate(page)
  await expect(page.getByRole('heading', { name: 'Events' })).toBeVisible()
  await expect(page.getByText('live', { exact: true })).toBeVisible()

  const response = await page.request.post('/api/v1/proxies', {
    headers: { Authorization: `Bearer ${controlToken}`, 'Content-Type': 'application/json' },
    data: {
      name: 'event-trigger', active: false, protocol: 'tcp', listen: '127.0.0.1:31357', upstream: '127.0.0.1:31358', filters: ['unknown-filter'],
    },
  })
  expect(response.status()).toBe(400)

  await expect(page.getByText('configuration update rejected', { exact: true })).toBeVisible()
  await expect(page.getByText('control · control_configuration_rejected', { exact: true })).toBeVisible()
})

test('operator can retry a failed event-history query', async ({ page }) => {
  let requests = 0
  await page.route(/\/api\/v1\/events\?limit=100$/, async (route) => {
    requests += 1
    if (requests <= 2) {
      await route.abort()
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        events: [{ id: 42, time: '2026-07-24T10:00:00Z', level: 'warn', component: 'control', kind: 'recovered', message: 'Recovered event history' }],
      }),
    })
  })
  await authenticate(page)
  await expect(page.getByText('Unable to load events.')).toBeVisible()

  await page.getByRole('button', { name: 'Retry' }).click()
  await expect(page.getByText('Recovered event history', { exact: true })).toBeVisible()
})

test('operator ignores an invalid event-stream payload', async ({ page }) => {
  let connections = 0
  await page.route(/\/api\/v1\/events\?limit=100$/, (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ events: [] }) }))
  await page.route('**/api/v1/events/stream', async (route) => {
    connections += 1
    await route.fulfill({
      contentType: 'text/event-stream',
      body: 'event: observe\ndata: {"id":"not-a-number","message":"untrusted event"}\n\n',
    })
  })
  await authenticate(page)

  await expect.poll(() => connections).toBeGreaterThanOrEqual(1)
  await expect(page.getByText('reconnecting', { exact: true })).toBeVisible()
  await expect(page.getByText('No events recorded.')).toBeVisible()
  await expect(page.getByText('untrusted event', { exact: true })).toHaveCount(0)
})

test('operator is signed out when a reconnected event stream loses authorization', async ({ page }) => {
  let connections = 0
  await page.route('**/api/v1/events/stream', async (route) => {
    connections += 1
    if (connections === 1) {
      await route.fulfill({ contentType: 'text/event-stream', body: '' })
      return
    }
    await route.fulfill({ status: 401 })
  })
  await authenticate(page)

  await expect.poll(() => connections).toBeGreaterThanOrEqual(2)
  await expect(page.getByRole('alert')).toHaveText('Token was not accepted.')
  await expect(page.getByLabel('Control token')).toBeVisible()
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('ctf-proxy.auth-token'))).toBeNull()
})

test('operator sees the event stream reconnect after it closes', async ({ page }) => {
  let connections = 0
  await page.route('**/api/v1/events/stream', async (route) => {
    connections += 1
    if (connections === 1) {
      await route.fulfill({ contentType: 'text/event-stream', body: '' })
      return
    }
    await route.fulfill({ status: 503 })
  })
  await authenticate(page)

  await expect.poll(() => connections).toBeGreaterThanOrEqual(2)
  await expect(page.getByText('reconnecting', { exact: true })).toBeVisible()
})
