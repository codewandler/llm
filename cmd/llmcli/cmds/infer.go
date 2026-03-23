package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm"
)

// NewInferCmd returns the infer command.
func NewInferCmd(root *RootFlags) *cobra.Command {
	var model string
	var system string
	var verbose bool
	var reasoning string

	cmd := &cobra.Command{
		Use:   "infer <message>",
		Short: "Send a message to Claude and stream the response",
		Long: `Send a message to Claude using stored OAuth credentials.

Uses all stored credential accounts, trying each in alphabetical order
until one succeeds (useful for rate limit fallback).

Examples:
  llmcli infer "Hello, how are you?"              # Uses fast model (haiku)
  llmcli infer -m default "Explain Go channels"   # Balanced (sonnet)
  llmcli infer -m powerful "Write a poem about Go" # Most capable (opus)
  llmcli infer -s "You are a pirate" "Hello"      # With system prompt
  llmcli infer -m codex --reasoning high "Hello"  # With reasoning`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfer(cmd.Context(), args[0], model, system, reasoning, verbose, root)
		},
	}

	cmd.Flags().StringVarP(&model, "model", "m", "fast", "Model to use (fast, default, powerful, codex, or full path)")
	cmd.Flags().StringVarP(&system, "system", "s", "", "System prompt to prepend")
	cmd.Flags().StringVar(&reasoning, "reasoning", "", "Reasoning effort: low, medium, high (for o-series / codex models)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show usage statistics")
	return cmd
}

