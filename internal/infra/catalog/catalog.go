// Package catalog provides a registry of known models with their
// HuggingFace download locations and metadata.
// This is TuTu's "model phonebook" — it maps friendly names like
// "llama3" to actual GGUF file URLs on HuggingFace.
package catalog

// ModelEntry describes a downloadable model.
type ModelEntry struct {
	Name         string   // Friendly name (e.g. "llama3")
	Description  string   // What the model is for
	Family       string   // Model family (e.g. "llama")
	Parameters   string   // Parameter count (e.g. "8B")
	Quantization string   // Quantization level (e.g. "Q4_K_M")
	Format       string   // File format (always "gguf" for now)
	SizeBytes    int64    // Approximate download size
	HFRepo       string   // HuggingFace repo (e.g. "QuantFactory/Meta-Llama-3-8B-Instruct-GGUF")
	HFFile       string   // GGUF filename inside the repo
	Tags         []string // Searchable tags: ["llama3", "llama3:latest", "llama3:8b"]
	ContextSize  int      // Default context window
	ChatTemplate string   // Chat template style: "llama3", "chatml", "phi3"
}

// Catalog is the built-in list of downloadable models.
// These point to small, quantized models suitable for local inference.
// Users can always pull by full HuggingFace path for unlisted models.
var Catalog = []ModelEntry{
	{
		Name:         "tinyllama",
		Description:  "TinyLlama 1.1B — fast, small, good for testing",
		Family:       "llama",
		Parameters:   "1.1B",
		Quantization: "Q4_K_M",
		Format:       "gguf",
		SizeBytes:    669_000_000, // ~669 MB
		HFRepo:       "TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF",
		HFFile:       "tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf",
		Tags:         []string{"tinyllama", "tinyllama:latest", "tinyllama:1.1b"},
		ContextSize:  2048,
		ChatTemplate: "chatml",
	},
	{
		Name:         "phi3",
		Description:  "Microsoft Phi-3 Mini 3.8B — strong for its size",
		Family:       "phi3",
		Parameters:   "3.8B",
		Quantization: "Q4_K_M",
		Format:       "gguf",
		SizeBytes:    2_400_000_000, // ~2.4 GB
		HFRepo:       "microsoft/Phi-3-mini-4k-instruct-gguf",
		HFFile:       "Phi-3-mini-4k-instruct-q4.gguf",
		Tags:         []string{"phi3", "phi3:latest", "phi3:mini", "phi3:3.8b"},
		ContextSize:  4096,
		ChatTemplate: "phi3",
	},
	{
		Name:         "qwen2.5",
		Description:  "Qwen 2.5 1.5B — fast multilingual model",
		Family:       "qwen2",
		Parameters:   "1.5B",
		Quantization: "Q4_K_M",
		Format:       "gguf",
		SizeBytes:    986_000_000, // ~986 MB
		HFRepo:       "Qwen/Qwen2.5-1.5B-Instruct-GGUF",
		HFFile:       "qwen2.5-1.5b-instruct-q4_k_m.gguf",
		Tags:         []string{"qwen2.5", "qwen2.5:latest", "qwen2.5:1.5b"},
		ContextSize:  4096,
		ChatTemplate: "chatml",
	},
	{
		Name:         "llama3",
		Description:  "Meta Llama 3.2 1B Instruct — compact and capable",
		Family:       "llama",
		Parameters:   "1B",
		Quantization: "Q4_K_M",
		Format:       "gguf",
		SizeBytes:    750_000_000, // ~750 MB
		HFRepo:       "hugging-quants/Llama-3.2-1B-Instruct-Q4_K_M-GGUF",
		HFFile:       "llama-3.2-1b-instruct-q4_k_m.gguf",
		Tags:         []string{"llama3", "llama3:latest", "llama3:1b", "llama3.2", "llama3.2:1b"},
		ContextSize:  4096,
		ChatTemplate: "llama3",
	},
	{
		Name:         "llama3:8b",
		Description:  "Meta Llama 3.1 8B Instruct — full-size, best quality",
		Family:       "llama",
		Parameters:   "8B",
		Quantization: "Q4_K_M",
		Format:       "gguf",
		SizeBytes:    4_900_000_000, // ~4.9 GB
		HFRepo:       "bartowski/Meta-Llama-3.1-8B-Instruct-GGUF",
		HFFile:       "Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf",
		Tags:         []string{"llama3:8b", "llama3.1:8b"},
		ContextSize:  8192,
		ChatTemplate: "llama3",
	},
	{
		Name:         "gemma2",
		Description:  "Google Gemma 2 2B — efficient and strong reasoning",
		Family:       "gemma",
		Parameters:   "2B",
		Quantization: "Q4_K_M",
		Format:       "gguf",
		SizeBytes:    1_600_000_000, // ~1.6 GB
		HFRepo:       "bartowski/gemma-2-2b-it-GGUF",
		HFFile:       "gemma-2-2b-it-Q4_K_M.gguf",
		Tags:         []string{"gemma2", "gemma2:latest", "gemma2:2b"},
		ContextSize:  8192,
		ChatTemplate: "gemma",
	},
	{
		Name:         "smollm2",
		Description:  "SmolLM2 360M — ultra-tiny, instant responses, great for testing",
		Family:       "llama",
		Parameters:   "360M",
		Quantization: "Q8_0",
		Format:       "gguf",
		SizeBytes:    386_000_000, // ~386 MB
		HFRepo:       "HuggingFaceTB/SmolLM2-360M-Instruct-GGUF",
		HFFile:       "smollm2-360m-instruct-q8_0.gguf",
		Tags:         []string{"smollm2", "smollm2:latest", "smollm2:360m"},
		ContextSize:  2048,
		ChatTemplate: "chatml",
	},
	{
		Name:         "mistral",
		Description:  "Mistral 7B Instruct v0.3 — strong general-purpose model",
		Family:       "mistral",
		Parameters:   "7B",
		Quantization: "Q4_K_M",
		Format:       "gguf",
		SizeBytes:    4_370_000_000, // ~4.37 GB
		HFRepo:       "bartowski/Mistral-7B-Instruct-v0.3-GGUF",
		HFFile:       "Mistral-7B-Instruct-v0.3-Q4_K_M.gguf",
		Tags:         []string{"mistral", "mistral:latest", "mistral:7b"},
		ContextSize:  8192,
		ChatTemplate: "mistral",
	},
}

// Lookup finds a model entry by name or tag.
// Returns nil if not found.
func Lookup(name string) *ModelEntry {
	for i := range Catalog {
		for _, tag := range Catalog[i].Tags {
			if tag == name {
				return &Catalog[i]
			}
		}
	}
	return nil
}

// DownloadURL returns the HuggingFace direct download URL for a model.
func (e *ModelEntry) DownloadURL() string {
	return "https://huggingface.co/" + e.HFRepo + "/resolve/main/" + e.HFFile
}
