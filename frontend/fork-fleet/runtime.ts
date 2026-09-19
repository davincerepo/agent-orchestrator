import path from "node:path";
import profile from "./profile.json";

export function resolveFleetRuntime(home: string, env: Record<string, string | undefined>) {
	const override = env.AO_FLEET_HOME?.trim();
	if (override && !path.isAbsolute(override)) throw new Error("AO_FLEET_HOME must be an absolute path");
	const officialRoot = path.resolve(home, ".ao");
	const root = path.resolve(override || path.join(officialRoot, "fleet"));
	const relative = path.relative(officialRoot, root);
	const insideOfficial = relative === "" || (!relative.startsWith(`..${path.sep}`) && relative !== ".." && !path.isAbsolute(relative));
	if (insideOfficial && relative !== "fleet" && !relative.startsWith(`fleet${path.sep}`)) {
		throw new Error("Fleet must use ~/.ao/fleet or a directory outside the official AO state root");
	}
	const port = Number(env.AO_FLEET_PORT?.trim() || profile.port);
	if (!Number.isInteger(port) || port < 1 || port > 65535 || port === 3001 || port === 3002) {
		throw new Error("AO_FLEET_PORT must be 1-65535 and different from AO's 3001/3002 ports");
	}
	return {
		root,
		electronDir: path.join(root, "electron"),
		browserDir: path.join(root, "br"),
		logPath: path.join(root, "daemon.log"),
		env: {
			AO_DATA_DIR: path.join(root, "data"),
			AO_RUN_FILE: path.join(root, "running.json"),
			AO_PORT: String(port),
			// Always launch the binary shipped in this package, even when started
			// from an official AO terminal carrying its own overrides.
			AO_DAEMON_COMMAND: "",
			AO_DEV_DAEMON_BINARY: "",
			// Existing loopback OAuth support avoids claiming ao-app:// globally.
			AO_CLOUD_AUTH_REDIRECT: "http://127.0.0.1:3000/callback",
			AO_TELEMETRY_REMOTE: "off",
			AO_SENTRY_DSN: "",
		},
	};
}
