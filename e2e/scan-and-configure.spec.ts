import { expect, test } from "@playwright/test";
import { authenticate } from "./helpers";

test("operator can scan, configure, and restore a Compose deployment", async ({
  page,
}) => {
  await authenticate(page);
  await page.getByRole("link", { name: "Proxies" }).click();
  await page.getByRole("button", { name: "Scan and configure" }).click();
  await expect(
    page.getByRole("heading", { name: "Scan and configure" }),
  ).toBeVisible();
  await expect(page.getByText("demo (compose.yaml)")).toBeVisible();
  const selection = page.getByLabel(/Select web .*18080/);
  await expect(selection).toBeEnabled();
  const protocol = page.getByLabel(/Protocol for web .*18080/);
  await expect(protocol).toBeDisabled();
  const disabledStyle = await protocol.evaluate((element) => {
    const style = window.getComputedStyle(element);
    return {
      background: style.backgroundColor,
      border: style.borderTopColor,
      color: style.color,
    };
  });
  await expect(protocol).toHaveCSS("opacity", "1");
  await selection.check();
  await expect(protocol).toBeEnabled();
  const enabledStyle = await protocol.evaluate((element) => {
    const style = window.getComputedStyle(element);
    return {
      background: style.backgroundColor,
      border: style.borderTopColor,
      color: style.color,
    };
  });
  expect(disabledStyle.background).not.toBe(enabledStyle.background);
  expect(disabledStyle.border).not.toBe(enabledStyle.border);
  expect(disabledStyle.color).not.toBe(enabledStyle.color);
  await page.getByRole("button", { name: "Apply 1 selected" }).click();
  await page.getByRole("button", { name: "Confirm" }).click();

  await expect(page.getByText(/demo \(compose\.yaml\) \/ web/)).toBeVisible();
  await expect(page.getByText(/active/)).toBeVisible();

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Restore", exact: true }).click();
  await expect(page.getByText("No managed deployments.")).toBeVisible();
});
