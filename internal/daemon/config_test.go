package daemon

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.API.Host != "127.0.0.1" {
		t.Errorf("API.Host = %q, want %q", cfg.API.Host, "127.0.0.1")
	}
	if cfg.API.Port != 11434 {
		t.Errorf("API.Port = %d, want %d", cfg.API.Port, 11434)
	}
	if cfg.Models.MaxStorage != "50GB" {
		t.Errorf("Models.MaxStorage = %q, want %q", cfg.Models.MaxStorage, "50GB")
	}
	if cfg.Inference.ContextLength != 4096 {
		t.Errorf("Inference.ContextLength = %d, want %d", cfg.Inference.ContextLength, 4096)
	}
}

func TestParseStorageSize(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"50GB", 50 * 1024 * 1024 * 1024},
		{"1TB", 1 * 1024 * 1024 * 1024 * 1024},
		{"100MB", 100 * 1024 * 1024},
		{"", 50 * 1024 * 1024 * 1024}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseStorageSize(tt.input)
			if got != tt.want {
				t.Errorf("parseStorageSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
