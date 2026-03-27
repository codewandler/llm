package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

// NewInferCmd returns the infer command.
func NewInferCmd(root *RootFlags) *cobra.Command {
	var model string
	var system string
	var verbose bool
	var thinkingEffort string
	var outputEffort string
	var demoTools bool

	cmd := &cobra.Command{
		Use:   "infer <message>",
		Short: "Send a message to Claude and stream the response",
		Long: `Send a message to Claude using stored OAuth credentials.

Uses all stored credential accounts, trying each in alphabetical order
until one succeeds (useful for rate limit fallback).

Examples:
  llmcli infer "Hello, how are you?"				# Uses fast model (haiku)
  llmcli infer -m default "Explain Go channels"		# Balanced (sonnet)
  llmcli infer -m powerful "Write a poem about Go"	# Most capable (opus)
  llmcli infer -s "You are a pirate" "Hello"		# Add system prompt
  llmcli infer -m codex --thinking high "Hello"		# Add thinking
  llmcli infer --effort high "Explain this"			# High output effort response`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfer(cmd.Context(), args[0], model, system, thinkingEffort, outputEffort, verbose, demoTools, root)
		},
	}

	cmd.Flags().StringVarP(&model, "model", "m", "fast", "Model to use (fast, default, powerful, codex, or full path)")
	cmd.Flags().StringVarP(&system, "system", "s", "", "System prompt to prepend")
	cmd.Flags().StringVar(&thinkingEffort, "thinking", "", "Thinking effort: low, medium, high (for o-series / codex models)")
	cmd.Flags().StringVar(&outputEffort, "effort", "", "Output effort: low, medium, high, max (Anthropic Sonnet 4.6+ / Opus 4.6+)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show usage statistics")
	cmd.Flags().BoolVar(&demoTools, "demo-tools", false, "Enable demo tool loop (add_fact + complete_turn) and default persona")
	return cmd
}

func runInfer(ctx context.Context, userMsg, model, system, thinking, effort string, verbose bool, demoTools bool, root *RootFlags) error {
	httpClient, logHandler := root.BuildHTTPClient()
	concreteProvider, err := createProvider(ctx, httpClient, root.BuildLLMOptions(logHandler)...)
	if err != nil {
		return err
	}
	var provider llm.Provider = concreteProvider

	spec := buildInferSpec(userMsg, model, system, thinking, effort, demoTools)

	// --- Token estimate (verbose only) ---
	var tokenEstimate *llm.TokenCount
	if verbose {
		if tc, ok := provider.(llm.TokenCounter); ok {
			est, err := tc.CountTokens(ctx, llm.TokenCountRequest{
				Model:    spec.Model,
				Messages: spec.Messages,
				Tools:    spec.Tools,
			})
			if err == nil {
				tokenEstimate = est
				printTokenEstimate(est)
			}
		}
	}

	stream, err := provider.CreateStream(ctx, llm.Request{
		Model:          spec.Model,
		Messages:       spec.Messages,
		ThinkingEffort: spec.ThinkingEffort,
		OutputEffort:   spec.OutputEffort,
		ToolChoice:     spec.ToolChoice,
		Tools:          spec.Tools,
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	var inReasoning bool
	var hadTokenOutput bool

	proc := llm.NewEventProcessor(ctx, stream).
		OnEvent(llm.EventHandlerFunc(func(ev llm.Event) {
			if root.LogEvents {
				d, _ := json.MarshalIndent(ev, "  ", "  ")
				fmt.Printf("\n[EVT :: %s]\n%s\n", ev.Type(), string(d))
			}
		}))

	if len(spec.ToolHandlers) > 0 {
		proc = proc.HandleTool(spec.ToolHandlers...)
	}

	proc = proc.
		OnTextDelta(func(chunk string) {
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
		OnReasoningDelta(func(chunk string) {
			if !inReasoning {
				fmt.Print(ansiDim)
				inReasoning = true
			}
			fmt.Print(chunk)
		}).
		OnToolDelta(func(d llm.ToolDeltaPart) {
			//
		})

	result := proc.Result()

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
func printVerboseInfo(result llm.Result, est *llm.TokenCount) {
	type field struct {
		label string
		value string
	}
	var fields []field

	usage := result.Usage()

	// Stop reason
	if result.StopReason() != "" {
		fields = append(fields, field{"stop_reason", string(result.StopReason())})
	}

	// Reasoning summary (character count — full text already streamed live)
	if result.Reasoning() != "" {
		fields = append(fields, field{"reasoning", fmt.Sprintf("%d chars", len(result.Reasoning()))})
	}

	// Tool calls
	if len(result.ToolCalls()) > 0 {
		for i, tc := range result.ToolCalls() {
			argsJSON, _ := json.Marshal(tc.ToolArgs())
			label := fmt.Sprintf("tool[%d]", i)
			fields = append(fields, field{label, fmt.Sprintf("%s(%s) id:%s", tc.ToolName(), argsJSON, tc.ToolCallID())})
		}
	}

	// Tool results
	msgs := result.Next()
	for i, msg := range msgs {
		if tm, ok := msg.(llm.ToolMessage); ok {
			label := fmt.Sprintf("result[%d]", i)
			value := tm.ToolOutput()
			if tm.IsError() {
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
	if len(msgs) > 0 {
		fmt.Println()
		fmt.Println("next messages:")
		for _, msg := range msgs {
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

type inferSpec struct {
	Model          string
	Messages       llm.Messages
	ThinkingEffort llm.ThinkingEffort
	OutputEffort   llm.OutputEffort
	ToolChoice     llm.ToolChoice
	Tools          []tool.Definition
	ToolHandlers   []tool.NamedHandler
}

type addFactParams struct {
	Fact string `json:"fact"`
}

type completeTurnParams struct {
	Success bool `json:"success"`
}

type defaultToolResult struct {
	Message string `json:"message"`
	Success bool   `json:"success"`
}

const defaultDemoSystemPrompt = "You are Tessa. Before you do anything -> Introduce yourself! You must complete by calling `complete_turn` tool. This can happen together with adding facts"

func buildInferSpec(userMsg, model, system, thinking, effort string, demoTools bool) inferSpec {
	msgs := make(llm.Messages, 0, 2)
	cacheHint := &llm.CacheHint{Enabled: true}

	if system != "" {
		msgs = append(msgs, llm.System(system, cacheHint))
	} else if demoTools {
		msgs = append(msgs, llm.System(defaultDemoSystemPrompt, cacheHint))
	}
	msgs = append(msgs, llm.User(userMsg))

	spec := inferSpec{
		Model:          model,
		Messages:       msgs,
		ThinkingEffort: llm.ThinkingEffort(thinking),
		OutputEffort:   llm.OutputEffort(effort),
	}

	if !demoTools {
		return spec
	}

	spec.Tools = tool.NewToolSet(
		tool.NewSpec[addFactParams]("add_fact", "Store a single fact"),
		tool.NewSpec[completeTurnParams]("complete_turn", "Complete the current turn."),
	).Definitions()
	spec.ToolChoice = llm.ToolChoiceRequired{}
	spec.ToolHandlers = []tool.NamedHandler{
		tool.NewHandler("complete_turn", func(ctx context.Context, in completeTurnParams) (*defaultToolResult, error) {
			return &defaultToolResult{Message: "Turn complete", Success: in.Success}, nil
		}),
		tool.NewHandler("add_fact", func(ctx context.Context, in addFactParams) (*defaultToolResult, error) {
			return &defaultToolResult{Message: fmt.Sprintf("Fact added: %s", in.Fact), Success: true}, nil
		}),
	}

	return spec
}
