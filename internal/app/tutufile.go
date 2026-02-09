// Package app provides application-layer orchestration services.
// It wires domain logic with infrastructure, never the reverse.
package app

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/tutu-network/tutu/internal/domain"
)

// ParseTuTufile parses a TuTufile from a reader.
// Supports directives: FROM, PARAMETER, SYSTEM, TEMPLATE, ADAPTER, MESSAGE, LICENSE.
// Multi-line values use triple-quote delimiters (""").
func ParseTuTufile(r io.Reader) (*domain.TuTufile, error) {
	tf := &domain.TuTufile{
		Parameters: make(map[string][]string),
	}

	scanner := bufio.NewScanner(r)
	var multiLine *string
	var inMultiLine bool

	for scanner.Scan() {
		line := scanner.Text()

		// Handle multi-line blocks (""" delimiters)
		if inMultiLine {
			trimmed := strings.TrimSpace(line)
			if trimmed == `"""` {
				inMultiLine = false
				multiLine = nil
				continue
			}
			*multiLine += line + "\n"
			continue
		}

		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse directive
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue // Ignore malformed lines
		}

		directive := strings.ToUpper(parts[0])
		value := strings.TrimSpace(parts[1])

		switch directive {
		case "FROM":
			tf.From = value

		case "PARAMETER":
			kv := strings.SplitN(value, " ", 2)
			if len(kv) == 2 {
				key := strings.TrimSpace(kv[0])
				val := strings.TrimSpace(kv[1])
				tf.Parameters[key] = append(tf.Parameters[key], val)
			}

		case "SYSTEM":
			if strings.HasPrefix(value, `"""`) {
				tf.System = ""
				multiLine = &tf.System
				inMultiLine = true
			} else {
				tf.System = unquote(value)
			}

		case "TEMPLATE":
			if strings.HasPrefix(value, `"""`) {
				tf.Template = ""
				multiLine = &tf.Template
				inMultiLine = true
			} else {
				tf.Template = unquote(value)
			}

		case "ADAPTER":
			tf.Adapter = value

		case "MESSAGE":
			msg, err := parseMessage(value)
			if err == nil {
				tf.Messages = append(tf.Messages, msg)
			}

		case "LICENSE":
			if strings.HasPrefix(value, `"""`) {
				tf.License = ""
				multiLine = &tf.License
				inMultiLine = true
			} else {
				tf.License = unquote(value)
			}

		default:
			// Unknown directives are silently ignored for forward compatibility
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read TuTufile: %w", err)
	}

	if tf.From == "" {
		return nil, domain.ErrNoFromDirective
	}

	return tf, nil
}

// parseMessage parses "role content" from a MESSAGE directive.
func parseMessage(value string) (domain.Message, error) {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 {
		return domain.Message{}, fmt.Errorf("invalid MESSAGE format: %q", value)
	}
	return domain.Message{
		Role:    strings.TrimSpace(parts[0]),
		Content: unquote(strings.TrimSpace(parts[1])),
	}, nil
}

// unquote removes surrounding double quotes if present.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
