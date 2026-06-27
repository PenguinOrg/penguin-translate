#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

typedef struct {
    int32_t t, col, row, unk;
    int64_t step;
    int64_t data_ptr;
} ocr_img;

typedef struct {
    float x1, y1, x2, y2, x3, y3, x4, y4;
} ocr_bbox;

static const char *kModelKey = "kj)TGtrK>f]b[Piow.gU+nC@s\"\"\"\"\"\"4";

#define NLINES 2
static const char *kLines[NLINES] = {
    "\xE3\x81\x93\xE3\x82\x8C\xE3\x81\xAF",
    "\xE3\x83\x86\xE3\x82\xB9\xE3\x83\x88",
};
static ocr_bbox kBoxes[NLINES] = {
    {10, 10, 210, 10, 210, 34, 10, 34},
    {10, 50, 210, 50, 210, 74, 10, 74},
};

#define LINE_HANDLE_BASE 0x200

static struct {
    int32_t img_t, col, row;
    int64_t step;
    uint64_t byte_sum;
    int64_t max_lines;
    int model_key_ok;
    int run_count, release_count;
} g;

static void write_record(void) {
    char path[1024];
    DWORD n = GetEnvironmentVariableA("WT_FAKE_OCR_OUT", path, sizeof(path));
    if (n == 0 || n >= sizeof(path)) return;
    FILE *f = fopen(path, "w");
    if (!f) return;
    fprintf(f,
        "{\"img_t\":%d,\"col\":%d,\"row\":%d,\"step\":%lld,\"byte_sum\":%llu,"
        "\"max_lines\":%lld,\"model_key_ok\":%d,\"run_count\":%d,\"release_count\":%d}\n",
        g.img_t, g.col, g.row, (long long)g.step, (unsigned long long)g.byte_sum,
        (long long)g.max_lines, g.model_key_ok, g.run_count, g.release_count);
    fclose(f);
}

int64_t CreateOcrInitOptions(int64_t *ctx) {
    if (ctx) *ctx = 0x1;
    return 0;
}

int64_t OcrInitOptionsSetUseModelDelayLoad(int64_t ctx, int64_t v) {
    (void)ctx; (void)v;
    return 0;
}

int64_t CreateOcrPipeline(const char *model, const char *key, int64_t ctx, int64_t *pipeline) {
    (void)model; (void)ctx;
    g.model_key_ok = (key && strcmp(key, kModelKey) == 0) ? 1 : 0;
    if (pipeline) *pipeline = 0x2;
    write_record();
    return 0;
}

int64_t CreateOcrProcessOptions(int64_t *opt) {
    if (opt) *opt = 0x3;
    return 0;
}

int64_t OcrProcessOptionsSetMaxRecognitionLineCount(int64_t opt, int64_t n) {
    (void)opt;
    g.max_lines = n;
    return 0;
}

int64_t RunOcrPipeline(int64_t pipeline, ocr_img *im, int64_t opt, int64_t *instance) {
    (void)pipeline; (void)opt;
    if (im) {
        g.img_t = im->t;
        g.col = im->col;
        g.row = im->row;
        g.step = im->step;
        g.byte_sum = 0;
        const unsigned char *p = (const unsigned char *)(intptr_t)im->data_ptr;
        if (p && im->step > 0 && im->row > 0) {
            size_t total = (size_t)im->step * (size_t)im->row;
            for (size_t i = 0; i < total; i++) g.byte_sum += p[i];
        }
    }
    g.run_count++;
    if (instance) *instance = 0x100;
    write_record();
    return 0;
}

int64_t GetOcrLineCount(int64_t instance, int64_t *count) {
    (void)instance;
    if (count) *count = NLINES;
    return 0;
}

int64_t GetOcrLine(int64_t instance, int64_t i, int64_t *line) {
    (void)instance;
    if (i < 0 || i >= NLINES) return 1;
    if (line) *line = LINE_HANDLE_BASE + i;
    return 0;
}

int64_t GetOcrLineContent(int64_t line, int64_t *content) {
    int64_t idx = line - LINE_HANDLE_BASE;
    if (idx < 0 || idx >= NLINES) return 1;
    if (content) *content = (int64_t)(intptr_t)kLines[idx];
    return 0;
}

int64_t GetOcrLineBoundingBox(int64_t line, ocr_bbox **box) {
    int64_t idx = line - LINE_HANDLE_BASE;
    if (idx < 0 || idx >= NLINES) return 1;
    if (box) *box = &kBoxes[idx];
    return 0;
}

int64_t GetOcrLineWordCount(int64_t line, int64_t *count) {
    (void)line;
    if (count) *count = 0;
    return 0;
}
int64_t GetOcrWord(int64_t line, int64_t i, int64_t *word) {
    (void)line; (void)i;
    if (word) *word = 0;
    return 1;
}
int64_t GetOcrWordContent(int64_t word, int64_t *content) {
    (void)word;
    if (content) *content = 0;
    return 1;
}
int64_t GetOcrWordBoundingBox(int64_t word, ocr_bbox **box) {
    (void)word;
    if (box) *box = NULL;
    return 1;
}

int64_t ReleaseOcrResult(int64_t instance) {
    (void)instance;
    g.release_count++;
    write_record();
    return 0;
}
