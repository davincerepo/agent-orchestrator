import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

const fixture = vi.hoisted(() => ({ enabled: false, home: "", setPath: vi.fn() }));
vi.mock("electron", () => ({ app: { setPath: fixture.setPath } }));
vi.mock("../src/shared/desktop-flavor", () => ({ get isFleetPortable() { return fixture.enabled; } }));

beforeEach(() => {
	vi.resetModules();
	fixture.setPath.mockClear();
	fixture.home = mkdtempSync(path.join(tmpdir(), "fleet-bootstrap-"));
	for (const key of ["AO_FLEET_HOME", "AO_FLEET_PORT", "AO_DATA_DIR", "AO_RUN_FILE", "AO_PORT", "AO_DAEMON_COMMAND", "AO_DEV_DAEMON_BINARY", "AO_CLOUD_AUTH_REDIRECT", "AO_TELEMETRY_REMOTE", "AO_SENTRY_DSN"]) vi.stubEnv(key, undefined);
	vi.stubEnv("AO_FLEET_HOME", path.join(fixture.home, "fleet"));
});
afterEach(() => {
	vi.unstubAllEnvs();
	rmSync(fixture.home, { recursive: true, force: true });
});

it("has no side effects for the upstream build", async () => {
	fixture.enabled = false;
	const { fleetRuntime, resolveDesktopDaemonLaunch } = await import("../src/main/fleet-bootstrap");
	expect(fleetRuntime).toBeNull();
	expect(fixture.setPath).not.toHaveBeenCalled();
	expect(existsSync(path.join(fixture.home, ".ao"))).toBe(false);
	expect(resolveDesktopDaemonLaunch({ AO_DAEMON_COMMAND: "custom" }, true, "resources", "app", fixture.home, "win32")?.command).toBe("custom");
});

it("sets the profile before singleton setup and pins all daemon launches to Fleet", async () => {
	fixture.enabled = true;
	vi.stubEnv("AO_DATA_DIR", "official-data");
	const { fleetRuntime, resolveDesktopDaemonLaunch } = await import("../src/main/fleet-bootstrap");
	expect(fleetRuntime).not.toBeNull();
	expect(fixture.setPath).toHaveBeenCalledWith("userData", path.join(fixture.home, "fleet/electron"));
	expect(fixture.setPath).toHaveBeenCalledWith("sessionData", fleetRuntime!.electronDir);
	expect(process.env.AO_DATA_DIR).toBe(fleetRuntime!.env.AO_DATA_DIR);
	const launch = resolveDesktopDaemonLaunch({ AO_DAEMON_COMMAND: "official" }, true, "fleet/resources", "app", fixture.home, "win32");
	expect(launch).toMatchObject({ source: "bundled", command: "fleet/resources/daemon/ao.exe", cwd: fleetRuntime!.root });
	expect(existsSync(path.join(fixture.home, ".ao/data"))).toBe(false);
});
