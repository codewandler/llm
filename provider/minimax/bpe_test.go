package minimax

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDataDir(t *testing.T) {
	dir := getDataDir()

	assert.Contains(t, dir, ".cache")
	assert.Contains(t, dir, "llm")
	assert.Contains(t, dir, "minimax")
	assert.NotContains(t, dir, "~")
	assert.True(t, filepath.IsAbs(dir), "expected absolute path, got: %s", dir)
}

func TestDataFilesExist_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "no-such-dir")

	_, err := os.Stat(filepath.Join(testDir, "minimax_vocab.json"))
	assert.True(t, os.IsNotExist(err), "vocab should not exist")

	_, err = os.Stat(filepath.Join(testDir, "minimax_merges.txt"))
	assert.True(t, os.IsNotExist(err), "merges should not exist")
}

func TestDataFilesExist_WithFiles(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "minimax_vocab.json"), []byte(`{"hello": 0, "world": 1}`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "minimax_merges.txt"), []byte("hello world\ntest merge\n"), 0644)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "minimax_vocab.json"))
	assert.False(t, os.IsNotExist(err), "vocab should exist")

	_, err = os.Stat(filepath.Join(tmpDir, "minimax_merges.txt"))
	assert.False(t, os.IsNotExist(err), "merges should exist")
}

func skipIfNoData(t *testing.T) *minimaxBPE {
	t.Helper()
	bpe, err := newMinimaxBPE()
	if err != nil {
		t.Skipf("tokenizer data not available (will be downloaded on first use): %v", err)
	}
	return bpe
}

func TestEncodeDecode_Basic(t *testing.T) {
	bpe := skipIfNoData(t)

	tests := []struct {
		name      string
		text      string
		wantEmpty bool
	}{
		{"empty", "", true},
		{"single word", "hello", false},
		{"hello world", "hello world", false},
		{"with punctuation", "Hello, world!", false},
		{"longer text", "The quick brown fox jumps over the lazy dog.", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := bpe.encode(tc.text)
			require.NoError(t, err)

			if tc.wantEmpty {
				assert.Empty(t, ids, "expected empty token IDs for empty text")
			} else {
				assert.NotEmpty(t, ids, "expected non-empty token IDs")
			}

			decoded, err := bpe.decode(ids)
			require.NoError(t, err)
			assert.Equal(t, tc.text, decoded)
		})
	}
}

func TestEncodeDecode_CJK(t *testing.T) {
	bpe := skipIfNoData(t)

	tests := []struct {
		name string
		text string
	}{
		{"chinese", "你好世界"},
		{"japanese", "こんにちは世界"},
		{"korean", "안녕하세요 세계"},
		{"mixed", "Hello 你好 world 世界"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := bpe.encode(tc.text)
			require.NoError(t, err)
			assert.NotEmpty(t, ids, "expected non-empty tokens for CJK text")

			decoded, err := bpe.decode(ids)
			require.NoError(t, err)
			assert.Equal(t, tc.text, decoded)
		})
	}
}

func TestEncodeDecode_Contractions(t *testing.T) {
	bpe := skipIfNoData(t)

	tests := []struct {
		name string
		text string
	}{
		{"don't", "don't"},
		{"won't", "won't"},
		{"it's", "it's"},
		{"they're", "they're"},
		{"I've", "I've"},
		{"I'll", "I'll"},
		{"she'd", "she'd"},
		{"sentence", "I don't think they're going to let us in."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := bpe.encode(tc.text)
			require.NoError(t, err)
			assert.NotEmpty(t, ids)

			decoded, err := bpe.decode(ids)
			require.NoError(t, err)
			assert.Equal(t, tc.text, decoded)
		})
	}
}

func TestEncodeDecode_URLs(t *testing.T) {
	bpe := skipIfNoData(t)

	tests := []struct {
		name string
		text string
	}{
		{"simple", "https://example.com"},
		{"with path", "https://example.com/path/to/resource"},
		{"with query", "https://example.com/path?q=1&b=2"},
		{"with fragment", "https://example.com/page#section"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := bpe.encode(tc.text)
			require.NoError(t, err)
			assert.NotEmpty(t, ids)

			decoded, err := bpe.decode(ids)
			require.NoError(t, err)
			assert.Equal(t, tc.text, decoded)
		})
	}
}

