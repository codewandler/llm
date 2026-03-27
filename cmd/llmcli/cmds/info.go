package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm/provider/anthropic/claude"
)

const (
	profileEndpoint = "https://api.anthropic.com/api/oauth/profile"
	rolesEndpoint   = "https://api.anthropic.com/api/oauth/claude_cli/roles"
)

// OAuthProfileResponse represents the response from /api/oauth/profile.
type OAuthProfileResponse struct {
	Account      AccountInfo      `json:"account"`
	Organization OrganizationInfo `json:"organization"`
	Application  ApplicationInfo  `json:"application"`
}

type AccountInfo struct {
	UUID         string `json:"uuid"`
	FullName     string `json:"full_name"`
	DisplayName  string `json:"display_name"`
	Email        string `json:"email"`
	HasClaudeMax bool   `json:"has_claude_max"`
	HasClaudePro bool   `json:"has_claude_pro"`
	CreatedAt    string `json:"created_at"`
}

type OrganizationInfo struct {
	UUID                 string `json:"uuid"`
	Name                 string `json:"name"`
	OrganizationType     string `json:"organization_type"`
	BillingType          string `json:"billing_type"`
	RateLimitTier        string `json:"rate_limit_tier"`
	HasExtraUsageEnabled bool   `json:"has_extra_usage_enabled"`
	SubscriptionStatus   string `json:"subscription_status"`
}

type ApplicationInfo struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// OAuthRolesResponse represents the response from /api/oauth/claude_cli/roles.
type OAuthRolesResponse struct {
	OrganizationUUID string `json:"organization_uuid"`
	OrganizationName string `json:"organization_name"`
	OrganizationRole string `json:"organization_role"`
	WorkspaceUUID    string `json:"workspace_uuid,omitempty"`
	WorkspaceName    string `json:"workspace_name,omitempty"`
	WorkspaceRole    string `json:"workspace_role,omitempty"`
}

// NewClaudeCmd returns the claude command group.
func NewClaudeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude",
		Short: "Claude-specific commands",
	}

	cmd.AddCommand(NewAuthInfoCmd())
	cmd.AddCommand(NewSettingsCmd())

	return cmd
}

// NewAuthInfoCmd returns the auth-info command for Claude OAuth profile.
func NewAuthInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth-info",
		Short: "Show Claude account and organization information from OAuth",
		Long: `Fetches and displays your Claude account profile and organization details.

Uses the local Claude credentials from ~/.claude/.credentials.json by default.

Example:
  llmcli claude auth-info`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthInfo(cmd.Context())
		},
	}

	return cmd
}

func runAuthInfo(ctx context.Context) error {
	// Get local credentials
	store, err := claude.NewLocalTokenStore()
	if err != nil {
		return fmt.Errorf("no local Claude credentials found: %w", err)
	}

	token, err := store.Load(ctx, "default")
	if err != nil {
		return fmt.Errorf("load token: %w", err)
	}
	if token == nil {
		return fmt.Errorf("no local Claude credentials found")
	}

	if token.IsExpired() {
		fmt.Println("Warning: Token is expired, some information may be unavailable")
	}

	// Fetch profile
	profile, err := fetchProfile(ctx, token.AccessToken)
	if err != nil {
		return fmt.Errorf("fetch profile: %w", err)
	}

	// Fetch roles
	roles, err := fetchRoles(ctx, token.AccessToken)
	if err != nil {
		// Non-fatal: roles endpoint may not be available
		fmt.Printf("Note: Could not fetch roles: %v\n", err)
	}

	// Print the information
	printProfile(profile)
	if roles != nil {
		printRoles(roles)
	}

	return nil
}

func fetchProfile(ctx context.Context, accessToken string) (*OAuthProfileResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", profileEndpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var profile OAuthProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return &profile, nil
}

func fetchRoles(ctx context.Context, accessToken string) (*OAuthRolesResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rolesEndpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var roles OAuthRolesResponse
	if err := json.NewDecoder(resp.Body).Decode(&roles); err != nil {
		return nil, err
	}

	return &roles, nil
}

