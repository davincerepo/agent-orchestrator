import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";
import { parseDaemonProbe } from "../shared/daemon-attach";
import { parseRunFile } from "../shared/daemon-discovery";
import { bundledDaemonIdentityError, type DaemonLaunchSpec } from "../shared/daemon-launch";

const runFile = promisify(execFile);
const alive = (pid: number) => {
	try { process.kill(pid, 0); return true; }
	catch (error) { return (error as NodeJS.ErrnoException).code !== "ESRCH"; }
};

const defaults = {
	read: (file: string) => readFile(file, "utf8"),
	fetch: globalThis.fetch,
	alive,
	now: Date.now,
	sleep: (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)),
	run: async (launch: DaemonLaunchSpec, env: NodeJS.ProcessEnv) => {
		await runFile(launch.command, [...launch.args, "--stop-background"], {
			cwd: launch.cwd, env, windowsHide: true, timeout: 100_000, maxBuffer: 32_768,
		});
	},
};

// Desktop supervision only: stop the validated daemon over its loopback API,
// wait for process exit (a closed HTTP port is insufficient), then delegate
// instance-scoped host cleanup to the bundled daemon's stop-only lifecycle.
export async function stopFleetProcesses(
	launch: DaemonLaunchSpec,
	env: NodeJS.ProcessEnv,
	handshakePath: string,
	ownedPid?: number,
	deps = defaults,
): Promise<void> {
	if (launch.shell || launch.args.join(" ") !== "daemon") {
		throw new Error("Fleet shutdown requires the bundled daemon executable.");
	}
	let contents: string | null = null;
	try { contents = await deps.read(handshakePath); }
	catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error; }
	const info = contents === null ? null : parseRunFile(contents);
	if (contents !== null && (!info || info.pid <= 0)) throw new Error("Fleet daemon ownership file is invalid.");
	const pids = new Set<number>(ownedPid ? [ownedPid] : []);
	if (info && deps.alive(info.pid)) {
		const base = `http://127.0.0.1:${info.port}`;
		const response = await deps.fetch(`${base}/healthz`, { signal: AbortSignal.timeout(5_000) });
		const probe = response.ok ? parseDaemonProbe("healthz", await response.json()) : null;
		if (!probe || probe.pid !== info.pid) throw new Error("Could not verify the running Fleet daemon; no process was killed.");
		const identityError = bundledDaemonIdentityError(probe, launch.command, undefined,
			(a, b) => path.resolve(a).toLowerCase() === path.resolve(b).toLowerCase());
		if (identityError) throw new Error(identityError);
		pids.add(info.pid);
		const stopped = await deps.fetch(`${base}/shutdown`, { method: "POST", signal: AbortSignal.timeout(5_000) });
		if (!stopped.ok) throw new Error(`Fleet shutdown returned HTTP ${stopped.status}.`);
	}
	const deadline = deps.now() + 60_000;
	while ([...pids].some(deps.alive)) {
		if (deps.now() >= deadline) throw new Error("Fleet daemon has not exited; background cleanup was not started. Retry after it stops.");
		await deps.sleep(100);
	}
	await deps.run(launch, env);
}
