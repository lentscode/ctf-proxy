import { expect, test } from '@playwright/test'

test('operator is kept signed out when the control token is invalid', async ({ page }) => {
  await page.goto('/')

  await page.getByLabel('Control token').fill('incorrect-token')
  await page.getByRole('button', { name: 'Continue' }).click()

  await expect(page.getByRole('alert')).toHaveText('Token was not accepted.')
  await expect(page.getByLabel('Control token')).toBeVisible()
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('ctf-proxy.auth-token'))).toBeNull()
})

test('operator is signed out when a stored token is no longer valid', async ({ page }) => {
  await page.addInitScript(() => sessionStorage.setItem('ctf-proxy.auth-token', 'expired-token'))
  await page.goto('/')

  await expect(page.getByRole('alert')).toHaveText('Token was not accepted.')
  await expect(page.getByLabel('Control token')).toBeVisible()
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('ctf-proxy.auth-token'))).toBeNull()
})

test('operator sees a reachable-service error separately from a rejected token', async ({ page }) => {
  await page.route('**/healthz', (route) => route.abort())
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()

  await expect(page.getByRole('alert')).toHaveText('Unable to reach ctf-proxy.')
  await expect(page.getByLabel('Control token')).toHaveValue('e2e-token')
})

// This scenario verifies authentication and creation of an inactive TCP proxy.
test('operator can authenticate and create an inactive TCP proxy', async ({ page }) => {
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()

  await expect(page.getByRole('link', { name: 'Proxies' })).toBeVisible()
  await expect(page.getByText('No proxies configured.')).toBeVisible()
  await expect(page.getByText('No events recorded.')).toBeVisible()
  await page.getByRole('link', { name: 'Proxies' }).click()

  await page.getByLabel('Name').fill('notes-tcp')
  await page.getByLabel('Listen').fill('127.0.0.1:31337')
  await page.getByLabel('Upstream').fill('127.0.0.1:31338')
  await page.getByLabel('Start active').uncheck()
  await page.getByRole('button', { name: 'Save proxy' }).click()

  await expect(page.getByRole('button', { name: 'notes-tcp' })).toBeVisible()
  await page.getByRole('link', { name: 'Dashboard' }).click()
  await page.getByRole('link', { name: 'notes-tcp' }).click()
  await expect(page).toHaveURL(/\/proxies\?proxy=notes-tcp/)
  await expect(page.getByRole('heading', { name: 'Edit notes-tcp' })).toBeFocused()

  await page.getByLabel('Upstream').fill('127.0.0.1:31339')
  await page.getByRole('button', { name: 'Save proxy' }).click()
  await expect(page.getByLabel('Upstream')).toHaveValue('127.0.0.1:31339')

  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('button', { name: 'Remove proxy' }).click()
  await expect(page.getByRole('heading', { name: 'Edit notes-tcp' })).toBeVisible()

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: 'Remove proxy' }).click()
  await expect(page.getByText('No proxies configured.')).toBeVisible()
})

