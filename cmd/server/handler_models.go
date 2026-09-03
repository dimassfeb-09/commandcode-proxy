package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleModels serves GET /v1/models from the hardcoded probe table,
// enriched with registry metadata where known.
func handleModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireKey(w, r); !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	models := make([]any, 0, len(validModels))
	for _, id := range validModels {
		entry := map[string]any{"id": id, "object": "model", "created": 1700000000, "owned_by": "commandcode-proxy",
			"supported_parameters": []string{"temperature", "max_tokens", "tools"}}
		if meta, ok := modelMetas[id]; ok {
			entry["name"], entry["description"] = meta.name, meta.desc
			if meta.context > 0 {
				entry["context_length"] = meta.context
			}
			modalities := append([]string{}, meta.modalities...)
			entry["architecture"] = map[string]any{"input_modalities": modalities,
				"modality": strings.Join(modalities, "+") + "->text", "output_modalities": []string{"text"}}
			if meta.reasoning {
				entry["reasoning"] = map[string]any{"supported": true}
			}
		}
		models = append(models, entry)
	}
	json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
}
