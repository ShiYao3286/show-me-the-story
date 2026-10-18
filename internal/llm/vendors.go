package llm

import "strings"

// vendorNames maps the vendor keys that aggregating gateways and providers put in
// `owned_by` (or use as the id prefix) to the Chinese names users recognise.
//
// This is a display hint only: unknown keys fall back to the raw key, so new
// vendors keep working without a code change. Keys are matched lower-cased.
var vendorNames = map[string]string{
	// Model vendors
	"openai":           "OpenAI",
	"anthropic":        "Anthropic",
	"deepseek":         "DeepSeek",
	"deepseek-account": "DeepSeek 官方",
	"zhipu":            "智谱 AI",
	"bigmodel":         "智谱 AI",
	"glm":              "智谱 GLM",
	"moonshot":         "月之暗面 Kimi",
	"kimi":             "月之暗面 Kimi",
	"qwen":             "阿里通义千问",
	"alibaba":          "阿里通义千问",
	"dashscope":        "阿里通义千问",
	"baidu":            "百度文心",
	"ernie":            "百度文心",
	"tencent":          "腾讯混元",
	"hunyuan":          "腾讯混元",
	"bytedance":        "字节豆包",
	"doubao":           "字节豆包",
	"volcengine":       "火山引擎",
	"minimax":          "MiniMax",
	"baichuan":         "百川智能",
	"01ai":             "零一万物",
	"yi":               "零一万物",
	"stepfun":          "阶跃星辰",
	"siliconflow":      "硅基流动",
	"xai":              "xAI",
	"grok":             "xAI Grok",
	"google":           "Google",
	"gemini":           "Google Gemini",
	"meta":             "Meta",
	"llama":            "Meta Llama",
	"mistral":          "Mistral AI",
	"cohere":           "Cohere",
	"nvidia":           "NVIDIA",
	"nemotron":         "NVIDIA Nemotron",
	"microsoft":        "微软",
	"azure":            "Azure OpenAI",
	"aws":              "AWS Bedrock",
	"bedrock":          "AWS Bedrock",
	"openrouter":       "OpenRouter",
	"together":         "Together AI",
	"fireworks":        "Fireworks AI",
	"groq":             "Groq",
	"perplexity":       "Perplexity",
	"codearts":         "华为 CodeArts",
	"huawei":           "华为",
	"hunyuandit":       "腾讯混元",

	// Local runtimes
	"ollama":    "Ollama（本地）",
	"lmstudio":  "LM Studio（本地）",
	"lm-studio": "LM Studio（本地）",
	"vllm":      "vLLM（本地）",
	"llamacpp":  "llama.cpp（本地）",
	"llama.cpp": "llama.cpp（本地）",
	"local":     "本地",
	"localhost": "本地",

	// This gateway's own buckets: keep them but make them readable
	"our-free-model": "免费模型",
	"free":           "免费模型",
	"workbuddy":      "WorkBuddy",
	"codebuddy":      "CodeBuddy",
	"cline":          "Cline",
	"raccoon":        "Raccoon",
	"buddy":          "Buddy",
	"zcode":          "ZCode",
	"ds":             "DeepSeek",
	"me":             "个人",
	"mao":            "Mao",
	"cs":             "CS",
	"gpt":            "GPT",
	"chatgpt":        "ChatGPT",
}

// VendorLabel returns the display name for a vendor key. When the key is unknown
// it is returned unchanged, so untranslated vendors still show something sensible.
func VendorLabel(key string) string {
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	if label, ok := vendorNames[strings.ToLower(k)]; ok {
		return label
	}
	// Try the first path segment ("vendor/model" style keys).
	if i := strings.Index(k, "/"); i > 0 {
		return VendorLabel(k[:i])
	}
	return k
}
