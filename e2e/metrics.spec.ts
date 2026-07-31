import { expect, test } from "@playwright/test";
import { authenticate } from "./helpers";

const values = {
  requests: 4,
  responses: 3,
  client_to_upstream_bytes: 1024,
  upstream_to_client_bytes: 2048,
  rejections_total: 1,
  filter_rejections: 1,
  capacity_rejections: 0,
  upstream_failures: 0,
  rejection_denominator: 4,
  rejection_ratio: 0.25,
};

test("operator can inspect current and historical service metrics", async ({
  page,
}) => {
  await page.route("**/api/v1/metrics", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        collected_since: "2026-07-31T08:00:00Z",
        schedule: {
          competition_start: "2026-07-31T08:00:00Z",
          round_duration_seconds: 120,
          retention_rounds: 720,
        },
        current_round: {
          number: 1,
          starts_at: "2026-07-31T08:02:00Z",
          ends_at: "2026-07-31T08:04:00Z",
          metrics: values,
        },
        proxies: [
          { name: "web", protocol: "http", configured: true, metrics: values },
        ],
      }),
    }),
  );
  await page.route("**/api/v1/metrics/rounds?proxy=web", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        rounds: [
          {
            number: 0,
            starts_at: "2026-07-31T08:00:00Z",
            ends_at: "2026-07-31T08:02:00Z",
            metrics: { ...values, requests: 2, rejection_ratio: 0 },
          },
          {
            number: 1,
            starts_at: "2026-07-31T08:02:00Z",
            ends_at: "2026-07-31T08:04:00Z",
            metrics: values,
          },
        ],
      }),
    }),
  );
  await authenticate(page);
  await expect(
    page.getByRole("heading", { name: "Traffic metrics" }),
  ).toBeVisible();
  await expect(page.getByText("4 requests / 3 responses")).toBeVisible();
  await expect(page.getByText("25.0%")).toBeVisible();
  const service = page.getByRole("button", { name: /web http/ });
  await service.focus();
  await service.press("Enter");
  await expect(
    page.getByRole("heading", { name: "web round history" }),
  ).toBeVisible();
  await expect(
    page.getByRole("img", { name: "Forwarded bytes by round" }),
  ).toBeVisible();
});

test("dashboard explains disabled metrics without treating it as an auth error", async ({
  page,
}) => {
  await page.route("**/api/v1/metrics", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: { code: "unavailable" } }),
    }),
  );
  await authenticate(page);
  await expect(page.getByText("Traffic metrics are disabled.")).toBeVisible();
  await expect(page.getByRole("link", { name: "Dashboard" })).toBeVisible();
});
