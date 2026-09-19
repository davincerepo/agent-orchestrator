// Run with node --test, separately from the renderer's Vitest suite.
import { test } from "node:test";
import assert from "node:assert/strict";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { DatabaseSync } from "node:sqlite";
import { apply, assertStopped, inspect, remap, rollback } from "./migration.mjs";

const sql = `
CREATE TABLE projects(id TEXT PRIMARY KEY,path TEXT,config TEXT);
CREATE TABLE sessions(id TEXT PRIMARY KEY,project_id TEXT REFERENCES projects(id),harness TEXT,session_mode TEXT,is_terminated INT,workspace_path TEXT,workspace_repo_path TEXT,native_transcript_path TEXT,provider_conversation_id TEXT,runtime_handle_id TEXT,runtime_launch_id TEXT,agent_session_id_launch_id TEXT,controller_generation TEXT,browser_capability_verifier TEXT,preview_url TEXT,activity_state TEXT,prompt TEXT);
CREATE TABLE session_worktrees(session_id TEXT REFERENCES sessions(id),worktree_path TEXT);
CREATE TABLE conversation_turns(id TEXT,state TEXT);
CREATE TABLE conversation_messages(id TEXT,text TEXT,delivery_content_json TEXT);
CREATE TABLE shell_terminals(handle_id TEXT,working_dir TEXT);
`;
function git(directory, ...args) {
	const result = spawnSync("git", ["-C", directory, ...args], { encoding: "utf8", windowsHide: true });
	assert.equal(result.status, 0, result.stderr);
	return result.stdout.trim();
}
async function fixture(t) {
	const base = await fs.mkdtemp(path.join(os.tmpdir(), "fleet-migration-test-"));
	// Cleanup is confined to this test's resolved temporary directory.
	t.after(async () => {
		assert.ok(path.resolve(base).startsWith(path.join(os.tmpdir(), "fleet-migration-test-")));
		await fs.rm(base, { recursive: true, force: true });
	});
	const sourceRoot = path.join(base, "official"), targetRoot = path.join(base, "fleet"), codexHome = path.join(base, "codex");
	const source = path.join(sourceRoot, "data"), target = path.join(targetRoot, "data"), repo = path.join(base, "repo");
	for (const p of [source, target, repo, path.join(codexHome, "sessions")]) await fs.mkdir(p, { recursive: true });
	git(repo, "init", "-b", "main"); git(repo, "config", "user.name", "Fleet Test"); git(repo, "config", "user.email", "fleet@example.invalid");
	await fs.writeFile(path.join(repo, "tracked.txt"), "base\n"); git(repo, "add", "."); git(repo, "commit", "-m", "initial");
	const worktree = path.join(source, "worktrees", "project", "one");
	git(repo, "worktree", "add", "-b", "ao/one", worktree);
	await fs.writeFile(path.join(worktree, "tracked.txt"), "dirty work\n");
	await fs.writeFile(path.join(worktree, "untracked.txt"), "unsaved work\n");
	// A nested extra worktree not represented in the database must migrate too.
	const child = path.join(worktree, "extra"); git(repo, "worktree", "add", "-b", "ao/extra", child);
	await fs.mkdir(path.join(source, "attachments")); await fs.writeFile(path.join(source, "attachments", "test.txt"), "attachment");
	await fs.symlink(path.join(source, "attachments"), path.join(worktree, "linked-files"), "junction");
	await fs.mkdir(path.join(source, "runtime")); await fs.writeFile(path.join(source, "runtime", "host.json"), "old process handle");
	for (const p of [source, target]) { const db = new DatabaseSync(path.join(p, "ao.db")); db.exec(sql); db.close(); }
	const db = new DatabaseSync(path.join(source, "ao.db"));
	db.exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0");
	db.prepare("INSERT INTO projects VALUES(?,?,?)").run("project", repo, JSON.stringify({ worktreePath: worktree, description: worktree }));
	db.prepare("INSERT INTO sessions VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)").run("one", "project", "codex", "chat", 0, worktree, repo, "", "thread-123", "old-handle", "old-launch", "old-launch", "old-controller", "old-token", "http://127.0.0.1:3001", "idle", `Keep this historical text: ${worktree}`);
	db.prepare("INSERT INTO session_worktrees VALUES(?,?)").run("one", worktree);
	db.exec("INSERT INTO conversation_turns VALUES('turn','completed'); INSERT INTO shell_terminals VALUES('old-handle','old-cwd')");
	db.prepare("INSERT INTO conversation_messages VALUES(?,?,?)").run("message", worktree, JSON.stringify({ path: path.join(source, "attachments", "test.txt"), text: worktree }));
	db.close();
	await fs.writeFile(path.join(codexHome, "sessions", "rollout-thread-123.jsonl"), "native history");
	await fs.writeFile(path.join(target, "keep.txt"), "previous Fleet data");
	return { sourceRoot, targetRoot, codexHome, source, target, repo, worktree };
}

