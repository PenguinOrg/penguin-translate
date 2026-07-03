package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"translation-overlay/internal/platform/domain"
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

func TestUpdateSerializesConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	repo := NewJSONRepository(path)
	if err := repo.Save(Default()); err != nil {
		t.Fatal(err)
	}

	const n = 32
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Update(func(st *domain.Settings) error {
				st.Window.SkipWords = append(st.Window.SkipWords, fmt.Sprintf("w%d", i))
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if got, _ := repo.Load(); len(got.Window.SkipWords) != n {
		t.Fatalf("cache lost updates: %d skip words, want %d", len(got.Window.SkipWords), n)
	}
	fresh, err := NewJSONRepository(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Window.SkipWords) != n {
		t.Fatalf("disk lost updates: %d skip words, want %d", len(fresh.Window.SkipWords), n)
	}
}

func TestUpdateSurfacesErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{corrupt`), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := NewJSONRepository(path)
	if _, err := repo.Update(func(*domain.Settings) error { return nil }); err == nil {
		t.Fatal("Update over an unreadable settings.json succeeded; want error")
	}

	repo = NewJSONRepository(filepath.Join(t.TempDir(), "settings.json"))
	st := Default()
	st.OpenAIAPIKey = "keep"
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("mutate failed")
	if _, err := repo.Update(func(st *domain.Settings) error {
		st.OpenAIAPIKey = "clobbered"
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("Update mutate error = %v, want %v", err, boom)
	}
	if got, _ := repo.Load(); got.OpenAIAPIKey != "keep" {
		t.Fatalf("failed mutate persisted anyway: %q", got.OpenAIAPIKey)
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONRepository(filepath.Join(dir, "settings.json"))
	if err := repo.Save(Default()); err != nil {
		t.Fatal(err)
	}
	// Second save must rename over the existing file.
	if err := repo.Save(Default()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory after Save = %v, want [settings.json]", names)
	}
}
