import { describe, expect, it } from "vitest";
import path from "node:path";
import { resolveFleetRuntime } from "./runtime";
import { fleetBuildDefines } from "./build";
import { fleetForgeConfig } from "./forge";

const home = path.resolve("/fleet-test-user");

describe("Fleet isolation", () => {
	it("ignores inherited official AO routing and daemon overrides", () => {
		const runtime = resolveFleetRuntime(home, {
			AO_DATA_DIR: path.join(home, ".ao/data"), AO_RUN_FILE: path.join(home, ".ao/running.json"),
			AO_PORT: "3001", AO_DAEMON_COMMAND: "official-ao daemon", AO_CLOUD_AUTH_REDIRECT: "ao-app://callback",
		});
		expect(runtime.root).toBe(path.join(home, ".ao/fleet"));
		expect(runtime.env.AO_DATA_DIR).toBe(path.join(runtime.root, "data"));
		expect(runtime.env.AO_RUN_FILE).toBe(path.join(runtime.root, "running.json"));
		expect(runtime.env.AO_PORT).toBe("13001");
		expect(runtime.env.AO_FLEET_HOME).toBe(runtime.root);
		expect(runtime.env.AO_FLEET_PORT).toBe("13001");
		expect(runtime.env.AO_DAEMON_COMMAND).toBe("");
		expect(runtime.env.AO_CLOUD_AUTH_REDIRECT).toBe("http://127.0.0.1:3000/callback");
		for (const p of [runtime.electronDir, runtime.browserDir, runtime.logPath]) {
			expect(path.dirname(p)).toBe(runtime.root);
		}
	});
	it("accepts a dedicated absolute root and port", () => {
		const root = path.join(home, "fleet-lab");
		const runtime = resolveFleetRuntime(home, { AO_FLEET_HOME: root, AO_FLEET_PORT: "13002" });
		expect(runtime.root).toBe(root);
		expect(runtime.env.AO_PORT).toBe("13002");
		expect(runtime.env.AO_FLEET_HOME).toBe(root);
		expect(runtime.env.AO_FLEET_PORT).toBe("13002");
	});
	it.each(["", "data", "electron", "dev", "fleet/../data"])("rejects official AO state: %s", (suffix) => {
		expect(() => resolveFleetRuntime(home, { AO_FLEET_HOME: path.join(home, ".ao", suffix) })).toThrow("official AO");
	});
	it("rejects a relative data root", () => {
		expect(() => resolveFleetRuntime(home, { AO_FLEET_HOME: "data" })).toThrow("absolute");
	});
	it.each(["3001", "3002", "0", "65536", "NaN", "1.5"])("rejects unsafe port %s", (port) => {
		expect(() => resolveFleetRuntime(home, { AO_FLEET_PORT: port })).toThrow("AO_FLEET_PORT");
	});
});

describe("Fleet build overlay", () => {
	it("leaves the default flavor disabled", () => {
		expect(fleetBuildDefines({})).toEqual({ __AO_FLEET_PORTABLE__: "false" });
		expect(fleetBuildDefines({ AO_DESKTOP_FLAVOR: "fleet-portable" })).toEqual({ __AO_FLEET_PORTABLE__: "true" });
	});
	it("separates application identity without changing the upstream config", () => {
		const hooks = { prePackage: async () => {} };
		const base = { packagerConfig: { name: "Official", executableName: "agent-orchestrator", protocols: [{ name: "AO", schemes: ["ao-app"] }] }, hooks, publishers: [], makers: [] };
		const fleet = fleetForgeConfig(base);
		expect(fleet.hooks).toBe(hooks);
		expect(fleet.packagerConfig).toMatchObject({ name: "Fleet", executableName: "fleet", appBundleId: "dev.fleet.agent-orchestrator", protocols: [] });
		expect(fleet.makers).toEqual([]);
		expect(fleet.publishers).toEqual([]);
		expect(base.packagerConfig.name).toBe("Official");
		expect(base.packagerConfig.protocols[0].schemes).toEqual(["ao-app"]);
	});
});