test("remapping respects directory boundaries and leaves prose/external paths alone", () => {
	const source = path.resolve("old/data"), target = path.resolve("new/data");
	assert.equal(remap(path.join(source, "file"), source, target), path.join(target, "file"));
	assert.equal(remap(`${source}-other/file`, source, target), `${source}-other/file`);
	assert.equal(remap(`text ${source}`, source, target), `text ${source}`);
});
test("migrates dirty/nested worktrees, links and history; rollback retains migrated files", async (t) => {
	const f = await fixture(t);
	const before = git(f.repo, "worktree", "list", "--porcelain");
	const plan = await inspect(f);
	assert.equal(plan.worktrees.length, 2); assert.equal(plan.links.length, 1);
	assert.equal(git(f.repo, "worktree", "list", "--porcelain"), before);
	const result = await apply(f);
	const migrated = remap(f.worktree, f.source, f.target);
	assert.equal(git(migrated, "rev-parse", "--show-toplevel").replaceAll("\\", "/"), migrated.replaceAll("\\", "/"));
	assert.match(git(migrated, "status", "--porcelain"), /tracked.txt/);
	assert.equal(await fs.readFile(path.join(migrated, "untracked.txt"), "utf8"), "unsaved work\n");
	assert.equal(await fs.realpath(path.join(migrated, "linked-files")), await fs.realpath(path.join(f.target, "attachments")));
	assert.equal(await fs.stat(path.join(f.target, "runtime")).catch(() => null), null);
	const db = new DatabaseSync(path.join(f.target, "ao.db"), { readOnly: true });
	const row = db.prepare("SELECT * FROM sessions").get();
	assert.equal(row.workspace_path, migrated); assert.equal(row.workspace_repo_path, f.repo);
	assert.equal(row.provider_conversation_id, "thread-123"); assert.equal(row.runtime_handle_id, ""); assert.equal(row.activity_state, "exited");
	assert.equal(row.prompt, `Keep this historical text: ${f.worktree}`);
	const message = db.prepare("SELECT * FROM conversation_messages").get();
	assert.equal(message.text, f.worktree); assert.equal(JSON.parse(message.delivery_content_json).path, path.join(f.target, "attachments", "test.txt"));
	assert.equal(JSON.parse(message.delivery_content_json).text, f.worktree);
	assert.equal(db.prepare("SELECT count(*) AS n FROM shell_terminals").get().n, 0); db.close();
	await fs.writeFile(path.join(migrated, "after-migration.txt"), "retain this on rollback");
	await rollback(result.manifest);
	assert.equal(git(f.repo, "worktree", "list", "--porcelain"), before);
	assert.equal(await fs.readFile(path.join(f.target, "keep.txt"), "utf8"), "previous Fleet data");
	const retained = (await fs.readdir(path.dirname(result.manifest))).find((n) => n.startsWith("retained-fleet-data-"));
	assert.equal(await fs.readFile(path.join(path.dirname(result.manifest), retained, path.relative(f.source, f.worktree), "after-migration.txt"), "utf8"), "retain this on rollback");
	assert.equal((await rollback(result.manifest)).phase, "rolled-back");
});
test("refuses incompatible schema, nonempty target, missing native history and busy turns", async (t) => {
	const f = await fixture(t);
	let db = new DatabaseSync(path.join(f.target, "ao.db")); db.exec("CREATE TABLE future_version(x)"); db.close();
	await assert.rejects(inspect(f), /schemas differ/);
	db = new DatabaseSync(path.join(f.target, "ao.db")); db.exec("DROP TABLE future_version; INSERT INTO sessions(id) VALUES('existing')"); db.close();
	await assert.rejects(inspect(f), /already contains sessions/);
	db = new DatabaseSync(path.join(f.target, "ao.db")); db.exec("DELETE FROM sessions"); db.close();
	await fs.unlink(path.join(f.codexHome, "sessions", "rollout-thread-123.jsonl"));
	await assert.rejects(inspect(f), /Native Codex history missing/);
	db = new DatabaseSync(path.join(f.source, "ao.db")); db.exec("UPDATE conversation_turns SET state='running'"); db.close();
	await assert.rejects(inspect(f), /unfinished conversation turns/);
});
test("refuses live daemons and aliased/overlapping data directories", async (t) => {
	const f = await fixture(t);
	const lock = new DatabaseSync(path.join(f.targetRoot, "migration-lock.db"));
	lock.exec("BEGIN EXCLUSIVE");
	try { await assert.rejects(apply(f), /already running/); } finally { lock.close(); }
	await fs.writeFile(path.join(f.sourceRoot, "running.json"), JSON.stringify({ pid: process.pid }));
	await assert.rejects(assertStopped(f.sourceRoot, f.targetRoot), /Close AO/);
	await assert.rejects(inspect({ ...f, targetRoot: f.sourceRoot }), /must be separate/);
	const alias = path.join(path.dirname(f.targetRoot), "alias"); await fs.symlink(f.targetRoot, alias, "junction");
	await assert.rejects(inspect({ ...f, targetRoot: alias }), /aliases/);
});
test("allows only Fleet 140 model columns and keeps the imported schema for daemon migration", async (t) => {
	const f = await fixture(t);
	for (const directory of [f.source, f.target]) {
		const db = new DatabaseSync(path.join(directory, "ao.db"));
		db.exec("CREATE TABLE conversations(id TEXT); CREATE TABLE goose_db_version(version_id INTEGER, is_applied INTEGER); INSERT INTO goose_db_version VALUES(139,1)");
		db.close();
	}
	let target = new DatabaseSync(path.join(f.target, "ao.db"));
	target.exec("ALTER TABLE conversations ADD COLUMN service_tier TEXT; ALTER TABLE sessions ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT ''; ALTER TABLE sessions ADD COLUMN service_tier TEXT NOT NULL DEFAULT ''; INSERT INTO goose_db_version VALUES(140,1)");
	target.close();
	assert.equal((await inspect(f)).sessions, 1);
	target = new DatabaseSync(path.join(f.target, "ao.db"));
	target.exec("CREATE INDEX unexpected_index ON sessions(id)"); target.close();
	await assert.rejects(inspect(f), /schemas differ/);
	target = new DatabaseSync(path.join(f.target, "ao.db"));
	target.exec("DROP INDEX unexpected_index; UPDATE goose_db_version SET version_id=141 WHERE version_id=140"); target.close();
	await assert.rejects(inspect(f), /schemas differ/);
	target = new DatabaseSync(path.join(f.target, "ao.db"));
	target.exec("UPDATE goose_db_version SET version_id=140 WHERE version_id=141"); target.close();
	const result = await apply(f);
	for (const directory of [f.source, f.target]) {
		const db = new DatabaseSync(path.join(directory, "ao.db"), { readOnly: true });
		assert.equal(db.prepare("SELECT MAX(version_id) AS n FROM goose_db_version").get().n, 139);
		assert.ok(!db.prepare("PRAGMA table_info(sessions)").all().some((column) => column.name === "service_tier"));
		db.close();
	}
	await rollback(result.manifest);
	target = new DatabaseSync(path.join(f.target, "ao.db"), { readOnly: true });
	assert.equal(target.prepare("SELECT MAX(version_id) AS n FROM goose_db_version").get().n, 140);
	target.close();
});
test("backs up committed WAL history and accepts missing history only for archived sessions", async (t) => {
	const f = await fixture(t);
	const source = new DatabaseSync(path.join(f.source, "ao.db"));
	try {
	source.exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0");
	source.exec("INSERT INTO conversation_messages VALUES('wal-only','committed in WAL','{}')");
	source.prepare("INSERT INTO sessions(id,harness,session_mode,is_terminated,provider_conversation_id) VALUES(?,?,?,?,?)").run("archived", "codex", "chat", 1, "missing-old-thread");
	assert.deepEqual((await inspect(f)).native.missingArchived, ["archived"]);
	const result = await apply(f);
	const destination = new DatabaseSync(path.join(f.target, "ao.db"), { readOnly: true });
	assert.equal(destination.prepare("SELECT text FROM conversation_messages WHERE id='wal-only'").get().text, "committed in WAL");
	destination.close();
	// Simulate a crash after the final registration repair, before marking done.
	const journal = JSON.parse(await fs.readFile(result.manifest, "utf8"));
	journal.phase = "repairing"; await fs.writeFile(result.manifest, JSON.stringify(journal));
	await rollback(result.manifest);
	assert.equal(git(f.worktree, "rev-parse", "--show-toplevel").replaceAll("\\", "/"), f.worktree.replaceAll("\\", "/"));
	} finally { source.close(); }
});
