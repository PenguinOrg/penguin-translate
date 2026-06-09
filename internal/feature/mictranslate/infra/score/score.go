package score

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var (
	punctJA = regexp.MustCompile(`[、。「」『』【】〈〉《》〔〕（）()！!？\?\.,\s・:;"']+`)
	punctZH = regexp.MustCompile(`[，。！？、．…\s"'「」『』（）()\[\]{}:;,.!?]+`)
	punctKO = regexp.MustCompile(`[，。！？、．…\s"'「」『』（）()\[\]{}:;,.!?·―～~\-]+`)
)

type FuriganaToken struct {
	Surface string `json:"surface"`
	Reading string `json:"reading"`
}

type Request struct {
	Expected  string
	Spoken    string
	Threshold int
	Furigana  []FuriganaToken
	Lang      string
}

type Response struct {
	Score                  float64 `json:"score"`
	Accepted               bool    `json:"accepted"`
	ThresholdUsed          int     `json:"threshold_used"`
	ScoreStrict            float64 `json:"score_strict"`
	NormalizedExpected     string  `json:"normalized_expected"`
	NormalizedSpoken       string  `json:"normalized_spoken"`
	SpokenHighlightBase    string  `json:"spoken_highlight_base"`
	SpokenMatchRanges      [][]int `json:"spoken_match_ranges"`
	ExpectedReading        string  `json:"expected_reading,omitempty"`
	NormalizedExpectedRead string  `json:"normalized_expected_reading,omitempty"`
}

func Evaluate(req Request) Response {
	req.Expected = strings.TrimSpace(req.Expected)
	req.Spoken = strings.TrimSpace(req.Spoken)
	if req.Threshold <= 0 {
		req.Threshold = 100
	}
	switch strings.ToLower(strings.TrimSpace(req.Lang)) {
	case "zh", "cn", "chinese":
		return evalZH(req)
	case "ko", "kr", "korean":
		return evalKO(req)
	default:
		return evalJA(req)
	}
}

func evalJA(req Request) Response {
	scoreStrict := ratio(normalizeJA(req.Expected), normalizeJA(req.Spoken))
	expectedReading := expectedKanaFromFurigana(req.Furigana)
	useReading := expectedReading != "" && normalizeJA(expectedReading) != ""
	scorePrimary := scoreStrict
	highlightExpected := req.Expected
	if useReading {
		scorePrimary = ratio(normalizeJA(expectedReading), normalizeJA(req.Spoken))
		highlightExpected = expectedReading
	}
	base, ranges := spokenMatchRangesJA(highlightExpected, req.Spoken)
	out := Response{
		Score:               round1(scorePrimary),
		Accepted:            scorePrimary >= float64(req.Threshold),
		ThresholdUsed:       req.Threshold,
		ScoreStrict:         round1(scoreStrict),
		NormalizedExpected:  normalizeJA(req.Expected),
		NormalizedSpoken:    normalizeJA(req.Spoken),
		SpokenHighlightBase: base,
		SpokenMatchRanges:   ranges,
	}
	if useReading {
		out.ExpectedReading = expectedReading
		out.NormalizedExpectedRead = normalizeJA(expectedReading)
	}
	return out
}

func evalZH(req Request) Response {
	scorePrimary := ratio(normalizeZH(req.Expected), normalizeZH(req.Spoken))
	base, ranges := spokenMatchRangesMapped(req.Expected, req.Spoken, punctZH)
	return Response{
		Score:               round1(scorePrimary),
		Accepted:            scorePrimary >= float64(req.Threshold),
		ThresholdUsed:       req.Threshold,
		ScoreStrict:         round1(scorePrimary),
		NormalizedExpected:  normalizeZH(req.Expected),
		NormalizedSpoken:    normalizeZH(req.Spoken),
		SpokenHighlightBase: base,
		SpokenMatchRanges:   ranges,
	}
}

func evalKO(req Request) Response {
	scorePrimary := ratio(normalizeKO(req.Expected), normalizeKO(req.Spoken))
	base, ranges := spokenMatchRangesMapped(req.Expected, req.Spoken, punctKO)
	return Response{
		Score:               round1(scorePrimary),
		Accepted:            scorePrimary >= float64(req.Threshold),
		ThresholdUsed:       req.Threshold,
		ScoreStrict:         round1(scorePrimary),
		NormalizedExpected:  normalizeKO(req.Expected),
		NormalizedSpoken:    normalizeKO(req.Spoken),
		SpokenHighlightBase: base,
		SpokenMatchRanges:   ranges,
	}
}

