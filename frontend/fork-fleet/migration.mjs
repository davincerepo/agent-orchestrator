// Offline, same-machine migration. Kept outside the AO daemon/API and renderer.
import { DatabaseSync, backup } from "node:sqlite";
import { spawnSync } from "node:child_process";
import { promises as fs, existsSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";

const quote = (value) => `"${value.replaceAll('"', '""')}"`;
const normalized = (value) => path.resolve(value).replaceAll("\\", "/").replace(/\/$/, "");
const key = (value) => process.platform === "win32" ? normalized(value).toLowerCase() : normalized(value);
export const within = (root, value) => key(value) === key(root) || key(value).startsWith(`${key(root)}/`);
export function remap(value, source, target) {
	if (typeof value !== "string" || !path.isAbsolute(value) || !within(source, value)) return value;
	return path.join(target, path.relative(source, value));
}
function command(exe, args) {
	const result = spawnSync(exe, args, { encoding: "utf8", windowsHide: true, maxBuffer: 16 * 1024 * 1024 });
	if (result.error || result.status !== 0) throw new Error(`${exe} failed: ${result.error?.message || result.stderr || result.stdout}`);
	return result.stdout.trim();
}
const git = (cwd, ...args) => command("git", ["-C", cwd, ...args]);
function open(file) {
	const db = new DatabaseSync(file, { readOnly: true });
	db.exec("PRAGMA query_only=ON; PRAGMA busy_timeout=5000");
	return db;
}
function schema(db) {
	return db.prepare("SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY type,name").all();
}
function compatibleSchema(source, target) {
	const sourceSchema = schema(source), targetSchema = schema(target);
	if (JSON.stringify(sourceSchema) === JSON.stringify(targetSchema)) return true;
	// Fleet 140 only adds model parameters. Compare the exact resulting schema
	// in memory; never patch the official database or accept unrelated drift.
	const version = (db) => db.prepare("SELECT MAX(version_id) AS n FROM goose_db_version WHERE is_applied=1").get().n;
	if (!sourceSchema.some((entry) => entry.name === "goose_db_version") ||
		!targetSchema.some((entry) => entry.name === "goose_db_version") ||
		version(source) !== 139 || version(target) !== 140) return false;
	const probe = new DatabaseSync(":memory:");
	try {
		for (const name of ["conversations", "sessions"]) {
			const table = sourceSchema.find((entry) => entry.type === "table" && entry.name === name);
			if (!table) return false;
			probe.exec(table.sql);
		}
		probe.exec("ALTER TABLE conversations ADD COLUMN service_tier TEXT; ALTER TABLE sessions ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT ''; ALTER TABLE sessions ADD COLUMN service_tier TEXT NOT NULL DEFAULT '';");
		const tables = new Map(schema(probe).filter((entry) => entry.type === "table").map((entry) => [entry.name, entry]));
		return JSON.stringify(sourceSchema.map((entry) => entry.type === "table" ? tables.get(entry.name) ?? entry : entry)) === JSON.stringify(targetSchema);
	} catch {
		return false;
	} finally { probe.close(); }
}
const count = (db, table) => Number(db.prepare(`SELECT count(*) AS n FROM ${quote(table)}`).get().n);
function checkDatabase(db) {
	if (db.prepare("PRAGMA quick_check").get().quick_check !== "ok") throw new Error("Database quick_check failed");
	if (db.prepare("PRAGMA foreign_key_check").all().length) throw new Error("Database foreign_key_check failed");
}
function checkSessions(db) {
	const unsupported = db.prepare("SELECT count(*) AS n FROM sessions WHERE harness <> 'codex' OR session_mode <> 'chat'").get().n;
	if (unsupported) throw new Error("This migration supports Codex Chat sessions only; other harnesses/TUI need a separate migration");
	const busy = db.prepare("SELECT count(*) AS n FROM conversation_turns WHERE state NOT IN ('completed','failed','interrupted','cancelled')").get().n;
	if (busy) throw new Error("There are unfinished conversation turns; stop/settle them in AO before migration");
}
async function realDirectory(directory) {
	const absolute = path.resolve(directory);
	const real = await fs.realpath(absolute);
	if (key(real) !== key(absolute)) throw new Error(`Directory aliases/junctions are not supported: ${absolute}`);
	return absolute;
}
async function roots(sourceRoot, targetRoot) {
	const source = await realDirectory(path.join(sourceRoot, "data"));
	const target = await realDirectory(path.join(targetRoot, "data"));
	if (within(source, target) || within(target, source)) throw new Error("Source and target data directories must be separate");
	return { source, target };
}
export async function assertStopped(sourceRoot, targetRoot) {
	for (const root of [sourceRoot, targetRoot]) {
		const file = path.join(root, "running.json");
		if (!existsSync(file)) continue;
		const { pid } = JSON.parse(await fs.readFile(file, "utf8"));
		if (!Number.isInteger(pid) || pid <= 0) throw new Error(`Invalid run file: ${file}`);
		let alive = false;
		try { process.kill(pid, 0); alive = true; } catch (error) { if (error.code !== "ESRCH") throw error; }
		if (alive) throw new Error(`Close AO/Fleet and their agents first (PID ${pid})`);
	}
	if (process.platform === "win32") {
		// Persistent hosts can survive closing the desktop and deleting running.json.
		const output = command("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", "ConvertTo-Json -Compress -InputObject @(Get-CimInstance Win32_Process -Filter \"Name = 'ao.exe' OR Name = 'fleet.exe' OR Name = 'agent-orchestrator.exe'\" | Select-Object ProcessId,CommandLine)"]);
		const processes = output ? JSON.parse(output) : [];
		const defaultProfile = key(sourceRoot) === key(path.join(homedir(), ".ao")) || key(targetRoot) === key(path.join(homedir(), ".ao/fleet"));
		if (processes.some((proc) => defaultProfile || [sourceRoot, targetRoot].some((root) => (proc.CommandLine || "").toLowerCase().replaceAll("\\", "/").includes(key(root))))) throw new Error("AO/Fleet or a persistent AO host is still running; close it before applying/rolling back");
	}
}
const excluded = new Set(["ao.db", "ao.db-wal", "ao.db-shm", "runtime", "mobile"]);
function include(relative) {
	const first = relative.split(path.sep)[0];
	return !excluded.has(first) && !first.startsWith("telemetry_");
}
async function inventory(root) {
	const links = [], gitFiles = [];
	let files = 0, bytes = 0;
	async function walk(directory, insideGit = false) {
		for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
			const file = path.join(directory, entry.name), relative = path.relative(root, file);
			if (!include(relative)) continue;
			if (entry.isSymbolicLink()) {
				const link = await fs.readlink(file);
				const destination = path.resolve(directory, link);
				let isDirectory;
				try { isDirectory = (await fs.stat(file)).isDirectory(); }
				catch { throw new Error(`Broken link must be resolved before migration: ${file}`); }
				links.push({ relative, link, destination, directory: isDirectory });
			} else if (entry.isDirectory()) {
				await walk(file, insideGit || entry.name === ".git");
			} else if (entry.isFile()) {
				files++; bytes += (await fs.stat(file)).size;
				if (!insideGit && entry.name === ".git") gitFiles.push(file);
			} else throw new Error(`Unsupported filesystem entry: ${file}`);
		}
	}
	await walk(root);
	const worktrees = [];
	for (const file of gitFiles) {
		const cwd = path.dirname(file);
		const admin = git(cwd, "rev-parse", "--absolute-git-dir");
		const common = git(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir");
		// Submodules have .git files too, but no worktree registration to repair.
		if (!existsSync(path.join(admin, "gitdir"))) {
			throw new Error(`Submodule/unrecognized .git indirection needs manual migration: ${cwd}`);
		}
		const registered = (await fs.readFile(path.join(admin, "gitdir"), "utf8")).trim();
		if (key(registered) !== key(file)) throw new Error(`Worktree registration already points elsewhere: ${cwd}`);
		if (existsSync(path.join(admin, "locked"))) throw new Error(`Unlock this worktree before migration: ${cwd}`);
		worktrees.push({ relative: path.relative(root, cwd), admin, common, gitFile: await fs.readFile(file, "utf8"), registration: registered });
	}
	return { files, bytes, links, worktrees };
}
async function nativeSessions(db, codexHome) {
	const rows = db.prepare("SELECT id,is_terminated,provider_conversation_id FROM sessions").all();
	const found = new Set();
	async function walk(folder) {
		if (!existsSync(folder)) return;
		for (const entry of await fs.readdir(folder, { withFileTypes: true })) {
			if (entry.isDirectory()) await walk(path.join(folder, entry.name));
			else if (entry.isFile() && entry.name.endsWith(".jsonl")) {
				for (const row of rows) if (row.provider_conversation_id && entry.name.includes(row.provider_conversation_id)) found.add(row.id);
			}
		}
	}
	await walk(path.join(codexHome, "sessions"));
	await walk(path.join(codexHome, "archived_sessions"));
	const missing = rows.filter((row) => !found.has(row.id));
	if (missing.some((row) => !row.is_terminated)) throw new Error(`Native Codex history missing for active sessions: ${missing.filter((r) => !r.is_terminated).map((r) => r.id).join(", ")}`);
	return { found: found.size, missingArchived: missing.map((row) => row.id) };
}
export async function inspect({ sourceRoot, targetRoot, codexHome, log = () => {} }) {
	const { source, target } = await roots(sourceRoot, targetRoot);
	const src = open(path.join(source, "ao.db")), dst = open(path.join(target, "ao.db"));
	try {
		if (!compatibleSchema(src, dst)) throw new Error("Database schemas differ; initialize Fleet with a compatible build before migration");
		if (count(dst, "sessions")) throw new Error("Fleet already contains sessions; merging histories is not supported");
		checkSessions(src);
		const sessions = count(src, "sessions");
		const native = await nativeSessions(src, codexHome);
		log("Inspecting files, links, and Git worktree registrations...");
		const tree = await inventory(source);
		for (const row of src.prepare("SELECT id,workspace_path FROM sessions WHERE is_terminated=0").all()) {
			if (!row.workspace_path || !within(source, row.workspace_path) || !existsSync(row.workspace_path)) throw new Error(`Active workspace missing or outside source data: ${row.id}`);
		}
		return { sourceRoot: path.resolve(sourceRoot), targetRoot: path.resolve(targetRoot), source, target, codexHome: path.resolve(codexHome), sessions, native, ...tree };
	} finally { src.close(); dst.close(); }
}
async function copyData(source, target) {
	await fs.mkdir(target);
	if (process.platform === "win32") {
		// Preserve credentials' ACLs, copy junctions rather than following them.
		const omitted = [...excluded, ...((await fs.readdir(source)).filter((n) => n.startsWith("telemetry_")))].map((n) => path.join(source, n));
		const result = spawnSync("robocopy.exe", [source, target, "/E", "/COPY:DATS", "/DCOPY:DAT", "/SL", "/SJ", "/R:1", "/W:1", "/MT:16", "/NFL", "/NDL", "/NJH", "/NJS", "/NP", "/XD", ...omitted, "/XF", ...omitted], { encoding: "utf8", windowsHide: true, maxBuffer: 8 * 1024 * 1024 });
		if (result.error || result.status === null || result.status >= 8) throw new Error(`Copy failed: ${result.error?.message || result.stdout || result.stderr}`);
	} else {
		for (const entry of await fs.readdir(source)) {
			if (include(entry)) await fs.cp(path.join(source, entry), path.join(target, entry), { recursive: true, verbatimSymlinks: true, preserveTimestamps: true, errorOnExist: true, force: false });
		}
	}
}
function rewriteJSON(value, source, target, name = "") {
	if (Array.isArray(value)) return value.map((v) => rewriteJSON(v, source, target, name));
	if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, rewriteJSON(v, source, target, k)]));
	return /path|dir|home|cwd/i.test(name) ? remap(value, source, target) : value;
}
function rewriteDatabase(file, source, target) {
	const db = new DatabaseSync(file);
	try {
		db.exec("PRAGMA foreign_keys=ON; BEGIN IMMEDIATE");
		const tables = db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").all();
		for (const { name } of tables) {
			const columns = db.prepare(`PRAGMA table_info(${quote(name)})`).all();
			const primary = columns.filter((c) => c.pk).sort((a, b) => a.pk - b.pk).map((c) => c.name);
			if (!primary.length) primary.push("_rowid_");
			for (const col of columns) {
				// Only operational absolute path fields; never rewrite prose, commands,
				// provider event logs, or the immutable change_log.
				const isPath = /(?:^|_)(?:path|dir|home)$/.test(col.name);
				const isJSON = ["config", "delivery_content_json", "detail_json", "reviewer_agent_config"].includes(col.name);
				if (!isPath && !isJSON) continue;
				const values = db.prepare(`SELECT ${primary.map((pk, i) => `${quote(pk)} AS migration_key_${i}`).join(",")}, ${quote(col.name)} AS value FROM ${quote(name)} WHERE ${quote(col.name)} IS NOT NULL AND ${quote(col.name)} <> ''`).all();
				const update = db.prepare(`UPDATE ${quote(name)} SET ${quote(col.name)}=? WHERE ${primary.map((pk) => `${quote(pk)} IS ?`).join(" AND ")}`);
				for (const row of values) {
					const { value } = row;
					let updated = value;
					if (isPath) updated = remap(value, source, target);
					else {
						try {
							const parsed = JSON.parse(value);
							const mapped = JSON.stringify(rewriteJSON(parsed, source, target));
							// Preserve the original escaping/formatting if no path changed.
							if (mapped !== JSON.stringify(parsed)) updated = mapped;
						} catch { continue; }
					}
					if (updated !== value) update.run(updated, ...primary.map((_, i) => row[`migration_key_${i}`]));
				}
			}
		}
		// No imported row may reconnect to the old application's live resources.
		db.exec("DELETE FROM shell_terminals; UPDATE sessions SET runtime_handle_id='',runtime_launch_id='',agent_session_id_launch_id='',controller_generation='',browser_capability_verifier='',preview_url='',activity_state=CASE WHEN is_terminated=0 THEN 'exited' ELSE activity_state END");
		db.exec("COMMIT");
		checkDatabase(db);
		db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
	} finally { db.close(); }
}
async function save(file, value) {
	await fs.writeFile(`${file}.tmp`, JSON.stringify(value, null, 2));
	await fs.rename(`${file}.tmp`, file);
}
async function repairWorktree(plan, worktree, forward) {
	const to = path.join(forward ? plan.target : plan.source, worktree.relative);
	const common = forward ? remap(worktree.common, plan.source, plan.target) : worktree.common;
	const admin = forward ? remap(worktree.admin, plan.source, plan.target) : worktree.admin;
	const gitFile = path.join(to, ".git");
	const contents = forward ? `gitdir: ${normalized(admin)}\n` : worktree.gitFile;
	if (await fs.readFile(gitFile, "utf8") !== contents) {
		// Git marks .git hidden on Windows; opening an existing hidden file with
		// O_TRUNC fails with EPERM. Update through r+ without changing attributes.
		const handle = await fs.open(gitFile, "r+");
		try { await handle.writeFile(contents); await handle.truncate(Buffer.byteLength(contents)); }
		finally { await handle.close(); }
	}
	command("git", ["--git-dir", common, "worktree", "repair", to]);
	if (key(git(to, "rev-parse", "--show-toplevel")) !== key(to)) throw new Error(`Git worktree verification failed: ${to}`);
}
async function withMigrationLock(root, operation) {
	// SQLite supplies an OS-backed exclusive lock released even on process crash.
	// This tiny separate DB never contains application data or credentials.
	const lock = new DatabaseSync(path.join(root, "migration-lock.db"));
	try {
		try { lock.exec("PRAGMA busy_timeout=0; BEGIN EXCLUSIVE"); }
		catch { throw new Error("Another migration/rollback is already running for this Fleet profile"); }
		return await operation();
	} finally { lock.close(); }
}
export async function apply(options) {
	await roots(options.sourceRoot, options.targetRoot);
	return withMigrationLock(options.targetRoot, () => applyInternal(options));
}
async function applyInternal(options) {
	await assertStopped(options.sourceRoot, options.targetRoot);
	const plan = await inspect(options);
	const log = options.log || (() => {});
	const id = new Date().toISOString().replaceAll(/[:.]/g, "-");
	const folder = path.join(plan.targetRoot, "migrations", id);
	await fs.mkdir(folder, { recursive: true });
	const manifest = path.join(folder, "migration.json");
	const state = { version: 1, phase: "preparing", plan, repaired: [], folder };
	await save(manifest, state);
	const src = open(path.join(plan.source, "ao.db"));
	try {
		checkDatabase(src);
		log("Backing up the source database (including committed WAL data)...");
		await backup(src, path.join(folder, "official.ao.db"));
	} finally { src.close(); }
	try {
		log(`Copying ${plan.files} files (${(plan.bytes / 1024 ** 3).toFixed(2)} GiB); source files are retained...`);
		const staging = path.join(folder, "staged-data");
		await copyData(plan.source, staging);
		await fs.copyFile(path.join(folder, "official.ao.db"), path.join(staging, "ao.db"));
		log("Rewriting paths and validating the copied database...");
		rewriteDatabase(path.join(staging, "ao.db"), plan.source, plan.target);
		await assertStopped(plan.sourceRoot, plan.targetRoot);
		state.phase = "installing"; await save(manifest, state);
		await fs.rename(plan.target, path.join(folder, "previous-fleet-data"));
		await fs.rename(staging, plan.target);
		state.phase = "repairing"; await save(manifest, state);
		// Recreate links using final paths; never recurse through their targets.
		for (const link of plan.links) {
			const file = path.join(plan.target, link.relative);
			await fs.unlink(file);
			const destination = remap(link.destination, plan.source, plan.target);
			await fs.symlink(destination, file, link.directory ? "junction" : "file");
		}
		for (const worktree of plan.worktrees) {
			// Journal before mutation, so even a process crash can be rolled back.
			state.repaired.push(worktree.relative); await save(manifest, state);
			await repairWorktree(plan, worktree, true);
		}
		state.phase = "complete"; await save(manifest, state);
		log(`Migration complete. Journal and rollback backups: ${manifest}`);
		return { manifest, sessions: plan.sessions, worktrees: plan.worktrees.length, missingArchivedNativeHistory: plan.native.missingArchived };
	} catch (error) {
		state.error = error.message; await save(manifest, state);
		// Keep every copied file; rollback never recursively deletes user data.
		try { await rollbackInternal(manifest, { log }, true); }
		catch (rollbackError) { throw new Error(`${error.message}\nRollback requires attention: ${rollbackError.message}\nJournal: ${manifest}`); }
		throw new Error(`${error.message}\nOriginal registrations and Fleet data restored. Journal: ${manifest}`);
	}
}
export async function rollback(manifest, options = {}) {
	return rollbackInternal(manifest, options, false);
}
async function rollbackInternal(manifest, { log = () => {} } = {}, locked) {
	const state = JSON.parse(await fs.readFile(manifest, "utf8"));
	if (state.version !== 1 || !["preparing", "installing", "repairing", "complete", "rolling-back", "rolled-back"].includes(state.phase)) throw new Error("Unsupported migration journal");
	if (state.phase === "rolled-back") return { manifest, phase: state.phase };
	const { plan, folder } = state;
	if (key(path.dirname(path.resolve(manifest))) !== key(folder) || !within(path.join(plan.targetRoot, "migrations"), folder) || key(plan.source) !== key(path.join(plan.sourceRoot, "data")) || key(plan.target) !== key(path.join(plan.targetRoot, "data")) || within(plan.source, plan.target) || within(plan.target, plan.source)) throw new Error("Invalid migration journal paths");
	if (!locked) return withMigrationLock(plan.targetRoot, () => rollbackInternal(manifest, { log }, true));
	await assertStopped(plan.sourceRoot, plan.targetRoot);
	state.phase = "rolling-back"; await save(manifest, state);
	for (const relative of [...state.repaired].reverse()) {
		const worktree = plan.worktrees.find((item) => item.relative === relative);
		if (!worktree || !within(plan.source, path.join(plan.source, relative))) throw new Error("Invalid worktree in migration journal");
		await repairWorktree(plan, worktree, false);
		state.repaired = state.repaired.filter((item) => item !== relative); await save(manifest, state);
	}
	if (existsSync(path.join(folder, "previous-fleet-data"))) {
		if (existsSync(plan.target)) await fs.rename(plan.target, path.join(folder, `retained-fleet-data-${Date.now()}`));
		await fs.rename(path.join(folder, "previous-fleet-data"), plan.target);
	}
	state.phase = "rolled-back"; await save(manifest, state);
	log("Rollback complete. Original data restored; copied/migrated data retained in the migration folder.");
	return { manifest, phase: state.phase };
}
