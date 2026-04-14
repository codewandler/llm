package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/tool"
	"github.com/spf13/cobra"
)

// NewInferCmd returns the infer command.
func NewInferCmd(root *RootFlags) *cobra.Command {
	var opts inferOpts

	cmd := &cobra.Command{
		Use:   "infer <message>",
		Short: "Send a message to an LLM and stream the response",
		Long: `Send a message using stored OAuth credentials.

Uses all stored credential accounts, trying each in alphabetical order
until one succeeds (useful for rate limit fallback).

Examples:
  llmcli infer "Hello, how are you?"					# auto thinking, provider-default effort
  llmcli infer --effort high "Explain channels"			# high effort, auto thinking
  llmcli infer --effort max --thinking on "Explain this"	# max effort, force thinking on
  llmcli infer --thinking off "Quick answer"				# disable thinking
  llmcli infer -m powerful "Write a poem about Go"		# Most capable (opus)
  llmcli infer -s "You are a pirate" "Hello"				# Add system prompt
  llmcli infer --max-tokens 512 "Short answer please"		# Limit output length
  llmcli infer --temperature 0.2 "Precise answer"			# Low randomness
  llmcli infer --tool-choice none --demo-tools "List facts"	# Tools available but not forced
  llmcli infer --output-format json "Return a JSON object"	# Constrain to JSON output`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.UserMsg = args[0]
			return runInfer(cmd.Context(), opts, root)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.Model, "model", "m", "fast", "Model alias or full path")
	f.StringVarP(&opts.System, "system", "s", "", "System prompt")
	f.BoolVarP(&opts.Verbose, "verbose", "v", false, "Verbose output")
	f.BoolVar(&opts.DemoTools, "demo-tools", false, "Enable demo tool loop (add_fact + complete_turn)")
	f.IntVar(&opts.MaxTokens, "max-tokens", 8_000, "Max tokens to generate")
	f.Float64Var(&opts.Temperature, "temperature", 0, "Sampling temperature 0.0\u20132.0 (0 = provider default)")
	f.Float64Var(&opts.TopP, "top-p", 0, "Nucleus sampling 0.0\u20131.0 (0 = provider default)")
	f.IntVar(&opts.TopK, "top-k", 0, "Top-K limit (0 = provider default)")
	f.TextVar(&opts.Thinking, "thinking", llm.ThinkingMode(""), "Thinking mode: auto, on, off")
	f.TextVar(&opts.Effort, "effort", llm.Effort(""), "Effort: low, medium, high, max")
	f.TextVar(&opts.ToolChoice, "tool-choice", llm.ToolChoiceFlag{}, "Tool selection: auto, none, required, tool:<name>")
	f.TextVar(&opts.OutputFormat, "output-format", llm.OutputFormat(""), "Output format: text, json")

	return cmd
}

type inferOpts struct {
	// Populated from the positional argument, not a flag.
	UserMsg string

	// Flags — cobra writes directly via the appropriate Var methods.
	Model        string
	System       string
	Verbose      bool
	DemoTools    bool
	MaxTokens    int
	Temperature  float64
	TopP         float64
	TopK         int
	Thinking     llm.ThinkingMode   // f.TextVar
	Effort       llm.Effort         // f.TextVar
	ToolChoice   llm.ToolChoiceFlag // f.TextVar; nil Value = "not specified"
	OutputFormat llm.OutputFormat   // f.TextVar

	// Populated by runInfer when DemoTools is true, not from flags.
	demoToolHandlers []tool.NamedHandler
}

// resolveToolChoice returns the effective tool choice for the request.
// When --demo-tools is active and --tool-choice was not set, defaults to
// ToolChoiceRequired. An explicit --tool-choice flag always takes precedence.
func (o inferOpts) resolveToolChoice() llm.ToolChoice {
	if o.ToolChoice.Value != nil {
		return o.ToolChoice.Value
	}
	if o.DemoTools {
		return llm.ToolChoiceRequired{}
	}
	return nil
}