func expectedKanaFromFurigana(tokens []FuriganaToken) string {
	if len(tokens) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range tokens {
		r := strings.TrimSpace(t.Reading)
		if r == "" {
			r = t.Surface
		}
		b.WriteString(r)
	}
	return b.String()
}

func normalizeJA(s string) string {
	if s == "" {
		return ""
	}
	s = norm.NFKC.String(s)
	s = kataToHira(s)
	return punctJA.ReplaceAllString(s, "")
}

func normalizeZH(s string) string {
	if s == "" {
		return ""
	}
	return punctZH.ReplaceAllString(norm.NFKC.String(s), "")
}

func normalizeKO(s string) string {
	if s == "" {
		return ""
	}
	return punctKO.ReplaceAllString(norm.NFKC.String(s), "")
}

func kataToHira(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x30A1 && r <= 0x30F6 {
			r -= 0x60
		}
		b.WriteRune(r)
	}
	return b.String()
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

func ratio(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 100
	}
	lcs := lcsLen([]rune(a), []rune(b))
	denom := len(a) + len(b)
	if denom == 0 {
		return 0
	}
	return 200 * float64(lcs) / float64(denom)
}

func lcsLen(a, b []rune) int {
	m, n := len(a), len(b)
	if m == 0 || n == 0 {
		return 0
	}
	prev := make([]int, n+1)
	cur := make([]int, n+1)
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return prev[n]
}

func spokenMatchRangesJA(expected, spoken string) (string, [][]int) {
	return spokenMatchRangesMapped(expected, spoken, punctJA)
}

func spokenMatchRangesMapped(expected, spoken string, punct *regexp.Regexp) (string, [][]int) {
	s := norm.NFKC.String(strings.TrimSpace(spoken))
	normExp, _ := normalizeMapped(expected, punct, true)
	normSp, spIdx := normalizeMapped(spoken, punct, true)
	if normExp == "" || normSp == "" {
		return s, nil
	}
	matchedNorm := make([]bool, len(normSp))
	ops := sequenceOps([]rune(normExp), []rune(normSp))
	for _, op := range ops {
		if op.tag != "equal" {
			continue
		}
		for k := op.j1; k < op.j2; k++ {
			matchedNorm[k] = true
		}
	}
	matchedS := make([]bool, len(s))
	for k, ok := range matchedNorm {
		if ok && k < len(spIdx) && spIdx[k] < len(matchedS) {
			matchedS[spIdx[k]] = true
		}
	}
	var ranges [][]int
	i := 0
	for i < len(matchedS) {
		if !matchedS[i] {
			i++
			continue
		}
		j := i + 1
		for j < len(matchedS) && matchedS[j] {
			j++
		}
		ranges = append(ranges, []int{i, j})
		i = j
	}
	if len(ranges) == 0 && normSp != "" && normExp == normSp {
		return normSp, [][]int{{0, len(normSp)}}
	}
	return s, ranges
}

func normalizeMapped(text string, punct *regexp.Regexp, jaHira bool) (string, []int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	s0 := norm.NFKC.String(text)
	if jaHira && punct == punctJA {
		s0 = kataToHira(s0)
	}
	var out []rune
	var idxs []int
	runes := []rune(s0)
	i := 0
	for i < len(runes) {
		loc := punct.FindStringIndex(string(runes[i:]))
		if loc != nil && loc[0] == 0 {
			i += len([]rune(string(runes[i:])[loc[0]:loc[1]]))
			continue
		}
		out = append(out, runes[i])
		idxs = append(idxs, i)
		i++
	}
	return string(out), idxs
}

type seqOp struct {
	tag    string
	i1, i2 int
	j1, j2 int
}

func sequenceOps(a, b []rune) []seqOp {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	var ops []seqOp
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			ops = append(ops, seqOp{tag: "equal", i1: i - 1, i2: i, j1: j - 1, j2: j})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			ops = append(ops, seqOp{tag: "insert", j1: j - 1, j2: j})
			j--
		} else {
			ops = append(ops, seqOp{tag: "delete", i1: i - 1, i2: i})
			i--
		}
	}

	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	return ops
}
