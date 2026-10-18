package config

// ProviderPreset describes a well-known OpenAI-compatible endpoint so the UI can
// pre-fill the base URL, strict-URL flag and a sensible model list.
type ProviderPreset struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	BaseURL       string   `json:"base_url"`
	URLStrict     bool     `json:"url_strict"`
	DefaultModel  string   `json:"default_model"`
	SuggestedModels []string `json:"suggested_models"`
	// Local marks presets that usually run on the machine itself (Ollama, LM Studio,
	// vLLM...). They typically need no API key and are the reason people keep
	// several ports around.
	Local bool `json:"local"`
}

// ProviderPresets returns the built-in presets. Keep this list small and factual:
// each entry must be an OpenAI-compatible /chat/completions endpoint.
func ProviderPresets() []ProviderPreset {
	return []ProviderPreset{
		{
			ID:           "openai",
			Name:         "OpenAI",
			BaseURL:      "https://api.openai.com/v1",
			DefaultModel: "gpt-4o",
			SuggestedModels: []string{
				"gpt-4o", "gpt-4o-mini", "gpt-4.1", "gpt-4.1-mini", "o3-mini",
			},
		},
		{
			ID:           "deepseek",
			Name:         "DeepSeek",
			BaseURL:      "https://api.deepseek.com",
			DefaultModel: "deepseek-chat",
			SuggestedModels: []string{
				"deepseek-chat", "deepseek-reasoner",
			},
		},
		{
			ID:           "zhipu",
			Name:         "智谱 GLM",
			BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
			DefaultModel: "glm-4-plus",
			SuggestedModels: []string{
				"glm-4-plus", "glm-4-air", "glm-4-flash",
			},
		},
		{
			ID:           "moonshot",
			Name:         "Moonshot Kimi",
			BaseURL:      "https://api.moonshot.cn/v1",
			DefaultModel: "moonshot-v1-32k",
			SuggestedModels: []string{
				"moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k",
			},
		},
		{
			ID:           "qwen",
			Name:         "阿里通义千问",
			BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
			DefaultModel: "qwen-plus",
			SuggestedModels: []string{
				"qwen-plus", "qwen-max", "qwen-turbo", "qwen-long",
			},
		},
		{
			ID:           "siliconflow",
			Name:         "硅基流动 SiliconFlow",
			BaseURL:      "https://api.siliconflow.cn/v1",
			DefaultModel: "deepseek-ai/DeepSeek-V3",
			SuggestedModels: []string{
				"deepseek-ai/DeepSeek-V3", "Qwen/Qwen2.5-72B-Instruct",
			},
		},
		{
			ID:           "ollama",
			Name:         "Ollama（本地）",
			BaseURL:      "http://127.0.0.1:11434/v1",
			DefaultModel: "llama3.1",
			SuggestedModels: []string{
				"llama3.1", "qwen2.5", "deepseek-r1", "mistral",
			},
			Local: true,
		},
		{
			ID:           "lmstudio",
			Name:         "LM Studio（本地）",
			BaseURL:      "http://127.0.0.1:1234/v1",
			DefaultModel: "",
			Local:        true,
		},
		{
			ID:           "vllm",
			Name:         "vLLM / 自建（本地）",
			BaseURL:      "http://127.0.0.1:8000/v1",
			DefaultModel: "",
			Local:        true,
		},
		{
			ID:           "custom",
			Name:         "自定义",
			BaseURL:      "",
			DefaultModel: "",
		},
	}
}

// FindProviderPreset returns the preset with the given id.
func FindProviderPreset(id string) (ProviderPreset, bool) {
	for _, p := range ProviderPresets() {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderPreset{}, false
}