test('operator can manage a structured filter from its proxy', async ({ page }) => {
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()
  await page.getByRole('link', { name: 'Proxies' }).click()

  await page.getByLabel('Name').fill('web-http')
  await page.getByLabel('Protocol').selectOption('http')
  await page.getByLabel('Listen').fill('127.0.0.1:31347')
  await page.getByLabel('Upstream').fill('http://127.0.0.1:31348')
  await page.getByLabel('Start active').uncheck()
  await page.getByRole('button', { name: 'Save proxy' }).click()

  const proxyDirectory = page.getByRole('button', { name: 'web-http' }).locator('..')
  await proxyDirectory.getByRole('link', { name: 'Manage filters · 0' }).click()
  await expect(page).toHaveURL(/\/filters\?proxy=web-http/)
  await expect(page.getByRole('heading', { name: 'web-http' })).toBeFocused()

  await page.getByLabel('web-http').getByRole('button', { name: 'Add filter' }).click()
  await page.getByLabel('Filter name').fill('block-admin')
  await page.getByLabel('Condition 1 field').selectOption('http.path')
  await page.getByLabel('Condition 1 operator').selectOption('prefix')
  await page.getByLabel('Condition 1 match value').fill('/admin')
  await page.getByRole('button', { name: 'Create filter' }).click()
  await expect(page.getByText('block-admin', { exact: true })).toBeVisible()

  const filterRow = page.getByRole('group', { name: 'Filter block-admin' })
  await filterRow.getByRole('button', { name: 'Edit' }).click()
  await page.getByLabel('Condition 1 match value').fill('/private')
  await page.getByRole('button', { name: 'Save filter' }).click()

  await page.getByRole('group', { name: 'Filter block-admin' }).getByRole('button', { name: 'Edit' }).click()
  await expect(page.getByLabel('Condition 1 match value')).toHaveValue('/private')
  await page.getByRole('button', { name: 'Cancel' }).click()

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('group', { name: 'Filter block-admin' }).getByRole('button', { name: 'Remove' }).click()
  await expect(page.getByText('No filters attached.')).toBeVisible()
})

test('operator sees a new control event from the live event stream', async ({ page }) => {
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByRole('heading', { name: 'Events' })).toBeVisible()
  await expect(page.getByText('live', { exact: true })).toBeVisible()

  // Deliberately request an unknown filter through the real control API. It
  // leaves the configuration untouched and causes the manager to publish a
  // sanitized configuration-rejected event to the SSE hub.
  const response = await page.request.post('/api/v1/proxies', {
    headers: {
      Authorization: 'Bearer e2e-token',
      'Content-Type': 'application/json',
    },
    data: {
      name: 'event-trigger',
      active: false,
      protocol: 'tcp',
      listen: '127.0.0.1:31357',
      upstream: '127.0.0.1:31358',
      filters: ['unknown-filter'],
    },
  })
  expect(response.status()).toBe(400)

  await expect(page.getByText('configuration update rejected', { exact: true })).toBeVisible()
  await expect(page.getByText('control · control_configuration_rejected', { exact: true })).toBeVisible()
})

test('operator can retry a failed proxy query', async ({ page }) => {
  let requests = 0
  await page.route('**/api/v1/proxies', async (route) => {
    requests += 1
    if (requests <= 2) {
      await route.abort()
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        proxies: [{
          name: 'recovered-proxy', active: false, protocol: 'tcp', listen: '127.0.0.1:31407', upstream: '127.0.0.1:31408', filters: [], state: 'inactive',
        }],
      }),
    })
  })
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByText('Unable to load proxies.')).toBeVisible()

  await page.getByRole('button', { name: 'Retry' }).click()
  await expect(page.getByRole('link', { name: 'recovered-proxy' })).toBeVisible()
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
        events: [{
          id: 42, time: '2026-07-24T10:00:00Z', level: 'warn', component: 'control', kind: 'recovered', message: 'Recovered event history',
        }],
      }),
    })
  })
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByText('Unable to load events.')).toBeVisible()

  await page.getByRole('button', { name: 'Retry' }).click()
  await expect(page.getByText('Recovered event history', { exact: true })).toBeVisible()
})

test('operator ignores an invalid event-stream payload', async ({ page }) => {
  let connections = 0
  await page.route(/\/api\/v1\/events\?limit=100$/, (route) => route.fulfill({
    contentType: 'application/json', body: JSON.stringify({ events: [] }),
  }))
  await page.route('**/api/v1/events/stream', async (route) => {
    connections += 1
    await route.fulfill({
      contentType: 'text/event-stream',
      body: 'event: observe\ndata: {"id":"not-a-number","message":"untrusted event"}\n\n',
    })
  })
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()

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
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()

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
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()

  await expect.poll(() => connections).toBeGreaterThanOrEqual(2)
  await expect(page.getByText('reconnecting', { exact: true })).toBeVisible()
})