func TestEncodeDecode_Emoji(t *testing.T) {
	bpe := skipIfNoData(t)

	tests := []struct {
		name string
		text string
	}{
		{"globe", "Hello 🌍"},
		{"party", "🎉 party!"},
		{"multi", "Hello 🌍🎉 world"},
		{"flags", "🇺🇸🇬🇧🇩🇪"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := bpe.encode(tc.text)
			require.NoError(t, err)
			assert.NotEmpty(t, ids)

			decoded, err := bpe.decode(ids)
			require.NoError(t, err)
			assert.Equal(t, tc.text, decoded)
		})
	}
}

func TestEncode_Tokenization(t *testing.T) {
	bpe := skipIfNoData(t)

	tests := []struct {
		name    string
		text    string
		wantMin int
		wantMax int
	}{
		{"empty string", "", 0, 0},
		{"single char", "a", 1, 2},
		{"two words", "hi", 1, 2},
		{"sentence", "hello world this is a test", 5, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := bpe.encode(tc.text)
			require.NoError(t, err)

			n := len(encoded)
			assert.GreaterOrEqual(t, n, tc.wantMin, "expected at least %d tokens, got %d", tc.wantMin, n)
			assert.LessOrEqual(t, n, tc.wantMax, "expected at most %d tokens, got %d", tc.wantMax, n)
		})
	}
}

func TestCountTextMinimax_Basic(t *testing.T) {
	count, err := countTextMinimax("hello world")
	if err != nil {
		t.Skipf("tokenizer data not available: %v", err)
	}

	assert.Greater(t, count, 0, "expected positive token count")
	assert.LessOrEqual(t, count, 3, "hello world should be 2-3 tokens")
	assert.GreaterOrEqual(t, count, 2, "hello world should be 2-3 tokens")
}

func TestCountTextMinimax_VariousLengths(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantMin int
		wantMax int
	}{
		{"empty", "", 0, 0},
		{"single char", "a", 1, 2},
		{"short word", "hi", 1, 2},
		{"medium", "hello world", 2, 3},
		{"longer", "The quick brown fox jumps over the lazy dog", 8, 12},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, err := countTextMinimax(tc.text)
			if err != nil {
				t.Skipf("tokenizer data not available: %v", err)
			}

			assert.GreaterOrEqual(t, count, tc.wantMin)
			assert.LessOrEqual(t, count, tc.wantMax)
		})
	}
}

func TestDecode_Reconstruction(t *testing.T) {
	bpe := skipIfNoData(t)

	text := "hello world"
	ids, err := bpe.encode(text)
	require.NoError(t, err)
	require.NotEmpty(t, ids)

	decoded, err := bpe.decode(ids)
	require.NoError(t, err)
	assert.Equal(t, text, decoded)
}

func TestBytesToUnicode(t *testing.T) {
	byteEnc, byteDec := bytesToUnicode()

	// Must map all 256 byte values
	assert.Equal(t, 256, len(byteEnc))
	assert.Equal(t, 256, len(byteDec))

	// Roundtrip: every byte maps to a unique rune and back
	seen := make(map[rune]bool)
	for b := 0; b < 256; b++ {
		r := byteEnc[byte(b)]
		assert.False(t, seen[r], "duplicate rune mapping for byte %d", b)
		seen[r] = true
		assert.Equal(t, byte(b), byteDec[r], "roundtrip failed for byte %d", b)
	}

	// Printable ASCII maps to itself
	for b := byte('!'); b <= byte('~'); b++ {
		assert.Equal(t, rune(b), byteEnc[b], "printable ASCII byte %d should map to itself", b)
	}
}

func TestBPE_VocabAndMergeCount(t *testing.T) {
	bpe := skipIfNoData(t)

	assert.Greater(t, bpe.vocabSize(), 100000, "expected large vocabulary (>100k tokens)")
	assert.Greater(t, bpe.mergeCount(), 100000, "expected large merge table (>100k rules)")
}
