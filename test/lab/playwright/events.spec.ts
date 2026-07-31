import { expect, test } from "@playwright/test";
import { authenticate } from "./helpers";

test("operator sees sanitized real filter events", async ({ page }) => {
  await authenticate(page);
  await expect(page.getByRole("heading", { name: "Events" })).toBeVisible();
  await expect(
    page.getByText("filter_rejected", { exact: false }).first(),
  ).toBeVisible();
  await expect(page.getByText("username=admin", { exact: false })).toHaveCount(
    0,
  );
  await expect(page.getByText("{{flag}}", { exact: false })).toHaveCount(0);
  await expect(page.getByText("X-Lab-Probe", { exact: false })).toHaveCount(0);
});
