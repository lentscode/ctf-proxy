import { expect, test, type Page } from '@playwright/test'
import { authenticate, required } from './helpers'

async function attachFilter(page: Page, proxy: string, filter: string): Promise<void> {
  await page.getByRole('button').filter({ hasText: proxy }).first().click()
  await page.getByLabel('Available filters').selectOption(filter)
  await page.getByRole('button', { name: 'Attach filter' }).click()
  await page.getByRole('button', { name: 'Save proxy' }).click()
  await expect(page.getByText('Proxy saved', { exact: true }).last()).toBeVisible()
}

test('operator attaches predefined lab filters through the dashboard', async ({ page }) => {
  await authenticate(page)
  await page.getByRole('link', { name: 'Proxies' }).click()
  await attachFilter(page, required('LAB_TCP_ARCHIVE_PROXY'), 'lab-block-archive-traversal')
  await attachFilter(page, required('LAB_HTTP_LOGIN_PROXY'), 'lab-block-login-admin')
  await attachFilter(page, required('LAB_HTTP_TEMPLATE_PROXY'), 'lab-block-template-flag-response')
  await attachFilter(page, required('LAB_HTTP_TEMPLATE_PROXY'), 'lab-block-template-probe-header')
})
