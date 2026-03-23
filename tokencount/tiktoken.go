// Package tokencount provides a shared offline tiktoken wrapper for LLM token
// estimation. It maps model IDs to BPE encodings and counts tokens without any
// network calls, using embedded BPE tables from tiktoken-go-loader.
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
)

// encodingCache caches initialized tiktoken.Tiktoken instances by encoding name.
var (
	encodingCacheMu sync.Mutex
	encodingCache   = make(map[string]*tiktoken.Tiktoken)
)

// EncodingForModel returns the BPE encoding name appropriate for the given
// model ID, using prefix matching.
//
// Mappings:
//   - o200k_base: gpt-4o*, gpt-4.1*, gpt-4.5*, o1*, o3*, o4*
//   - cl100k_base: claude-*, gpt-4* (non-o suffixed), gpt-3.5*, and all unknowns
//
// The second return value is false when the model was not recognised and the
// fallback encoding (cl100k_base) was returned.
func EncodingForModel(modelID string) (encoding string, ok bool) {
	id := strings.ToLower(modelID)

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
// o200k_base).
func CountText(encoding, text string) (int, error) {
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
