import { expect, test, type Page } from "@playwright/test";
import { authenticate, required } from "./helpers";

async function attachFilter(
  page: Page,
  proxy: string,
  filter: string,
): Promise<void> {
  await page.getByRole("button").filter({ hasText: proxy }).first().click();
  await page.getByLabel("Available filters").selectOption(filter);
  await page.getByRole("button", { name: "Attach filter" }).click();
  await page.getByRole("button", { name: "Save proxy" }).click();
  await expect(
    page.getByText("Proxy saved", { exact: true }).last(),
  ).toBeVisible();
}

async function createTemplateExploitFilter(
  page: Page,
  proxy: string,
): Promise<void> {
  await page.getByRole("link", { name: "Filters", exact: true }).click();
  const section = page
    .getByRole("heading", { name: proxy })
    .locator("..")
    .locator("..")
    .locator("..");
  await section.getByRole("button", { name: "Add filter" }).click();
  await page.getByLabel("Filter name").fill("lab-block-template-flag-request");
  await page.getByLabel("Protocol").selectOption("http");
  await page.getByLabel("Direction").selectOption("request");
  await page.getByLabel("Condition 1 field").selectOption("http.body");
  await page.getByLabel("Condition 1 operator").selectOption("contains");
  await page.getByLabel("Condition 1 match value").fill("%7B%7Bflag%7D%7D");
  await page.getByLabel("Enable this filter").check();
  await page.getByRole("button", { name: "Create filter" }).click();
  await expect(
    section.getByText("lab-block-template-flag-request", { exact: true }),
  ).toBeVisible();
}

test("operator attaches predefined lab filters through the dashboard", async ({
  page,
}) => {
  await authenticate(page);
  await page.getByRole("link", { name: "Proxies" }).click();
  await attachFilter(
    page,
    required("LAB_TCP_ARCHIVE_PROXY"),
    "lab-block-archive-traversal",
  );
  await attachFilter(
    page,
    required("LAB_HTTP_LOGIN_PROXY"),
    "lab-block-login-admin",
  );
  await attachFilter(
    page,
    required("LAB_HTTP_TEMPLATE_PROXY"),
    "lab-block-template-probe-header",
  );
  await createTemplateExploitFilter(page, required("LAB_HTTP_TEMPLATE_PROXY"));
});
