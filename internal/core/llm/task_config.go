package llm

// TaskType identifies the type of LLM task.
type TaskType string

// Task type constants.
const (
	TaskTypeSummarize      TaskType = "summarize"
	TaskTypeClusterSummary TaskType = "cluster_summary"
	TaskTypeClusterTopic   TaskType = "cluster_topic"
	TaskTypeNarrative      TaskType = "narrative"
	TaskTypeTranslate      TaskType = "translate"
	TaskTypeComplete       TaskType = "complete"
	TaskTypeRelevanceGate  TaskType = "relevance_gate"
	TaskTypeCompress       TaskType = "compress"
	TaskTypeImageGen       TaskType = "image_gen"
)

// ProviderModel specifies a provider and model combination.
type ProviderModel struct {
	Provider ProviderName
	Model    string
}

// TaskProviderChain defines the provider/model fallback chain for a task.
type TaskProviderChain struct {
	Default   ProviderModel
	Fallbacks []ProviderModel
}

// DefaultTaskConfig returns the default task configuration per the proposal.
// Each task has its own provider/model fallback chain.
func DefaultTaskConfig() map[TaskType]TaskProviderChain {
	return map[TaskType]TaskProviderChain{
		// Summarize: Google → OpenAI
		TaskTypeSummarize: {
			Default: ProviderModel{Provider: ProviderGoogle, Model: "gemini-2.0-flash-lite"},
			Fallbacks: []ProviderModel{
				{Provider: ProviderOpenAI, Model: "gpt-5-nano"},
			},
		},

		// Cluster Summary: Cohere → OpenAI → Google
		TaskTypeClusterSummary: {
			Default: ProviderModel{Provider: ProviderCohere, Model: "command-r"},
			Fallbacks: []ProviderModel{
				{Provider: ProviderOpenAI, Model: "gpt-5"},
				{Provider: ProviderGoogle, Model: "gemini-2.0-flash-lite"},
			},
		},

		// Cluster Topic: OpenAI → Google → OpenRouter
		TaskTypeClusterTopic: {
			Default: ProviderModel{Provider: ProviderOpenAI, Model: "gpt-5-nano"},
			Fallbacks: []ProviderModel{
				{Provider: ProviderGoogle, Model: "gemini-2.0-flash-lite"},
				{Provider: ProviderOpenRouter, Model: "mistralai/mistral-7b-instruct"},
			},
		},

		// Narrative: Google → Anthropic → OpenAI
		TaskTypeNarrative: {
			Default: ProviderModel{Provider: ProviderGoogle, Model: "gemini-2.0-flash-lite"},
			Fallbacks: []ProviderModel{
				{Provider: ProviderAnthropic, Model: "claude-haiku-4.5"},
				{Provider: ProviderOpenAI, Model: "gpt-5.2"},
			},
		},

		// Translate: OpenRouter (Llama) → OpenAI
		TaskTypeTranslate: {
			Default: ProviderModel{Provider: ProviderOpenRouter, Model: "meta-llama/llama-3.1-8b-instruct"},
			Fallbacks: []ProviderModel{
				{Provider: ProviderOpenAI, Model: "gpt-5-nano"},
			},
		},

		// Complete: OpenRouter (Llama) → OpenAI
		TaskTypeComplete: {
			Default: ProviderModel{Provider: ProviderOpenRouter, Model: "meta-llama/llama-3.1-8b-instruct"},
			Fallbacks: []ProviderModel{
				{Provider: ProviderOpenAI, Model: "gpt-4o-mini"},
			},
		},

		// RelevanceGate: OpenRouter (Llama) → OpenAI
		TaskTypeRelevanceGate: {
			Default: ProviderModel{Provider: ProviderOpenRouter, Model: "meta-llama/llama-3.1-8b-instruct"},
			Fallbacks: []ProviderModel{
				{Provider: ProviderOpenAI, Model: "gpt-5-nano"},
			},
		},

		// Compress: OpenAI → OpenRouter
		TaskTypeCompress: {
			Default: ProviderModel{Provider: ProviderOpenAI, Model: "gpt-4o-mini"},
			Fallbacks: []ProviderModel{
				{Provider: ProviderOpenRouter, Model: "openai/gpt-oss-120b"},
			},
		},

		// ImageGen: OpenAI only
		TaskTypeImageGen: {
			Default:   ProviderModel{Provider: ProviderOpenAI, Model: "gpt-image-1.5"},
			Fallbacks: []ProviderModel{},
		},
	}
}

// GetProviderChain returns the ordered list of provider/model combinations for a task.
func (tc TaskProviderChain) GetProviderChain() []ProviderModel {
	chain := make([]ProviderModel, 0, 1+len(tc.Fallbacks))
	chain = append(chain, tc.Default)
	chain = append(chain, tc.Fallbacks...)

	return chain
}
