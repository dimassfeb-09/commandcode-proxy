package main

// validModels passed a live "hi" probe per model on 2026-09-03 on this
// account (36/65). Hardcoded: the gateway exposes no list-models endpoint.
var validModels = []string{
	"xiaomi/mimo-v2.5", "xiaomi/mimo-v2.5-pro",
	"Qwen/Qwen3.8-Max", "Qwen/Qwen3.8-Flash", "Qwen/Qwen3.8-27B",
	"Qwen/Qwen3.7-Max", "Qwen/Qwen3.7-Plus", "Qwen/Qwen3.7-Flash",
	"Qwen/Qwen3.6-Max-Preview", "Qwen/Qwen3.6-Plus",
	"zai-org/GLM-5.3", "zai-org/GLM-5.2", "zai-org/GLM-5.1", "zai-org/GLM-5",
	"zai-org/GLM-5.2-Fast", "z-ai/glm-5.3-flash",
	"moonshotai/Kimi-K3", "moonshotai/Kimi-K2.7-Code", "moonshotai/Kimi-K2.7-Code-Highspeed",
	"moonshotai/Kimi-K2.6", "moonshotai/Kimi-K2.5",
	"deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-flash",
	"deepseek/deepseek-v4-flash-fast", "deepseek/deepseek-v4-flash-vision-exp",
	"MiniMaxAI/MiniMax-M3", "MiniMaxAI/MiniMax-M2.5",
	"stepfun/Step-3.7-Flash", "stepfun/Step-3.5-Flash",
	"tencent/hy3-paid", "tencent/hy4-preview",
	"gpt-5.6-luna", "xai/grok-4.5",
	"thinkingmachines/inkling", "thinkingmachines/inkling-small",
	"meta/muse-spark-1.2-contributor",
}

// modelMeta: metadata per model, diekstrak dari registry statis CLI (qR).
// Diregenerate tiap update command-code; bukan dari endpoint (tidak ada).
type modelMeta struct {
	name, desc string
	context    int // 0 = tak diumumkan CLI
	modalities []string
	reasoning  bool
}

