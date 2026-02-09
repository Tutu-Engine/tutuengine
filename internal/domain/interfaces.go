package domain

import "context"

// ─── Service Interfaces ─────────────────────────────────────────────────────
// These interfaces define boundaries between layers.
// Infrastructure implements them; application layer depends on them.

// InferenceEngine abstracts the AI inference backend (llama.cpp via CGO).
type InferenceEngine interface {
	// Generate streams tokens for the given request.
	Generate(ctx context.Context, req InferenceRequest) (<-chan Token, error)

	// Embed generates embeddings for the given inputs.
	Embed(ctx context.Context, model string, input []string) ([][]float32, error)

	// LoadedModels returns models currently held in memory.
	LoadedModels() []LoadedModel

	// UnloadAll releases all models from memory.
	UnloadAll() error
}

// ModelStore abstracts persistent model metadata storage.
type ModelStore interface {
	UpsertModel(info ModelInfo) error
	GetModel(name string) (*ModelInfo, error)
	ListModels() ([]ModelInfo, error)
	DeleteModel(name string) error
	TouchModel(name string) error // Update last_used
}

// ModelManager abstracts pull/resolve/show operations on the local model store.
// Implemented by infra/registry.Manager.
type ModelManager interface {
	// Pull downloads a model by name with progress reporting.
	Pull(name string, progress func(status string, pct float64)) error

	// Resolve returns the local file path for a model's weights.
	Resolve(name string) (string, error)

	// HasLocal checks whether a model exists locally.
	HasLocal(ref ModelRef) (bool, error)

	// List returns all locally installed models.
	List() ([]ModelInfo, error)

	// Remove deletes a model from local storage.
	Remove(name string) error

	// Show returns detailed info about a model.
	Show(name string) (*ModelInfo, error)

	// CreateFromTuTufile creates a custom model from a TuTufile definition.
	CreateFromTuTufile(name string, tf TuTufile) error
}
