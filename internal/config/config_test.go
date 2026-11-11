package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreviewAddressDefaultsAndEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("auth:\n  password: test-only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Logs.PreviewScheme != "https" || cfg.Logs.PreviewPort != 443 {
		t.Fatalf("preview defaults: %+v", cfg.Logs)
	}
	t.Setenv("IPGUARD_LOGS_PREVIEWSCHEME", "http")
	t.Setenv("IPGUARD_LOGS_PREVIEWPORT", "8080")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Logs.PreviewScheme != "http" || cfg.Logs.PreviewPort != 8080 {
		t.Fatalf("preview env override: %+v", cfg.Logs)
	}
	t.Setenv("IPGUARD_LOGS_PREVIEWPORT", "65536")
	if _, err = Load(path); err == nil {
		t.Fatal("invalid preview port accepted")
	}
	t.Setenv("IPGUARD_LOGS_PREVIEWPORT", "4443")
	t.Setenv("IPGUARD_LOGS_PREVIEWSCHEME", "javascript")
	if _, err = Load(path); err == nil {
		t.Fatal("invalid preview scheme accepted")
	}
}
