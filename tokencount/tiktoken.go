// Package tokencount provides a shared offline tiktoken wrapper for LLM token
// estimation. It maps model IDs to BPE encodings and counts tokens without any
// network calls, using embedded BPE tables from tiktoken-go-loader.
//
// Custom encodings (e.g. MiniMax BPE) can be registered at init time via
// RegisterEncoding so that provider packages can wire in their own tokenizers
// without creating an import cycle (tokencount ← provider ← tokencount).
package tokencount

import (
	"fmt"
	"strings"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

func init() {
	// Wire the offline (embedded) BPE loader so that no runtime downloads occur.
	tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}

const (
	EncodingCL100K = "cl100k_base"
	EncodingO200K  = "o200k_base"
	// EncodingMinimax is the encoding name for the MiniMax BPE tokenizer.
	// The implementation is registered by provider/minimax at init time via
	// RegisterEncoding.
	EncodingMinimax = "minimax_bpe"
)

// customEncodings holds CountText implementations registered by external
// packages (e.g. provider/minimax) for encodings not handled by tiktoken.
var (
	customEncodingsMu sync.RWMutex
	customEncodings   = make(map[string]func(text string) (int, error))
)

// RegisterEncoding registers a custom CountText implementation for the given
// encoding name. It is called from provider init() functions to wire in
// tokenizers that live outside the tokencount package, avoiding import cycles.
//
// Registering the same name twice panics to catch accidental double-registration.
func RegisterEncoding(name string, fn func(text string) (int, error)) {
	customEncodingsMu.Lock()
	defer customEncodingsMu.Unlock()
	if _, exists := customEncodings[name]; exists {
		panic(fmt.Sprintf("tokencount: encoding %q already registered", name))
	}
	customEncodings[name] = fn
}

// encodingCache caches initialized tiktoken.Tiktoken instances by encoding name.
var (
	encodingCacheMu sync.Mutex
	encodingCache   = make(map[string]*tiktoken.Tiktoken)
)

// EncodingForModel returns the BPE encoding name appropriate for the given
// model ID, using prefix matching.
//
// Mappings:
//   - minimax_bpe: minimax-*, MiniMax-*
//   - o200k_base: gpt-4o*, gpt-4.1*, gpt-4.5*, o1*, o3*, o4*
//   - cl100k_base: claude-*, gpt-4* (non-o suffixed), gpt-3.5*, and all unknowns
//
// The second return value is false when the model was not recognised and the
// fallback encoding (cl100k_base) was returned.
func EncodingForModel(modelID string) (encoding string, ok bool) {
	id := strings.ToLower(modelID)

	// minimax_bpe models
	if strings.HasPrefix(id, "minimax-") {
		return EncodingMinimax, true
	}

	// o200k_base models
	o200kPrefixes := []string{
		"gpt-4o",
		"gpt-4.1",
		"gpt-4.5",
		"o1",
		"o3",
		"o4",
	}
	for _, pfx := range o200kPrefixes {
		if strings.HasPrefix(id, pfx) {
			return EncodingO200K, true
		}
	}

	// cl100k_base models (known)
	cl100kPrefixes := []string{
		"claude-",
		"gpt-4",
		"gpt-3.5",
	}
	for _, pfx := range cl100kPrefixes {
		if strings.HasPrefix(id, pfx) {
			return EncodingCL100K, true
		}
	}

	// Unknown model — best-effort fallback
	return EncodingCL100K, false
}

// CountText returns the number of tokens in text using the named BPE encoding.
// The encoding must be one of the constants in this package (cl100k_base,
// o200k_base, minimax_bpe) or a name registered via RegisterEncoding.
func CountText(encoding, text string) (int, error) {
	// Check custom (registered) encodings first.
	customEncodingsMu.RLock()
	fn, isCustom := customEncodings[encoding]
	customEncodingsMu.RUnlock()
	if isCustom {
		return fn(text)
	}

	tk, err := getEncoding(encoding)
	if err != nil {
		return 0, err
	}
	return len(tk.Encode(text, nil, nil)), nil
}

// CountTextForModel is a convenience wrapper that calls EncodingForModel and
// then CountText.
func CountTextForModel(modelID, text string) (int, error) {
	enc, _ := EncodingForModel(modelID)
	return CountText(enc, text)
}

func getEncoding(encoding string) (*tiktoken.Tiktoken, error) {
	encodingCacheMu.Lock()
	defer encodingCacheMu.Unlock()

	if tk, ok := encodingCache[encoding]; ok {
		return tk, nil
	}

	tk, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, fmt.Errorf("tokencount: get encoding %q: %w", encoding, err)
	}
	encodingCache[encoding] = tk
	return tk, nil
}
