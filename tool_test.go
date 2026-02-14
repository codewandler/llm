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