func printProfile(p *OAuthProfileResponse) {
	fmt.Println()
	fmt.Println("=== Account ===")
	fmt.Printf("  Name:         %s\n", p.Account.FullName)
	fmt.Printf("  Email:        %s\n", p.Account.Email)
	fmt.Printf("  Account UUID: %s\n", p.Account.UUID)
	fmt.Printf("  Created:      %s\n", p.Account.CreatedAt)

	if p.Account.HasClaudeMax || p.Account.HasClaudePro {
		fmt.Print("  Plans:        ")
		var plans []string
		if p.Account.HasClaudePro {
			plans = append(plans, "Pro")
		}
		if p.Account.HasClaudeMax {
			plans = append(plans, "Max")
		}
		fmt.Println(plans)
	}

	fmt.Println()
	fmt.Println("=== Organization ===")
	fmt.Printf("  Name:              %s\n", p.Organization.Name)
	fmt.Printf("  UUID:              %s\n", p.Organization.UUID)
	fmt.Printf("  Type:              %s\n", p.Organization.OrganizationType)
	fmt.Printf("  Billing:           %s\n", p.Organization.BillingType)
	fmt.Printf("  Rate Limit Tier:   %s\n", p.Organization.RateLimitTier)
	fmt.Printf("  Extra Usage:       ")
	if p.Organization.HasExtraUsageEnabled {
		fmt.Println("enabled")
	} else {
		fmt.Println("disabled")
	}
	fmt.Printf("  Subscription:      %s\n", p.Organization.SubscriptionStatus)

	fmt.Println()
	fmt.Println("=== Application ===")
	fmt.Printf("  App:    %s\n", p.Application.Name)
	fmt.Printf("  UUID:   %s\n", p.Application.UUID)
}

func printRoles(r *OAuthRolesResponse) {
	fmt.Println()
	fmt.Println("=== Roles ===")
	fmt.Printf("  Organization: %s (%s)\n", r.OrganizationRole, r.OrganizationName)
	if r.WorkspaceRole != "" && r.WorkspaceName != "" {
		fmt.Printf("  Workspace:    %s (%s)\n", r.WorkspaceRole, r.WorkspaceName)
	}
}

// ClaudeSettings represents the non-project settings from ~/.claude.json.
type ClaudeSettings struct {
	OAuthAccount                  *OAuthAccountInfo `json:"oauthAccount,omitempty"`
	UserID                        string            `json:"userID,omitempty"`
	AnonymousID                   string            `json:"anonymousId,omitempty"`
	HasCompletedOnboarding        bool              `json:"hasCompletedOnboarding"`
	FirstStartTime                string            `json:"firstStartTime,omitempty"`
	InstallMethod                 string            `json:"installMethod,omitempty"`
	AutoUpdates                   bool              `json:"autoUpdates,omitempty"`
	AutoUpdatesProtectedForNative bool              `json:"autoUpdatesProtectedForNative,omitempty"`
	ClaudeCodeFirstTokenDate      string            `json:"claudeCodeFirstTokenDate,omitempty"`
	NumStartups                   int               `json:"numStartups"`
	LastReleaseNotesSeen          string            `json:"lastReleaseNotesSeen,omitempty"`
	MCPServers                    json.RawMessage   `json:"mcpServers,omitempty"`
	MetricsStatusCache            json.RawMessage   `json:"metricsStatusCache,omitempty"`
	AdditionalModelOptionsCache   json.RawMessage   `json:"additionalModelOptionsCache,omitempty"`
	ChangelogLastFetched          int64             `json:"changelogLastFetched,omitempty"`
	CachedGrowthBookFeatures      json.RawMessage   `json:"cachedGrowthBookFeatures,omitempty"`
	PassesLastSeenRemaining       int               `json:"passesLastSeenRemaining"`
	HasAvailableSubscription      bool              `json:"hasAvailableSubscription"`
	EffortCalloutDismissed        bool              `json:"effortCalloutDismissed"`
	EffortCalloutV2Dismissed      bool              `json:"effortCalloutV2Dismissed"`
	LastPlanModeUse               int64             `json:"lastPlanModeUse,omitempty"`
}

// OAuthAccountInfo represents the oauthAccount field from settings.
type OAuthAccountInfo struct {
	AccountUUID           string `json:"accountUuid"`
	DisplayName           string `json:"displayName"`
	EmailAddress          string `json:"emailAddress"`
	OrganizationUUID      string `json:"organizationUuid"`
	OrganizationName      string `json:"organizationName"`
	OrganizationRole      string `json:"organizationRole"`
	WorkspaceRole         any    `json:"workspaceRole,omitempty"`
	HasExtraUsageEnabled  bool   `json:"hasExtraUsageEnabled"`
	BillingType           string `json:"billingType"`
	AccountCreatedAt      string `json:"accountCreatedAt"`
	SubscriptionCreatedAt string `json:"subscriptionCreatedAt"`
}

