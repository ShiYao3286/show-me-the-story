package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"showmethestory/internal/fsutil"
)

// ProfileID is the stable identifier of one saved API endpoint profile.
// The active profile's fields are mirrored into the top-level APIConfig so that
// every existing caller keeps working unchanged.
type ProfileID string

// APIProfile is one saved endpoint (base URL + key + model + limits).
// Multiple profiles let an author switch between providers, or between several
// keys/ports of the same provider, without retyping anything.
type APIProfile struct {
	ID         ProfileID `json:"id"`
	Name       string    `json:"name"`
	Provider   string    `json:"provider,omitempty"` // preset id this came from, e.g. "openai"
	APIKey     string    `json:"api_key"`
	BaseURL    string    `json:"base_url"`
	URLStrict  bool      `json:"url_strict,omitempty"`
	Model      string    `json:"model"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	HTTPTimeoutSeconds  int `json:"http_timeout_seconds,omitempty"`
	ContextBudgetTokens int `json:"context_budget_tokens,omitempty"`
}

// ToAPIConfig projects a profile onto the flat config the rest of the app reads.
func (p APIProfile) ToAPIConfig() APIConfig {
	c := APIConfig{
		APIKey:              p.APIKey,
		BaseURL:             p.BaseURL,
		URLStrict:           p.URLStrict,
		Model:               p.Model,
		MaxTokens:           p.MaxTokens,
		HTTPTimeoutSeconds:  p.HTTPTimeoutSeconds,
		ContextBudgetTokens: p.ContextBudgetTokens,
	}
	applyAPIConfigDefaults(&c)
	return c
}

// FromAPIConfig builds a profile from the flat config.
func FromAPIConfig(id ProfileID, name, provider string, c *APIConfig) APIProfile {
	p := APIProfile{
		ID:                  id,
		Name:                name,
		Provider:            provider,
		APIKey:              c.APIKey,
		BaseURL:             c.BaseURL,
		URLStrict:           c.URLStrict,
		Model:               c.Model,
		MaxTokens:           c.MaxTokens,
		HTTPTimeoutSeconds:  c.HTTPTimeoutSeconds,
		ContextBudgetTokens: c.ContextBudgetTokens,
	}
	return p
}

// APIConfigsFile is the on-disk multi-profile document.
//
// Backwards compatibility: a legacy api.json (the flat APIConfig written by
// older builds) is still accepted by LoadAPIConfigs, which wraps it into a
// single profile. Writes always use the new shape.
type APIConfigsFile struct {
	Version  int          `json:"version"`
	ActiveID ProfileID    `json:"active_id"`
	Profiles []APIProfile `json:"profiles"`
}

const apiConfigsFileVersion = 1

// DefaultProfileID is used when a legacy api.json is migrated.
const DefaultProfileID ProfileID = "default"

func applyAPIConfigDefaults(c *APIConfig) {
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultMaxTokens
	}
	if c.HTTPTimeoutSeconds <= 0 {
		c.HTTPTimeoutSeconds = DefaultHTTPTimeoutSeconds
	}
	if c.ContextBudgetTokens <= 0 {
		c.ContextBudgetTokens = DefaultContextBudgetTokens
	}
}

// LoadAPIConfigs reads api.json in either shape and always returns at least one
// profile plus a valid active id.
func LoadAPIConfigs(path string) (*APIConfigsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			doc := &APIConfigsFile{
				Version:  apiConfigsFileVersion,
				ActiveID: DefaultProfileID,
				Profiles: []APIProfile{FromAPIConfig(DefaultProfileID, "", "", DefaultAPIConfig())},
			}
			if saveErr := SaveAPIConfigs(path, doc); saveErr != nil {
				return nil, fmt.Errorf("创建默认API配置文件失败: %w", saveErr)
			}
			return doc, nil
		}
		return nil, fmt.Errorf("读取API配置文件失败: %w", err)
	}

	// New shape?
	var doc APIConfigsFile
	if err := json.Unmarshal(data, &doc); err == nil && len(doc.Profiles) > 0 {
		normalizeAPIConfigs(&doc)
		return &doc, nil
	}

	// Legacy flat shape.
	var legacy APIConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("解析API配置文件失败: %w", err)
	}
	applyAPIConfigDefaults(&legacy)
	doc = APIConfigsFile{
		Version:  apiConfigsFileVersion,
		ActiveID: DefaultProfileID,
		Profiles: []APIProfile{FromAPIConfig(DefaultProfileID, "", "", &legacy)},
	}
	return &doc, nil
}

func normalizeAPIConfigs(doc *APIConfigsFile) {
	if doc.Version <= 0 {
		doc.Version = apiConfigsFileVersion
	}
	seen := map[ProfileID]bool{}
	kept := doc.Profiles[:0]
	for i := range doc.Profiles {
		p := &doc.Profiles[i]
		if p.ID == "" || seen[p.ID] {
			p.ID = ProfileID(fmt.Sprintf("p%d", i+1))
			for seen[p.ID] {
				p.ID = ProfileID(string(p.ID) + "x")
			}
		}
		seen[p.ID] = true
		if p.MaxTokens <= 0 {
			p.MaxTokens = DefaultMaxTokens
		}
		if p.HTTPTimeoutSeconds <= 0 {
			p.HTTPTimeoutSeconds = DefaultHTTPTimeoutSeconds
		}
		if p.ContextBudgetTokens <= 0 {
			p.ContextBudgetTokens = DefaultContextBudgetTokens
		}
		kept = append(kept, *p)
	}
	doc.Profiles = kept
	if len(doc.Profiles) == 0 {
		doc.Profiles = []APIProfile{FromAPIConfig(DefaultProfileID, "", "", DefaultAPIConfig())}
	}
	if doc.ActiveID == "" || !seen[doc.ActiveID] {
		doc.ActiveID = doc.Profiles[0].ID
	}
}

// Active returns the active profile, or the first one if the id is stale.
func (d *APIConfigsFile) Active() *APIProfile {
	for i := range d.Profiles {
		if d.Profiles[i].ID == d.ActiveID {
			return &d.Profiles[i]
		}
	}
	if len(d.Profiles) > 0 {
		d.ActiveID = d.Profiles[0].ID
		return &d.Profiles[0]
	}
	return nil
}

// Find returns the profile with the given id.
func (d *APIConfigsFile) Find(id ProfileID) *APIProfile {
	for i := range d.Profiles {
		if d.Profiles[i].ID == id {
			return &d.Profiles[i]
		}
	}
	return nil
}

// Upsert inserts or replaces a profile by id and returns the id that was stored.
// When p.ID is empty a fresh id is allocated; callers must use the returned value
// because p is taken by value.
func (d *APIConfigsFile) Upsert(p APIProfile) ProfileID {
	if p.ID == "" {
		p.ID = d.nextID()
	}
	if p.MaxTokens <= 0 {
		p.MaxTokens = DefaultMaxTokens
	}
	if p.HTTPTimeoutSeconds <= 0 {
		p.HTTPTimeoutSeconds = DefaultHTTPTimeoutSeconds
	}
	if p.ContextBudgetTokens <= 0 {
		p.ContextBudgetTokens = DefaultContextBudgetTokens
	}
	for i := range d.Profiles {
		if d.Profiles[i].ID == p.ID {
			d.Profiles[i] = p
			return p.ID
		}
	}
	d.Profiles = append(d.Profiles, p)
	return p.ID
}

func (d *APIConfigsFile) nextID() ProfileID {
	used := map[string]bool{}
	for _, p := range d.Profiles {
		used[string(p.ID)] = true
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("p%d", i)
		if !used[id] {
			return ProfileID(id)
		}
	}
}

// Remove deletes a profile. Removing the active one activates the first
// remaining profile. The last profile cannot be removed.
func (d *APIConfigsFile) Remove(id ProfileID) error {
	if len(d.Profiles) <= 1 {
		return fmt.Errorf("至少需要保留一个 API 配置")
	}
	idx := -1
	for i := range d.Profiles {
		if d.Profiles[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("配置不存在")
	}
	d.Profiles = append(d.Profiles[:idx], d.Profiles[idx+1:]...)
	if d.ActiveID == id {
		d.ActiveID = d.Profiles[0].ID
	}
	return nil
}

// SetActive switches the active profile and returns it.
func (d *APIConfigsFile) SetActive(id ProfileID) (*APIProfile, error) {
	p := d.Find(id)
	if p == nil {
		return nil, fmt.Errorf("配置不存在")
	}
	d.ActiveID = id
	return p, nil
}

// SortProfiles keeps a stable, human-friendly order (by name, then id).
func (d *APIConfigsFile) SortProfiles() {
	sort.SliceStable(d.Profiles, func(i, j int) bool {
		ni, nj := strings.ToLower(d.Profiles[i].Name), strings.ToLower(d.Profiles[j].Name)
		if ni != nj {
			return ni < nj
		}
		return d.Profiles[i].ID < d.Profiles[j].ID
	})
}

// SaveAPIConfigs writes the multi-profile document atomically.
func SaveAPIConfigs(path string, doc *APIConfigsFile) error {
	if doc.Version <= 0 {
		doc.Version = apiConfigsFileVersion
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data)
}

// SaveAPIConfigsFlat is kept for callers that only have a flat config: it
// updates the active profile in place, preserving all other profiles.
func SaveAPIConfigsFlat(path string, flat *APIConfig) (*APIConfigsFile, error) {
	doc, err := LoadAPIConfigs(path)
	if err != nil {
		return nil, err
	}
	active := doc.Active()
	id := DefaultProfileID
	if active != nil {
		id = active.ID
	}
	name, provider := "", ""
	if active != nil {
		name, provider = active.Name, active.Provider
	}
	applyAPIConfigDefaults(flat)
	doc.ActiveID = doc.Upsert(FromAPIConfig(id, name, provider, flat))
	if err := SaveAPIConfigs(path, doc); err != nil {
		return nil, err
	}
	return doc, nil
}
