package caption

import "testing"

func TestEnrichChineseAlignsPinyin(t *testing.T) {
	e := EnrichLine("你好世界", "zh")
	want := []RubyPair{{"你", "nǐ"}, {"好", "hǎo"}, {"世", "shì"}, {"界", "jiè"}}
	if len(e.ZhPinyin) != len(want) {
		t.Fatalf("got %d pairs, want %d: %+v", len(e.ZhPinyin), len(want), e.ZhPinyin)
	}
	for i, w := range want {
		if e.ZhPinyin[i] != w {
			t.Errorf("pair %d = %+v, want %+v", i, e.ZhPinyin[i], w)
		}
	}
}

func TestEnrichChineseSkipsNonHan(t *testing.T) {
	e := EnrichLine("你好,世界", "zh")
	if len(e.ZhPinyin) != 5 {
		t.Fatalf("got %d pairs, want 5: %+v", len(e.ZhPinyin), e.ZhPinyin)
	}
	want := []RubyPair{{"你", "nǐ"}, {"好", "hǎo"}, {",", ""}, {"世", "shì"}, {"界", "jiè"}}
	for i, w := range want {
		if e.ZhPinyin[i] != w {
			t.Errorf("pair %d = %+v, want %+v", i, e.ZhPinyin[i], w)
		}
	}
}
