package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRefreshesCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	repo := NewJSONRepository(path)

	st := Default()
	st.OpenAIAPIKey = "first"
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Load(); got.OpenAIAPIKey != "first" {
		t.Fatalf("after first Save, Load = %q, want %q", got.OpenAIAPIKey, "first")
	}

	st.OpenAIAPIKey = "second"
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Load(); got.OpenAIAPIKey != "second" {
		t.Fatalf("Save did not refresh cache: Load = %q, want %q", got.OpenAIAPIKey, "second")
	}
}

func TestLoadServedFromCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	repo := NewJSONRepository(path)

	st := Default()
	st.OpenAIAPIKey = "cached"
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Load(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(`{"openai_api_key":"disk-edit"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.OpenAIAPIKey != "cached" {
		t.Fatalf("Load hit disk instead of cache: got %q, want %q", got.OpenAIAPIKey, "cached")
	}
}

func TestLoadSanitizesCorruptedModelStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st := Default()
	st.MicTranslate.OpenRouterTranscribeModel = "��\x06\x1fH\x07\x00\x00\x10\x02arge-v3-turbo"
	st.Audio.DiarizeModel = "]\bd\x1f8\x05\x00\x00�\x02e-diarize"
	b, err := json.Marshal(struct {
		Practice any `json:"practice"`
		Audio    any `json:"audio"`
	}{st.MicTranslate, st.Audio})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := NewJSONRepository(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !isCleanText(got.MicTranslate.OpenRouterTranscribeModel) {
		t.Errorf("practice transcribe model still corrupt: %q", got.MicTranslate.OpenRouterTranscribeModel)
	}
	if !isCleanText(got.Audio.DiarizeModel) {
		t.Errorf("audio diarize model still corrupt: %q", got.Audio.DiarizeModel)
	}
}

func TestLoadDoesNotAliasCachedReferenceTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	repo := NewJSONRepository(path)

	st := Default()
	st.MicTranslate.Plugins = map[string]json.RawMessage{"a": json.RawMessage(`1`)}
	st.Window.SkipWords = []string{"foo"}
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}

	first, _ := repo.Load()
	first.MicTranslate.Plugins["a"] = json.RawMessage(`999`)
	first.MicTranslate.Plugins["b"] = json.RawMessage(`2`)
	if len(first.Window.SkipWords) > 0 {
		first.Window.SkipWords[0] = "mutated"
	}

	second, _ := repo.Load()
	if string(second.MicTranslate.Plugins["a"]) != "1" {
		t.Fatalf("cached plugin map was aliased: got %q, want %q", second.MicTranslate.Plugins["a"], "1")
	}
	if _, leaked := second.MicTranslate.Plugins["b"]; leaked {
		t.Fatal("inserting into a returned plugin map leaked into the cache")
	}
	if len(second.Window.SkipWords) > 0 && second.Window.SkipWords[0] != "foo" {
		t.Fatalf("cached skip words were aliased: got %q, want %q", second.Window.SkipWords[0], "foo")
	}
}