var modelMetas = map[string]modelMeta{
	"xiaomi/mimo-v2.5":                      {"MiMo V2.5", "efficient long-context agentic coding", 1000000, []string{"text", "image"}, false},
	"xiaomi/mimo-v2.5-pro":                  {"MiMo V2.5 Pro", "high-capability long-context agentic coding", 1000000, []string{"text"}, false},
	"Qwen/Qwen3.8-Max":                      {"Qwen 3.8 Max", "autonomous long-horizon coding & professional work", 1000000, []string{"text", "image"}, true},
	"Qwen/Qwen3.8-Flash":                    {"Qwen 3.8 Flash", "fast low-cost agentic coding & reasoning", 1000000, []string{"text", "image"}, true},
	"Qwen/Qwen3.8-27B":                      {"Qwen 3.8 27B", "compact vision-language coding & agentic work", 262144, []string{"text", "image"}, true},
	"Qwen/Qwen3.7-Max":                      {"Qwen 3.7 Max", "frontier coding & long-horizon agent execution", 1000000, []string{"text"}, true},
	"Qwen/Qwen3.7-Plus":                     {"Qwen 3.7 Plus", "agentic coding & reasoning at lower cost", 1000000, []string{"text", "image"}, true},
	"Qwen/Qwen3.7-Flash":                    {"Qwen 3.7 Flash", "fast low-cost agentic coding & reasoning", 1000000, []string{"text", "image"}, true},
	"Qwen/Qwen3.6-Max-Preview":              {"Qwen 3.6 Max Preview", "vibe coding & efficient agent execution", 0, []string{"text"}, true},
	"Qwen/Qwen3.6-Plus":                     {"Qwen 3.6 Plus", "agentic coding & reasoning", 0, []string{"text", "image"}, true},
	"zai-org/GLM-5.3":                       {"GLM-5.3", "frontier coding with emergent cyber capabilities", 1000000, []string{"text"}, true},
	"zai-org/GLM-5.2":                       {"GLM-5.2", "powerful coding with 1M context and long-horizon tasks", 1000000, []string{"text"}, true},
	"zai-org/GLM-5.1":                       {"GLM-5.1", "long-horizon autonomous coding agent", 0, []string{"text"}, false},
	"zai-org/GLM-5":                         {"GLM-5", "multi-mode thinking & long-range planning", 200000, []string{"text"}, false},
	"zai-org/GLM-5.2-Fast":                  {"GLM-5.2 Fast", "high-throughput GLM-5.2 with 1M context", 1000000, []string{"text"}, false},
	"z-ai/glm-5.3-flash":                    {"GLM-5.3 Flash", "fast, affordable GLM coding with 1M context", 1048576, []string{"text", "image"}, true},
	"moonshotai/Kimi-K3":                    {"Kimi K3", "long-horizon coding & knowledge work with 1M context", 1000000, []string{"text", "image"}, true},
	"moonshotai/Kimi-K2.7-Code":             {"Kimi K2.7 Code", "improved long-horizon coding with vision", 256000, []string{"text", "image"}, true},
	"moonshotai/Kimi-K2.7-Code-Highspeed":   {"Kimi K2.7 Code HighSpeed", "high-speed long-horizon coding with vision", 262000, []string{"text", "image"}, true},
	"moonshotai/Kimi-K2.6":                  {"Kimi K2.6", "long-horizon coding with vision", 256000, []string{"text", "image"}, false},
	"moonshotai/Kimi-K2.5":                  {"Kimi K2.5", "multimodal frontend coding", 256000, []string{"text", "image"}, false},
	"deepseek/deepseek-v4-pro":              {"DeepSeek V4 Pro (latest)", "hybrid-attention long-context reasoning", 1000000, []string{"text"}, true},
	"deepseek/deepseek-v4-flash":            {"DeepSeek V4 Flash (latest)", "fast hybrid-attention reasoning", 1000000, []string{"text"}, true},
	"deepseek/deepseek-v4-flash-fast":       {"DeepSeek V4 Flash Fast", "low-latency V4 Flash deployment", 1000000, []string{"text"}, true},
	"deepseek/deepseek-v4-flash-vision-exp": {"DeepSeek V4 Flash Vision (exp)", "fast hybrid-attention reasoning with vision", 1000000, []string{"text", "image"}, true},
	"MiniMaxAI/MiniMax-M3":                  {"MiniMax M3", "frontier coding, agents & native multimodality", 1000000, []string{"text", "image"}, true},
	"MiniMaxAI/MiniMax-M2.5":                {"MiniMax M2.5", "cross-platform full-stack agentic dev", 200000, []string{"text"}, false},
	"stepfun/Step-3.7-Flash":                {"Step 3.7 Flash", "multimodal sparse-MoE reasoning", 256000, []string{"text", "image"}, true},
	"stepfun/Step-3.5-Flash":                {"Step 3.5 Flash", "fast sparse-MoE agentic reasoning", 1000000, []string{"text"}, true},
	"tencent/hy3-paid":                      {"Tencent Hy3", "sparse-MoE reasoning & agentic tool use", 262144, []string{"text"}, true},
	"tencent/hy4-preview":                   {"Tencent Hy4 Preview", "agentic coding & sustained multi-step tool use", 1048576, []string{"text"}, true},
	"gpt-5.6-luna":                          {"GPT-5.6 Luna", "optimized for cost-sensitive workloads", 1050000, []string{"text", "image"}, true},
	"xai/grok-4.5":                          {"Grok 4.5", "smartest model for coding, agentic tasks, knowledge work", 500000, []string{"text", "image"}, true},
	"thinkingmachines/inkling":              {"Inkling", "multimodal MoE reasoning", 256000, []string{"text", "image"}, true},
	"thinkingmachines/inkling-small":        {"Inkling Small", "lightweight MoE reasoning at lower cost and latency", 1000000, []string{"text", "image"}, true},
	"meta/muse-spark-1.2-contributor":       {"Muse Spark 1.2 Contributor", "Muse Spark 1.2 at ~95% off", 1048576, []string{"text", "image"}, true},
}
