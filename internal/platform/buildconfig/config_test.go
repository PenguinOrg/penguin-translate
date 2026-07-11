package buildconfig

import "testing"

func TestPenguinBase(t *testing.T) {
	original := PenguinBaseURL
	t.Cleanup(func() { PenguinBaseURL = original })

	PenguinBaseURL = "  https://penguin.example///  "
	if got := PenguinBase(); got != "https://penguin.example" {
		t.Fatalf("PenguinBase() = %q, want normalized build value", got)
	}
}
