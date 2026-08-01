import { expect, test } from "@playwright/test";
import { authenticate, clearProxies } from "./helpers";

test("operator can create an independent filter and assign it to multiple proxies", async ({
  page,
}) => {
  await clearProxies(page);
  await authenticate(page);
  await page.getByRole("link", { name: "Proxies" }).click();

  for (const [name, listen, upstream] of [
    ["web-http-a", "127.0.0.1:31347", "http://127.0.0.1:31348"],
    ["web-http-b", "127.0.0.1:31349", "http://127.0.0.1:31350"],
  ]) {
    if (name !== "web-http-a") {
      await page.getByRole("button", { name: "Add proxy" }).click();
    }
    await page.getByLabel("Name").fill(name);
    await page.getByLabel("Protocol").selectOption("http");
    await page.getByLabel("Listen").fill(listen);
    await page.getByLabel("Upstream").fill(upstream);
    await page.getByLabel("Start active").uncheck();
    await page.getByRole("button", { name: "Save proxy" }).click();
  }

  await page.getByRole("link", { name: "Filter library · 0" }).first().click();
  await expect(page).toHaveURL(/\/filters$/);
  await page.getByRole("button", { name: "Add filter" }).click();
  await page.getByLabel("Filter name").fill("library-block-admin");
  await page.getByLabel("Condition 1 field").selectOption("http.path");
  await page.getByLabel("Condition 1 operator").selectOption("prefix");
  await page.getByLabel("Condition 1 match value").fill("/admin");
  await page.getByRole("button", { name: "Create filter" }).click();

  await expect(page).toHaveURL(/\/filters\?filter=library-block-admin/);
  await expect(
    page.getByRole("heading", { name: "library-block-admin", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("This filter is not assigned to any proxy."),
  ).toBeVisible();

  await page
    .getByRole("button", { name: "Choose proxies · 0 selected" })
    .click();
  await page.getByLabel(/web-http-a/).check();
  await page.getByLabel(/web-http-b/).check();
  await page.getByRole("button", { name: "Save assignments" }).click();
  await expect(
    page.getByText("Currently assigned to: web-http-a, web-http-b."),
  ).toBeVisible();

  await page.getByLabel("Condition 1 match value").fill("/private");
  await page.getByRole("button", { name: "Save filter" }).click();
  await expect(page.getByLabel("Condition 1 match value")).toHaveValue(
    "/private",
  );

  await page
    .getByRole("button", { name: "Choose proxies · 2 selected" })
    .click();
  await page.getByLabel(/web-http-a/).uncheck();
  await page.getByRole("button", { name: "Save assignments" }).click();
  await expect(
    page.getByText("Currently assigned to: web-http-b."),
  ).toBeVisible();

  await page
    .getByRole("button", { name: "Choose proxies · 1 selected" })
    .click();
  await page.getByLabel(/web-http-b/).uncheck();
  await page.getByRole("button", { name: "Save assignments" }).click();
  await expect(
    page.getByText("This filter is not assigned to any proxy."),
  ).toBeVisible();

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Delete filter" }).click();
  await expect(
    page.getByRole("button", { name: /library-block-admin yaml/ }),
  ).toHaveCount(0);
});
