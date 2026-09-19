import type { ForgeConfig } from "@electron-forge/shared-types";
import profile from "./profile.json";

// Windows directory package overlay; the upstream build keeps its defaults.
export function fleetForgeConfig(base: ForgeConfig): ForgeConfig {
	return {
		...base,
		packagerConfig: {
			...base.packagerConfig,
			name: profile.name,
			appBundleId: profile.appId,
			executableName: profile.executableName,
			protocols: [],
		},
		// No installer, protocol registration or publisher for this build flavor.
		makers: [],
		publishers: [],
	};
}