// NewSettingsCmd returns the settings command for Claude local settings.
func NewSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Show Claude Code local settings",
		Long: `Reads and displays settings from ~/.claude.json.

Shows user preferences, MCP servers, and other non-project settings.

Example:
  llmcli claude settings`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettings(cmd.Context())
		},
	}

	return cmd
}

func runSettings(ctx context.Context) error {
	settings, err := loadClaudeSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	printSettings(settings)
	return nil
}

func loadClaudeSettings() (*ClaudeSettings, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	path := homeDir + "/.claude.json"
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var settings ClaudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return &settings, nil
}

func printSettings(s *ClaudeSettings) {
	fmt.Println()

	// Account info
	if s.OAuthAccount != nil {
		fmt.Println("=== Account ===")
		fmt.Printf("  Display Name:      %s\n", s.OAuthAccount.DisplayName)
		fmt.Printf("  Email:             %s\n", s.OAuthAccount.EmailAddress)
		fmt.Printf("  Account UUID:      %s\n", s.OAuthAccount.AccountUUID)
		fmt.Printf("  Created:           %s\n", s.OAuthAccount.AccountCreatedAt)
		fmt.Printf("  Extra Usage:       %t\n", s.OAuthAccount.HasExtraUsageEnabled)
		fmt.Println()
		fmt.Println("=== Organization ===")
		fmt.Printf("  Name:              %s\n", s.OAuthAccount.OrganizationName)
		fmt.Printf("  UUID:              %s\n", s.OAuthAccount.OrganizationUUID)
		fmt.Printf("  Role:              %s\n", s.OAuthAccount.OrganizationRole)
		fmt.Printf("  Billing:           %s\n", s.OAuthAccount.BillingType)
		fmt.Printf("  Subscription:      %s\n", s.OAuthAccount.SubscriptionCreatedAt)
		fmt.Println()
	}

	// User IDs
	fmt.Println("=== User IDs ===")
	if s.UserID != "" {
		fmt.Printf("  User ID:    %s\n", s.UserID)
	}
	if s.AnonymousID != "" {
		fmt.Printf("  Anonymous:  %s\n", s.AnonymousID)
	}
	fmt.Println()

	// Onboarding & Usage
	fmt.Println("=== Usage & Onboarding ===")
	fmt.Printf("  First Start:      %s\n", s.FirstStartTime)
	fmt.Printf("  Onboarding Done:  %t\n", s.HasCompletedOnboarding)
	fmt.Printf("  Startup Count:    %d\n", s.NumStartups)
	fmt.Printf("  Install Method:   %s\n", s.InstallMethod)
	fmt.Printf("  Last Release:     %s\n", s.LastReleaseNotesSeen)
	fmt.Printf("  Last Plan Mode:   %s\n", formatTimestamp(s.LastPlanModeUse))
	fmt.Println()

	// MCP Servers
	if len(s.MCPServers) > 0 && !isJSONNullOrEmpty(s.MCPServers) {
		fmt.Println("=== MCP Servers ===")
		var mcpList []map[string]interface{}
		if err := json.Unmarshal(s.MCPServers, &mcpList); err == nil && mcpList != nil && len(mcpList) > 0 {
			for _, srv := range mcpList {
				name, _ := srv["name"].(string)
				kind, _ := srv["kind"].(string)
				fmt.Printf("  - %s (%s)\n", name, kind)
			}
		} else {
			fmt.Println("  (empty)")
		}
		fmt.Println()
	}

	// Auto Updates
	fmt.Println("=== Updates ===")
	fmt.Printf("  Auto Updates:        %t\n", s.AutoUpdates)
	fmt.Printf("  Native Protected:    %t\n", s.AutoUpdatesProtectedForNative)
	fmt.Printf("  Claude Code First:   %s\n", s.ClaudeCodeFirstTokenDate)
	fmt.Println()

	// Subscriptions
	fmt.Println("=== Subscriptions ===")
	fmt.Printf("  Available:      %t\n", s.HasAvailableSubscription)
	fmt.Printf("  Passes Left:    %d\n", s.PassesLastSeenRemaining)
	fmt.Println()
}

func isJSONNullOrEmpty(data json.RawMessage) bool {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return true
	}
	if v == nil {
		return true
	}
	if arr, ok := v.([]interface{}); ok && len(arr) == 0 {
		return true
	}
	return false
}

func formatTimestamp(ts int64) string {
	if ts == 0 {
		return "never"
	}
	t := time.UnixMilli(ts)
	return t.Format("2006-01-02 15:04:05")
}
