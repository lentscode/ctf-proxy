import { expect, test } from '@playwright/test'
import { controlToken } from './helpers'

test('embedded dashboard serves assets and client-side routes', async ({ page }) => {
  const response = await page.goto('/proxies')
  expect(response?.status()).toBe(200)
  await expect(page.getByLabel('Control token')).toBeVisible()

  const scriptPath = await page.locator('script[type="module"]').getAttribute('src')
  expect(scriptPath).toBeTruthy()
  const asset = await page.request.get(scriptPath!)
  expect(asset.ok()).toBeTruthy()
})

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
  await page.getByLabel('Control token').fill(controlToken)
  await page.getByRole('button', { name: 'Continue' }).click()

  await expect(page.getByRole('alert')).toHaveText('Unable to reach ctf-proxy.')
  await expect(page.getByLabel('Control token')).toHaveValue(controlToken)
})
