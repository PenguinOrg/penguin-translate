package translate

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"translation-overlay/internal/platform/cloudapi"
)

type batchLineIn struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

type batchLineOut struct {
	ID    int    `json:"id"`
	En    string `json:"en"`
	Roman string `json:"roman"`
}

func (c *Client) ToTargetBatch(lines []string, sourceLang string) ([]LineResult, error) {
	out, missing, err := c.toTargetBatch(lines, sourceLang)
	if err != nil && len(missing) == len(lines) {
		return nil, err
	}
	if len(missing) > 0 {
		log.Printf("window-translate: batch mapped %d/%d lines, filling %d missing",
			len(lines)-len(missing), len(lines), len(missing))
		for _, i := range missing {
			res, e2 := c.ToTargetLine(lines[i], sourceLang)
			if e2 != nil {
				return nil, e2
			}
			out[i] = res
		}
	}
	return out, nil
}

func (c *Client) toTargetBatch(lines []string, sourceLang string) ([]LineResult, []int, error) {
	if len(lines) == 0 {
		return nil, nil, nil
	}

	joined := strings.Join(lines, "\n")

	items := make([]batchLineIn, len(lines))
	for i, line := range lines {
		items[i] = batchLineIn{ID: i, Text: line}
	}
	inJSON, err := json.Marshal(items)
	if err != nil {
		return nil, allIndices(len(lines)), err
	}

	n := len(lines)
	sys := batchSystemPrompt(sourceLangName(detectLang(joined, sourceLang)), c.Target, n)
	user := string(inJSON)

	maxTok := 600 + len(lines)*180
	if maxTok > 8000 {
		maxTok = 8000
	}

	content, err := cloudapi.ChatCompletionWith(c.Creds, c.Model, sys, user, cloudapi.ChatOptions{
		Temperature:    0.2,
		ResponseFormat: "json_object",
		MaxTokens:      maxTok,
	})
	if err != nil {
		return nil, allIndices(len(lines)), err
	}
	content = stripCodeFence(strings.TrimSpace(content))
	if content == "" {
		return nil, allIndices(len(lines)), fmt.Errorf("model returned empty content (may not support JSON mode — try gpt-4o-mini or max batch lines = 1)")
	}
	out, missing := mapBatchResponse(content, lines)
	if len(missing) == 0 {
		return out, nil, nil
	}
	if allResultsEmpty(out) {
		return nil, missing, fmt.Errorf("batch parse: no mapped lines (got %s)", truncate(content, 200))
	}
	return out, missing, fmt.Errorf("batch incomplete: %d/%d lines mapped", n-len(missing), n)
}

func allResultsEmpty(rr []LineResult) bool {
	for _, r := range rr {
		if strings.TrimSpace(r.En) != "" {
			return false
		}
	}
	return true
}

func mapBatchResponse(content string, lines []string) ([]LineResult, []int) {
	n := len(lines)
	out := make([]LineResult, n)

	var indexed struct {
		Items []batchLineOut `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &indexed); err == nil && len(indexed.Items) > 0 {
		for _, it := range indexed.Items {
			if it.ID < 0 || it.ID >= n {
				continue
			}
			en := StripLeadingPunctuation(strings.TrimSpace(it.En))
			if en == "" {
				continue
			}
			if IsRefusal(en) {
				en = StripLeadingPunctuation(lines[it.ID])
			}
			out[it.ID] = LineResult{
				En:    en,
				Roman: strings.TrimSpace(it.Roman),
			}
		}
		return out, missingResultIndices(out)
	}

	var alt struct {
		Translations []struct {
			ID          int    `json:"id"`
			En          string `json:"en"`
			Roman       string `json:"roman"`
			Pinyin      string `json:"pinyin"`
			Text        string `json:"text"`
			Translation string `json:"translation"`
		} `json:"translations"`
	}
	if err := json.Unmarshal([]byte(content), &alt); err == nil && len(alt.Translations) > 0 {
		for _, it := range alt.Translations {
			if it.ID < 0 || it.ID >= n {
				continue
			}
			en := StripLeadingPunctuation(strings.TrimSpace(it.En))
			if en == "" {
				en = StripLeadingPunctuation(strings.TrimSpace(it.Text))
			}
			if en == "" {
				en = StripLeadingPunctuation(strings.TrimSpace(it.Translation))
			}
			if en == "" {
				continue
			}
			if IsRefusal(en) {
				en = StripLeadingPunctuation(lines[it.ID])
			}
			roman := strings.TrimSpace(it.Roman)
			if roman == "" {
				roman = strings.TrimSpace(it.Pinyin)
			}
			out[it.ID] = LineResult{En: en, Roman: roman}
		}
		return out, missingResultIndices(out)
	}

	var legacy struct {
		Translations []string `json:"translations"`
	}
	if err := json.Unmarshal([]byte(content), &legacy); err == nil && len(legacy.Translations) == n {
		for i, raw := range legacy.Translations {
			en := StripLeadingPunctuation(strings.TrimSpace(raw))
			if IsRefusal(en) {
				en = StripLeadingPunctuation(lines[i])
			}
			out[i] = LineResult{En: en}
		}
		return out, missingResultIndices(out)
	}

	return out, allIndices(n)
}

func missingResultIndices(out []LineResult) []int {
	var miss []int
	for i, r := range out {
		if strings.TrimSpace(r.En) == "" {
			miss = append(miss, i)
		}
	}
	return miss
}

func allIndices(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}
