package translate

import "testing"

func TestResolveKnownSkipRules(t *testing.T) {
	lines := []string{
		"needs api",
		"",
	}
	results, pending := ResolveKnown(lines, nil)
	if len(results) != len(lines) {
		t.Fatalf("results len: got %d want %d", len(results), len(lines))
	}
	if len(pending) != 1 || pending[0] != 0 {
		t.Fatalf("pending: got %v want [0]", pending)
	}
	if results[0].En != "" {
		t.Fatalf("expected empty result before translation: %q", results[0].En)
	}
}

func TestPendingTranslation(t *testing.T) {
	lines := []string{"done", "wait"}
	results := []LineResult{{En: "ok"}, {}}
	if !PendingTranslation(lines, results) {
		t.Fatal("expected pending")
	}
	results[1] = LineResult{En: "also ok"}
	if PendingTranslation(lines, results) {
		t.Fatal("expected complete")
	}
}

func TestTranslateLinesFiresProgressBeforeRemote(t *testing.T) {
	calls := 0
	var first []LineResult
	_, err := TranslateLines(
		[]string{"alpha", "beta"},
		"ja",
		&stubTranslator{delay: 0},
		nil,
		BatchOpts{MaxLines: 1, MaxParallel: 2},
		func(partial []LineResult) {
			calls++
			if calls == 1 {
				first = append([]LineResult(nil), partial...)
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("progress calls: got %d want >= 2", calls)
	}
	if len(first) != 2 {
		t.Fatalf("first progress len: %d", len(first))
	}
}

type stubTranslator struct {
	delay int
}

func (s *stubTranslator) ToTargetLine(text, sourceLang string) (LineResult, error) {
	return LineResult{En: "en:" + text}, nil
}

func (s *stubTranslator) ToTargetBatch(lines []string, sourceLang string) ([]LineResult, error) {
	out := make([]LineResult, len(lines))
	for i, line := range lines {
		out[i] = LineResult{En: "en:" + line}
	}
	return out, nil
}
