package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolDefinitionFor_BasicStruct(t *testing.T) {
	type BasicParams struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	tool := ToolDefinitionFor[BasicParams]("test_tool", "A test tool")

	assert.Equal(t, "test_tool", tool.Name)
	assert.Equal(t, "A test tool", tool.Description)
	require.NotNil(t, tool.Parameters)

	// Check type
	assert.Equal(t, "object", tool.Parameters["type"])

	// Check properties
	props, ok := tool.Parameters["properties"].(map[string]any)
	require.True(t, ok, "properties should be a map")
	require.Len(t, props, 2)

	// Check name property
	nameProp, ok := props["name"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", nameProp["type"])

	// Check age property
	ageProp, ok := props["age"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "integer", ageProp["type"])
}

func TestToolDefinitionFor_WithDescriptions(t *testing.T) {
	type ParamsWithDesc struct {
		Location string `json:"location" jsonschema:"description=City name or coordinates"`
		Format   string `json:"format" jsonschema:"description=Output format"`
	}

	tool := ToolDefinitionFor[ParamsWithDesc]("get_data", "Get some data")

	props := tool.Parameters["properties"].(map[string]any)

	locationProp := props["location"].(map[string]any)
	assert.Equal(t, "City name or coordinates", locationProp["description"])

	formatProp := props["format"].(map[string]any)
	assert.Equal(t, "Output format", formatProp["description"])
}

func TestToolDefinitionFor_WithRequired(t *testing.T) {
	type ParamsWithRequired struct {
		FieldOne string `json:"field_one" jsonschema:"required"`
		FieldTwo string `json:"field_two"`
	}

	tool := ToolDefinitionFor[ParamsWithRequired]("test", "test")

	// Only fields with jsonschema:"required" are marked as required
	required, ok := tool.Parameters["required"].([]any)
	require.True(t, ok, "required should be an array")
	assert.Contains(t, required, "field_one")
	assert.NotContains(t, required, "field_two")
}

func TestToolDefinitionFor_WithEnum(t *testing.T) {
	type ParamsWithEnum struct {
		Unit string `json:"unit" jsonschema:"enum=celsius,enum=fahrenheit,enum=kelvin"`
	}

	tool := ToolDefinitionFor[ParamsWithEnum]("test", "test")

	props := tool.Parameters["properties"].(map[string]any)
	unitProp := props["unit"].(map[string]any)

	enum, ok := unitProp["enum"].([]any)
	require.True(t, ok, "enum should be an array")
	require.Len(t, enum, 3)
	assert.Contains(t, enum, "celsius")
	assert.Contains(t, enum, "fahrenheit")
	assert.Contains(t, enum, "kelvin")
}

func TestToolDefinitionFor_NestedStruct(t *testing.T) {
	type Address struct {
		Street string `json:"street"`
		City   string `json:"city"`
	}

	type Person struct {
		Name    string  `json:"name"`
		Address Address `json:"address"`
	}

	tool := ToolDefinitionFor[Person]("test", "test")

	props := tool.Parameters["properties"].(map[string]any)

	// Check that address is inlined (not a $ref)
	addressProp := props["address"].(map[string]any)
	assert.Equal(t, "object", addressProp["type"])

	// Check nested properties
	nestedProps := addressProp["properties"].(map[string]any)
	assert.Contains(t, nestedProps, "street")
	assert.Contains(t, nestedProps, "city")
}

func TestToolDefinitionFor_PointerFields(t *testing.T) {
	type ParamsWithPointer struct {
		RequiredField string  `json:"required_field" jsonschema:"required"`
		OptionalField *string `json:"optional_field"`
	}

	tool := ToolDefinitionFor[ParamsWithPointer]("test", "test")

	props := tool.Parameters["properties"].(map[string]any)

	// Both fields should generate properties
	_, hasRequired := props["required_field"]
	assert.True(t, hasRequired, "required field should be present")

	_, hasOptional := props["optional_field"]
	assert.True(t, hasOptional, "optional field should be present")

	// Only explicitly marked fields are required
	required, ok := tool.Parameters["required"].([]any)
	require.True(t, ok)
	assert.Contains(t, required, "required_field")
	assert.NotContains(t, required, "optional_field")
}

func TestToolDefinitionFor_SliceFields(t *testing.T) {
	type ParamsWithSlice struct {
		Tags []string `json:"tags" jsonschema:"description=List of tags"`
	}

	tool := ToolDefinitionFor[ParamsWithSlice]("test", "test")

	props := tool.Parameters["properties"].(map[string]any)
	tagsProp := props["tags"].(map[string]any)

	assert.Equal(t, "array", tagsProp["type"])
	assert.Equal(t, "List of tags", tagsProp["description"])

	// Check items type
	items := tagsProp["items"].(map[string]any)
	assert.Equal(t, "string", items["type"])
}

func TestToolDefinitionFor_ComplexExample(t *testing.T) {
	type GetWeatherParams struct {
		Location string   `json:"location" jsonschema:"description=City name or coordinates,required"`
		Unit     string   `json:"unit" jsonschema:"description=Temperature unit,enum=celsius,enum=fahrenheit"`
		Days     int      `json:"days" jsonschema:"description=Number of forecast days,minimum=1,maximum=7"`
		Include  []string `json:"include" jsonschema:"description=Additional data to include"`
	}

	tool := ToolDefinitionFor[GetWeatherParams]("get_weather", "Get weather forecast for a location")

	// Check basic structure
	assert.Equal(t, "get_weather", tool.Name)
	assert.Equal(t, "Get weather forecast for a location", tool.Description)
	assert.Equal(t, "object", tool.Parameters["type"])

	// Check required (only location is explicitly marked)
	required := tool.Parameters["required"].([]any)
	assert.Contains(t, required, "location")
	assert.NotContains(t, required, "unit")
	assert.NotContains(t, required, "days")
	assert.NotContains(t, required, "include")

	// Check properties
	props := tool.Parameters["properties"].(map[string]any)

	locationProp := props["location"].(map[string]any)
	assert.Equal(t, "string", locationProp["type"])
	assert.Equal(t, "City name or coordinates", locationProp["description"])

	unitProp := props["unit"].(map[string]any)
	assert.Equal(t, "string", unitProp["type"])
	enum := unitProp["enum"].([]any)
	assert.Len(t, enum, 2)

	daysProp := props["days"].(map[string]any)
	assert.Equal(t, "integer", daysProp["type"])
	assert.Equal(t, "Number of forecast days", daysProp["description"])
	assert.Equal(t, float64(1), daysProp["minimum"])
	assert.Equal(t, float64(7), daysProp["maximum"])

	includeProp := props["include"].(map[string]any)
	assert.Equal(t, "array", includeProp["type"])
}

func TestToolDefinitionFor_NoSchemaMetadata(t *testing.T) {
	type SimpleParams struct {
		Value string `json:"value"`
	}

	tool := ToolDefinitionFor[SimpleParams]("test", "test")

	// Should not have $schema or $id fields
	_, hasSchema := tool.Parameters["$schema"]
	assert.False(t, hasSchema, "should not have $schema field")

	_, hasId := tool.Parameters["$id"]
	assert.False(t, hasId, "should not have $id field")
}

func TestToolDefinitionFor_BooleanField(t *testing.T) {
	type ParamsWithBool struct {
		Enabled bool `json:"enabled" jsonschema:"description=Enable feature"`
	}

	tool := ToolDefinitionFor[ParamsWithBool]("test", "test")

	props := tool.Parameters["properties"].(map[string]any)
	enabledProp := props["enabled"].(map[string]any)

	assert.Equal(t, "boolean", enabledProp["type"])
	assert.Equal(t, "Enable feature", enabledProp["description"])
}

func TestToolDefinitionFor_NumberTypes(t *testing.T) {
	type ParamsWithNumbers struct {
		IntField     int     `json:"int_field"`
		Int64Field   int64   `json:"int64_field"`
		FloatField   float32 `json:"float_field"`
		Float64Field float64 `json:"float64_field"`
	}

	tool := ToolDefinitionFor[ParamsWithNumbers]("test", "test")

	props := tool.Parameters["properties"].(map[string]any)

	intProp := props["int_field"].(map[string]any)
	assert.Equal(t, "integer", intProp["type"])

	int64Prop := props["int64_field"].(map[string]any)
	assert.Equal(t, "integer", int64Prop["type"])

	floatProp := props["float_field"].(map[string]any)
	assert.Equal(t, "number", floatProp["type"])

	float64Prop := props["float64_field"].(map[string]any)
	assert.Equal(t, "number", float64Prop["type"])
}

func TestToolDefinitionFor_EmptyStruct(t *testing.T) {
	type EmptyParams struct{}

	tool := ToolDefinitionFor[EmptyParams]("no_params", "A tool with no parameters")

	assert.Equal(t, "no_params", tool.Name)
	assert.Equal(t, "A tool with no parameters", tool.Description)
	assert.Equal(t, "object", tool.Parameters["type"])

	props := tool.Parameters["properties"].(map[string]any)
	assert.Empty(t, props)
}

func TestToolDefinitionFor_CompatibleWithProviders(t *testing.T) {
	// This test verifies the output is compatible with what providers expect
	type TestParams struct {
		Query string `json:"query" jsonschema:"description=Search query,required"`
	}

	tool := ToolDefinitionFor[TestParams]("search", "Search for items")

	// Should have the basic structure providers expect
	assert.NotNil(t, tool.Parameters["type"])
	assert.NotNil(t, tool.Parameters["properties"])

	// The format matches what we use manually in tests
	props := tool.Parameters["properties"].(map[string]any)
	queryProp := props["query"].(map[string]any)
	assert.Equal(t, "string", queryProp["type"])
	assert.Equal(t, "Search query", queryProp["description"])

	required := tool.Parameters["required"].([]any)
	assert.Contains(t, required, "query")
}

// --- ToolSpec Tests ---

func TestNewToolSpec_Definition(t *testing.T) {
	type TestParams struct {
		Name string `json:"name" jsonschema:"description=A name,required"`
	}

	spec := NewToolSpec[TestParams]("test_tool", "A test tool")

	def := spec.Definition()
	assert.Equal(t, "test_tool", def.Name)
	assert.Equal(t, "A test tool", def.Description)
	assert.NotNil(t, def.Parameters)

	// Should match ToolDefinitionFor output
	expectedDef := ToolDefinitionFor[TestParams]("test_tool", "A test tool")
	assert.Equal(t, expectedDef, def)
}

func TestToolSpec_ParseValid(t *testing.T) {
	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"required"`
		Unit     string `json:"unit"`
	}

	spec := NewToolSpec[GetWeatherParams]("get_weather", "Get weather")

	rawCall := ToolCall{
		ID:   "call_123",
		Name: "get_weather",
		Arguments: map[string]any{
			"location": "Paris",
			"unit":     "celsius",
		},
	}

	parsed, err := spec.parse(rawCall)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	typedCall, ok := parsed.(*TypedToolCall[GetWeatherParams])
	require.True(t, ok, "should be TypedToolCall[GetWeatherParams]")

	assert.Equal(t, "call_123", typedCall.ID)
	assert.Equal(t, "get_weather", typedCall.Name)
	assert.Equal(t, "Paris", typedCall.Params.Location)
	assert.Equal(t, "celsius", typedCall.Params.Unit)
}

func TestToolSpec_ParseValidation_MissingRequired(t *testing.T) {
	type Params struct {
		Required string `json:"required" jsonschema:"required"`
		Optional string `json:"optional"`
	}

	spec := NewToolSpec[Params]("test", "test")

	rawCall := ToolCall{
		ID:        "call_123",
		Name:      "test",
		Arguments: map[string]any{"optional": "value"},
	}

	_, err := spec.parse(rawCall)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate")
}

func TestToolSpec_ParseValidation_WrongType(t *testing.T) {
	type Params struct {
		Count int `json:"count" jsonschema:"required"`
	}

	spec := NewToolSpec[Params]("test", "test")

	rawCall := ToolCall{
		ID:        "call_123",
		Name:      "test",
		Arguments: map[string]any{"count": "not a number"},
	}

	_, err := spec.parse(rawCall)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate")
}

func TestToolSpec_ParseValidation_InvalidEnum(t *testing.T) {
	type Params struct {
		Unit string `json:"unit" jsonschema:"required,enum=celsius,enum=fahrenheit"`
	}

	spec := NewToolSpec[Params]("test", "test")

	rawCall := ToolCall{
		ID:        "call_123",
		Name:      "test",
		Arguments: map[string]any{"unit": "kelvin"},
	}

	_, err := spec.parse(rawCall)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate")
}

func TestToolSpec_ParseValidation_NumericRange(t *testing.T) {
	type Params struct {
		Age int `json:"age" jsonschema:"required,minimum=0,maximum=120"`
	}

	spec := NewToolSpec[Params]("test", "test")

	// Test below minimum
	rawCall := ToolCall{
		ID:        "call_123",
		Name:      "test",
		Arguments: map[string]any{"age": -1},
	}

	_, err := spec.parse(rawCall)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate")

	// Test above maximum
	rawCall.Arguments = map[string]any{"age": 150}
	_, err = spec.parse(rawCall)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate")

	// Test valid range
	rawCall.Arguments = map[string]any{"age": 25}
	parsed, err := spec.parse(rawCall)
	require.NoError(t, err)
	typedCall := parsed.(*TypedToolCall[Params])
	assert.Equal(t, 25, typedCall.Params.Age)
}

func TestToolSpec_ParseEmptyArgs(t *testing.T) {
	type EmptyParams struct{}

	spec := NewToolSpec[EmptyParams]("test", "test")

	rawCall := ToolCall{
		ID:        "call_123",
		Name:      "test",
		Arguments: map[string]any{},
	}

	parsed, err := spec.parse(rawCall)
	require.NoError(t, err)

	typedCall := parsed.(*TypedToolCall[EmptyParams])
	assert.Equal(t, "call_123", typedCall.ID)
}

// --- ToolSet Tests ---

func TestToolSet_Definitions(t *testing.T) {
	type Params1 struct {
		Field1 string `json:"field1"`
	}
	type Params2 struct {
		Field2 int `json:"field2"`
	}

	spec1 := NewToolSpec[Params1]("tool1", "First tool")
	spec2 := NewToolSpec[Params2]("tool2", "Second tool")

	toolSet := NewToolSet(spec1, spec2)

	defs := toolSet.Definitions()
	require.Len(t, defs, 2)

	assert.Equal(t, "tool1", defs[0].Name)
	assert.Equal(t, "First tool", defs[0].Description)

	assert.Equal(t, "tool2", defs[1].Name)
	assert.Equal(t, "Second tool", defs[1].Description)
}

func TestToolSet_Parse_SingleTool(t *testing.T) {
	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"required"`
	}

	spec := NewToolSpec[GetWeatherParams]("get_weather", "Get weather")
	toolSet := NewToolSet(spec)

	rawCalls := []ToolCall{
		{
			ID:        "call_123",
			Name:      "get_weather",
			Arguments: map[string]any{"location": "Paris"},
		},
	}

	parsed, err := toolSet.Parse(rawCalls)
	require.NoError(t, err)
	require.Len(t, parsed, 1)

	call := parsed[0]
	assert.Equal(t, "get_weather", call.ToolName())
	assert.Equal(t, "call_123", call.ToolCallID())

	typedCall, ok := call.(*TypedToolCall[GetWeatherParams])
	require.True(t, ok)
	assert.Equal(t, "Paris", typedCall.Params.Location)
}

