package unified

import (
	"testing"

	"github.com/codewandler/llm/msg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     Request
		wantErr string
	}{
		{
			name:    "missing model",
			req:     Request{Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}}},
			wantErr: "model is required",
		},
		{
			name:    "missing messages",
			req:     Request{Model: "gpt-4o"},
			wantErr: "messages are required",
		},
		{
			name: "json schema missing schema",
			req: Request{
				Model:    "gpt-4o",
				Output:   &OutputSpec{Mode: OutputModeJSONSchema},
				Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}},
			},
			wantErr: "requires schema",
		},
		{
			name: "text output cannot carry schema",
			req: Request{
				Model:    "gpt-4o",
				Output:   &OutputSpec{Mode: OutputModeText, Schema: map[string]any{"type": "object"}},
				Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}},
			},
			wantErr: "requires mode json_schema",
		},
		{
			name: "invalid request cache ttl",
			req: Request{
				Model:     "gpt-4o",
				CacheHint: &msg.CacheHint{Enabled: true, TTL: "10m"},
				Messages:  []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}},
			},
			wantErr: "ttl must be one of",
		},
		{
			name: "invalid message cache ttl",
			req: Request{
				Model: "gpt-4o",
				Messages: []Message{{
					Role:      RoleUser,
					CacheHint: &msg.CacheHint{Enabled: true, TTL: "10m"},
					Parts:     []Part{{Type: PartTypeText, Text: "hi"}},
				}},
			},
			wantErr: "messages[0].cache_hint",
		},
		{
			name: "ok",
			req: Request{
				Model:    "gpt-4o",
				Output:   &OutputSpec{Mode: OutputModeJSONSchema, Schema: map[string]any{"type": "object"}},
				Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
