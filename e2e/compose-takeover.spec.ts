import { expect, test } from '@playwright/test'
import { authenticate } from './helpers'

test('operator can apply and restore a Compose takeover', async ({ page }) => {
  await authenticate(page)
  await page.getByRole('link', { name: 'Compose takeover' }).click()
  await expect(page.getByRole('heading', { name: 'Compose takeover' })).toBeVisible()

  await page.getByRole('button', { name: 'Scan projects' }).click()
  await expect(page.getByText('demo (compose.yaml)')).toBeVisible()
  const selection = page.getByLabel(/Select web .*18080/)
  await expect(selection).toBeEnabled()
  await selection.check()
  await page.getByRole('button', { name: 'Apply 1 selected' }).click()
  await page.getByRole('button', { name: 'Confirm' }).click()

  await expect(page.getByText(/demo \(compose\.yaml\) \/ web/)).toBeVisible()
  await expect(page.getByText(/active/)).toBeVisible()

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: 'Restore', exact: true }).click()
  await expect(page.getByText('No managed Compose deployments.')).toBeVisible()
})
