package unified

import (
	"testing"

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
			name: "ok",
			req:  Request{Model: "gpt-4o", Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}}},
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
