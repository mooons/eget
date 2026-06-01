package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jessevdk/go-flags"
)

func TestLoadConfigurationFileParsesTargetApp(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "eget.toml")
	if err := os.WriteFile(configPath, []byte(`
[global]
target = "~/bin"
target_app = "/Applications"

["owner/repo"]
target = "~/.local/bin"
target_app = "/Applications/Utilities"
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := LoadConfigurationFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if config.Global.TargetApp != "/Applications" {
		t.Fatalf("global target_app = %q", config.Global.TargetApp)
	}
	if got := config.Repositories["owner/repo"].TargetApp; got != "/Applications/Utilities" {
		t.Fatalf("repo target_app = %q", got)
	}
}

func TestSetProjectOptionsFromConfigInheritsGlobalTargetApp(t *testing.T) {
	config := loadConfigForTest(t, `
[global]
target = "~/bin"
target_app = "/Applications"

["owner/repo"]
`)
	parser := flags.NewParser(&CliFlags{}, flags.Default)
	opts := &Flags{}

	if err := SetGlobalOptionsFromConfig(config, parser, opts, CliFlags{}); err != nil {
		t.Fatalf("set global options: %v", err)
	}
	if err := SetProjectOptionsFromConfig(config, parser, opts, CliFlags{}, "owner/repo"); err != nil {
		t.Fatalf("set project options: %v", err)
	}

	if got := opts.AppOutput; got != "/Applications" {
		t.Fatalf("app target = %q", got)
	}
}

func TestSetProjectOptionsFromConfigUsesRepoTargetApp(t *testing.T) {
	config := loadConfigForTest(t, `
[global]
target = "~/bin"
target_app = "/Applications"

["owner/repo"]
target = "~/.local/bin"
target_app = "/Applications/Utilities"
`)
	parser := flags.NewParser(&CliFlags{}, flags.Default)
	opts := &Flags{}

	if err := SetGlobalOptionsFromConfig(config, parser, opts, CliFlags{}); err != nil {
		t.Fatalf("set global options: %v", err)
	}
	if err := SetProjectOptionsFromConfig(config, parser, opts, CliFlags{}, "owner/repo"); err != nil {
		t.Fatalf("set project options: %v", err)
	}

	if got := opts.AppOutput; got != "/Applications/Utilities" {
		t.Fatalf("app target = %q", got)
	}
}

func loadConfigForTest(t *testing.T, body string) *Config {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "eget.toml")
	if err := os.WriteFile(configPath, []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	old := os.Getenv("EGET_CONFIG")
	if err := os.Setenv("EGET_CONFIG", configPath); err != nil {
		t.Fatalf("set EGET_CONFIG: %v", err)
	}
	t.Cleanup(func() {
		if old == "" {
			_ = os.Unsetenv("EGET_CONFIG")
			return
		}
		_ = os.Setenv("EGET_CONFIG", old)
	})

	config, err := InitializeConfig()
	if err != nil {
		t.Fatalf("initialize config: %v", err)
	}
	return config
}
