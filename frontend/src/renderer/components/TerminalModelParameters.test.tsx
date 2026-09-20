import { focusManager, onlineManager, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TerminalModelParameters } from "./TerminalModelParameters";

const { getMock } = vi.hoisted(() => ({ getMock: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: getMock } }));

const observed = { supported: true, model: "gpt-native", reasoningEffort: "high", serviceTier: "priority" };
function setup() {
	const client = new QueryClient();
	return { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> };
}
const pane = (id = "worker", generation = "launch-1", enabled = true) => (
	<TerminalModelParameters sessionId={id} generation={generation} enabled={enabled} />
);
beforeEach(() => { getMock.mockReset(); });
afterEach(() => { focusManager.setFocused(undefined); onlineManager.setOnline(true); });

describe("terminal saved model parameters", () => {
	it("checks each entry once, without focus, reconnect, render or polling refresh", async () => {
		const options = setup(); getMock.mockResolvedValue({ data: observed });
		const view = render(pane(), options);
		await screen.findByText("Model: gpt-native");
		expect(screen.getByText("Effort: high")).toBeInTheDocument();
		expect(screen.getByText("Fast: On")).toBeInTheDocument();
		view.rerender(pane());
		await act(async () => {
			focusManager.setFocused(false); focusManager.setFocused(true);
			onlineManager.setOnline(false); onlineManager.setOnline(true);
		});
		expect(getMock).toHaveBeenCalledTimes(1);
		view.unmount();
		getMock.mockResolvedValue({ data: { ...observed, model: "changed-in-terminal", serviceTier: "default" } });
		render(pane(), options);
		await screen.findByText("Model: changed-in-terminal");
		expect(screen.getByText("Fast: Off")).toBeInTheDocument();
		expect(getMock).toHaveBeenCalledTimes(2);
	});

	it("hides a cached success after a failed visit and retries only on re-entry", async () => {
		const options = setup(); getMock.mockResolvedValueOnce({ data: observed });
		const first = render(pane(), options); await screen.findByText("Model: gpt-native"); first.unmount();
		getMock.mockResolvedValueOnce({ error: { message: "unconfirmed" } });
		const failed = render(pane(), options);
		await screen.findByText("Unconfirmed · reopen terminal to retry");
		expect(screen.getByText("Model: Unknown")).toBeInTheDocument();
		expect(screen.queryByText("Model: gpt-native")).not.toBeInTheDocument();
		failed.rerender(pane());
		await act(async () => { focusManager.setFocused(false); focusManager.setFocused(true); });
		expect(getMock).toHaveBeenCalledTimes(2);
		failed.unmount(); getMock.mockResolvedValueOnce({ data: observed });
		render(pane(), options); await screen.findByText("Model: gpt-native");
		expect(getMock).toHaveBeenCalledTimes(3);
	});

	it("keeps absent legacy fields unknown and hides unsupported providers", async () => {
		getMock.mockResolvedValueOnce({ data: { supported: true, model: "legacy-model" } });
		const view = render(pane(), setup()); await screen.findByText("Model: legacy-model");
		expect(screen.getByText("Effort: Unknown")).toBeInTheDocument();
		expect(screen.getByText("Fast: Unknown")).toBeInTheDocument();
		getMock.mockResolvedValueOnce({ data: { supported: false } });
		view.rerender(pane("unsupported"));
		await waitFor(() => expect(screen.queryByTestId("terminal-model-parameters")).not.toBeInTheDocument());
	});

	it("shares pending reads on re-entry and isolates late responses by session and generation", async () => {
		const options = setup(); let finish!: (value: unknown) => void;
		getMock.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
		const first = render(pane(), options); await waitFor(() => expect(getMock).toHaveBeenCalledTimes(1)); first.unmount();
		const view = render(pane(), options); expect(getMock).toHaveBeenCalledTimes(1);
		getMock.mockResolvedValueOnce({ data: { ...observed, model: "other-worker" } });
		view.rerender(pane("other")); await screen.findByText("Model: other-worker");
		await act(async () => finish({ data: observed }));
		expect(screen.queryByText("Model: gpt-native")).not.toBeInTheDocument();
		getMock.mockResolvedValueOnce({ data: { ...observed, model: "new-generation" } });
		view.rerender(pane("other", "launch-2")); await screen.findByText("Model: new-generation");
		expect(getMock).toHaveBeenCalledTimes(3);
	});

	it("waits for the daemon and reads once after recovery", async () => {
		getMock.mockResolvedValue({ data: observed });
		const view = render(pane("worker", "launch-1", false), setup());
		expect(getMock).not.toHaveBeenCalled();
		view.rerender(pane()); await screen.findByText("Model: gpt-native");
		view.rerender(pane("worker", "launch-1", false)); view.rerender(pane());
		await waitFor(() => expect(getMock).toHaveBeenCalledTimes(2));
	});
});
