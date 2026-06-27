package caption

import (
	"strings"
	"testing"
)

func TestBuildCaptionContextUsesSelectionAndNames(t *testing.T) {
	got := BuildCaptionContext(true, []string{"ja", "zh"}, "Aria, Nightfall")
	for _, want := range []string{
		"Expected spoken languages: Japanese, Chinese.",
		"Common VR/gaming vocabulary:",
		"Names and terms likely in this session: Aria, Nightfall.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("context missing %q\n got: %s", want, got)
		}
	}
	if BuildCaptionContext(false, []string{"ja"}, "x") != "" {
		t.Error("disabled context should be empty")
	}
}

func TestRollingWindowAugmentsContext(t *testing.T) {
	ResetHistory()
	t.Cleanup(ResetHistory)

	base := BuildCaptionContext(true, []string{"ja"}, "")
	if got := withRecentTranscript(base); got != base {
		t.Errorf("no history should leave context unchanged")
	}
	if withRecentTranscript("") != "" {
		t.Error("disabled context must stay empty even with history")
	}

	pushHistory("カナデは来た", "Kanade arrived")
	pushHistory("ミラーワールド", "the mirror world")

	asr := withRecentTranscript(base)
	if !strings.Contains(asr, "Recent dialogue") || !strings.Contains(asr, "カナデは来た") {
		t.Errorf("ASR context missing rolling history: %s", asr)
	}
	pair := recentPairContext()
	if !strings.Contains(pair, "カナデは来た => Kanade arrived") {
		t.Errorf("pair context missing source=>english: %s", pair)
	}
}
