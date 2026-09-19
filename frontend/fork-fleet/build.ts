import profile from "./profile.json";

export function fleetBuildDefines(env: Record<string, string | undefined> = process.env) {
	return { __AO_FLEET_PORTABLE__: JSON.stringify(env.AO_DESKTOP_FLAVOR === profile.flavor) };
}
