package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"showmethestory/internal/config"
	"showmethestory/internal/llm"
)

// apiConfigsView is what the UI receives: every profile plus the active id and
// the built-in provider presets. Secrets are returned as-is because this is a
// local single-user app and the existing single-config endpoint already did so;
// the UI masks them.
type apiConfigsView struct {
	ActiveID  string                  `json:"active_id"`
	Profiles  []config.APIProfile     `json:"profiles"`
	Presets   []config.ProviderPreset `json:"presets"`
	Active    *config.APIConfig       `json:"active"`
}

func (h *Handlers) loadAPIConfigsDoc() (*config.APIConfigsFile, error) {
	return config.LoadAPIConfigs(h.apiCfgPath)
}

// GetAPIConfigs returns all saved endpoints and the presets.
func (h *Handlers) GetAPIConfigs(w http.ResponseWriter, r *http.Request) {
	doc, err := h.loadAPIConfigsDoc()
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "load_api_config_failed", err)
		return
	}
	active := doc.Active()
	var flat *config.APIConfig
	if active != nil {
		c := active.ToAPIConfig()
		flat = &c
	}
	profiles := make([]config.APIProfile, len(doc.Profiles))
	copy(profiles, doc.Profiles)
	h.writeJSON(w, http.StatusOK, apiConfigsView{
		ActiveID: string(doc.ActiveID),
		Profiles: profiles,
		Presets:  config.ProviderPresets(),
		Active:   flat,
	})
}

// PostAPIConfigProfile creates or updates one profile.
// Body: {id?, name, provider?, base_url, url_strict, api_key, model, max_tokens, http_timeout_seconds, context_budget_tokens, activate?}
func (h *Handlers) PostAPIConfigProfile(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var body struct {
		config.APIProfile
		Activate bool `json:"activate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "api_profile_name_required")
		return
	}

	doc, err := h.loadAPIConfigsDoc()
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "load_api_config_failed", err)
		return
	}

	p := body.APIProfile
	p.ID = config.ProfileID(strings.TrimSpace(string(p.ID)))
	p.Name = name
	// Upsert allocates an id when p.ID is empty; use the returned value.
	p.ID = doc.Upsert(p)

	if body.Activate || doc.ActiveID == "" {
		doc.ActiveID = p.ID
	}
	if err := config.SaveAPIConfigs(h.apiCfgPath, doc); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_api_config_failed", err)
		return
	}

	// If the saved profile is the active one, refresh the live config used by tasks.
	if doc.ActiveID == p.ID {
		h.applyActiveAPIConfig(doc)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"active_id": string(doc.ActiveID),
		"id":        string(p.ID),
	})
}

// PostAPIConfigActivate switches the active profile.
// Body: {id}
func (h *Handlers) PostAPIConfigActivate(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_profile_id")
		return
	}
	doc, err := h.loadAPIConfigsDoc()
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "load_api_config_failed", err)
		return
	}
	if _, err := doc.SetActive(config.ProfileID(body.ID)); err != nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "api_profile_not_found")
		return
	}
	if err := config.SaveAPIConfigs(h.apiCfgPath, doc); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_api_config_failed", err)
		return
	}
	h.applyActiveAPIConfig(doc)
	h.writeJSON(w, http.StatusOK, map[string]string{"active_id": string(doc.ActiveID)})
}

// DeleteAPIConfigProfile removes a profile (the last one cannot be removed).
func (h *Handlers) DeleteAPIConfigProfile(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_profile_id")
		return
	}
	doc, err := h.loadAPIConfigsDoc()
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "load_api_config_failed", err)
		return
	}
	wasActive := string(doc.ActiveID) == id
	if err := doc.Remove(config.ProfileID(id)); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "api_profile_delete_failed", err.Error())
		return
	}
	if err := config.SaveAPIConfigs(h.apiCfgPath, doc); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_api_config_failed", err)
		return
	}
	if wasActive {
		h.applyActiveAPIConfig(doc)
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"active_id": string(doc.ActiveID)})
}

// PostAPIConfigModels fetches the model list for the endpoint described in the
// body (so the UI can list models before saving). If the body has no base_url it
// falls back to the active profile.
// Body: {base_url?, url_strict?, api_key?}
func (h *Handlers) PostAPIConfigModels(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BaseURL   string `json:"base_url"`
		URLStrict bool   `json:"url_strict"`
		APIKey    string `json:"api_key"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	cfg := &config.APIConfig{
		BaseURL:   strings.TrimSpace(body.BaseURL),
		URLStrict: body.URLStrict,
		APIKey:    body.APIKey,
	}
	if cfg.BaseURL == "" {
		if h.apiCfg != nil {
			cfg.BaseURL = h.apiCfg.BaseURL
			cfg.URLStrict = h.apiCfg.URLStrict
			if cfg.APIKey == "" {
				cfg.APIKey = h.apiCfg.APIKey
			}
		}
	}
	if cfg.BaseURL == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "api_base_url_required")
		return
	}

	models, err := llm.ListModels(r.Context(), cfg)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadGateway, "api_models_failed", err.Error())
		return
	}
	sort.SliceStable(models, func(i, j int) bool {
		return strings.ToLower(models[i].ID) < strings.ToLower(models[j].ID)
	})
	h.writeJSON(w, http.StatusOK, map[string]any{"models": models, "count": len(models)})
}

// applyActiveAPIConfig copies the active profile into the live config so running
// tasks pick up the switch immediately.
func (h *Handlers) applyActiveAPIConfig(doc *config.APIConfigsFile) {
	p := doc.Active()
	if p == nil {
		return
	}
	flat := p.ToAPIConfig()
	llm.EnsureContextBudget(&flat)
	h.apiCfg = &flat
}

