package persist

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata golden files")

func TestDefaultSettingsMatchGolden(t *testing.T) {
	got, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "settings.golden.json")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("settings.json shape drifted from %s — re-run with -update if intended.\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	once := Default()
	twice := cloneSettings(once)
	normalize(&twice)
	if !reflect.DeepEqual(once, twice) {
		t.Errorf("normalize is not idempotent:\nonce:  %#v\ntwice: %#v", once, twice)
	}
}
