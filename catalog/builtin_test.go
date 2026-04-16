package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBuiltIn(t *testing.T) {
	c, err := LoadBuiltIn()
	require.NoError(t, err)
	assert.NotEmpty(t, c.Models)
	assert.NotEmpty(t, c.Services)
	assert.NotEmpty(t, c.Offerings)
	_, hasAnthropic := c.Services["anthropic"]
	_, hasOpenAI := c.Services["openai"]
	assert.True(t, hasAnthropic)
	assert.True(t, hasOpenAI)
}