func TestToolSet_Parse_MultipleTools(t *testing.T) {
	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"required"`
	}
	type SearchParams struct {
		Query string `json:"query" jsonschema:"required"`
	}

	weatherSpec := NewToolSpec[GetWeatherParams]("get_weather", "Get weather")
	searchSpec := NewToolSpec[SearchParams]("search", "Search")
	toolSet := NewToolSet(weatherSpec, searchSpec)

	rawCalls := []ToolCall{
		{
			ID:        "call_1",
			Name:      "get_weather",
			Arguments: map[string]any{"location": "London"},
		},
		{
			ID:        "call_2",
			Name:      "search",
			Arguments: map[string]any{"query": "golang"},
		},
	}

	parsed, err := toolSet.Parse(rawCalls)
	require.NoError(t, err)
	require.Len(t, parsed, 2)

	// Test type switching
	for _, call := range parsed {
		switch c := call.(type) {
		case *TypedToolCall[GetWeatherParams]:
			assert.Equal(t, "call_1", c.ID)
			assert.Equal(t, "London", c.Params.Location)
		case *TypedToolCall[SearchParams]:
			assert.Equal(t, "call_2", c.ID)
			assert.Equal(t, "golang", c.Params.Query)
		default:
			t.Fatalf("unexpected type: %T", call)
		}
	}
}

func TestToolSet_Parse_UnknownTool(t *testing.T) {
	type Params struct {
		Value string `json:"value"`
	}

	spec := NewToolSpec[Params]("known", "Known tool")
	toolSet := NewToolSet(spec)

	rawCalls := []ToolCall{
		{ID: "call_1", Name: "known", Arguments: map[string]any{"value": "ok"}},
		{ID: "call_2", Name: "unknown", Arguments: map[string]any{"value": "bad"}},
	}

	parsed, err := toolSet.Parse(rawCalls)

	// Should return the successfully parsed call
	require.Len(t, parsed, 1)
	assert.Equal(t, "call_1", parsed[0].ToolCallID())

	// Should return error for unknown tool
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool: unknown")
}

func TestToolSet_Parse_ValidationError(t *testing.T) {
	type Params struct {
		Required string `json:"required" jsonschema:"required"`
	}

	spec := NewToolSpec[Params]("test", "test")
	toolSet := NewToolSet(spec)

	rawCalls := []ToolCall{
		{ID: "call_1", Name: "test", Arguments: map[string]any{"required": "ok"}},
		{ID: "call_2", Name: "test", Arguments: map[string]any{}}, // missing required
	}

	parsed, err := toolSet.Parse(rawCalls)

	// Should return the valid call
	require.Len(t, parsed, 1)
	assert.Equal(t, "call_1", parsed[0].ToolCallID())

	// Should return error for invalid args
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate")
}

func TestToolSet_Parse_EmptyCalls(t *testing.T) {
	type Params struct{}

	spec := NewToolSpec[Params]("test", "test")
	toolSet := NewToolSet(spec)

	parsed, err := toolSet.Parse(nil)
	assert.NoError(t, err)
	assert.Empty(t, parsed)

	parsed, err = toolSet.Parse([]ToolCall{})
	assert.NoError(t, err)
	assert.Empty(t, parsed)
}

// --- TypedToolCall Tests ---

func TestTypedToolCall_Interface(t *testing.T) {
	type Params struct {
		Value string `json:"value"`
	}

	call := &TypedToolCall[Params]{
		ID:     "call_123",
		Name:   "test_tool",
		Params: Params{Value: "test"},
	}

	// Verify it implements ParsedToolCall
	var _ ParsedToolCall = call

	assert.Equal(t, "test_tool", call.ToolName())
	assert.Equal(t, "call_123", call.ToolCallID())
}
