import { app } from "electron";
import { mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { resolveFleetRuntime } from "../../fork-fleet/runtime";
import { isFleetPortable } from "../shared/desktop-flavor";
import { resolveDaemonLaunch as resolveBaseDaemonLaunch } from "../shared/daemon-launch";

// Import first: cloud auth and telemetry read their environment at module load.
export const fleetRuntime = isFleetPortable ? resolveFleetRuntime(homedir(), process.env) : null;
if (fleetRuntime) {
	Object.assign(process.env, fleetRuntime.env);
	mkdirSync(fleetRuntime.electronDir, { recursive: true });
	app.setPath("userData", fleetRuntime.electronDir);
	app.setPath("sessionData", fleetRuntime.electronDir);
	app.setPath("crashDumps", fleetRuntime.electronDir);
}

export function resolveDesktopDaemonLaunch(...args: Parameters<typeof resolveBaseDaemonLaunch>) {
	if (fleetRuntime) args[0] = { ...args[0], ...fleetRuntime.env };
	const launch = resolveBaseDaemonLaunch(...args);
	return fleetRuntime && launch ? { ...launch, cwd: fleetRuntime.root } : launch;
}
