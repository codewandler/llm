// Package cmds provides CLI commands for llmcli.
package cmds

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/spf13/cobra"
)

const (
	defaultKey = "claude"
	localKey   = "@local"
)

// NewAuthCmd returns the auth command group.
func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Claude OAuth authentication",
	}

	cmd.AddCommand(
		newLoginCmd(),
		newStatusCmd(),
		newLogoutCmd(),
		newListCmd(),
		newRefreshCmd(),
	)

	return cmd
}

func newLoginCmd() *cobra.Command {
	var key string

	cmd := &cobra.Command{
		Use:   "login [provider]",
		Short: "Authenticate with Claude via OAuth",
		Long: `Starts the OAuth flow to authenticate with Claude.
Opens a browser for you to log in, displays an authorization code,
which you then paste back into the terminal.

Example:
  llmcli auth login claude              # Store as default key "claude"
  llmcli auth login claude --key=work   # Store as "work"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := "claude"
			if len(args) > 0 {
				provider = args[0]
			}
			if provider != "claude" {
				return fmt.Errorf("only 'claude' provider is supported for OAuth login")
			}
			return runLogin(cmd.Context(), key)
		},
	}

	cmd.Flags().StringVar(&key, "key", defaultKey, "Key to store credentials under")
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [key]",
		Short: "Show authentication status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := defaultKey
			if len(args) > 0 {
				key = args[0]
			}
			return runStatus(cmd.Context(), key)
		},
	}
	return cmd
}

func newLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout <key>",
		Short: "Remove stored credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(cmd.Context(), args[0])
		},
	}
	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context())
		},
	}
	return cmd
}

func newRefreshCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "refresh [key]",
		Short: "Force refresh a stored token",
		Long: `Forces a token refresh regardless of expiration.
Useful for testing that token refresh and persistence work correctly.

Use "@local" to refresh the local Claude credentials (~/.claude/.credentials.json).

Example:
  llmcli auth refresh           # Refresh default key
  llmcli auth refresh @local    # Refresh local Claude credentials
  llmcli auth refresh work      # Refresh specific key`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := defaultKey
			if len(args) > 0 {
				key = args[0]
			}
			return runRefresh(cmd.Context(), key, verbose)
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed refresh information")
	return cmd
}

// getStoreAndKey returns the appropriate token store and internal key.
// For "@local", returns LocalTokenStore with key "default".
// For other keys, returns FileTokenStore with the given key.
func getStoreAndKey(key string) (claude.TokenStore, string, error) {
	if key == localKey {
		store, err := claude.NewLocalTokenStore()
		if err != nil {
			return nil, "", fmt.Errorf("local Claude credentials not found: %w", err)
		}
		return store, "default", nil
	}

	store, err := getTokenStore()
	if err != nil {
		return nil, "", err
	}
	return store, key, nil
}

// --- Command implementations ---

func runRefresh(ctx context.Context, key string, verbose bool) error {
	key, err := normalizeCredentialKey(key)
	if err != nil {
		return err
	}

	store, internalKey, err := getStoreAndKey(key)
	if err != nil {
		return err
	}

	// Load current token
	oldToken, err := store.Load(ctx, internalKey)
	if err != nil {
		return fmt.Errorf("load token: %w", err)
	}
	if oldToken == nil {
		return fmt.Errorf("no token found for key %q", key)
	}

	fmt.Printf("Refreshing token for %s...\n", key)
	if verbose {
		fmt.Printf("Before: expires %s (token: %s...)\n",
			oldToken.ExpiresAt.Format(time.RFC3339),
			truncateToken(oldToken.AccessToken))
	} else {
		fmt.Printf("Before: expires %s\n", oldToken.ExpiresAt.Format(time.RFC3339))
	}

	// Refresh the token
	if verbose {
		fmt.Printf("Calling POST %s\n", "https://console.anthropic.com/v1/oauth/token")
	}

	result, err := claude.RefreshTokenVerbose(ctx, oldToken.RefreshToken)
	if err != nil {
		return fmt.Errorf("refresh failed: %w", err)
	}

	if verbose {
		fmt.Printf("Response: 200 OK (took %s)\n", result.Duration.Round(time.Millisecond))
	}

	// Save the new token
	if err := store.Save(ctx, internalKey, result.Token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	if verbose {
		fmt.Printf("After:  expires %s (token: %s...)\n",
			result.Token.ExpiresAt.Format(time.RFC3339),
			truncateToken(result.Token.AccessToken))
	} else {
		fmt.Printf("After:  expires %s\n", result.Token.ExpiresAt.Format(time.RFC3339))
	}

	// Verify by reloading from disk
	fmt.Print("Verifying persistence... ")
	reloaded, err := store.Load(ctx, internalKey)
	if err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("reload after save: %w", err)
	}
	if reloaded.AccessToken != result.Token.AccessToken {
		fmt.Println("FAILED")
		return fmt.Errorf("token mismatch after reload")
	}
	fmt.Println("OK")

	return nil
}

// truncateToken returns the first 12 characters of a token for display.
func truncateToken(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:12]
}

