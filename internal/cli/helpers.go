package cli

import (
	"bufio"
	"io"

	"github.com/tutu-network/tutu/internal/domain"
	"github.com/tutu-network/tutu/internal/infra/registry"
)

// newLineScanner creates a line scanner from a reader.
func newLineScanner(r io.Reader) *bufio.Scanner {
	return bufio.NewScanner(r)
}

// registry_ParseRef is a wrapper to avoid stutter in import naming.
func registry_ParseRef(name string) domain.ModelRef {
	return registry.ParseRef(name)
}
