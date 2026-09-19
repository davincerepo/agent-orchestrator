package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFleetCLIEnvironment(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		name, root, port string
		invalid          bool
	}{
		{name: "defaults"},
		{name: "custom", root: filepath.Join(t.TempDir(), "fleet data"), port: "14001"},
		{name: "relative", root: "relative", invalid: true},
		{name: "official root", root: filepath.Join(home, ".ao"), invalid: true},
		{name: "official data", root: filepath.Join(home, ".ao", "data"), invalid: true},
		{name: "prefix sibling", root: filepath.Join(home, ".ao", "fleet-old"), invalid: true},
		{name: "official port", port: "3001", invalid: true},
		{name: "official mobile port", port: "3002", invalid: true},
		{name: "invalid port", port: "no", invalid: true},
		{name: "out of range", port: "65536", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := map[string]string{"AO_FLEET_HOME": tc.root, "AO_FLEET_PORT": tc.port, "AO_DATA_DIR": "official-data", "AO_RUN_FILE": "official-run", "AO_PORT": "3001", "AO_DAEMON_COMMAND": "other-daemon"}
			env, err := fleetCLIEnvironment(home, func(key string) string { return in[key] })
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid configuration accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			root := tc.root
			if root == "" {
				root = filepath.Join(home, ".ao", "fleet")
			}
			port := tc.port
			if port == "" {
				port = "13001"
			}
			if env["AO_DATA_DIR"] != filepath.Join(root, "data") || env["AO_RUN_FILE"] != filepath.Join(root, "running.json") || env["AO_PORT"] != port || env["AO_DAEMON_COMMAND"] != "" {
				t.Fatalf("environment=%v", env)
			}
		})
	}
}

func TestFleetCLIConfigOverridesOrdinaryAOEnvironment(t *testing.T) {
	previous := desktopFlavor
	t.Cleanup(func() { desktopFlavor = previous })
	for _, key := range []string{"AO_DATA_DIR", "AO_RUN_FILE", "AO_PORT", "AO_DAEMON_COMMAND", "AO_DEV_DAEMON_BINARY", "AO_CLOUD_AUTH_REDIRECT", "AO_TELEMETRY_REMOTE", "AO_SENTRY_DSN"} {
		t.Setenv(key, "inherited-official-value")
	}
	root := t.TempDir()
	t.Setenv("AO_FLEET_HOME", root)
	t.Setenv("AO_FLEET_PORT", "14001")
	t.Setenv("AO_DESKTOP_FLAVOR", "fleet-portable")
	desktopFlavor = ""
	if err := configureFleetCLI(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AO_DATA_DIR") != "inherited-official-value" {
		t.Fatal("ordinary build was reconfigured by environment")
	}
	desktopFlavor = "fleet-portable"
	if err := configureFleetCLI(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AO_DATA_DIR") != filepath.Join(root, "data") || os.Getenv("AO_RUN_FILE") != filepath.Join(root, "running.json") || os.Getenv("AO_PORT") != "14001" {
		t.Fatal("Fleet isolation not applied")
	}
	if !strings.HasPrefix(VersionString(), "AO Fleet ") {
		t.Fatal("version does not identify Fleet")
	}
}

func TestFleetCLIVersionCommands(t *testing.T) {
	previous := desktopFlavor
	desktopFlavor = "fleet-portable"
	t.Cleanup(func() { desktopFlavor = previous })
	setConfigEnv(t)
	for _, arg := range []string{"-v", "--version", "version"} {
		t.Run(arg, func(t *testing.T) {
			out, _, err := executeCLI(t, Deps{}, arg)
			if err != nil || strings.TrimSpace(out) != VersionString() || !strings.HasPrefix(out, "AO Fleet ") {
				t.Fatalf("ao %s: output=%q err=%v", arg, out, err)
			}
		})
	}
}

func TestFleetCLIStartDoesNotUseOfficialMarkerOrDownload(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Fleet portable is Windows-only")
	}
	previous := desktopFlavor
	desktopFlavor = "fleet-portable"
	t.Cleanup(func() { desktopFlavor = previous })
	cfg := setConfigEnv(t)
	writeMarker(t, cfg, makeBundle(t, "official.exe"))
	root := t.TempDir()
	app := filepath.Join(root, "fleet.exe")
	if err := os.WriteFile(app, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	started := ""
	deps := Deps{
		Executable: func() (string, error) { return filepath.Join(root, "resources", "daemon", "ao.exe"), nil },
		StartProcess: func(cfg processStartConfig) error {
			started = cfg.Path
			if len(cfg.Args) != 0 {
				t.Fatal("Fleet was tagged as an npm install")
			}
			return nil
		},
	}
	_, _, err := executeCLI(t, deps, "start", "--json")
	if err != nil || started != app {
		t.Fatalf("started=%s err=%v", started, err)
	}
	if err := os.Remove(app); err != nil {
		t.Fatal(err)
	}
	started = ""
	_, _, err = executeCLI(t, deps, "start")
	if err == nil || started != "" {
		t.Fatalf("missing package fell back to official AO: start=%s err=%v", started, err)
	}
}

func TestFleetCLIStartUsesAdjacentDesktop(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Fleet portable is Windows-only")
	}
	root := t.TempDir()
	app := filepath.Join(root, "fleet.exe")
	if err := os.WriteFile(app, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	c := commandContext{deps: Deps{Executable: func() (string, error) { return filepath.Join(root, "resources", "daemon", "ao.exe"), nil }}}
	got, err := c.fleetDesktopPath()
	if err != nil || got != app {
		t.Fatalf("path=%q err=%v", got, err)
	}
	if err := os.Remove(app); err != nil {
		t.Fatal(err)
	}
	if _, err := c.fleetDesktopPath(); err == nil {
		t.Fatal("missing Fleet package should fail, never resolve/download official AO")
	}
}
