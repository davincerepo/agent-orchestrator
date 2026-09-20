import { expect, test } from "@playwright/test";
import { installFakeAgent } from "./support/fake-bridge";
import { installFakeTerminalMux } from "./support/fake-terminal-mux";

// Renderer smoke with a fake daemon response; native identity and read-only
// behavior are exercised separately by the Go service/manager tests.
test("renderer: terminal parameters refresh on entry without changing the terminal @T0 @TRM", async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 800 });
	await installFakeAgent(page, { workers: [
		{ id: "codex-worker", title: "Codex worker", provider: "codex", mode: "tui" },
		{ id: "other-worker", title: "Other worker", provider: "claude-code", mode: "tui" },
	] });
	await installFakeTerminalMux(page, { "codex-worker/terminal_0": "Codex ready", "other-worker/terminal_0": "Other ready" });
	let requests = 0;
	await page.route("**/api/v1/sessions/*/terminal-model-parameters", async (route) => {
		requests++;
		expect(route.request().method()).toBe("GET");
		await route.fulfill({
			status: requests === 2 ? 409 : 200,
			contentType: "application/json",
			body: JSON.stringify(requests === 2 ? { error: { code: "MODEL_PARAMETERS_UNCONFIRMED" } }
				: { supported: true, model: "gpt-native-thread", reasoningEffort: "high", serviceTier: requests === 1 ? "priority" : "default" }),
		});
	});
	await page.goto("/#/projects/fake-proj/sessions/codex-worker");
	const bar = page.getByTestId("terminal-model-parameters");
	await expect(bar).toContainText("Model: gpt-native-thread");
	await expect(bar).toContainText("Fast: On");
	await expect(page.locator(".xterm-helper-textarea:focus")).toHaveCount(1);
	await page.getByRole("button", { name: "Open Other worker", exact: true }).click();
	await expect(bar).toHaveCount(0);
	expect(requests).toBe(1);
	await page.getByRole("button", { name: "Open Codex worker", exact: true }).click();
	await expect(bar).toContainText("Unconfirmed");
	await expect(bar).toContainText("Model: Unknown");
	await expect(bar).not.toContainText("gpt-native-thread");
	await page.getByRole("button", { name: "Open Other worker", exact: true }).click();
	await page.getByRole("button", { name: "Open Codex worker", exact: true }).click();
	await expect(bar).toContainText("Fast: Off");
	expect(requests).toBe(3);
	expect(await page.evaluate(() => window.__aoFakeTerminalMux?.stats().inputs ?? {})).toEqual({});
	await page.screenshot({ path: test.info().outputPath("terminal-parameters.png") });
});
