import { focusManager, onlineManager, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { act, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { conversationModelsQueryKey, useConversationModels } from "./useConversation";

const { getMock } = vi.hoisted(() => ({ getMock: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: getMock } }));

const actual = [{ id: "sol", default: true, defaultEffort: "xhigh" }];
function setup() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	function Wrapper({ children }: { children: ReactNode }) {
		return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
	}
	return { client, wrapper: Wrapper };
}

beforeEach(() => { getMock.mockReset(); });
afterEach(() => {
	focusManager.setFocused(undefined);
	onlineManager.setOnline(true);
});

describe("useConversationModels visit recovery", () => {
	it("checks once per entry, including a recent cache, without focus/network/render refetches", async () => {
		const { client, wrapper } = setup();
		getMock.mockResolvedValue({ data: { models: actual } });
		client.setQueryData(conversationModelsQueryKey("worker"), [{ id: "astra", default: true }]);
		const view = renderHook(() => useConversationModels("worker", true), { wrapper });
		await waitFor(() => expect(view.result.current.models).toEqual(actual));
		view.rerender();
		await act(async () => {
			focusManager.setFocused(false);
			focusManager.setFocused(true);
			onlineManager.setOnline(false);
			onlineManager.setOnline(true);
		});
		expect(getMock).toHaveBeenCalledTimes(1);
		view.unmount();
		const next = renderHook(() => useConversationModels("worker", true), { wrapper });
		await waitFor(() => expect(getMock).toHaveBeenCalledTimes(2));
		expect(next.result.current.models).toEqual(actual);
	});

	it("does not retry a failure until re-entry and hides the stale catalog default", async () => {
		const { client, wrapper } = setup();
		client.setQueryData(conversationModelsQueryKey("worker"), [{ id: "astra", default: true }]);
		getMock.mockResolvedValueOnce({ error: { message: "provider unavailable" } });
		const view = renderHook(() => useConversationModels("worker", true), { wrapper });
		await waitFor(() => expect(view.result.current.error).toContain("could not be confirmed"));
		expect(view.result.current.models).toEqual([]);
		view.rerender();
		await act(async () => {
			focusManager.setFocused(false);
			focusManager.setFocused(true);
			onlineManager.setOnline(false);
			onlineManager.setOnline(true);
		});
		expect(client.getQueryState(conversationModelsQueryKey("worker"))?.fetchStatus).toBe("idle");
		expect(getMock).toHaveBeenCalledTimes(1);
		view.unmount();
		getMock.mockResolvedValueOnce({ data: { models: actual } });
		const next = renderHook(() => useConversationModels("worker", true), { wrapper });
		await waitFor(() => expect(next.result.current.models).toEqual(actual));
		expect(next.result.current.error).toBeUndefined();
		expect(getMock).toHaveBeenCalledTimes(2);
	});

	it("shares the pending request when the same worker is opened again", async () => {
		const { wrapper } = setup();
		let finish!: (value: unknown) => void;
		getMock.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
		const first = renderHook(() => useConversationModels("worker", true), { wrapper });
		await waitFor(() => expect(getMock).toHaveBeenCalledTimes(1));
		first.unmount();
		const second = renderHook(() => useConversationModels("worker", true), { wrapper });
		await act(async () => finish({ data: { models: actual } }));
		await waitFor(() => expect(second.result.current.models).toEqual(actual));
		expect(getMock).toHaveBeenCalledTimes(1);
	});

	it("keeps late responses scoped to their worker during rapid switching", async () => {
		const { client, wrapper } = setup();
		let finishA!: (value: unknown) => void;
		getMock.mockImplementation((_path, options) => {
			if (options.params.path.sessionId === "a") return new Promise((resolve) => { finishA = resolve; });
			return Promise.resolve({ data: { models: actual } });
		});
		const view = renderHook(({ id }) => useConversationModels(id, true), { wrapper, initialProps: { id: "a" } });
		view.rerender({ id: "b" });
		await waitFor(() => expect(view.result.current.models).toEqual(actual));
		await act(async () => finishA({ data: { models: [{ id: "other", default: true }] } }));
		expect(view.result.current.models).toEqual(actual);
		expect(client.getQueryData(conversationModelsQueryKey("a"))).toEqual([{ id: "other", default: true }]);
		expect(getMock).toHaveBeenCalledTimes(2);
	});

	it("waits for the controller and checks again after controller recovery", async () => {
		const { wrapper } = setup();
		getMock.mockResolvedValue({ data: { models: actual } });
		const view = renderHook(({ ready }) => useConversationModels("worker", ready), { wrapper, initialProps: { ready: false } });
		expect(getMock).not.toHaveBeenCalled();
		view.rerender({ ready: true });
		await waitFor(() => expect(view.result.current.models).toEqual(actual));
		view.rerender({ ready: false });
		view.rerender({ ready: true });
		await waitFor(() => expect(getMock).toHaveBeenCalledTimes(2));
	});
});
