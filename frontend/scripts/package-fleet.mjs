import { createHash } from "node:crypto";
import { createReadStream, existsSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(import.meta.url);
const profile = JSON.parse(readFileSync(join(root, "fork-fleet/profile.json"), "utf8"));
const { version } = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
if (process.platform !== "win32" || process.arch !== "x64") throw new Error("Build Fleet portable on Windows x64");
if (Number(process.versions.node.split(".")[0]) < 24) throw new Error("Use Node.js 24+ to build Fleet portable");
const env = {
	...process.env,
	AO_DESKTOP_FLAVOR: profile.flavor,
	AO_RELEASE_REPO: profile.releaseRepo,
};

function run(command, args) {
	const result = spawnSync(command, args, { cwd: root, env, stdio: "inherit", windowsHide: true });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`${basename(command)} failed (${result.status})`);
}

// Reuse resource preparation without running the upstream make/publish path.
for (const script of ["build-daemon.mjs", "build-tmux.mjs", "prepare-agent-browser.mjs", "build-acp-runtime.mjs"]) {
	run(process.execPath, [join(root, "scripts", script)]);
}
run(process.execPath, [require.resolve("@electron-forge/cli/dist/electron-forge.js"), "package", "--platform=win32", "--arch=x64"]);

const output = join(root, "out");
const directory = `${profile.name}-win32-x64`;
const appDir = join(output, directory);
for (const file of ["fleet.exe", "resources/app.asar", "resources/daemon/ao.exe", "resources/acp-runtime/node/node.exe"]) {
	if (!existsSync(join(appDir, file))) throw new Error(`Incomplete Fleet package: ${file}`);
}
writeFileSync(join(appDir, "README-Fleet.txt"), [
	`Fleet ${version} - Windows x64 portable`,
	"Run fleet.exe. Keep the entire folder together; no installer is needed.",
	"Data: %USERPROFILE%\\.ao\\fleet (separate from the official AO application).",
	"Update: quit Fleet and replace this application folder. Keep your data directory.",
	"Automatic updates and the official ao-app:// protocol registration are disabled.",
	"Advanced: AO_FLEET_HOME sets an absolute data root; AO_FLEET_PORT defaults to 13001.",
	"Do not point Fleet at an official AO data directory. Other AO_* isolation overrides are ignored.",
	"",
].join("\r\n"));
const archive = join(output, `Fleet-${version}-win32-x64.zip`);
run("tar.exe", ["-a", "-c", "-f", archive, "-C", output, directory]);
const hash = createHash("sha256");
for await (const chunk of createReadStream(archive)) hash.update(chunk);
writeFileSync(`${archive}.sha256`, `${hash.digest("hex")}  ${basename(archive)}\n`);
console.log(`Fleet directory: ${appDir}\nFleet archive: ${archive}`);
