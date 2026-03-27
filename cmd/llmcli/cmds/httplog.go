package cmds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// httpLogHandler is a slog.Handler that pretty-prints HTTP request/response
// events emitted by llm.loggingTransport. All other log records are discarded.
type httpLogHandler struct {
	w          io.Writer
	allHeaders bool

	// SSE line buffer — chunks may arrive mid-line, so we accumulate until \n
	lineBuf strings.Builder
	// current SSE frame being assembled (event + data)
	sseEvent string
	sseData  string
	// pendingNewline is set when token output was printed to stdout and we need
	// to emit a blank line on stderr before the next SSE frame.
	pendingNewline bool
}

// newHTTPLogHandler returns a slog.Handler that pretty-prints HTTP traffic.
// When allHeaders is true, all response headers are printed instead of the
// curated allowlist.
func newHTTPLogHandler(allHeaders bool) *httpLogHandler {
	return &httpLogHandler{w: os.Stderr, allHeaders: allHeaders}
}

// MarkTokenOutput signals that the LLM layer has emitted token output to
// stdout. The next SSE frame will be preceded by a blank line on stderr so
// the log output starts on a clean line.
func (h *httpLogHandler) MarkTokenOutput() {
	h.pendingNewline = true
}

// responseHeaderAllowlist defines which response headers are worth showing.
// Cloudflare, cookie, CSP, cache-control, and other infrastructure headers
// are excluded — they add noise without debugging value.
var responseHeaderAllowlist = map[string]bool{
	// Identity / tracing
	"request-id":   true,
	"x-request-id": true,

	// Content
	"content-type": true,

	// Anthropic rate limits — directly actionable
	"anthropic-organization-id":                           true,
	"anthropic-ratelimit-unified-status":                  true,
	"anthropic-ratelimit-unified-overage-status":          true,
	"anthropic-ratelimit-unified-overage-disabled-reason": true,
	"anthropic-ratelimit-unified-fallback-percentage":     true,
	"anthropic-ratelimit-unified-representative-claim":    true,
	"anthropic-ratelimit-unified-5h-status":               true,
	"anthropic-ratelimit-unified-5h-utilization":          true,
	"anthropic-ratelimit-unified-5h-reset":                true,
	"anthropic-ratelimit-unified-7d-status":               true,
	"anthropic-ratelimit-unified-7d-utilization":          true,
	"anthropic-ratelimit-unified-7d-reset":                true,
	"anthropic-ratelimit-unified-reset":                   true,

	// OpenAI rate limits
	"x-ratelimit-limit-requests":     true,
	"x-ratelimit-limit-tokens":       true,
	"x-ratelimit-remaining-requests": true,
	"x-ratelimit-remaining-tokens":   true,
	"x-ratelimit-reset-requests":     true,
	"x-ratelimit-reset-tokens":       true,
}

const (
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiReset = "\033[0m"
)

// noisySSEEvents are printed compactly on one line rather than expanded.
var noisySSEEvents = map[string]bool{
	"ping":                true,
	"content_block_start": true,
	"content_block_stop":  true,
}

// requestHeaderDenylist contains request header names (lowercase) that are
// suppressed even in debug mode — too noisy, too long, or sensitive.
var requestHeaderDenylist = map[string]bool{
	"x-amz-security-token":   true, // temporary STS credential — very long, sensitive
	"amz-sdk-invocation-id":  true, // internal SDK retry tracking
	"amz-sdk-request":        true, // internal SDK attempt counter
	"x-amz-sso_bearer_token": true, // SSO bearer token — sensitive
	"user-agent":             true, // AWS SDK UA string — long and uninteresting
}

func (h *httpLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *httpLogHandler) WithAttrs(_ []slog.Attr) slog.Handler         { return h }
func (h *httpLogHandler) WithGroup(_ string) slog.Handler              { return h }

func (h *httpLogHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]string)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})

	switch r.Message {
	case "http request":
		h.printRequest(attrs)
	case "http response":
		h.printResponse(attrs)
	case "http response body":
		h.feedChunk(attrs["chunk"])
	case "http error":
		h.printError(attrs)
	}
	return nil
}

func (h *httpLogHandler) printRequest(attrs map[string]string) {
	_, _ = fmt.Fprintf(h.w, "\n──► %s %s\n", attrs["method"], attrs["url"])
	h.printHeaders(attrs, "req.header.", nil)
	if body, ok := attrs["req.body"]; ok {
		_, _ = fmt.Fprintln(h.w, indentAll(prettyJSON(body), "    "))
	}
	_, _ = fmt.Fprintln(h.w)
}

