import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { homedir } from "node:os";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { _electron as electron } from "playwright";

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const executablePath = resolve(process.argv[2] || join(frontend, "out/Fleet-win32-x64/fleet.exe"));
const labBase = resolve(process.env.AO_FLEET_TEST_ROOT || join(frontend, "out/fleet-smoke"));
mkdirSync(labBase, { recursive: true });
const home = mkdtempSync(join(labBase, "user-"));
const state = join(home, ".ao/fleet");
const officialHome = homedir();
const protocol = () => spawnSync("reg.exe", ["query", "HKCU\\Software\\Classes\\ao-app\\shell\\open\\command", "/ve"], { encoding: "utf8", windowsHide: true }).stdout;
const originalProtocol = protocol();
async function health(port) {
	try { return await (await fetch(`http://127.0.0.1:${port}/healthz`, { signal: AbortSignal.timeout(2000) })).json(); }
	catch { return null; }
}
const originalDaemon = await health(3001);
const env = {
	...process.env,
	USERPROFILE: home, HOME: home, CODEX_HOME: join(home, ".codex"),
	GH_CONFIG_DIR: join(home, ".config/gh"),
	APPDATA: join(home, "AppData/Roaming"), LOCALAPPDATA: join(home, "AppData/Local"),
	// Deliberately conflicting inherited variables must never reach the daemon.
	AO_DATA_DIR: join(officialHome, ".ao/data"),
	AO_RUN_FILE: join(officialHome, ".ao/running.json"),
	AO_PORT: "3001", AO_DAEMON_COMMAND: "fleet-smoke-must-not-run",
	AO_FLEET_PORT: "13011", ELECTRON_ENABLE_LOGGING: "1",
};
delete env.AO_FLEET_HOME;
delete env.ELECTRON_RUN_AS_NODE;
for (const directory of [env.CODEX_HOME, env.APPDATA, env.LOCALAPPDATA, ...["Downloads", "Desktop", "Documents", "Pictures", "Music", "Videos"].map((name) => join(home, name))]) mkdirSync(directory, { recursive: true });
if (await health(13011)) throw new Error("Smoke port 13011 is already occupied; close the previous test instance first");
let application;
let report;
let verifiedDaemonPid;
const messages = [];
try {
	application = await electron.launch({ executablePath, env, timeout: 60_000 });
	application.process().stderr?.on("data", (data) => messages.push(String(data)));
	const identity = await application.evaluate(({ app }) => ({ name: app.getName(), userData: app.getPath("userData"), sessionData: app.getPath("sessionData"), dataDir: process.env.AO_DATA_DIR, runFile: process.env.AO_RUN_FILE, port: process.env.AO_PORT }));
	assert.equal(identity.name, "AO Fleet");
	assert.equal(identity.userData, join(state, "electron"));
	assert.equal(identity.sessionData, identity.userData);
	assert.equal(identity.dataDir, join(state, "data"));
	assert.equal(identity.runFile, join(state, "running.json"));
	assert.equal(identity.port, "13011");
	console.log("Fleet profile isolated; waiting for its renderer and daemon.");
	let page;
	const deadline = Date.now() + 90_000;
	while (Date.now() < deadline) {
		page = application.windows().find((candidate) => candidate.url().startsWith("app://renderer/"));
		if (page && await page.evaluate(() => window.ao?.daemon.getStatus().then((s) => s.state === "ready")).catch(() => false)) break;
		await new Promise((resolve) => setTimeout(resolve, 300));
	}
	assert.ok(page, "packaged renderer did not load");
	const daemon = await page.evaluate(() => window.ao.daemon.getStatus());
	assert.equal(daemon.state, "ready", JSON.stringify(daemon));
	assert.equal(daemon.port, 13011);
	const runFile = JSON.parse(readFileSync(identity.runFile, "utf8"));
	const fleetHealth = await health(13011);
	assert.equal(runFile.pid, fleetHealth?.pid);
	assert.equal(resolve(fleetHealth.executablePath), join(dirname(executablePath), "resources/daemon/ao.exe"));
	verifiedDaemonPid = fleetHealth.pid;
	const updates = await page.evaluate(async () => {
		await window.ao.updateSettings.set({ enabled: true, channel: "latest", nightlyAck: false, feature: null });
		await window.ao.updates.check();
		await window.ao.updates.download();
		await window.ao.updates.install();
		await window.ao.updates.returnHome();
		return { status: await window.ao.updates.getStatus(), settings: await window.ao.updateSettings.get() };
	});
	assert.equal(updates.status.state, "unsupported");
	assert.equal(updates.settings.enabled, false);
	assert.equal(protocol(), originalProtocol, "Fleet modified the official ao-app:// registration");
	const officialAfter = await health(3001);
	if (originalDaemon) assert.equal(officialAfter?.pid, originalDaemon.pid, "official daemon changed during Fleet startup");
	// Wait for usable UI, not just the early supervisor-ready notification.
	await page.getByRole("button", { name: "Settings", exact: true }).first().click({ timeout: 60_000 });
	await page.getByTestId("settings-page").waitFor();
	assert.equal(await page.getByRole("button", { name: "Updates", exact: true }).count(), 0);
	assert.equal(await page.title(), "AO Fleet");
	assert.ok((await application.evaluate(({ BaseWindow }) => BaseWindow.getAllWindows().map((win) => win.getTitle()))).includes("AO Fleet"));
	// Exercise the real Help > About IPC without leaving a native modal open.
	await application.evaluate(({ dialog }) => {
		const original = dialog.showMessageBox;
		globalThis.__restoreFleetAbout = () => { dialog.showMessageBox = original; };
		dialog.showMessageBox = async (...args) => {
			globalThis.__fleetAbout = args.at(-1);
			return { response: 0, checkboxChecked: false };
		};
	});
	let about;
	try {
		await page.evaluate(() => window.ao.menu.action("help.about"));
		about = await application.evaluate(() => globalThis.__fleetAbout);
		assert.equal(about.title, "About AO Fleet");
		assert.equal(about.message, "AO Fleet");
	} finally {
		await application.evaluate(() => { globalThis.__restoreFleetAbout(); delete globalThis.__restoreFleetAbout; delete globalThis.__fleetAbout; });
	}
	await page.screenshot({ path: join(home, "fleet.png") });
	report = { identity, daemon, fleetPid: fleetHealth.pid, officialDaemonPreserved: originalDaemon ? originalDaemon.pid === officialAfter?.pid : "not running", protocolPreserved: true, updates, updatesEntryHidden: true, about, screenshot: join(home, "fleet.png") };
} finally {
	try {
		// First-run onboarding can create a durable GitHub login PTY. Close only
		// terminals from the verified scratch daemon before it stops, so tests
		// do not leave an ao.exe PTY host locking the next package build.
		if (verifiedDaemonPid && (await health(13011))?.pid === verifiedDaemonPid) {
			const base = "http://127.0.0.1:13011/api/v1/shell-terminals";
			const response = await fetch(base, { signal: AbortSignal.timeout(5000) });
			assert.ok(response.ok, "could not list smoke terminals for cleanup");
			for (const terminal of (await response.json()).shellTerminals ?? []) {
				const closed = await fetch(`${base}/${encodeURIComponent(terminal.handleId)}`, { method: "DELETE", signal: AbortSignal.timeout(5000) });
				assert.ok(closed.ok, "could not close a smoke terminal");
			}
		}
	} finally {
		if (application) await application.close();
		writeFileSync(join(home, "electron.log"), messages.join(""));
	}
}
const stoppedDeadline = Date.now() + 10_000;
while (Date.now() < stoppedDeadline && await health(13011)) await new Promise((resolve) => setTimeout(resolve, 200));
assert.equal(await health(13011), null, "Fleet left its app-owned daemon running");
if (originalDaemon) assert.equal((await health(3001))?.pid, originalDaemon.pid, "closing Fleet stopped the official daemon");
report.daemonStoppedOnQuit = true;
writeFileSync(join(home, "result.json"), JSON.stringify(report, null, 2));
console.log(JSON.stringify(report, null, 2));
