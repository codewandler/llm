package minimax

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/codewandler/llm/tokencount"
)

func init() {
	// Register the MiniMax BPE tokenizer with the tokencount package so that
	// CountText(tokencount.EncodingMinimax, text) dispatches here without an
	// import cycle (tokencount cannot import provider/minimax).
	tokencount.RegisterEncoding(tokencount.EncodingMinimax, countTextMinimax)
}

// defaultDataDir is the XDG cache directory for the MiniMax tokenizer data.
const defaultDataDir = "~/.cache/llm/minimax"

// getDataDir returns the directory where tokenizer data is stored.
// It expands ~ to the user's home directory.
func getDataDir() string {
	if strings.HasPrefix(defaultDataDir, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, defaultDataDir[2:])
		}
	}
	return defaultDataDir
}

// dataFilesExist checks whether the required MiniMax tokenizer data files
// (vocab and merges) are present in the cache directory.
func dataFilesExist() bool {
	dataDir := getDataDir()
	if _, err := os.Stat(filepath.Join(dataDir, "minimax_vocab.json")); os.IsNotExist(err) {
		return false
	}
	if _, err := os.Stat(filepath.Join(dataDir, "minimax_merges.txt")); os.IsNotExist(err) {
		return false
	}
	return true
}

// downloadAndExtract downloads tokenizer.json from HuggingFace (MiniMaxAI/MiniMax-M2.1)
// and extracts the vocab and merge rules to the cache directory.
//
// This downloads ~9.27 MB and writes ~8.9 MB (vocab + merges). It runs once on
// first use; subsequent calls load from the local cache.
func downloadAndExtract() error {
	dataDir := getDataDir()

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("minimax bpe: create data dir: %w", err)
	}

	log.Printf("downloading MiniMax tokenizer from HuggingFace...")

	const url = "https://huggingface.co/MiniMaxAI/MiniMax-M2.1/resolve/main/tokenizer.json"

	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("minimax bpe: download tokenizer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("minimax bpe: download tokenizer: status %d", resp.StatusCode)
	}

	var tok struct {
		Model struct {
			Vocab  map[string]int `json:"vocab"`
			Merges []string       `json:"merges"`
		} `json:"model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("minimax bpe: parse tokenizer.json: %w", err)
	}

	log.Printf("extracting vocab (%d tokens) and merges (%d rules)...",
		len(tok.Model.Vocab), len(tok.Model.Merges))

	vocabData, err := json.Marshal(tok.Model.Vocab)
	if err != nil {
		return fmt.Errorf("minimax bpe: marshal vocab: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "minimax_vocab.json"), vocabData, 0644); err != nil {
		return fmt.Errorf("minimax bpe: write vocab: %w", err)
	}

	mergesContent := strings.Join(tok.Model.Merges, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dataDir, "minimax_merges.txt"), []byte(mergesContent), 0644); err != nil {
		return fmt.Errorf("minimax bpe: write merges: %w", err)
	}

	log.Printf("MiniMax tokenizer data extracted to %s", dataDir)
	return nil
}

// ---------- GPT-2 byte-level BPE ----------

// bytesToUnicode builds the canonical GPT-2 byte↔unicode mapping.
// Printable ASCII and Latin-1 supplement bytes map to themselves as runes;
// all other byte values (control chars, etc.) are shifted to U+0100..U+01FF
// to avoid whitespace/control characters that would break BPE.
//
// Reference: https://github.com/openai/gpt-2/blob/master/src/encoder.py
func bytesToUnicode() (byteToRune map[byte]rune, runeToByte map[rune]byte) {
	byteToRune = make(map[byte]rune, 256)
	runeToByte = make(map[rune]byte, 256)

	// Ranges that map byte value → same Unicode code point:
	//   '!' (33) .. '~' (126)
	//   '¡' (161) .. '¬' (172)
	//   '®' (174) .. 'ÿ' (255)
	var bs []int
	for b := int('!'); b <= int('~'); b++ {
		bs = append(bs, b)
	}
	for b := int('¡'); b <= int('¬'); b++ {
		bs = append(bs, b)
	}
	for b := int('®'); b <= int('ÿ'); b++ {
		bs = append(bs, b)
	}

	bsSet := make(map[int]bool, len(bs))
	for _, b := range bs {
		bsSet[b] = true
	}

	cs := make([]int, len(bs))
	copy(cs, bs)

	// Remaining bytes (0–32, 127–160, 173) get shifted to 256+.
	n := 0
	for b := 0; b < 256; b++ {
		if !bsSet[b] {
			bs = append(bs, b)
			cs = append(cs, 256+n)
			n++
		}
	}

	for i, b := range bs {
		byteToRune[byte(b)] = rune(cs[i])
		runeToByte[rune(cs[i])] = byte(b)
	}
	return byteToRune, runeToByte
}

// gpt2Pattern is the GPT-2 pre-tokenization regex.
// It splits text into chunks that are independently BPE-encoded.
//
// Reference: https://github.com/openai/gpt-2/blob/master/src/encoder.py
var gpt2Pattern = regexp.MustCompile(
	`'s|'t|'re|'ve|'m|'ll|'d` +
		`| ?\pL+` +
		`| ?\pN+` +
		`| ?[^\s\pL\pN]+` +
		`|\s+`,
)

// minimaxBPE implements a BPE tokenizer for MiniMax models using the canonical
// GPT-2 byte-level BPE algorithm.
type minimaxBPE struct {
	vocab      map[string]int
	mergeRanks map[string]int // "tok1 tok2" → rank (lower = applied first)
	byteEnc    map[byte]rune  // byte → unicode char
	byteDec    map[rune]byte  // unicode char → byte
	cache      sync.Map       // BPE cache: encoded word string → BPE result string
}

