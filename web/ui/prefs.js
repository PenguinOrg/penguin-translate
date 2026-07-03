import Alpine from '/ui/shared/alpine.esm.js';
import { httpErrorMessage } from '/ui/shared/http.js';
import { rmsToScale } from '/ui/shared/dsp.js';

const TRANSCRIBE_MODELS = [
  { v: 'qwen/qwen3-asr-flash-2026-02-10', t: 'Qwen3 ASR Flash (OpenRouter)' },
  { v: 'mistralai/voxtral-mini-transcribe', t: 'Voxtral Mini Transcribe (OpenRouter)' },
  { v: 'openai/whisper-large-v3', t: 'Whisper large-v3 (OpenRouter)' },
  { v: 'nova-2', t: 'Nova-2 — broad languages (Deepgram)' },
  { v: 'nova-3', t: 'Nova-3 (Deepgram)' },
  { v: 'gpt-4o-mini-transcribe', t: 'GPT-4o mini transcribe (OpenAI)' },
  { v: 'gpt-4o-transcribe', t: 'GPT-4o transcribe (OpenAI)' },
  { v: 'whisper-1', t: 'Whisper-1 (OpenAI)' },
];
const MIC_ASR_DEFAULTS = {
  openrouter: 'qwen/qwen3-asr-flash-2026-02-10',
  openai: 'gpt-4o-mini-transcribe',
  deepgram: 'nova-2',
};
// Practice transcribes your spoken target attempt; the model id must belong to
// the chosen provider (the engine sends `transcribe_model` verbatim).
const PRACTICE_ASR_MODELS = {
  openrouter: [
    { v: 'qwen/qwen3-asr-flash-2026-02-10', t: 'Qwen3 ASR Flash (OpenRouter)' },
    { v: 'mistralai/voxtral-mini-transcribe', t: 'Voxtral Mini Transcribe (OpenRouter)' },
    { v: 'openai/whisper-large-v3', t: 'Whisper large-v3 (OpenRouter)' },
  ],
  openai: [
    { v: 'gpt-4o-mini-transcribe', t: 'GPT-4o mini transcribe (OpenAI)' },
    { v: 'gpt-4o-transcribe', t: 'GPT-4o transcribe (OpenAI)' },
    { v: 'whisper-1', t: 'Whisper-1 (OpenAI)' },
  ],
  deepgram: [
    { v: 'nova-2', t: 'Nova-2 — broad languages (Deepgram)' },
    { v: 'nova-3', t: 'Nova-3 (Deepgram)' },
  ],
};
// Practice "hear it" speaks the target phrase; the model id must belong to the
// chosen TTS provider. OpenRouter-Gemini and DashScope-Qwen are multilingual
// (handle JP/ZH/KO); Deepgram Aura voices are English-only (the model id IS the
// voice). All four combos below are verified against the live endpoints.
const PRACTICE_TTS_MODELS = {
  openrouter: [
    { v: 'google/gemini-3.1-flash-tts-preview', t: 'Gemini Flash TTS — 70+ languages (OpenRouter)' },
  ],
  dashscope: [
    { v: 'qwen3-tts-flash', t: 'Qwen3-TTS Flash — multilingual (DashScope)' },
  ],
  openai: [
    { v: 'gpt-4o-mini-tts', t: 'GPT-4o mini TTS (OpenAI)' },
    { v: 'tts-1', t: 'TTS-1 (OpenAI)' },
    { v: 'tts-1-hd', t: 'TTS-1 HD (OpenAI)' },
  ],
  deepgram: [
    { v: 'aura-2-thalia-en', t: 'Aura-2 Thalia · English (Deepgram)' },
    { v: 'aura-2-andromeda-en', t: 'Aura-2 Andromeda · English (Deepgram)' },
    { v: 'aura-asteria-en', t: 'Aura Asteria · English (Deepgram)' },
  ],
};
const TTS_DEFAULTS = { openrouter: 'google/gemini-3.1-flash-tts-preview', dashscope: 'qwen3-tts-flash', openai: 'gpt-4o-mini-tts', deepgram: 'aura-2-thalia-en' };
// Voices vary by provider; Deepgram has none (the model id carries the voice).
const TTS_VOICES = {
  openrouter: ['Kore', 'Puck', 'Zephyr', 'Charon', 'Fenrir', 'Aoede'],
  dashscope: ['Cherry', 'Ethan', 'Dylan', 'Jada', 'Sunny'],
  openai: ['alloy', 'ash', 'ballad', 'coral', 'echo', 'fable', 'nova', 'onyx', 'sage', 'shimmer'],
  deepgram: [],
};
const TTS_VOICE_DEFAULTS = { openrouter: 'Kore', dashscope: 'Cherry', openai: 'coral', deepgram: '' };
const TRANSLATE_MODELS = [
  { v: 'openai/gpt-4o-mini', t: 'GPT-4o mini (OpenRouter)' },
  { v: 'openai/gpt-4o', t: 'GPT-4o (OpenRouter)' },
  { v: 'google/gemini-2.5-flash-lite', t: 'Gemini 2.5 Flash Lite (OpenRouter)' },
  { v: 'gpt-4o-mini', t: 'GPT-4o mini (OpenAI)' },
];
const MULTIMODAL_MODELS = [
  { v: 'xiaomi/mimo-v2.5', t: 'MiMo v2.5 (OpenRouter)' },
  { v: 'google/gemini-2.5-flash-lite', t: 'Gemini 2.5 Flash Lite (OpenRouter)' },
  { v: 'google/gemini-2.5-flash', t: 'Gemini 2.5 Flash (OpenRouter)' },
  { v: 'mistralai/voxtral-small-24b-2507', t: 'Voxtral Small 24B (OpenRouter)' },
  { v: 'openai/gpt-audio-mini', t: 'GPT Audio mini (OpenAI)' },
];
const CAPTION_ASR_MODELS = [
  { v: 'qwen3-asr-flash', t: 'Qwen3 ASR Flash + context (DashScope)' },
  { v: 'qwen/qwen3-asr-flash-2026-02-10', t: 'Qwen3 ASR Flash (OpenRouter, no context)' },
  { v: 'mistralai/voxtral-mini-transcribe', t: 'Voxtral Mini Transcribe (OpenRouter)' },
  { v: 'nova-2', t: 'Nova-2 — broad languages (Deepgram)' },
  { v: 'nova-3', t: 'Nova-3 (Deepgram)' },
  { v: 'gpt-4o-mini-transcribe', t: 'GPT-4o mini transcribe (OpenAI)' },
  { v: 'whisper-1', t: 'Whisper-1 (OpenAI)' },
];
const CAPTION_TRANSLATE_MODELS = [
  { v: 'qwen-flash', t: 'Qwen Flash (DashScope)' },
  { v: 'qwen-plus', t: 'Qwen Plus (DashScope)' },
  { v: 'qwen-turbo', t: 'Qwen Turbo (DashScope)' },
  { v: 'google/gemini-2.5-flash-lite', t: 'Gemini 2.5 Flash Lite (OpenRouter)' },
  { v: 'openai/gpt-4o-mini', t: 'GPT-4o mini (OpenRouter)' },
  { v: 'gpt-4o-mini', t: 'GPT-4o mini (OpenAI)' },
];
// Deepgram is ASR-only, so its translate step runs on a chat provider.
const CAPTION_DEFAULTS = {
  dashscope: { transcribe_model: 'qwen3-asr-flash', translate_provider: 'dashscope', translate_model: 'qwen-flash' },
  openrouter: { transcribe_model: 'qwen/qwen3-asr-flash-2026-02-10', translate_provider: 'openrouter', translate_model: 'google/gemini-2.5-flash-lite' },
  openai: { transcribe_model: 'gpt-4o-mini-transcribe', translate_provider: 'openai', translate_model: 'gpt-4o-mini' },
  deepgram: { transcribe_model: 'nova-2', translate_provider: 'openai', translate_model: 'gpt-4o-mini' },
};
const TRANSLATE_MODEL_DEFAULTS = {
  openai: 'gpt-4o-mini',
  openrouter: 'google/gemini-2.5-flash-lite',
  dashscope: 'qwen-flash',
};
// Window-OCR translation runs on a chat model; OpenRouter uses prefixed ids, OpenAI bare ids.
const WINDOW_OR_MODELS = [
  { v: 'openai/gpt-4o-mini', t: 'GPT-4o mini (OpenRouter)' },
  { v: 'openai/gpt-4o', t: 'GPT-4o (OpenRouter)' },
  { v: 'google/gemini-2.5-flash-lite', t: 'Gemini 2.5 Flash Lite (OpenRouter)' },
  { v: 'google/gemini-2.5-flash', t: 'Gemini 2.5 Flash (OpenRouter)' },
];
const WINDOW_OAI_MODELS = [
  { v: 'gpt-4o-mini', t: 'GPT-4o mini (OpenAI)' },
  { v: 'gpt-4o', t: 'GPT-4o (OpenAI)' },
];
const CAPTION_PRESETS = [
  { v: 'gpt-audio-mini', k: 'prefs.cap.presetGptAudioMini',
    patch: { api_provider: 'openrouter', pipeline_mode: 'multimodal', multimodal_model: 'openai/gpt-audio-mini', context_enabled: true } },
  { v: 'gemini-flash-lite', k: 'prefs.cap.presetGeminiFlashLite',
    patch: { api_provider: 'openrouter', pipeline_mode: 'multimodal', multimodal_model: 'google/gemini-2.5-flash-lite', context_enabled: true } },
  { v: 'dashscope-qwen', k: 'prefs.cap.presetDashscope',
    patch: { api_provider: 'dashscope', pipeline_mode: 'split', transcribe_model: 'qwen3-asr-flash', translate_model: 'qwen-flash', context_enabled: true } },
  { v: 'deepgram-nova', k: 'prefs.cap.presetDeepgram',
    patch: { api_provider: 'deepgram', translate_provider: 'openai', pipeline_mode: 'split', transcribe_model: 'nova-2', translate_model: 'gpt-4o-mini', context_enabled: false } },
];