func runInfer(ctx context.Context, userMsg, model, system, reasoning string, verbose bool, root *RootFlags) error {
	httpClient, logHandler := root.BuildHTTPClient()
	concreteProvider, err := createProvider(ctx, httpClient, root.BuildLLMOptions(logHandler)...)
	if err != nil {
		return err
	}
	var provider llm.Provider = concreteProvider

	msgs := make(llm.Messages, 0)

	if system != "" {
		msgs.AddSystemMsg(system)
	} else {
		msgs.AddSystemMsg("You are Tessa. Before you do anything -> Introduce yourself! You must complete by calling `complete_turn` tool. This can happen together with adding facts")
	}
	msgs.AddUserMsg(userMsg)

	type (
		addFactParams struct {
			Fact string `json:"fact"`
		}
		completeTurnParams struct {
			Success bool `json:"success"`
		}
		DefaultToolResult struct {
			Message string `json:"message"`
			Success bool   `json:"success"`
		}
	)

	tools := llm.NewToolSet(
		llm.NewToolSpec[addFactParams]("add_fact", "Store a single fact"),
		llm.NewToolSpec[completeTurnParams]("complete_turn", "Complete the current turn."),
	).Definitions()

	// --- Token estimate (verbose only) ---
	var tokenEstimate *llm.TokenCount
	if verbose {
		if tc, ok := provider.(llm.TokenCounter); ok {
			est, err := tc.CountTokens(ctx, llm.TokenCountRequest{
				Model:    model,
				Messages: msgs,
				Tools:    tools,
			})
			if err == nil {
				tokenEstimate = est
				printTokenEstimate(est)
			}
		}
	}

	stream, err := provider.CreateStream(ctx, llm.StreamRequest{
		Model:           model,
		Messages:        msgs,
		ReasoningEffort: llm.ReasoningEffort(reasoning),
		ToolChoice:      llm.ToolChoiceAuto{},
		Tools:           tools,
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	var inReasoning bool
	var hadTokenOutput bool
	var seenTools = make(map[string]bool)

	proc := llm.Process(ctx, stream).
		HandleTool(
			llm.NewToolHandler("complete_turn", func(ctx context.Context, in completeTurnParams) (*DefaultToolResult, error) {
				return &DefaultToolResult{Message: "Turn complete", Success: in.Success}, nil
			}),
			llm.NewToolHandler("add_fact", func(ctx context.Context, in addFactParams) (*DefaultToolResult, error) {
				return &DefaultToolResult{
					Message: fmt.Sprintf("Fact added: %s", in.Fact),
					Success: true,
				}, nil
			}),
		).
		OnText(func(chunk string) {
			if inReasoning {
				fmt.Print(ansiReset)
				inReasoning = false
			}
			fmt.Print(chunk)
			hadTokenOutput = true
			if logHandler != nil {
				logHandler.MarkTokenOutput()
			}
		}).
		OnReasoning(func(chunk string) {
			if !inReasoning {
				fmt.Print(ansiDim)
				inReasoning = true
			}
			fmt.Print(chunk)
		}).
		OnToolDelta(func(d *llm.Delta) {
			if inReasoning {
				fmt.Print(ansiReset)
				inReasoning = false
			}
			if !seenTools[d.ToolID] {
				seenTools[d.ToolID] = true
				if hadTokenOutput {
					fmt.Println()
				}
				fmt.Printf("[tool:%s id:%s] ", d.ToolName, d.ToolID)
			}
			fmt.Print(d.ToolArgs)
			hadTokenOutput = true
			if logHandler != nil {
				logHandler.MarkTokenOutput()
			}
		})

	if root.LogEvents {
		proc.OnStart(func(s *llm.StreamStart) {
			logStreamEvent(llm.StreamEvent{Type: llm.StreamEventStart, Start: s})
		})
	}

	result := <-proc.Result()

	if inReasoning {
		fmt.Print(ansiReset)
	}
	if hadTokenOutput {
		fmt.Println()
	}

	if result.Error() != nil {
		return result.Error()
	}

	if verbose {
		printVerboseInfo(result, tokenEstimate)
	}

	return nil
}

// printTokenEstimate prints the pre-request token estimate section when running
// in verbose mode. Called before CreateStream.
func printTokenEstimate(est *llm.TokenCount) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s── token estimate ──%s\n", ansiDim, ansiReset)

	type field struct {
		label string
		value string
	}
	fields := []field{
		{"input (est)", fmt.Sprintf("%d", est.InputTokens)},
	}
	if est.SystemTokens > 0 {
		fields = append(fields, field{"  system", fmt.Sprintf("%d", est.SystemTokens)})
	}
	if est.UserTokens > 0 {
		fields = append(fields, field{"  user", fmt.Sprintf("%d", est.UserTokens)})
	}
	if est.AssistantTokens > 0 {
		fields = append(fields, field{"  assistant", fmt.Sprintf("%d", est.AssistantTokens)})
	}
	if est.ToolResultTokens > 0 {
		fields = append(fields, field{"  tool_results", fmt.Sprintf("%d", est.ToolResultTokens)})
	}
	if est.ToolsTokens > 0 {
		fields = append(fields, field{"  tools", fmt.Sprintf("%d", est.ToolsTokens)})
		for name, n := range est.PerTool {
			fields = append(fields, field{fmt.Sprintf("    %s", name), fmt.Sprintf("%d", n)})
		}
	}
	if est.OverheadTokens > 0 {
		fields = append(fields, field{"  overhead", fmt.Sprintf("%d", est.OverheadTokens)})
	}

	maxWidth := 0
	for _, f := range fields {
		if len(f.label) > maxWidth {
			maxWidth = len(f.label)
		}
	}
	for _, f := range fields {
		fmt.Fprintf(os.Stderr, "%*s: %s\n", maxWidth, f.label, f.value)
	}
}

// printVerboseInfo prints multi-line verbose metadata with right-aligned labels.
func printVerboseInfo(result *llm.StreamResult, est *llm.TokenCount) {
	type field struct {
		label string
		value string
	}
	var fields []field

	start := result.Start
	usage := result.Usage

	// Request ID (from the upstream API)
	if start != nil && start.RequestID != "" {
		fields = append(fields, field{"request_id", start.RequestID})
	}

	// API model (what the provider returned)
	if start != nil && start.Model != "" {
		fields = append(fields, field{"api_model", start.Model})
	}

	// Routing metadata (router / auto provider only)
	if r := result.Routed; r != nil {
		routedVal := r.Provider
		if r.ModelResolved != "" && r.ModelResolved != r.ModelRequested {
			routedVal += fmt.Sprintf("  %s → %s", r.ModelRequested, r.ModelResolved)
		} else if r.ModelResolved != "" {
			routedVal += "  " + r.ModelResolved
		}
		fields = append(fields, field{"routed_to", routedVal})
		if len(r.Errors) > 0 {
			for i, e := range r.Errors {
				fields = append(fields, field{fmt.Sprintf("  skipped[%d]", i), e.Error()})
			}
		}
	}

	// Stop reason
	if result.StopReason != "" {
		fields = append(fields, field{"stop_reason", string(result.StopReason)})
	}

	// Reasoning summary (character count — full text already streamed live)
	if result.Reasoning != "" {
		fields = append(fields, field{"reasoning", fmt.Sprintf("%d chars", len(result.Reasoning))})
	}

	// Tool calls
	if len(result.ToolCalls) > 0 {
		for i, tc := range result.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			label := fmt.Sprintf("tool[%d]", i)
			fields = append(fields, field{label, fmt.Sprintf("%s(%s) id:%s", tc.Name, argsJSON, tc.ID)})
		}
	}

	// Tool results
	if len(result.ToolResults) > 0 {
		for i, tr := range result.ToolResults {
			label := fmt.Sprintf("result[%d]", i)
			value := tr.Output
			if tr.IsError {
				value = "(error) " + value
			}
			if len(value) > 120 {
				value = value[:120] + "…"
			}
			fields = append(fields, field{label, value})
		}
	}

	// Token usage
	if usage != nil {
		tokLine := fmt.Sprintf("%d in, %d out", usage.InputTokens, usage.OutputTokens)
		if est != nil {
			drift := 0.0
			if usage.InputTokens > 0 {
				diff := float64(est.InputTokens - usage.InputTokens)
				if diff < 0 {
					diff = -diff
				}
				drift = diff / float64(usage.InputTokens) * 100
			}
			tokLine += fmt.Sprintf("  (est %d in, drift %.1f%%)", est.InputTokens, drift)
		}
		fields = append(fields, field{"tokens", tokLine})
	}

	// Cache usage (shown only when provider returned cache data)
	if usage != nil && (usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0) {
		fields = append(fields, field{"cache", fmt.Sprintf("%d read, %d written", usage.CacheReadTokens, usage.CacheWriteTokens)})
	}

	// Cost
	if usage != nil && usage.Cost > 0 {
		hasBreakdown := usage.InputCost > 0 || usage.CacheReadCost > 0 || usage.CacheWriteCost > 0
		if hasBreakdown {
			fields = append(fields, field{"cost", fmt.Sprintf("%s (in %s, cache-r %s, cache-w %s, out %s)",
				formatCost(usage.Cost),
				formatCost(usage.InputCost),
				formatCost(usage.CacheReadCost),
				formatCost(usage.CacheWriteCost),
				formatCost(usage.OutputCost),
			)})
		} else {
			fields = append(fields, field{"cost", formatCost(usage.Cost)})
		}
	}

	// Time to first token
	if start != nil && start.TimeToFirstToken > 0 {
		fields = append(fields, field{"ttft", fmt.Sprintf("%dms", start.TimeToFirstToken.Milliseconds())})
	}

	if len(fields) == 0 {
		return
	}

	// Calculate max label width for alignment
	maxWidth := 0
	for _, f := range fields {
		if len(f.label) > maxWidth {
			maxWidth = len(f.label)
		}
	}

	// Print with right-aligned labels
	fmt.Println()
	for _, f := range fields {
		fmt.Printf("%*s: %s\n", maxWidth, f.label, f.value)
	}

	// Next messages (what would be appended to the conversation history)
	if next := result.Next(); len(next) > 0 {
		fmt.Println()
		fmt.Println("next messages:")
		for _, msg := range next {
			b, err := json.MarshalIndent(msg, "  ", "  ")
			if err != nil {
				fmt.Printf("  (marshal error: %v)\n", err)
				continue
			}
			fmt.Printf("  %s\n", b)
		}
	}
}

