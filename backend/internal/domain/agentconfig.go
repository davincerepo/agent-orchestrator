package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PermissionMode controls how much review an agent requires before acting. It
// lives in domain (not ports) so the typed AgentConfig can carry it; ports
// re-exports it as a type alias so agent adapters keep referring to
// ports.PermissionMode unchanged.
type PermissionMode string

// The permission modes adapters map onto their agent's native approval flags.
const (
	// PermissionModeDefault is special: adapters choose their own baseline
	// behavior for it. Most defer to the agent's own config; some managed
	// adapters may map it to a safer non-interactive default.
	PermissionModeDefault           PermissionMode = "default"
	PermissionModeAcceptEdits       PermissionMode = "accept-edits"
	PermissionModeAuto              PermissionMode = "auto"
	PermissionModeBypassPermissions PermissionMode = "bypass-permissions"
)

// AgentConfig is the typed per-project agent configuration. It replaces the
// former free-form map so the fields are validated and the API/UI render a
// real form rather than arbitrary JSON. An empty value (IsZero) means unset.
type AgentConfig struct {
	// Model overrides the agent's default model (e.g. claude-opus-4-5).
	Model string `json:"model,omitempty"`
	// Effort selects a model-advertised reasoning level. Empty defers to the
	// provider/model default.
	Effort string `json:"effort,omitempty"`
	// ServiceTier inherits when empty; default explicitly disables Codex Fast.
	ServiceTier string `json:"serviceTier,omitempty"`
	// Mode selects an agent-owned operating mode when the adapter exposes modes
	// instead of raw model ids (currently Amp: low|medium|high|ultra).
	Mode string `json:"mode,omitempty"`
	// Permissions sets the agent's starting permission mode. Empty inherits the
	// project/role preference; new sessions fall back to Auto when none is saved.
	// Other adapter callers retain their existing baseline for an empty value.
	Permissions PermissionMode `json:"permissions,omitempty"`
}

// UnmarshalJSON accepts the former fleet key while persisting only upstream's
// effort spelling. An explicit effort (including empty) wins over the old key.
func (c *AgentConfig) UnmarshalJSON(data []byte) error {
	type config AgentConfig
	var wire struct {
		config
		Effort       *string `json:"effort"`
		LegacyEffort string  `json:"reasoningEffort"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*c = AgentConfig(wire.config)
	c.Effort = wire.LegacyEffort
	if wire.Effort != nil {
		c.Effort = *wire.Effort
	}
	return nil
}

// IsZero reports whether the config carries no settings, so storage can persist
// SQL NULL and resolution can skip an empty config.
func (c AgentConfig) IsZero() bool {
	return c == AgentConfig{}
}

// Valid reports whether the mode is one AO knows. Empty counts as valid: it means
// "the adapter's own baseline", which is a legitimate choice rather than a missing
// one.
func (m PermissionMode) Valid() bool {
	switch m {
	case "", PermissionModeDefault, PermissionModeAcceptEdits,
		PermissionModeAuto, PermissionModeBypassPermissions:
		return true
	default:
		return false
	}
}

// Validate rejects values outside the typed vocabulary so a bad config is
// refused when it is set (CLI/API) rather than silently dropped at spawn.
func (c AgentConfig) Validate() error {
	for key, value := range map[string]string{"effort": c.Effort, "serviceTier": c.ServiceTier} {
		if len(value) > 64 || strings.ContainsAny(value, " \t\r\n\x00") {
			return fmt.Errorf("invalid %s %q: expected a provider option id", key, value)
		}
	}
	if c.ServiceTier != "" && c.ServiceTier != "default" && c.ServiceTier != "priority" {
		return fmt.Errorf("invalid serviceTier %q: want default or priority", c.ServiceTier)
	}
	switch c.Mode {
	case "", "low", "medium", "high", "ultra":
	default:
		return fmt.Errorf("invalid mode %q: want one of low, medium, high, ultra", c.Mode)
	}
	if c.Permissions.Valid() {
		return nil
	}
	return fmt.Errorf("invalid permissions %q: want one of default, accept-edits, auto, bypass-permissions", c.Permissions)
}