let activeMeter = null;
let vrStatusTimer = null;

export function createPrefsStore(ctx) {
  const tt = (key, params) => Alpine.store('i18n').t(key, params);

  function stopMeter() { if (activeMeter) { try { activeMeter.stop(); } catch (_) {} activeMeter = null; } }
  function stopVrStatusPoll() { if (vrStatusTimer) { clearInterval(vrStatusTimer); vrStatusTimer = null; } }
  document.addEventListener('visibilitychange', () => { if (document.hidden) { stopMeter(); stopVrStatusPoll(); } });

  const store = {
    open: false,
    cat: 'languages',
    captionManual: false,
    advanced: (() => { try { return localStorage.getItem('pt.prefs.advanced') === '1'; } catch (_) { return false; } })(),
    settings: {},
    win: {},
    catalog: [],
    cuda: [],
    loaded: false,
    status: '',
    statusErr: false,
    loadBtnBusy: false,
    oscDraft: { enabled: false, host: '127.0.0.1', port: 9000, include_original: false, notification: true, pace_cps: 15, pace_min_seconds: 1.5, pace_max_seconds: 7 },
    diag: { line: '', tail: '' },
    vrStatus: { running: false, ok: false, detail: '', error: '' },
    TRANSCRIBE: TRANSCRIBE_MODELS,
    TRANSLATE: TRANSLATE_MODELS,
    MULTIMODAL: MULTIMODAL_MODELS,
    CAPTION_ASR: CAPTION_ASR_MODELS,
    CAPTION_TRANSLATE: CAPTION_TRANSLATE_MODELS,
    WINDOW_OR: WINDOW_OR_MODELS,
    WINDOW_OAI: WINDOW_OAI_MODELS,

    get practice() { return this.settings.practice || {}; },
    get audio() { return this.settings.audio || {}; },
    get window() { return this.win || {}; },
    get categories() {
      const c = [{ id: 'languages' }, { id: 'inference' }, { id: 'practice' }];
      if (this.advanced) c.push({ id: 'capture' });
      c.push({ id: 'window' }, { id: 'overlays' }, { id: 'integrations' }, { id: 'diagnostics' });
      return c;
    },
    get practiceAsrModelOpts() { return PRACTICE_ASR_MODELS[this.micAsrEngine] || PRACTICE_ASR_MODELS.openrouter; },
    get passThresholdOpts() { return [50, 60, 70, 80, 90, 100]; },
    get ttsEngine() { return this.practice.tts_engine || 'openrouter'; },
    get ttsProviderOpts() { return [{ v: 'openrouter', t: 'OpenRouter · Gemini' }, { v: 'dashscope', t: 'DashScope · Qwen' }, { v: 'openai', t: 'OpenAI' }, { v: 'deepgram', t: 'Deepgram · Aura (English)' }]; },
    get practiceTtsModelOpts() { return PRACTICE_TTS_MODELS[this.ttsEngine] || PRACTICE_TTS_MODELS.openrouter; },
    get ttsVoiceOpts() { return (TTS_VOICES[this.ttsEngine] || []).map((v) => ({ v, t: v })); },
    get ttsHasVoice() { return (TTS_VOICES[this.ttsEngine] || []).length > 0; },
    get assessmentMode() { return this.practice.assessment_mode || 'basic'; },
    get assessmentModeOpts() { return [{ v: 'basic', t: tt('prefs.prac.modeBasic') }, { v: 'azure', t: tt('prefs.prac.modeAzure') }]; },

    get providerOpts() { return [{ v: 'openrouter', t: 'OpenRouter' }, { v: 'openai', t: 'OpenAI' }]; },
    get micAsrProviderOpts() { return [{ v: 'openrouter', t: 'OpenRouter' }, { v: 'openai', t: 'OpenAI' }, { v: 'deepgram', t: 'Deepgram · Nova' }]; },
    get micAsrEngine() { return this.practice.english_asr_engine || 'openrouter'; },
    get captionProviderOpts() { return [{ v: 'dashscope', t: 'DashScope · Qwen (context)' }, { v: 'deepgram', t: 'Deepgram · Nova' }, { v: 'openrouter', t: 'OpenRouter' }, { v: 'openai', t: 'OpenAI' }]; },
    get translateProviderOpts() { return [{ v: 'openai', t: 'OpenAI' }, { v: 'openrouter', t: 'OpenRouter' }, { v: 'dashscope', t: 'DashScope · Qwen' }]; },
    get translateProvider() { return this.audio.translate_provider || this.audio.api_provider || 'openai'; },
    get asrProvider() { return this.audio.api_provider || 'openrouter'; },
    get activeCaptionPreset() {
      const a = this.audio;
      const prov = a.api_provider || 'openrouter';
      const pipe = a.pipeline_mode || 'split';
      for (const p of CAPTION_PRESETS) {
        const q = p.patch;
        if (q.api_provider !== prov || q.pipeline_mode !== pipe) continue;
        if (pipe === 'multimodal' && (a.multimodal_model || '') !== q.multimodal_model) continue;
        if (pipe === 'split' && (a.transcribe_model || '') !== q.transcribe_model) continue;
        return p.v;
      }
      return 'custom';
    },
    get captionPresetOpts() {
      const out = CAPTION_PRESETS.map((p) => ({ v: p.v, t: tt(p.k) }));
      out.push({ v: 'custom', t: tt('prefs.cap.presetCustom') });
      return out;
    },
    get captionPresetValue() {
      return (this.captionManual || this.activeCaptionPreset === 'custom') ? 'custom' : this.activeCaptionPreset;
    },
    get pipelineOpts() { return [{ v: 'split', t: tt('prefs.inf.modeSplit') }, { v: 'multimodal', t: tt('prefs.inf.modeMultimodal') }]; },
    get backOpts() { return [{ v: 'none', t: tt('prefs.inf.backNone') }, { v: 'local', t: tt('prefs.inf.backLocal') }, { v: 'openai', t: tt('prefs.inf.backCloud') }]; },
    get detectOpts() { return [{ v: 'streaming', t: tt('prefs.cap.detectStreaming') }, { v: 'filter', t: tt('prefs.cap.detectFilter') }, { v: 'rms', t: tt('prefs.cap.detectRms') }]; },
    get alignOpts() { return [{ v: 'left', t: tt('prefs.ov.alignLeft') }, { v: 'center', t: tt('prefs.ov.alignCenter') }, { v: 'right', t: tt('prefs.ov.alignRight') }]; },
    get gpuOpts() { return this.cuda.length ? this.cuda.map((d) => ({ v: d.id, t: d.label || d.id })) : [{ v: '', t: tt('prefs.inf.noGpus') }]; },

    get winBackend() { return this.window.translate_backend || 'openrouter'; },
    get winProviderOpts() { return [{ v: 'openrouter', t: 'OpenRouter' }, { v: 'openai', t: 'OpenAI' }, { v: 'nllb', t: tt('prefs.win.providerLocal') }]; },
    get winTarget() { return this.practice.my_language || 'en'; },
    get winSourceOpts() {
      const i18n = Alpine.store('i18n');
      const auto = { v: 'auto', t: tt('prefs.win.sourceAuto') };
      return [auto, ...this.catalog.map((l) => ({ v: l.id, t: (l.flag ? l.flag + ' ' : '') + i18n.langName(l.id, l.label) }))];
    },

    opts(presets, current) {
      const out = presets.map((o) => ({ v: o.v ?? o, t: o.t ?? (o.v ?? o) }));
      if (current && !out.some((o) => o.v === current)) out.push({ v: current, t: current + ' (current)' });
      return out;
    },
    modeBtnStyle(on) {
      return 'border:1px solid var(--border-strong);border-radius:6px;padding:3px 13px;font-size:12px;cursor:pointer;'
        + 'background:' + (on ? 'var(--accent)' : 'transparent') + ';color:' + (on ? '#fff' : 'var(--muted)');
    },

    async loadAll() {
      const load = async (url, apply, fallback, optional) => {
        try {
          const r = await fetch(url);
          if (optional && r.status === 404) { apply(fallback); return; }
          if (!r.ok) throw new Error(await httpErrorMessage(r, 'GET ' + url));
          apply(await r.json());
        } catch (e) {
          apply(fallback);
          ctx.Toasts.push({ title: tt('prefs.loadFailed'), msg: String(e?.message || e) });
        }
      };
      await load('/api/settings', (j) => { this.settings = j; }, {});
      // /api/config only exists on Windows builds (window-translate feature).
      await load('/api/config', (j) => { this.win = j; }, {}, true);
      await load('/api/languages', (j) => { this.catalog = j.catalog || []; }, { catalog: [] });
      await load('/api/cuda-devices', (j) => { this.cuda = j.devices || []; }, { devices: [] });
      this.syncOscDraft();
      this.loaded = true;
    },
    syncOscDraft() {
      const o = (this.practice.plugins || {}).vrchat_osc || {};
      this.oscDraft = { enabled: !!o.enabled, host: o.host || '127.0.0.1', port: Number(o.port) || 9000, include_original: !!o.include_original, notification: o.notification !== false, pace_cps: Number(o.pace_cps) || 15, pace_min_seconds: o.pace_min_seconds != null ? Number(o.pace_min_seconds) : 1.5, pace_max_seconds: Number(o.pace_max_seconds) || 7 };
    },

    async show(id) { this.open = true; if (!this.loaded) await this.loadAll(); this.select(id || this.cat || 'languages'); },
    close() { this.open = false; stopMeter(); stopVrStatusPoll(); },
    select(id) {
      stopMeter(); stopVrStatusPoll();
      if (id && this.categories.some((c) => c.id === id)) this.cat = id;
      else if (!this.categories.some((c) => c.id === this.cat)) this.cat = 'languages';
      this.status = ''; this.statusErr = false;
      if (this.cat === 'integrations') this.syncOscDraft();
      if (this.cat === 'diagnostics') this.refreshDiag();
      if (this.cat === 'capture') Alpine.nextTick(() => this.startMeter());
      if (this.cat === 'overlays') this.startVrStatusPoll();
    },
    setAdvanced(v) {
      this.advanced = !!v;
      try { localStorage.setItem('pt.prefs.advanced', v ? '1' : '0'); } catch (_) {}
      this.select(this.categories.some((c) => c.id === this.cat) ? this.cat : 'languages');
    },

    async save(patch) {
      try {
        const r = await fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(patch) });
        if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/settings'));
        this.settings = await r.json();
        this.status = tt('prefs.saved'); this.statusErr = false;
      } catch (e) {
        this.status = String(e?.message || e); this.statusErr = true;
        ctx.Toasts.push({ title: tt('prefs.saveFailed'), msg: String(e?.message || e) });
      }
    },
    async saveAudio(patch) { await this.save({ audio: patch }); document.dispatchEvent(new CustomEvent('audioprefschange', { detail: patch })); },
    // Window-OCR settings live on domain.Settings.Window and persist via /api/config (the
    // window server), which also re-wires the live translator. Reload before merging so a
    // window pick made in the overlay bar isn't clobbered by a stale cached config.
    async saveWindow(patch) {
      try {
        let cur = this.win;
        try { const g = await fetch('/api/config'); if (g.ok) cur = await g.json(); } catch (_) {}
        const body = { ...cur, ...patch };
        const r = await fetch('/api/config', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/config'));
        this.win = body;
        this.status = tt('prefs.saved'); this.statusErr = false;
      } catch (e) {
        this.status = String(e?.message || e); this.statusErr = true;
        ctx.Toasts.push({ title: tt('prefs.saveFailed'), msg: String(e?.message || e) });
      }
    },
    setCaptionProvider(p) {
      const patch = { api_provider: p, ...(CAPTION_DEFAULTS[p] || {}) };
      this.saveAudio(patch);
    },
    setTranslateProvider(tp) {
      this.saveAudio({ translate_provider: tp, translate_model: TRANSLATE_MODEL_DEFAULTS[tp] || 'gpt-4o-mini' });
    },
    setMicAsrEngine(e) {
      // Keep ja_repeat_asr_engine (what /api/transcribe prefers for practice) in
      // step with the Speak engine, and reset the model to one that provider owns
      // — the engine sends transcribe_model verbatim, so a cross-provider id fails.
      this.save({ practice: { english_asr_engine: e, ja_repeat_asr_engine: e, transcribe_model: MIC_ASR_DEFAULTS[e] || '' } });
    },
    setTtsEngine(e) {
      // Reset the model AND voice to ones the new provider owns — voice ids differ
      // per provider (OpenAI alloy/coral, Gemini Kore/Puck, Deepgram has none).
      this.save({ practice: { tts_engine: e, openai_tts_model: TTS_DEFAULTS[e] || '', tts_voice_name: TTS_VOICE_DEFAULTS[e] || '' } });
    },
    setCaptionPreset(v) {
      if (v === 'custom') { this.captionManual = true; return; }
      this.captionManual = false;
      const p = CAPTION_PRESETS.find((x) => x.v === v);
      if (p) this.saveAudio({ ...p.patch });
    },
    removeKey(which) {
      const flag = { openai: 'remove_openai_key', openrouter: 'remove_openrouter_key', dashscope: 'remove_dashscope_key', deepgram: 'remove_deepgram_key', azure: 'remove_azure_key' }[which];
      if (flag) this.save({ [flag]: true });
    },
    // Keys are write-only: GET /api/settings only reports *_key_configured, so the
    // input stays empty and is cleared again once a new key is stored.
    async saveKey(field, input) {
      const v = input.value.trim();
      if (!v) return;
      await this.save({ [field]: v });
      if (!this.statusErr) input.value = '';
    },

    saveOsc() {
      const d = this.oscDraft;
      const num = (v, dflt) => { const n = Number(v); return Number.isFinite(n) && n >= 0 ? n : dflt; };
      const cfg = { ...d, port: Number(d.port) || 9000, host: d.host || '127.0.0.1',
        pace_cps: num(d.pace_cps, 15) || 15, pace_min_seconds: num(d.pace_min_seconds, 1.5), pace_max_seconds: num(d.pace_max_seconds, 7) || 7 };
      this.save({ practice: { plugins: { vrchat_osc: cfg } } });
    },
    async sendOscTest() {
      try {
        const r = await fetch('/api/plugins/vrchat-osc/send', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: 'Penguin Translate — OSC test ✓' }) });
        if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/plugins/vrchat-osc/send'));
        this.status = tt('prefs.int.testSent'); this.statusErr = false;
      } catch (e) { ctx.Toasts.push({ title: tt('prefs.int.testFailed'), msg: String(e?.message || e) }); }
    },
    toggleDesktopOverlay(v) {
      this.saveAudio({ desktop_overlay_enabled: v });
      fetch('/api/desktop-overlay/' + (v ? 'start' : 'stop'), { method: 'POST' }).then(() => fetch('/api/audio/apply-overlay-layout', { method: 'POST' })).catch(() => {});
    },
    toggleVrOverlay(v) {
      this.saveAudio({ openvr_overlay_enabled: v });
      fetch('/api/overlay/' + (v ? 'start' : 'stop'), { method: 'POST' }).catch(() => {});
      if (v) this.startVrStatusPoll(); else { stopVrStatusPoll(); this.vrStatus = { running: false, ok: false, detail: '', error: '' }; }
    },
    startVrStatusPoll() {
      stopVrStatusPoll();
      this.refreshVrStatus();
      vrStatusTimer = setInterval(() => this.refreshVrStatus(), 2000);
    },
    async refreshVrStatus() {
      try {
        const r = await fetch('/api/overlay/status');
        if (!r.ok) throw new Error(await httpErrorMessage(r, 'GET /api/overlay/status'));
        const j = await r.json();
        this.vrStatus = { running: !!j.running, ok: !!j.initialized, detail: j.detail || '', error: j.last_error || '' };
      } catch (e) { this.vrStatus = { running: false, ok: false, detail: '', error: String(e?.message || e) }; }
    },
    async loadModels() {
      this.loadBtnBusy = true;
      try {
        const r = await fetch('/api/engine-load', { method: 'POST' });
        if (!r.ok) throw new Error(await httpErrorMessage(r));
        this.status = tt('prefs.inf.modelsLoading'); this.statusErr = false;
      } catch (e) { ctx.Toasts.push({ title: tt('prefs.inf.loadFailed'), msg: String(e?.message || e) }); }
      finally { this.loadBtnBusy = false; }
    },
    async refreshDiag() {
      try {
        const r = await fetch('/api/debug/logs'); if (!r.ok) throw new Error(await httpErrorMessage(r));
        const j = await r.json();
        let gpu = ''; try { const hr = await fetch('/api/engine-health'); if (hr.ok) gpu = (await hr.json()).device_detail || ''; } catch (_) {}
        this.diag.line = `Engine ${j.engine_url || '?'} · identity ${j.engine_identity_ok ? 'ok' : 'WRONG: ' + (j.engine_title || 'unknown')}` + (gpu ? ` · ${gpu}` : '');
        this.diag.tail = [(j.log_paths || []).filter(Boolean).join(' · '), j.engine_log, j.launcher_log].filter(Boolean).join('\n---\n') || '(empty)';
      } catch (e) { this.diag.tail = String(e?.message || e); }
    },

    startMeter() {
      stopMeter();
      const host = document.querySelector('#prefsPane .vad-meter'); if (!host) return;
      const fill = host.querySelector('.vad-fill'), mark = host.querySelector('.vad-mark');
      const dot = host.querySelector('.mic-dot'), label = host.querySelector('.mic-label');
      const slider = host.querySelector('input[type=range]'), note = host.querySelector('.vad-note');
      let level = 0, raf = 0, actx = null, stream = null, proc = null;
      const pct = () => Number(slider && slider.value) || 15;
      const setState = (c, t) => { if (dot) dot.style.background = c; if (label) label.textContent = t; };
      const setMark = () => { if (mark) mark.style.left = pct() + '%'; };
      setMark(); if (slider) slider.addEventListener('input', setMark);
      const frame = () => {
        const p = pct();
        if (fill) { fill.style.width = level + '%'; fill.style.background = level >= p ? 'var(--ok)' : 'var(--faint)'; }
        if (dot) dot.style.opacity = (0.4 + 0.6 * Math.abs(Math.sin(performance.now() / 420))).toFixed(2);
        raf = requestAnimationFrame(frame);
      };
      setState('var(--ok)', tt('prefs.cap.micLive'));
      (async () => {
        try {
          stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
          actx = new (window.AudioContext || window.webkitAudioContext)();
          const src = actx.createMediaStreamSource(stream);
          proc = actx.createScriptProcessor(2048, 1, 1);
          proc.onaudioprocess = (e) => { const inp = e.inputBuffer.getChannelData(0); let s = 0; for (let i = 0; i < inp.length; i++) s += inp[i] * inp[i]; level = rmsToScale(Math.sqrt(s / inp.length)); };
          const sink = actx.createGain(); sink.gain.value = 0; src.connect(proc); proc.connect(sink); sink.connect(actx.destination);
          frame();
        } catch (e) { setState('var(--err)', tt('prefs.cap.micUnavailable')); if (note) note.textContent = String(e?.message || e); }
      })();
      activeMeter = {
        stop() {
          if (raf) cancelAnimationFrame(raf);
          if (proc) { try { proc.disconnect(); } catch (_) {} proc.onaudioprocess = null; }
          if (stream) stream.getTracks().forEach((t) => t.stop());
          if (actx) actx.close().catch(() => {});
          if (slider) slider.removeEventListener('input', setMark);
          if (fill) fill.style.width = '0';
          if (dot) { dot.style.opacity = '1'; dot.style.background = 'var(--faint)'; }
        },
      };
    },
  };

  return store;
}