func (h *httpLogHandler) printResponse(attrs map[string]string) {
	// Flush any buffered SSE from a previous response (shouldn't happen, but safe)
	h.flushSSEFrame()
	h.lineBuf.Reset()

	_, _ = fmt.Fprintf(h.w, "◄── %s  (%s)\n", attrs["status"], attrs["duration"])
	var allowlist map[string]bool
	if !h.allHeaders {
		allowlist = responseHeaderAllowlist
	}
	h.printHeaders(attrs, "resp.header.", allowlist)
	_, _ = fmt.Fprintln(h.w)
}

// feedChunk accumulates raw bytes from the stream and processes complete lines.
func (h *httpLogHandler) feedChunk(chunk string) {
	h.lineBuf.WriteString(chunk)
	buf := h.lineBuf.String()
	h.lineBuf.Reset()

	for {
		idx := strings.IndexByte(buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(buf[:idx], "\r")
		buf = buf[idx+1:]
		h.handleSSELine(line)
	}

	// Put back anything that didn't end with \n yet
	if buf != "" {
		h.lineBuf.WriteString(buf)
	}
}

// handleSSELine processes one complete SSE line.
func (h *httpLogHandler) handleSSELine(line string) {
	if line == "" {
		// Blank line = end of SSE frame — render and reset
		h.flushSSEFrame()
		return
	}

	switch {
	case strings.HasPrefix(line, "event: "):
		h.sseEvent = strings.TrimPrefix(line, "event: ")

	case strings.HasPrefix(line, "data: "):
		h.sseData = strings.TrimPrefix(line, "data: ")

	default:
		// Non-SSE line (e.g. HTTP/1.1 trailers) — print as-is
		_, _ = fmt.Fprintf(h.w, "    %s\n", line)
	}
}

// flushSSEFrame renders the current event+data pair and resets state.
func (h *httpLogHandler) flushSSEFrame() {
	if h.sseData == "" && h.sseEvent == "" {
		return
	}

	eventType := h.sseEvent
	data := h.sseData
	h.sseEvent = ""
	h.sseData = ""

	// Determine the SSE event type from the data JSON if not set by event: line
	if eventType == "" {
		var probe struct {
			Type   string `json:"type"`
			Object string `json:"object"`
		}
		_ = json.Unmarshal([]byte(data), &probe)
		eventType = probe.Type
		if eventType == "" {
			eventType = probe.Object
		}
	}

	// If token output was printed to stdout, emit a blank separator line first
	// so the SSE frame starts on a clean line.
	if h.pendingNewline {
		_, _ = fmt.Fprintln(h.w)
		h.pendingNewline = false
	}

	if noisySSEEvents[eventType] {
		_, _ = fmt.Fprintf(h.w, "\n    %s[%s]%s\n", ansiBold, eventType, ansiReset)
		return
	}

	_, _ = fmt.Fprintf(h.w, "\n    %s[%s]%s\n", ansiBold, eventType, ansiReset)

	if data == "[DONE]" {
		_, _ = fmt.Fprintf(h.w, "    [DONE]\n")
		return
	}

	pretty := prettyJSON(data)
	_, _ = fmt.Fprintln(h.w, indentAll(pretty, "    "))
}

func (h *httpLogHandler) printError(attrs map[string]string) {
	_, _ = fmt.Fprintf(h.w, "%s✗   %s %s  (%s)  error: %s%s\n",
		ansiBold, attrs["method"], attrs["url"], attrs["duration"], attrs["error"], ansiReset)
}

// printHeaders prints attrs whose key starts with prefix.
// When allowlist is non-nil, only keys present with value true are printed (response mode).
// When allowlist is nil, all headers are printed except those in requestHeaderDenylist.
func (h *httpLogHandler) printHeaders(attrs map[string]string, prefix string, allowlist map[string]bool) {
	for k, v := range attrs {
		name, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		lower := strings.ToLower(name)
		if allowlist != nil {
			// Response mode: show only allowlisted headers
			if show, found := allowlist[lower]; !found || !show {
				continue
			}
		} else {
			// Request mode: suppress denylist headers
			if requestHeaderDenylist[lower] {
				continue
			}
		}
		if strings.EqualFold(name, "Authorization") {
			v = redactAuth(v)
		}
		_, _ = fmt.Fprintf(h.w, "    %s: %s\n", name, v)
	}
}

// prettyJSON returns a pretty-printed version of s if it is valid JSON,
// otherwise returns s unchanged (e.g. "[DONE]" or plain text).
func prettyJSON(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 || (s[0] != '{' && s[0] != '[') {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// indentAll prefixes every line of s with the given prefix.
func indentAll(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// redactAuth shortens an Authorization header value for safe logging.
func redactAuth(v string) string {
	parts := strings.SplitN(v, " ", 2)
	if len(parts) != 2 {
		return "***"
	}
	token := parts[1]
	keep := 8
	if len(token) <= keep {
		return v
	}
	return parts[0] + " " + token[:keep] + "…"
}
