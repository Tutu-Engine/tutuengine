package app

import (
	"strings"
	"testing"
)

func TestParseTuTufile_Basic(t *testing.T) {
	input := `FROM llama3.2
PARAMETER temperature 0.8
PARAMETER top_p 0.9
SYSTEM "You are a helpful assistant."
`
	tf, err := ParseTuTufile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTuTufile() error: %v", err)
	}

	if tf.From != "llama3.2" {
		t.Errorf("From = %q, want %q", tf.From, "llama3.2")
	}

	if v := tf.Parameters["temperature"]; len(v) != 1 || v[0] != "0.8" {
		t.Errorf("temperature = %v, want [\"0.8\"]", v)
	}

	if v := tf.Parameters["top_p"]; len(v) != 1 || v[0] != "0.9" {
		t.Errorf("top_p = %v, want [\"0.9\"]", v)
	}

	if tf.System != "You are a helpful assistant." {
		t.Errorf("System = %q, want %q", tf.System, "You are a helpful assistant.")
	}
}

func TestParseTuTufile_MultiLineSystem(t *testing.T) {
	input := `FROM llama3.2
SYSTEM """
You are a pirate.
Always answer in pirate speak.
"""
`
	tf, err := ParseTuTufile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTuTufile() error: %v", err)
	}

	if !strings.Contains(tf.System, "pirate") {
		t.Errorf("System should contain 'pirate', got %q", tf.System)
	}
	if !strings.Contains(tf.System, "pirate speak") {
		t.Errorf("System should contain 'pirate speak', got %q", tf.System)
	}
}

func TestParseTuTufile_Template(t *testing.T) {
	input := `FROM llama3
TEMPLATE """
{{ .System }}
{{ .Prompt }}
"""
`
	tf, err := ParseTuTufile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTuTufile() error: %v", err)
	}

	if !strings.Contains(tf.Template, "{{ .System }}") {
		t.Errorf("Template should contain '{{ .System }}', got %q", tf.Template)
	}
}

func TestParseTuTufile_Adapter(t *testing.T) {
	input := `FROM llama3
ADAPTER ./fine-tuned.bin
`
	tf, err := ParseTuTufile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTuTufile() error: %v", err)
	}

	if tf.Adapter != "./fine-tuned.bin" {
		t.Errorf("Adapter = %q, want %q", tf.Adapter, "./fine-tuned.bin")
	}
}

func TestParseTuTufile_Messages(t *testing.T) {
	input := `FROM llama3
MESSAGE user What is water?
MESSAGE assistant Water is H2O.
`
	tf, err := ParseTuTufile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTuTufile() error: %v", err)
	}

	if len(tf.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(tf.Messages))
	}

	if tf.Messages[0].Role != "user" || tf.Messages[0].Content != "What is water?" {
		t.Errorf("Messages[0] = %+v, unexpected", tf.Messages[0])
	}

	if tf.Messages[1].Role != "assistant" || tf.Messages[1].Content != "Water is H2O." {
		t.Errorf("Messages[1] = %+v, unexpected", tf.Messages[1])
	}
}

func TestParseTuTufile_License(t *testing.T) {
	input := `FROM llama3
LICENSE """
MIT License
Copyright 2024
"""
`
	tf, err := ParseTuTufile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTuTufile() error: %v", err)
	}

	if !strings.Contains(tf.License, "MIT License") {
		t.Errorf("License should contain 'MIT License', got %q", tf.License)
	}
}

func TestParseTuTufile_NoFrom(t *testing.T) {
	input := `PARAMETER temperature 0.8
SYSTEM "Hello"
`
	_, err := ParseTuTufile(strings.NewReader(input))
	if err == nil {
		t.Fatal("ParseTuTufile() should error without FROM directive")
	}
}

func TestParseTuTufile_EmptyInput(t *testing.T) {
	_, err := ParseTuTufile(strings.NewReader(""))
	if err == nil {
		t.Fatal("ParseTuTufile() should error on empty input")
	}
}

func TestParseTuTufile_CommentsAndBlanks(t *testing.T) {
	input := `# This is a comment
FROM llama3

# Another comment
PARAMETER temperature 0.5
`
	tf, err := ParseTuTufile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTuTufile() error: %v", err)
	}

	if tf.From != "llama3" {
		t.Errorf("From = %q, want %q", tf.From, "llama3")
	}
}

func TestParseTuTufile_MultipleStopTokens(t *testing.T) {
	input := `FROM llama3
PARAMETER stop <|end|>
PARAMETER stop <|user|>
PARAMETER stop <|system|>
`
	tf, err := ParseTuTufile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTuTufile() error: %v", err)
	}

	stops := tf.Parameters["stop"]
	if len(stops) != 3 {
		t.Errorf("len(stop) = %d, want 3", len(stops))
	}
}
