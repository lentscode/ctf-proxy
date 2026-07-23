import { expect, test } from '@playwright/test'

test('operator can authenticate and create an inactive TCP proxy', async ({ page }) => {
  await page.goto('/')

  await page.getByLabel('Control token').fill('e2e-token')
  await page.getByRole('button', { name: 'Continue' }).click()

  await expect(page.getByRole('link', { name: 'Proxies' })).toBeVisible()
  await page.getByRole('link', { name: 'Proxies' }).click()

  await page.getByLabel('Name').fill('notes-tcp')
  await page.getByLabel('Listen').fill('127.0.0.1:31337')
  await page.getByLabel('Upstream').fill('127.0.0.1:31338')
  await page.getByLabel('Start active').uncheck()
  await page.getByRole('button', { name: 'Save proxy' }).click()

  await expect(page.getByRole('button', { name: 'notes-tcp' })).toBeVisible()
})
