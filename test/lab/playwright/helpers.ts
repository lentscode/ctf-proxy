import { expect, type Page } from '@playwright/test'

export const token = process.env.LAB_CONTROL_TOKEN ?? ''

export function required(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required`)
  return value
}

export async function authenticate(page: Page): Promise<void> {
  await page.goto('/')
  await page.getByLabel('Control token').fill(token)
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible()
}

export function fakeFlag(label: string): string {
  return (label.toUpperCase().replaceAll('_', '') + '0'.repeat(35)).slice(0, 35) + '='
}