func runLogin(ctx context.Context, key string) error {
	key, err := normalizeCredentialKey(key)
	if err != nil {
		return err
	}
	if key == localKey {
		return fmt.Errorf("cannot login to @local; use Claude Code CLI to manage local credentials")
	}

	tokenStore, err := getTokenStore()
	if err != nil {
		return err
	}

	// Create OAuth flow with Anthropic's hosted callback page
	// (localhost callbacks are not allowed by Anthropic's OAuth)
	flow, err := claude.NewOAuthFlow("")
	if err != nil {
		return fmt.Errorf("create OAuth flow: %w", err)
	}

	// Open browser with authorization URL
	authURL := flow.AuthorizeURL()
	fmt.Println("Opening browser for authentication...")
	fmt.Println()
	fmt.Println("If the browser doesn't open, visit this URL:")
	fmt.Println(authURL)
	fmt.Println()

	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open browser: %v\n", err)
	}

	// Prompt user to paste the authorization code
	fmt.Println("After authorizing, you'll see an authorization code.")
	fmt.Print("Paste the code here: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read authorization code: %w", err)
		}
		return fmt.Errorf("no authorization code provided")
	}

	code := strings.TrimSpace(scanner.Text())
	if code == "" {
		return fmt.Errorf("no authorization code provided")
	}

	// Exchange code for tokens
	fmt.Println("Exchanging code for tokens...")
	token, err := flow.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("token exchange: %w", err)
	}

	// Save token
	if err := tokenStore.Save(ctx, key, token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	fmt.Printf("\nAuthentication successful! Credentials stored as %q\n", key)
	fmt.Printf("Token expires: %s\n", token.ExpiresAt.Format(time.RFC3339))
	return nil
}

func runStatus(ctx context.Context, key string) error {
	key, err := normalizeCredentialKey(key)
	if err != nil {
		return err
	}

	store, internalKey, err := getStoreAndKey(key)
	if err != nil {
		return err
	}

	token, err := store.Load(ctx, internalKey)
	if err != nil {
		return fmt.Errorf("load token: %w", err)
	}
	if token == nil {
		fmt.Printf("No credentials found for key %q\n", key)
		if key == localKey {
			fmt.Println("Use Claude Code CLI to authenticate")
		} else {
			fmt.Printf("Run 'llmcli auth login claude --key=%s' to authenticate\n", key)
		}
		return nil
	}

	fmt.Printf("Key:         %s\n", key)
	if key == localKey {
		dir, _ := claude.DefaultClaudeDir()
		fmt.Printf("Source:      %s/.credentials.json\n", dir)
	}
	fmt.Printf("Expires:     %s\n", token.ExpiresAt.Format(time.RFC3339))

	if token.IsExpired() {
		fmt.Printf("Status:      EXPIRED\n")
	} else {
		remaining := time.Until(token.ExpiresAt)
		fmt.Printf("Status:      Valid (expires in %s)\n", remaining.Round(time.Second))
	}

	return nil
}

func runLogout(ctx context.Context, key string) error {
	key, err := normalizeCredentialKey(key)
	if err != nil {
		return err
	}
	if key == localKey {
		return fmt.Errorf("cannot logout @local; use Claude Code CLI to manage local credentials")
	}

	tokenStore, err := getTokenStore()
	if err != nil {
		return err
	}

	// Check if exists first
	token, err := tokenStore.Load(ctx, key)
	if err != nil {
		return fmt.Errorf("load token: %w", err)
	}
	if token == nil {
		return fmt.Errorf("no credentials found for key %q", key)
	}

	if err := tokenStore.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}

	fmt.Printf("Credentials %q removed\n", key)
	return nil
}

func runList(ctx context.Context) error {
	tokenStore, err := getTokenStore()
	if err != nil {
		return err
	}

	keys, err := tokenStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list credentials: %w", err)
	}

	// Check for @local credentials
	var hasLocal bool
	var localStatus string
	if claude.LocalTokenProviderAvailable() {
		localStore, err := claude.NewLocalTokenStore()
		if err == nil {
			if token, _ := localStore.Load(ctx, "default"); token != nil {
				hasLocal = true
				if token.IsExpired() {
					localStatus = "expired"
				} else {
					localStatus = "valid"
				}
			}
		}
	}

	if len(keys) == 0 && !hasLocal {
		fmt.Println("No stored credentials")
		fmt.Println("Run 'llmcli auth login claude' to authenticate")
		return nil
	}

	fmt.Println("Stored credentials:")

	// Show @local first if available
	if hasLocal {
		dir, _ := claude.DefaultClaudeDir()
		fmt.Printf("  @local (%s) - %s/.credentials.json\n", localStatus, dir)
	}

	// Show other credentials
	for _, key := range keys {
		token, _ := tokenStore.Load(ctx, key)
		status := "valid"
		if token != nil && token.IsExpired() {
			status = "expired"
		}
		fmt.Printf("  %s (%s)\n", key, status)
	}
	return nil
}

// --- Helpers ---

func normalizeCredentialKey(key string) (string, error) {
	normalized := strings.TrimSpace(key)
	if normalized == "" {
		return "", fmt.Errorf("key cannot be empty")
	}
	return normalized, nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
