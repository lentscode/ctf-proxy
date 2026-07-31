import { expect, type Page } from "@playwright/test";

export const controlToken = "e2e-token";

export async function authenticate(page: Page): Promise<void> {
  await page.goto("/");
  await page.getByLabel("Control token").fill(controlToken);
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("link", { name: "Dashboard" })).toBeVisible();
}

export async function clearProxies(page: Page): Promise<void> {
  const list = await page.request.get("/api/v1/proxies", {
    headers: { Authorization: `Bearer ${controlToken}` },
  });
  expect(list.ok()).toBeTruthy();
  const body = (await list.json()) as { proxies: { name: string }[] };
  for (const proxy of body.proxies) {
    const deleted = await page.request.delete(
      `/api/v1/proxies/${encodeURIComponent(proxy.name)}`,
      {
        headers: { Authorization: `Bearer ${controlToken}` },
      },
    );
    expect(deleted.ok()).toBeTruthy();
  }
}
