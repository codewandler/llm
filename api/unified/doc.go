// Package unified defines the canonical internal request and stream schemas plus
// three bridge layers:
//   - request bridges between the unified request model and protocol wire requests
//   - stream bridges between protocol-native events and the unified stream model
//   - forward bridges from unified stream events into the current llm publisher path
package unified