func runInfer(ctx context.Context, opts inferOpts, root *RootFlags) error {
	httpClient, logHandler := root.BuildHTTPClient()
	concreteProvider, err := createProvider(ctx, httpClient, root.BuildLLMOptions(logHandler)...)
	if err != nil {
		return err
	}
	var provider llm.Provider = concreteProvider

	// Messages
	// System prompt: explicit --system takes precedence; demo-tools fills the gap.
	system := opts.System
	if system == "" && opts.DemoTools {
		system = defaultDemoSystemPrompt
	}

	// Build request.
	b := llm.NewRequestBuilder().
		Model(opts.Model).
		Effort(opts.Effort).
		Thinking(opts.Thinking).
		MaxTokens(opts.MaxTokens).
		Temperature(opts.Temperature).
		TopP(opts.TopP).
		TopK(opts.TopK).
		OutputFormat(opts.OutputFormat)
	if system != "" {
		b = b.System(system, llm.CacheTTL1h)
	}
	b = b.User(opts.UserMsg, llm.CacheTTL1h)

	toolChoice := opts.resolveToolChoice()
	if opts.DemoTools {
		defs, handlers := buildDemoTools()
		opts.demoToolHandlers = handlers
		b = b.Tools(defs...).ToolChoice(toolChoice)
	} else if toolChoice != nil {
		b = b.ToolChoice(toolChoice)
	}

	req, err := b.Build()
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	verbose := opts.Verbose

	// --- Token estimate (verbose only) ---
	var tokenEstimate *tokencount.TokenCount
	if verbose {
		if tc, ok := provider.(tokencount.TokenCounter); ok {
			est, err := tc.CountTokens(ctx, tokencount.TokenCountRequest{
				Model:    req.Model,
				Messages: req.Messages,
				Tools:    req.Tools,
			})
			if err == nil {
				tokenEstimate = est
				printTokenEstimate(est)
			}
		}
	}

	stream, err := provider.CreateStream(ctx, req)
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	var inReasoning bool
	var hadTokenOutput bool
	var verboseOutputPrinted bool

	printVerboseSeparator := func() {
		if verboseOutputPrinted {
			fmt.Fprintln(os.Stderr)
			verboseOutputPrinted = false
		}
	}

	proc := llm.NewEventProcessor(ctx, stream).
		OnEvent(llm.EventHandlerFunc(func(ev llm.Event) {
			if root.LogEvents {
				d, _ := json.MarshalIndent(ev, "  ", "  ")
				fmt.Printf("\n[EVT :: %s]\n%s\n", ev.Type(), string(d))
			}
		})).
		OnEvent(llm.TypedEventHandler[*llm.ProviderFailoverEvent](func(ev *llm.ProviderFailoverEvent) {
			if verbose {
				printProviderFailoverEvent(ev)
				verboseOutputPrinted = true
			}
		})).
		OnEvent(llm.TypedEventHandler[*llm.ModelResolvedEvent](func(ev *llm.ModelResolvedEvent) {
			if verbose {
				printModelResolvedEvent(ev)
				verboseOutputPrinted = true
			}
		})).
		OnEvent(llm.TypedEventHandler[*llm.StreamStartedEvent](func(ev *llm.StreamStartedEvent) {
			if verbose {
				printStreamStartedEvent(ev)
				verboseOutputPrinted = true
			}
		})).
		OnEvent(llm.TypedEventHandler[*llm.RequestEvent](func(ev *llm.RequestEvent) {
			if verbose {
				printRequestParamsEvent(ev)
				verboseOutputPrinted = true
			}
		}))

	if len(opts.demoToolHandlers) > 0 {
		proc = proc.HandleTool(opts.demoToolHandlers...)
	}

	proc = proc.
		OnTextDelta(func(chunk string) {
			printVerboseSeparator()
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
			printVerboseSeparator()
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
func printTokenEstimate(est *tokencount.TokenCount) {
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
func printVerboseInfo(result llm.Result, est *tokencount.TokenCount) {
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

	// Thought summary (character count — full text already streamed live)
	if result.Thought() != "" {
		fields = append(fields, field{"reasoning", fmt.Sprintf("%d chars", len(result.Thought()))})
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
	for i, m := range msgs {
		if m.Role == llm.RoleTool {
			label := fmt.Sprintf("result[%d]", i)
			value := m.ToolResults()[0].ToolOutput
			if m.ToolResults()[0].IsError {
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

type kvField struct {
	label string
	value string
}

// printStreamStartedEvent prints the stream-started metadata (request ID, model)
// when running in verbose mode.
func printStreamStartedEvent(ev *llm.StreamStartedEvent) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s── stream started ──%s\n", ansiDim, ansiReset)
	var fields []kvField
	if ev.Model != "" {
		fields = append(fields, kvField{"model", ev.Model})
	}
	if ev.RequestID != "" {
		fields = append(fields, kvField{"request_id", ev.RequestID})
	}
	for k, v := range ev.Extra {
		fields = append(fields, kvField{k, fmt.Sprint(v)})
	}
	printFields(fields)
}

// printModelResolvedEvent prints model name translation when running in verbose mode.
func printModelResolvedEvent(ev *llm.ModelResolvedEvent) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s── model resolved ──%s\n", ansiDim, ansiReset)
	var fields []kvField
	if ev.Resolver != "" {
		fields = append(fields, kvField{"resolver", ev.Resolver})
	}
	if ev.Name != "" {
		fields = append(fields, kvField{"name", ev.Name})
	}
	if ev.Resolved != "" {
		fields = append(fields, kvField{"resolved", ev.Resolved})
	}
	printFields(fields)
}

// printProviderFailoverEvent prints a provider failover when running in verbose mode.
func printProviderFailoverEvent(ev *llm.ProviderFailoverEvent) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s── provider failover ──%s\n", ansiDim, ansiReset)
	var fields []kvField
	if ev.Provider != "" {
		fields = append(fields, kvField{"from", ev.Provider})
	}
	if ev.FailoverProvider != "" {
		fields = append(fields, kvField{"to", ev.FailoverProvider})
	}
	if ev.Error != nil {
		fields = append(fields, kvField{"error", ev.Error.Error()})
	}
	printFields(fields)
}

// printRequestParamsEvent prints both the llm.Request-level params and the
// provider-resolved params from a single RequestEvent.
func printRequestParamsEvent(ev *llm.RequestEvent) {
	// --- llm.Request params (what the caller asked for) ---
	if req := ev.OriginalRequest; req.Model != "" {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "%s── request params ──%s\n", ansiDim, ansiReset)
		printParamMap(mapFromStruct(req, "messages", "tools"))
	}

	// --- Provider-resolved params (what was actually sent) ---
	if pr := ev.ProviderRequest; pr.Body != nil {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "%s── provider params ──%s\n", ansiDim, ansiReset)
		var bodyMap map[string]any
		if err := json.Unmarshal(pr.Body, &bodyMap); err == nil {
			delete(bodyMap, "messages")
			delete(bodyMap, "tools")
			printParamMap(bodyMap)
		}
	}
}

// mapFromStruct marshals v to JSON, unmarshals into map[string]any, and
// deletes the listed keys. Returns nil on error.
func mapFromStruct(v any, exclude ...string) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	for _, k := range exclude {
		delete(m, k)
	}
	return m
}

// printParamMap prints a map as sorted key: value lines. Nested objects
// are rendered as compact JSON.
func printParamMap(m map[string]any) {
	if len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var fields []kvField
	for _, k := range keys {
		v := m[k]
		switch val := v.(type) {
		case map[string]any:
			b, _ := json.Marshal(val)
			fields = append(fields, kvField{k, string(b)})
		case []any:
			b, _ := json.Marshal(val)
			fields = append(fields, kvField{k, string(b)})
		default:
			fields = append(fields, kvField{k, fmt.Sprint(v)})
		}
	}
	printFields(fields)
}

func printFields(fields []kvField) {
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

func buildDemoTools() ([]tool.Definition, []tool.NamedHandler) {
	defs := tool.NewToolSet(
		tool.NewSpec[addFactParams]("add_fact", "Store a single fact"),
		tool.NewSpec[completeTurnParams]("complete_turn", "Complete the current turn"),
	).Definitions()

	handlers := []tool.NamedHandler{
		tool.NewHandler("complete_turn", func(_ context.Context, in completeTurnParams) (*defaultToolResult, error) {
			return &defaultToolResult{Message: "Turn complete", Success: in.Success}, nil
		}),
		tool.NewHandler("add_fact", func(_ context.Context, in addFactParams) (*defaultToolResult, error) {
			return &defaultToolResult{Message: fmt.Sprintf("Fact added: %s", in.Fact), Success: true}, nil
		}),
	}
	return defs, handlers
}
