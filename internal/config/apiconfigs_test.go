package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAPIConfigsCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.json")

	doc, err := LoadAPIConfigs(path)
	if err != nil {
		t.Fatalf("LoadAPIConfigs: %v", err)
	}
	if len(doc.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(doc.Profiles))
	}
	if doc.ActiveID != DefaultProfileID {
		t.Fatalf("expected active %q, got %q", DefaultProfileID, doc.ActiveID)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected api.json to be written: %v", err)
	}
}

// A pre-existing flat api.json (older build) must be adopted, not rejected.
func TestLoadAPIConfigsMigratesLegacyFlatFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.json")
	legacy := APIConfig{
		APIKey:     "sk-legacy",
		BaseURL:    "https://api.deepseek.com",
		Model:      "deepseek-chat",
		MaxTokens:  8192,
		HTTPTimeoutSeconds: 300,
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := LoadAPIConfigs(path)
	if err != nil {
		t.Fatalf("LoadAPIConfigs: %v", err)
	}
	if len(doc.Profiles) != 1 {
		t.Fatalf("expected the legacy file to become 1 profile, got %d", len(doc.Profiles))
	}
	got := doc.Profiles[0]
	if got.APIKey != "sk-legacy" || got.BaseURL != "https://api.deepseek.com" || got.Model != "deepseek-chat" {
		t.Fatalf("legacy fields not preserved: %+v", got)
	}
	if got.HTTPTimeoutSeconds != 300 || got.MaxTokens != 8192 {
		t.Fatalf("legacy limits not preserved: %+v", got)
	}
}

func TestUpsertAllocatesDistinctIDs(t *testing.T) {
	doc := &APIConfigsFile{Version: apiConfigsFileVersion, ActiveID: DefaultProfileID}
	doc.Profiles = []APIProfile{FromAPIConfig(DefaultProfileID, "", "", DefaultAPIConfig())}

	id1 := doc.Upsert(APIProfile{Name: "a", BaseURL: "http://127.0.0.1:1"})
	id2 := doc.Upsert(APIProfile{Name: "b", BaseURL: "http://127.0.0.1:2"})
	if id1 == "" || id2 == "" {
		t.Fatalf("expected non-empty ids, got %q and %q", id1, id2)
	}
	if id1 == id2 {
		t.Fatalf("expected distinct ids, both were %q", id1)
	}
	if doc.Find(id1) == nil || doc.Find(id2) == nil {
		t.Fatalf("upserted profiles are not findable")
	}
}

func TestUpsertReplacesExistingID(t *testing.T) {
	doc := &APIConfigsFile{Version: apiConfigsFileVersion}
	doc.Profiles = []APIProfile{FromAPIConfig(DefaultProfileID, "", "", DefaultAPIConfig())}

	id := doc.Upsert(APIProfile{Name: "first", BaseURL: "http://a"})
	before := len(doc.Profiles)
	sameID := doc.Upsert(APIProfile{ID: id, Name: "second", BaseURL: "http://b"})
	if sameID != id {
		t.Fatalf("expected the same id, got %q want %q", sameID, id)
	}
	if len(doc.Profiles) != before {
		t.Fatalf("replace must not append: %d -> %d", before, len(doc.Profiles))
	}
	if p := doc.Find(id); p == nil || p.Name != "second" || p.BaseURL != "http://b" {
		t.Fatalf("profile not replaced: %+v", p)
	}
}

func TestSetActiveAndRemove(t *testing.T) {
	doc := &APIConfigsFile{Version: apiConfigsFileVersion}
	doc.Profiles = []APIProfile{FromAPIConfig(DefaultProfileID, "", "", DefaultAPIConfig())}
	second := doc.Upsert(APIProfile{Name: "second", BaseURL: "http://b"})

	if _, err := doc.SetActive(second); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if doc.ActiveID != second {
		t.Fatalf("active id not switched: %q", doc.ActiveID)
	}

	if err := doc.Remove(second); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if doc.Find(second) != nil {
		t.Fatalf("removed profile still findable")
	}
	// Removing the active profile must fall back to a remaining one.
	if doc.ActiveID == second || doc.Active() == nil {
		t.Fatalf("active id not reassigned after removal: %q", doc.ActiveID)
	}
}

func TestRemoveRefusesLastProfile(t *testing.T) {
	doc := &APIConfigsFile{Version: apiConfigsFileVersion}
	doc.Profiles = []APIProfile{FromAPIConfig(DefaultProfileID, "", "", DefaultAPIConfig())}
	if err := doc.Remove(DefaultProfileID); err == nil {
		t.Fatal("expected an error when removing the last profile")
	}
	if len(doc.Profiles) != 1 {
		t.Fatalf("last profile must be kept, got %d", len(doc.Profiles))
	}
}

func TestSaveAPIConfigsFlatOnlyTouchesActiveProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.json")
	doc := &APIConfigsFile{
		Version:  apiConfigsFileVersion,
		ActiveID: DefaultProfileID,
		Profiles: []APIProfile{
			FromAPIConfig(DefaultProfileID, "one", "", DefaultAPIConfig()),
			FromAPIConfig("p2", "two", "", DefaultAPIConfig()),
		},
	}
	doc.Profiles[1].BaseURL = "http://keep-me"
	if err := SaveAPIConfigs(path, doc); err != nil {
		t.Fatalf("SaveAPIConfigs: %v", err)
	}

	flat := APIConfig{BaseURL: "http://new", Model: "m", APIKey: "k"}
	if _, err := SaveAPIConfigsFlat(path, &flat); err != nil {
		t.Fatalf("SaveAPIConfigsFlat: %v", err)
	}

	reloaded, err := LoadAPIConfigs(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Profiles) != 2 {
		t.Fatalf("expected 2 profiles to survive, got %d", len(reloaded.Profiles))
	}
	active := reloaded.Active()
	if active == nil || active.BaseURL != "http://new" || active.Model != "m" {
		t.Fatalf("active profile not updated: %+v", active)
	}
	if p2 := reloaded.Find("p2"); p2 == nil || p2.BaseURL != "http://keep-me" {
		t.Fatalf("inactive profile was modified: %+v", p2)
	}
}

func TestProviderPresetsAreWellFormed(t *testing.T) {
	presets := ProviderPresets()
	if len(presets) == 0 {
		t.Fatal("no presets")
	}
	seen := map[string]bool{}
	for _, p := range presets {
		if p.ID == "" || p.Name == "" {
			t.Fatalf("preset missing id/name: %+v", p)
		}
		if seen[p.ID] {
			t.Fatalf("duplicate preset id %q", p.ID)
		}
		seen[p.ID] = true
		// Every preset except the "custom" escape hatch must prefill a base URL.
		if p.ID != "custom" && p.BaseURL == "" {
			t.Fatalf("preset %q has no base_url", p.ID)
		}
	}
	if _, ok := FindProviderPreset("deepseek"); !ok {
		t.Fatal("expected a deepseek preset")
	}
	if _, ok := FindProviderPreset("nope"); ok {
		t.Fatal("unexpected match for an unknown preset id")
	}
}

func TestActiveFallsBackWhenIDIsStale(t *testing.T) {
	doc := &APIConfigsFile{
		Version:  apiConfigsFileVersion,
		ActiveID: "gone",
		Profiles: []APIProfile{FromAPIConfig(DefaultProfileID, "", "", DefaultAPIConfig())},
	}
	p := doc.Active()
	if p == nil {
		t.Fatal("Active() returned nil")
	}
	if doc.ActiveID != DefaultProfileID {
		t.Fatalf("stale active id not repaired: %q", doc.ActiveID)
	}
}
