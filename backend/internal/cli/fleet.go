package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Stamped only by the Fleet package build. Ordinary AO binaries keep their
// existing defaults; environment variables cannot turn a Fleet binary into AO.
var desktopFlavor = ""

func configureFleetCLI() error {
	if desktopFlavor != "fleet-portable" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	env, err := fleetCLIEnvironment(home, os.Getenv)
	if err != nil {
		return err
	}
	for name, value := range env {
		if err := os.Setenv(name, value); err != nil {
			return err
		}
	}
	return nil
}

func fleetCLIEnvironment(home string, getenv func(string) string) (map[string]string, error) {
	official := filepath.Join(home, ".ao")
	root := strings.TrimSpace(getenv("AO_FLEET_HOME"))
	if root == "" {
		root = filepath.Join(home, ".ao-fleet")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("AO_FLEET_HOME must be an absolute path")
	}
	root = filepath.Clean(root)
	rel, err := filepath.Rel(official, root)
	if err != nil {
		// Windows permits an explicit Fleet directory on a different drive.
		rel = ".."
	}
	inside := rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	if inside {
		return nil, fmt.Errorf("Fleet must use ~/.ao-fleet or another directory outside the official AO state root")
	}
	portText := strings.TrimSpace(getenv("AO_FLEET_PORT"))
	if portText == "" {
		portText = "13001"
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || port == 3001 || port == 3002 {
		return nil, fmt.Errorf("AO_FLEET_PORT must be 1-65535 and different from AO's 3001/3002 ports")
	}
	return map[string]string{
		"AO_FLEET_HOME":          root,
		"AO_FLEET_PORT":          strconv.Itoa(port),
		"AO_DATA_DIR":            filepath.Join(root, "data"),
		"AO_RUN_FILE":            filepath.Join(root, "running.json"),
		"AO_PORT":                strconv.Itoa(port),
		"AO_DAEMON_COMMAND":      "",
		"AO_DEV_DAEMON_BINARY":   "",
		"AO_CLOUD_AUTH_REDIRECT": "http://127.0.0.1:3000/callback",
		"AO_TELEMETRY_REMOTE":    "off",
		"AO_SENTRY_DSN":          "",
	}, nil
}

func (c *commandContext) fleetDesktopPath() (string, error) {
	executable, err := c.deps.Executable()
	if err != nil {
		return "", err
	}
	app := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "..", "fleet.exe"))
	if !isUsableBundle(app) {
		return "", fmt.Errorf("Fleet desktop is missing at %s; reinstall the complete Fleet package", app)
	}
	return app, nil
}
