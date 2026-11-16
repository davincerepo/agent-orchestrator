import fleetProfile from "../../fork-fleet/profile.json";

// Replaced in both Vite builds. A launch-time environment variable cannot turn
// an official package into Fleet or remove isolation from a Fleet package.
declare const __AO_FLEET_PORTABLE__: boolean;
export const isFleetPortable = typeof __AO_FLEET_PORTABLE__ !== "undefined" && __AO_FLEET_PORTABLE__;
export const desktopProductName = isFleetPortable ? fleetProfile.displayName : "Agent Orchestrator";
