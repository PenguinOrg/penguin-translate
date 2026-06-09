package translate

import (
	"log"
	"strings"
	"sync"

	"translation-overlay/internal/feature/window/infra/cache"
)

const defaultMaxParallel = 4

type BatchOpts struct {
	MaxLines    int
	MaxChars    int
	MaxParallel int
}

type ProgressFn func(results []LineResult)

func BatchOptsFromConfig(maxLines, maxChars, maxParallel int) BatchOpts {
	o := BatchOpts{MaxLines: maxLines, MaxChars: maxChars, MaxParallel: maxParallel}
	if o.MaxLines <= 0 {
		o.MaxLines = 8
	}
	if o.MaxLines > 50 {
		o.MaxLines = 50
	}
	if o.MaxChars <= 0 {
		o.MaxChars = 3500
	}
	if o.MaxParallel <= 0 {
		o.MaxParallel = defaultMaxParallel
	}
	if o.MaxParallel > 8 {
		o.MaxParallel = 8
	}
	return o
}

func ResolveKnown(lines []string, store *cache.Store) ([]LineResult, []int) {
	out := make([]LineResult, len(lines))
	var pending []int
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if ShouldSkipLine(line) {
			continue
		}
		if ShouldSkipTranslation(line) {
			out[i] = LineResult{En: line}
			continue
		}
		if res, ok := getCached(store, line); ok && !IsRefusal(res.En) {
			out[i] = res
			continue
		}
		pending = append(pending, i)
	}
	return out, pending
}

func PendingTranslation(lines []string, results []LineResult) bool {
	if len(results) < len(lines) {
		return true
	}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || ShouldSkipLine(line) || ShouldSkipTranslation(line) {
			continue
		}
		if strings.TrimSpace(results[i].En) == "" {
			return true
		}
	}
	return false
}

func TranslateLines(
	lines []string,
	sourceLang string,
	tr Translator,
	store *cache.Store,
	opts BatchOpts,
	onProgress ProgressFn,
) ([]LineResult, error) {
	out := make([]LineResult, len(lines))
	var pendingIdx []int
	var pendingText []string

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if ShouldSkipLine(line) || ShouldSkipTranslation(line) {
			out[i] = LineResult{En: line}
			continue
		}
		if res, ok := getCached(store, line); ok && !IsRefusal(res.En) {
			out[i] = res
			continue
		}
		pendingIdx = append(pendingIdx, i)
		pendingText = append(pendingText, line)
	}

	if len(pendingText) == 0 {
		if onProgress != nil {
			onProgress(append([]LineResult(nil), out...))
		}
		return out, nil
	}

	fireProgress := func() {
		if onProgress == nil {
			return
		}
		cp := append([]LineResult(nil), out...)
		onProgress(cp)
	}

	fireProgress()

	notify := func(part []LineResult, off int) {
		for j := range part {
			idx := pendingIdx[off+j]
			out[idx] = part[j]
			putCached(store, pendingText[off+j], part[j])
		}
		fireProgress()
	}

	batch, err := translatePendingParallel(tr, pendingText, sourceLang, opts, notify)
	if err != nil {
		return nil, err
	}
	for j, idx := range pendingIdx {
		out[idx] = batch[j]
		putCached(store, pendingText[j], batch[j])
	}
	return out, nil
}

func splitTextChunks(texts []string, opts BatchOpts) [][]string {
	if len(texts) == 0 {
		return nil
	}
	maxLines := opts.MaxLines
	maxChars := opts.MaxChars
	if maxLines <= 0 {
		maxLines = 8
	}
	if maxChars <= 0 {
		maxChars = 3500
	}

	var chunks [][]string
	var cur []string
	curChars := 0
	flush := func() {
		if len(cur) > 0 {
			chunks = append(chunks, cur)
			cur = nil
			curChars = 0
		}
	}
	for _, t := range texts {
		tLen := len(t)
		if len(cur) > 0 && (len(cur) >= maxLines || curChars+tLen > maxChars) {
			flush()
		}
		cur = append(cur, t)
		curChars += tLen + 1
	}
	flush()
	return chunks
}

