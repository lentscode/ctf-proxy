import { expect, test } from '@playwright/test'
import { authenticate } from './helpers'

test('operator applies or restores the four-service real-world lab takeover', async ({ page }) => {
  test.setTimeout(120_000)
  await authenticate(page)
  await page.getByRole('link', { name: 'Proxies' }).click()
  await page.getByRole('button', { name: 'Scan and configure' }).click()
  await expect(page.getByRole('heading', { name: 'Scan and configure' })).toBeVisible()

  const restoreAll = page.getByRole('button', { name: 'Restore all' })
  await expect(restoreAll.or(page.getByText('No managed deployments.'))).toBeVisible()
  if (await restoreAll.isVisible()) {
    page.once('dialog', (dialog) => dialog.accept())
    await restoreAll.click()
    await expect(page.getByText('No managed deployments.')).toBeVisible({ timeout: 60_000 })
    return
  }

  for (const service of ['tcp-echo', 'tcp-archive', 'http-login', 'http-template']) {
    await expect(page.getByText(new RegExp(`${service} \\(compose\\.yaml\\)`))).toBeVisible()
  }
  for (const service of ['tcp-echo', 'tcp-archive', 'http-login', 'http-template']) {
    await page.getByLabel(new RegExp(`Select ${service}`)).check()
  }
  await page.getByLabel(/Protocol for http-login/).selectOption('http')
  await page.getByLabel(/Protocol for http-template/).selectOption('http')
  await page.getByRole('button', { name: 'Apply 4 selected' }).click()
  await page.getByRole('button', { name: 'Confirm' }).click()
  await expect(page.getByRole('button', { name: 'Restore', exact: true })).toHaveCount(4, { timeout: 60_000 })
})
