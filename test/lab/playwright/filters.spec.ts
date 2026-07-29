import { expect, test } from '@playwright/test'
import { authenticate, fakeFlag, required } from './helpers'

async function createFilter(page: import('@playwright/test').Page, proxy: string, name: string, protocol: 'tcp' | 'http', direction: 'request' | 'response', field: string, value: string, header = ''): Promise<void> {
  const section = page.getByRole('heading', { name: proxy }).locator('..').locator('..').locator('..')
  await section.getByRole('button', { name: 'Add filter' }).click()
  await page.getByLabel('Filter name').fill(name)
  await page.getByLabel('Protocol').selectOption(protocol)
  await page.getByLabel('Direction').selectOption(direction)
  await page.getByLabel('Condition 1 field').selectOption(field)
  if (header) await page.getByLabel('Condition 1 header name').fill(header)
  await page.getByLabel('Condition 1 operator').selectOption('contains')
  if (header) await page.getByLabel('Condition 1 operator').selectOption('exact')
  await page.getByLabel('Condition 1 match value').fill(value)
  await page.getByLabel('Enable this filter').check()
  await page.getByRole('button', { name: 'Create filter' }).click()
  await expect(section.getByText(name, { exact: true })).toBeVisible()
}

test('operator creates real traffic filters through the dashboard', async ({ page }) => {
  await authenticate(page)
  await page.getByRole('link', { name: 'Filters' }).click()
  await createFilter(page, required('LAB_TCP_ARCHIVE_PROXY'), 'lab-block-archive-traversal', 'tcp', 'request', 'tcp.body', '../')
  await createFilter(page, required('LAB_HTTP_LOGIN_PROXY'), 'lab-block-login-admin', 'http', 'request', 'http.body', 'username=admin')
  await createFilter(page, required('LAB_HTTP_TEMPLATE_PROXY'), 'lab-block-template-flag-response', 'http', 'response', 'http.body', fakeFlag('http2_template'))
  await createFilter(page, required('LAB_HTTP_TEMPLATE_PROXY'), 'lab-block-template-probe-header', 'http', 'request', 'http.header', 'blocked', 'X-Lab-Probe')
})
