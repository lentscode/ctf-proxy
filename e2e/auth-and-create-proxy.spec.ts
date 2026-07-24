import { expect, test } from '@playwright/test'

// This scenario verifies authentication and creation of an inactive TCP proxy.
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
