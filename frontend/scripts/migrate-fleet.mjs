import { homedir } from "node:os";
import path from "node:path";
import { parseArgs } from "node:util";
import { apply, inspect, rollback } from "../fork-fleet/migration.mjs";

const { values } = parseArgs({ options: {
	apply: { type: "boolean" }, rollback: { type: "string" },
	"source-root": { type: "string" }, "fleet-root": { type: "string" },
	"codex-home": { type: "string" }, help: { type: "boolean" },
} });
if (values.help) {
	console.log("node scripts/migrate-fleet.mjs [--apply | --rollback <migration.json>] [--source-root <~/.ao>] [--fleet-root <~/.ao/fleet>] [--codex-home <~/.codex>]\nDefault: read-only inspection. Apply/rollback require both applications and all AO hosts stopped. Requires Node.js 24+ and Git; Windows also uses robocopy and PowerShell. Only same-machine Codex Chat migration to an initialized, session-empty Fleet database is supported.");
} else {
	try {
		if (values.apply && values.rollback) throw new Error("Choose apply or rollback, not both");
		const options = {
			sourceRoot: path.resolve(values["source-root"] || path.join(homedir(), ".ao")),
			targetRoot: path.resolve(values["fleet-root"] || process.env.AO_FLEET_HOME || path.join(homedir(), ".ao/fleet")),
			codexHome: path.resolve(values["codex-home"] || process.env.CODEX_HOME || path.join(homedir(), ".codex")),
			log: console.log,
		};
		if (values.rollback) console.log(JSON.stringify(await rollback(path.resolve(values.rollback), options), null, 2));
		else if (values.apply) console.log(JSON.stringify(await apply(options), null, 2));
		else {
			const plan = await inspect(options);
			console.log(JSON.stringify({ source: plan.source, target: plan.target, sessions: plan.sessions, native: plan.native, files: plan.files, bytes: plan.bytes, worktrees: plan.worktrees.length, links: plan.links.length, mode: "inspection only; no data changed" }, null, 2));
		}
	} catch (error) { console.error(error.message); process.exitCode = 1; }
}