// formatCost formats cost with appropriate precision for the amount.
// Smaller costs get more decimal places for visibility.
func formatCost(cost float64) string {
	switch {
	case cost < 0.0001:
		return fmt.Sprintf("$%.8f", cost)
	case cost < 0.01:
		return fmt.Sprintf("$%.6f", cost)
	case cost < 1.0:
		return fmt.Sprintf("$%.4f", cost)
	default:
		return fmt.Sprintf("$%.2f", cost)
	}
}

// noisyStreamEvents are collapsed to a single header line with no body.
var noisyStreamEvents = map[llm.StreamEventType]bool{
	llm.StreamEventCreated: true,
	llm.StreamEventDelta:   true,
}

// logStreamEvent pretty-prints a StreamEvent to stderr using the same visual
// style as the SSE event renderer in httplog.go: bold [event_type] header
// followed by indented JSON body. Delta events are collapsed to a single line.
func logStreamEvent(ev llm.StreamEvent) {
	eventType := string(ev.Type)

	if noisyStreamEvents[ev.Type] {
		label := string(ev.Type)
		if ev.Type == llm.StreamEventDelta && ev.Delta != nil {
			label = fmt.Sprintf("%s:%s", label, ev.Delta.Type)
		}
		fmt.Fprintf(os.Stderr, "\n    %s[%s]%s\n", ansiBold, label, ansiReset)
		return
	}

	b, err := json.Marshal(ev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n    %s[%s]%s\n    (marshal error: %v)\n", ansiBold, eventType, ansiReset, err)
		return
	}

	fmt.Fprintf(os.Stderr, "\n    %s[%s]%s\n", ansiBold, eventType, ansiReset)
	fmt.Fprintln(os.Stderr, indentAll(prettyJSON(string(b)), "    "))
}
