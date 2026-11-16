// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { stopFleetProcesses } from "./fleet-quit";
import type { DaemonLaunchSpec } from "../shared/daemon-launch";
import { DAEMON_SERVICE_NAME } from "../shared/daemon-attach";

const launch: DaemonLaunchSpec = { command: "C:/Fleet/ao.exe", args: ["daemon"], cwd: "C:/FleetData", shell: false, source: "bundled" };
function fixture() {
	let live = true;
	let time = 0;
	const calls: string[] = [];
	const deps = {
		read: vi.fn(async () => JSON.stringify({ pid: 42, port: 13001, startedAt: "2026-09-19T00:00:00Z" })),
		fetch: vi.fn(async (url: string | URL | Request) => {
			calls.push(String(url));
			return new Response(JSON.stringify({ status: "ok", service: DAEMON_SERVICE_NAME, pid: 42, executablePath: launch.command }), { status: 200 });
		}) as typeof fetch,
		alive: vi.fn(() => live),
		now: () => time,
		sleep: vi.fn(async (ms: number) => { calls.push("wait"); time += ms; live = false; }),
		run: vi.fn(async () => { calls.push("hosts"); }),
	};
	return { deps, calls, setTime: (value: number) => { time = value; }, setLive: (value: boolean) => { live = value; } };
}

describe("Fleet complete exit", () => {
	it("waits for the daemon process, then stops hosts using the exact bundled launch", async () => {
		const { deps, calls } = fixture();
		const env = { AO_FLEET_HOME: "C:/FleetData" };
		await stopFleetProcesses(launch, env, "running.json", 42, deps);
		expect(calls).toEqual(["http://127.0.0.1:13001/healthz", "http://127.0.0.1:13001/shutdown", "wait", "hosts"]);
		expect(deps.run).toHaveBeenCalledWith(launch, env);
	});
	it("cleans orphaned hosts when the daemon is already gone", async () => {
		const { deps } = fixture();
		deps.read.mockRejectedValue(Object.assign(new Error("missing"), { code: "ENOENT" }));
		await stopFleetProcesses(launch, {}, "running.json", undefined, deps);
		expect(deps.fetch).not.toHaveBeenCalled();
		expect(deps.run).toHaveBeenCalledOnce();
	});
	it.each(["foreign executable", "different pid", "bad run file", "read denied", "HTTP failure", "host failure", "timeout"])("reports %s without claiming a full exit", async (failure) => {
		const { deps, setTime } = fixture();
		if (failure === "foreign executable" || failure === "different pid") {
			vi.mocked(deps.fetch).mockResolvedValue(new Response(JSON.stringify({ status: "ok", service: DAEMON_SERVICE_NAME, pid: failure === "different pid" ? 99 : 42, executablePath: "C:/Official/ao.exe" })));
		}
		if (failure === "bad run file") deps.read.mockResolvedValue("{");
		if (failure === "read denied") deps.read.mockRejectedValue(Object.assign(new Error("denied"), { code: "EACCES" }));
		if (failure === "HTTP failure") vi.mocked(deps.fetch).mockRejectedValue(new Error("offline"));
		if (failure === "host failure") deps.run.mockRejectedValue(new Error("host is still running"));
		if (failure === "timeout") deps.sleep.mockImplementation(async () => { setTime(60_001); });
		await expect(stopFleetProcesses(launch, {}, "running.json", 42, deps)).rejects.toThrow();
		if (failure !== "host failure") expect(deps.run).not.toHaveBeenCalled();
	});
});
