import { expect, test } from "@playwright/test";
import { authenticate, clearProxies } from "./helpers";

test("operator can create, edit, and remove an inactive TCP proxy", async ({
  page,
}) => {
  await clearProxies(page);
  await authenticate(page);

  await expect(page.getByText("No proxies configured.")).toBeVisible();
  await page.getByRole("link", { name: "Proxies" }).click();
  await page.getByLabel("Name").fill("notes-tcp");
  await page.getByLabel("Listen").fill("127.0.0.1:31337");
  await page.getByLabel("Upstream").fill("127.0.0.1:31338");
  await page.getByLabel("Start active").uncheck();
  await page.getByRole("button", { name: "Save proxy" }).click();

  await expect(page.getByRole("button", { name: "notes-tcp" })).toBeVisible();
  await page.getByRole("link", { name: "Dashboard" }).click();
  await page.getByRole("link", { name: "notes-tcp" }).click();
  await expect(page).toHaveURL(/\/proxies\?proxy=notes-tcp/);
  await expect(
    page.getByRole("heading", { name: "Edit notes-tcp" }),
  ).toBeFocused();

  await page.getByLabel("Upstream").fill("127.0.0.1:31339");
  await page.getByRole("button", { name: "Save proxy" }).click();
  await expect(page.getByLabel("Upstream")).toHaveValue("127.0.0.1:31339");

  page.once("dialog", (dialog) => dialog.dismiss());
  await page.getByRole("button", { name: "Remove proxy" }).click();
  await expect(
    page.getByRole("heading", { name: "Edit notes-tcp" }),
  ).toBeVisible();

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Remove proxy" }).click();
  await expect(page.getByText("No proxies configured.")).toBeVisible();
});

test("operator can retry a failed proxy query", async ({ page }) => {
  let requests = 0;
  await page.route("**/api/v1/proxies", async (route) => {
    requests += 1;
    if (requests <= 2) {
      await route.abort();
      return;
    }
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        proxies: [
          {
            name: "recovered-proxy",
            active: false,
            protocol: "tcp",
            listen: "127.0.0.1:31407",
            upstream: "127.0.0.1:31408",
            filters: [],
            state: "inactive",
          },
        ],
      }),
    });
  });
  await authenticate(page);
  await expect(page.getByText("Unable to load proxies.")).toBeVisible();

  await page.getByRole("button", { name: "Retry" }).click();
  await expect(
    page.getByRole("link", { name: "recovered-proxy" }),
  ).toBeVisible();
});
