package dockermr

import "github.com/codewandler/llm"

// Known model IDs in Docker Hub's ai/ namespace.
//
// Model IDs use the format ai/<name>:<tag>.
// Tags follow the pattern <size>-<quantization> (e.g. 7B-Q4_K_M).
// Using the bare name without a tag resolves to the recommended default
// quantization for that model family.
//
// Models are served by llama.cpp via the defaultEngine backend.
// They are downloaded separately with `docker model pull ai/<name>`.
const (
	ModelSmoLLM2       = "ai/smollm2"             // 360M Q4_K_M (default tag)
	ModelSmoLLM2Tiny   = "ai/smollm2:135M-Q4_K_M" // smallest available SmolLM2 variant
	ModelQwen25        = "ai/qwen2.5"             // 7B Q4_K_M
	ModelQwen25Small   = "ai/qwen2.5:0.5B-F16"    // cheapest Qwen2.5 variant
	ModelQwen3         = "ai/qwen3"
	ModelQwen3Coder    = "ai/qwen3-coder"
	ModelLlama32       = "ai/llama3.2"
	ModelLlama33       = "ai/llama3.3"
	ModelPhi4Mini      = "ai/phi4-mini"
	ModelPhi4          = "ai/phi4"
	ModelGemma3        = "ai/gemma3"
	ModelGemma4        = "ai/gemma4"
	ModelDeepSeekR1    = "ai/deepseek-r1"
	ModelMistralSmall  = "ai/mistral-small3.2"
	ModelGLM47Flash    = "ai/glm-4.7-flash"
	ModelGranite4Nano  = "ai/granite4.0-nano"
	ModelFunctionGemma = "ai/functiongemma"

	// ModelDefault is used when the caller does not specify a model.
	// SmolLM2 360M is the smallest available ai/ model and succeeds on
	// low-memory machines without a dedicated GPU.
	ModelDefault = ModelSmoLLM2
)

// curatedModels is the static list returned by Provider.Models().
// It contains well-known, publicly available models from Docker Hub's ai/
// namespace. Call FetchModels() to retrieve the live list of locally pulled
// models instead.
var curatedModels = llm.Models{
	{ID: ModelSmoLLM2, Name: "SmolLM2 360M", Provider: llm.ProviderNameDockerMR},
	{ID: ModelSmoLLM2Tiny, Name: "SmolLM2 135M", Provider: llm.ProviderNameDockerMR},
	{ID: ModelQwen25Small, Name: "Qwen2.5 0.5B", Provider: llm.ProviderNameDockerMR},
	{ID: ModelQwen25, Name: "Qwen2.5 7B", Provider: llm.ProviderNameDockerMR},
	{ID: ModelQwen3, Name: "Qwen3", Provider: llm.ProviderNameDockerMR},
	{ID: ModelQwen3Coder, Name: "Qwen3 Coder", Provider: llm.ProviderNameDockerMR},
	{ID: ModelLlama32, Name: "Llama 3.2", Provider: llm.ProviderNameDockerMR},
	{ID: ModelLlama33, Name: "Llama 3.3", Provider: llm.ProviderNameDockerMR},
	{ID: ModelPhi4Mini, Name: "Phi-4 Mini", Provider: llm.ProviderNameDockerMR},
	{ID: ModelPhi4, Name: "Phi-4", Provider: llm.ProviderNameDockerMR},
	{ID: ModelGemma3, Name: "Gemma 3", Provider: llm.ProviderNameDockerMR},
	{ID: ModelGemma4, Name: "Gemma 4", Provider: llm.ProviderNameDockerMR},
	{ID: ModelDeepSeekR1, Name: "DeepSeek R1", Provider: llm.ProviderNameDockerMR},
	{ID: ModelMistralSmall, Name: "Mistral Small 3.2", Provider: llm.ProviderNameDockerMR},
	{ID: ModelGLM47Flash, Name: "GLM-4.7 Flash", Provider: llm.ProviderNameDockerMR},
	{ID: ModelGranite4Nano, Name: "Granite 4.0 Nano", Provider: llm.ProviderNameDockerMR},
	{ID: ModelFunctionGemma, Name: "FunctionGemma", Provider: llm.ProviderNameDockerMR},
}