var (
	globalBPE     *minimaxBPE
	globalBPEOnce sync.Once
	globalBPEErr  error
)

// newMinimaxBPE loads and returns the singleton MiniMax BPE tokenizer.
// The first call downloads tokenizer data from HuggingFace if not cached.
func newMinimaxBPE() (*minimaxBPE, error) {
	globalBPEOnce.Do(func() {
		globalBPE, globalBPEErr = loadBPE()
	})
	return globalBPE, globalBPEErr
}

func loadBPE() (*minimaxBPE, error) {
	if !dataFilesExist() {
		if err := downloadAndExtract(); err != nil {
			return nil, err
		}
	}

	dataDir := getDataDir()

	vocabData, err := os.ReadFile(filepath.Join(dataDir, "minimax_vocab.json"))
	if err != nil {
		return nil, fmt.Errorf("minimax bpe: read vocab: %w", err)
	}

	var vocab map[string]int
	if err := json.Unmarshal(vocabData, &vocab); err != nil {
		return nil, fmt.Errorf("minimax bpe: parse vocab: %w", err)
	}

	mergesData, err := os.ReadFile(filepath.Join(dataDir, "minimax_merges.txt"))
	if err != nil {
		return nil, fmt.Errorf("minimax bpe: read merges: %w", err)
	}

	mergeLines := strings.Split(strings.TrimRight(string(mergesData), "\n"), "\n")
	mergeRanks := make(map[string]int, len(mergeLines))
	for i, merge := range mergeLines {
		mergeRanks[merge] = i
	}

	byteEnc, byteDec := bytesToUnicode()

	return &minimaxBPE{
		vocab:      vocab,
		mergeRanks: mergeRanks,
		byteEnc:    byteEnc,
		byteDec:    byteDec,
	}, nil
}

// encode tokenizes text into token IDs using GPT-2 style byte-level BPE.
func (m *minimaxBPE) encode(text string) ([]int, error) {
	if text == "" {
		return nil, nil
	}

	var ids []int
	for _, match := range gpt2Pattern.FindAllString(text, -1) {
		encoded := m.byteEncode(match)
		bpeResult := m.bpe(encoded)
		for _, tok := range strings.Split(bpeResult, " ") {
			if id, ok := m.vocab[tok]; ok {
				ids = append(ids, id)
			}
			// Unknown tokens after BPE should not occur with a proper vocab;
			// silently skip if they do (better than crashing).
		}
	}
	return ids, nil
}

// byteEncode converts a string to its byte-level unicode representation.
// Each byte of the UTF-8 encoded string is mapped through bytesToUnicode().
func (m *minimaxBPE) byteEncode(s string) string {
	raw := []byte(s)
	runes := make([]rune, len(raw))
	for i, b := range raw {
		runes[i] = m.byteEnc[b]
	}
	return string(runes)
}

// bpe applies BPE merges to a byte-encoded token string.
// Results are cached for performance.
func (m *minimaxBPE) bpe(token string) string {
	if cached, ok := m.cache.Load(token); ok {
		return cached.(string)
	}

	word := make([]string, 0, len([]rune(token)))
	for _, r := range token {
		word = append(word, string(r))
	}

	if len(word) <= 1 {
		result := strings.Join(word, " ")
		m.cache.Store(token, result)
		return result
	}

	for {
		bestRank := -1
		bestIdx := -1

		for i := 0; i < len(word)-1; i++ {
			pair := word[i] + " " + word[i+1]
			if rank, ok := m.mergeRanks[pair]; ok {
				if bestRank == -1 || rank < bestRank {
					bestRank = rank
					bestIdx = i
				}
			}
		}

		if bestIdx == -1 {
			break
		}

		merged := word[bestIdx] + word[bestIdx+1]
		newWord := make([]string, 0, len(word)-1)
		newWord = append(newWord, word[:bestIdx]...)
		newWord = append(newWord, merged)
		newWord = append(newWord, word[bestIdx+2:]...)
		word = newWord

		if len(word) == 1 {
			break
		}
	}

	result := strings.Join(word, " ")
	m.cache.Store(token, result)
	return result
}

// decode converts token IDs back to text.
func (m *minimaxBPE) decode(ids []int) (string, error) {
	revVocab := make(map[int]string, len(m.vocab))
	for token, id := range m.vocab {
		revVocab[id] = token
	}

	var buf strings.Builder
	for _, id := range ids {
		token, ok := revVocab[id]
		if !ok {
			buf.WriteString("[UNK]")
			continue
		}
		buf.WriteString(token)
	}

	text := buf.String()
	raw := make([]byte, 0, len(text))
	for _, r := range text {
		if b, ok := m.byteDec[r]; ok {
			raw = append(raw, b)
		} else {
			raw = append(raw, []byte(string(r))...)
		}
	}
	return string(raw), nil
}

// vocabSize returns the size of the vocabulary.
func (m *minimaxBPE) vocabSize() int { return len(m.vocab) }

// mergeCount returns the number of BPE merges.
func (m *minimaxBPE) mergeCount() int { return len(m.mergeRanks) }

// countTextMinimax returns the number of tokens in text using MiniMax BPE.
// It is registered with tokencount.RegisterEncoding so that
// tokencount.CountText(tokencount.EncodingMinimax, text) dispatches here.
func countTextMinimax(text string) (int, error) {
	bpe, err := newMinimaxBPE()
	if err != nil {
		return 0, err
	}
	ids, err := bpe.encode(text)
	if err != nil {
		return 0, fmt.Errorf("minimax bpe: encode: %w", err)
	}
	return len(ids), nil
}
