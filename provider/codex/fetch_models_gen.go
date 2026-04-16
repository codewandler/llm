//go:build ignore

// Command fetch-models regenerates models.json from the live Codex API.
// Run via:
//
//	go generate ./provider/codex/
//
// Requires local Codex credentials at ~/.codex/auth.json.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/codewandler/llm/provider/codex"
)

// localModelsResponse mirrors the internal modelsResponse / modelInfo types.
// Only fields relevant to model selection and routing are included; large
// operational fields (base_instructions, model_messages, …) are omitted by
// not declaring them here, so they are silently dropped on decode/re-encode.
type localModelsResponse struct {
	Models []localModelInfo `json:"models"`
}

type localModelInfo struct {
	Slug                     string                   `json:"slug"`
	DisplayName              string                   `json:"display_name"`
	Description              *string                  `json:"description,omitempty"`
	DefaultReasoningLevel    *string                  `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels []localReasoningPreset   `json:"supported_reasoning_levels,omitempty"`
	Visibility               string                   `json:"visibility"`
	SupportedInAPI           bool                     `json:"supported_in_api"`
	AvailableInPlans         []string                 `json:"available_in_plans,omitempty"`
	Priority                 int                      `json:"priority"`
	AdditionalSpeedTiers     []string                 `json:"additional_speed_tiers,omitempty"`
	SupportVerbosity         bool                     `json:"support_verbosity,omitempty"`
	DefaultVerbosity         *string                  `json:"default_verbosity,omitempty"`
	SupportsReasoningSummary bool                     `json:"supports_reasoning_summaries,omitempty"`
	DefaultReasoningSummary  *string                  `json:"default_reasoning_summary,omitempty"`
	ContextWindow            int                      `json:"context_window,omitempty"`
	InputModalities          []string                 `json:"input_modalities,omitempty"`
	OutputModalities         []string                 `json:"output_modalities,omitempty"`
	Deprecated               bool                     `json:"deprecated,omitempty"`
	SupportsParallelTools    bool                     `json:"supports_parallel_tool_calls,omitempty"`
	TruncationPolicy         *localTruncationPolicy   `json:"truncation_policy,omitempty"`
}

type localReasoningPreset struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type localTruncationPolicy struct {
	Mode  string `json:"mode"`
	Limit int    `json:"limit"`
}

func main() {
	out := flag.String("out", "models.json", "output file path")
	flag.Parse()

	auth, err := codex.LoadAuth()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load auth: %v\n", err)
		os.Exit(1)
	}

	p := codex.New(auth)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	raw, err := p.FetchRawModels(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch models: %v\n", err)
		os.Exit(1)
	}

	// Decode through the local struct — this strips large operational fields
	// (base_instructions, model_messages, etc.) that we don't need.
	var resp localModelsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "parse response: %v\n", err)
		os.Exit(1)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(resp); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, %d models)\n", *out, buf.Len(), len(resp.Models))
}
