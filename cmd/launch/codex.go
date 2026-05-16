package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ollama/ollama/envconfig"
	"golang.org/x/mod/semver"
)

// Codex implements Runner for Codex integration
type Codex struct{}

func (c *Codex) String() string { return "Codex" }

const codexProfileName = "ollama-launch"

func (c *Codex) args(model string, extra []string) []string {
	args := []string{"--profile", codexProfileName}
	if model != "" {
		args = append(args, "-m", model)
	}
	args = append(args, extra...)
	return args
}

func (c *Codex) Run(model string, args []string) error {
	if err := checkCodexVersion(); err != nil {
		return err
	}

	// Filter out --use-config from extra args and track if provided
	var customConfig string
	var filteredArgs []string
	home := homeDirMust()
	for _, arg := range args {
		if strings.HasPrefix(arg, "--use-config=") {
			path := strings.TrimPrefix(arg, "--use-config=")
			// Expand ~ to home directory (Go's os package doesn't do this)
			if strings.HasPrefix(path, "~/") {
				path = filepath.Join(home, path[2:])
			}
			customConfig = path
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	// Handle custom config: back up existing ~/.codex/config.toml and copy custom config
	var backupPath string
	if customConfig != "" {
		if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
			return err
		}
		targetPath := filepath.Join(home, ".codex", "config.toml")
		if _, err := os.Stat(targetPath); err == nil {
			backupPath = targetPath + ".backup"
			if err := copyFile(targetPath, backupPath); err != nil {
				return fmt.Errorf("failed to backup codex config: %w", err)
			}
		}
		if err := copyFile(customConfig, targetPath); err != nil {
			return fmt.Errorf("failed to copy custom codex config: %w", err)
		}
		defer func() {
			if backupPath != "" {
				os.Remove(targetPath)
				os.Rename(backupPath, targetPath)
			}
		}()
	} else {
		if err := ensureCodexConfig(); err != nil {
			return fmt.Errorf("failed to configure codex: %w", err)
		}
	}

	cmd := exec.Command("codex", c.args(model, filteredArgs)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"OPENAI_API_KEY=ollama",
	)
	return cmd.Run()
}

// homeDir expands ~ to home directory or returns path unchanged
func homeDir(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func homeDirMust() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return home
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// ensureCodexConfig writes a [profiles.ollama-launch] section to ~/.codex/config.toml
// with openai_base_url pointing to the local Ollama server.
func ensureCodexConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return err
	}

	configPath := filepath.Join(codexDir, "config.toml")
	return writeCodexProfile(configPath)
}

// writeCodexProfile ensures ~/.codex/config.toml has the ollama-launch profile
// and model provider sections with the correct base URL.
func writeCodexProfile(configPath string) error {
	baseURL := envconfig.Host().String() + "/v1/"

	sections := []struct {
		header string
		lines  []string
	}{
		{
			header: fmt.Sprintf("[profiles.%s]", codexProfileName),
			lines: []string{
				fmt.Sprintf("openai_base_url = %q", baseURL),
				`forced_login_method = "api"`,
				fmt.Sprintf("model_provider = %q", codexProfileName),
			},
		},
		{
			header: fmt.Sprintf("[model_providers.%s]", codexProfileName),
			lines: []string{
				`name = "Ollama"`,
				fmt.Sprintf("base_url = %q", baseURL),
			},
		},
	}

	content, readErr := os.ReadFile(configPath)
	text := ""
	if readErr == nil {
		text = string(content)
	}

	for _, s := range sections {
		block := strings.Join(append([]string{s.header}, s.lines...), "\n") + "\n"

		if idx := strings.Index(text, s.header); idx >= 0 {
			// Replace the existing section up to the next section header.
			rest := text[idx+len(s.header):]
			if endIdx := strings.Index(rest, "\n["); endIdx >= 0 {
				text = text[:idx] + block + rest[endIdx+1:]
			} else {
				text = text[:idx] + block
			}
		} else {
			// Append the section.
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			if text != "" {
				text += "\n"
			}
			text += block
		}
	}

	return os.WriteFile(configPath, []byte(text), 0o644)
}

func checkCodexVersion() error {
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("codex is not installed, install with: npm install -g @openai/codex")
	}

	out, err := exec.Command("codex", "--version").Output()
	if err != nil {
		return fmt.Errorf("failed to get codex version: %w", err)
	}

	// Parse output like "codex-cli 0.87.0"
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return fmt.Errorf("unexpected codex version output: %s", string(out))
	}

	version := "v" + fields[len(fields)-1]
	minVersion := "v0.81.0"

	if semver.Compare(version, minVersion) < 0 {
		return fmt.Errorf("codex version %s is too old, minimum required is %s, update with: npm update -g @openai/codex", fields[len(fields)-1], "0.81.0")
	}

	return nil
}