func maxParallelWorkers(n, limit int) int {
	if limit <= 0 {
		limit = defaultMaxParallel
	}
	if n < limit {
		return n
	}
	return limit
}

type chunkDone struct {
	off  int
	part []LineResult
	err  error
}

func translatePendingParallel(
	tr Translator,
	texts []string,
	sourceLang string,
	opts BatchOpts,
	onChunk func(part []LineResult, off int),
) ([]LineResult, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if opts.MaxLines == 1 {
		return translateLinesParallel(tr, texts, sourceLang, opts.MaxParallel, onChunk)
	}

	chunks := splitTextChunks(texts, opts)
	out := make([]LineResult, len(texts))
	if len(chunks) == 1 {
		part, err := translatePendingOne(tr, chunks[0], sourceLang)
		if err != nil {
			return nil, err
		}
		copy(out, part)
		if onChunk != nil {
			onChunk(part, 0)
		}
		return out, nil
	}

	par := maxParallelWorkers(len(chunks), opts.MaxParallel)
	sem := make(chan struct{}, par)
	results := make(chan chunkDone, len(chunks))

	off := 0
	type job struct {
		off   int
		chunk []string
	}
	jobs := make([]job, len(chunks))
	for i, chunk := range chunks {
		jobs[i] = job{off: off, chunk: chunk}
		off += len(chunk)
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			part, err := translatePendingOne(tr, j.chunk, sourceLang)
			results <- chunkDone{off: j.off, part: part, err: err}
		}(j)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		copy(out[res.off:], res.part)
		if onChunk != nil {
			onChunk(res.part, res.off)
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func translateLinesParallel(
	tr Translator,
	texts []string,
	sourceLang string,
	maxPar int,
	onChunk func(part []LineResult, off int),
) ([]LineResult, error) {
	out := make([]LineResult, len(texts))
	par := maxParallelWorkers(len(texts), maxPar)
	sem := make(chan struct{}, par)
	results := make(chan chunkDone, len(texts))

	var wg sync.WaitGroup
	for i, t := range texts {
		wg.Add(1)
		go func(i int, t string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := tr.ToTargetLine(t, sourceLang)
			if err != nil {
				results <- chunkDone{off: i, err: err}
				return
			}
			results <- chunkDone{off: i, part: []LineResult{res}}
		}(i, t)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		out[res.off] = res.part[0]
		if onChunk != nil {
			onChunk(res.part, res.off)
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func translatePendingOne(tr Translator, texts []string, sourceLang string) ([]LineResult, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if len(texts) == 1 {
		res, err := tr.ToTargetLine(texts[0], sourceLang)
		if err != nil {
			return nil, err
		}
		return []LineResult{res}, nil
	}

	batch, err := tr.ToTargetBatch(texts, sourceLang)
	if err == nil {
		return batch, nil
	}

	log.Printf("window-translate: batch of %d lines failed (%v), splitting", len(texts), err)
	if len(texts) == 2 {
		left, err1 := translatePendingOne(tr, texts[:1], sourceLang)
		right, err2 := translatePendingOne(tr, texts[1:], sourceLang)
		if err1 != nil {
			return nil, err1
		}
		if err2 != nil {
			return nil, err2
		}
		return append(left, right...), nil
	}

	mid := len(texts) / 2
	var left, right []LineResult
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		left, err1 = translatePendingOne(tr, texts[:mid], sourceLang)
	}()
	go func() {
		defer wg.Done()
		right, err2 = translatePendingOne(tr, texts[mid:], sourceLang)
	}()
	wg.Wait()
	if err1 != nil {
		return nil, err1
	}
	if err2 != nil {
		return nil, err2
	}
	return append(left, right...), nil
}
