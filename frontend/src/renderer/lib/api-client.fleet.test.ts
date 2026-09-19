import { afterEach, expect, it, vi } from "vitest";
vi.mock("../../shared/desktop-flavor", () => ({ isFleetPortable: true }));
vi.mock("./telemetry", () => ({ captureRendererEvent: vi.fn() }));
vi.mock("./sentry", () => ({ captureApiErrorToSentry: vi.fn() }));
afterEach(() => { vi.unstubAllEnvs(); vi.restoreAllMocks(); });

it("ignores a baked official API override and waits for Fleet's handshake", async () => {
	vi.stubEnv("VITE_AO_API_BASE_URL", "http://127.0.0.1:3001");
	const fetch = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response('{"projects":[]}', { headers: { "Content-Type": "application/json" } }));
	const { apiClient, hasTrustedApiBaseUrl, setApiBaseUrl } = await import("./api-client");
	expect(hasTrustedApiBaseUrl()).toBe(false);
	expect((await apiClient.GET("/api/v1/projects")).response.status).toBe(503);
	expect(fetch).not.toHaveBeenCalled();
	setApiBaseUrl("http://127.0.0.1:13001");
	await apiClient.GET("/api/v1/projects");
	expect(String(fetch.mock.calls[0][0])).toBe("http://127.0.0.1:13001/api/v1/projects");
});
