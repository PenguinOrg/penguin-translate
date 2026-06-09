import Alpine from '/ui/shared/alpine.esm.js';
import { httpErrorMessage } from '/ui/shared/http.js';
import { rmsToScale } from '/ui/shared/dsp.js';

const TRANSCRIBE_MODELS = [
  { v: 'qwen/qwen3-asr-flash-2026-02-10', t: 'Qwen3 ASR Flash (OpenRouter)' },
  { v: 'gpt-4o-mini-transcribe', t: 'GPT-4o mini transcribe (OpenAI)' },
  { v: 'gpt-4o-transcribe', t: 'GPT-4o transcribe (OpenAI)' },
  { v: 'whisper-1', t: 'Whisper-1 (OpenAI)' },
];
const TRANSLATE_MODELS = [
  { v: 'openai/gpt-4o-mini', t: 'GPT-4o mini (OpenRouter)' },
  { v: 'openai/gpt-4o', t: 'GPT-4o (OpenRouter)' },
  { v: 'google/gemini-2.0-flash-lite-001', t: 'Gemini 2.0 Flash Lite (OpenRouter)' },
  { v: 'gpt-4o-mini', t: 'GPT-4o mini (OpenAI)' },
];
const MULTIMODAL_MODELS = [
  { v: 'xiaomi/mimo-v2-flash', t: 'MiMo v2 Flash (OpenRouter)' },
  { v: 'google/gemini-2.0-flash-001', t: 'Gemini 2.0 Flash (OpenRouter)' },
];

let activeMeter = null;

export function createPrefsStore(ctx) {
  const tt = (key, params) => Alpine.store('i18n').t(key, params);

  function stopMeter() { if (activeMeter) { try { activeMeter.stop(); } catch (_) {} activeMeter = null; } }
  document.addEventListener('visibilitychange', () => { if (document.hidden) stopMeter(); });

  const store = {
    open: false,
    cat: 'languages',
    advanced: (() => { try { return localStorage.getItem('pt.prefs.advanced') === '1'; } catch (_) { return false; } })(),
    settings: {},
    catalog: [],
    cuda: [],
    loaded: false,
    status: '',
    statusErr: false,
    loadBtnBusy: false,
    oscDraft: { enabled: false, host: '127.0.0.1', port: 9000, include_original: false, notification: true },
    diag: { line: '', tail: '' },
    TRANSCRIBE: TRANSCRIBE_MODELS,
    TRANSLATE: TRANSLATE_MODELS,
    MULTIMODAL: MULTIMODAL_MODELS,

    get practice() { return this.settings.practice || {}; },
    get audio() { return this.settings.audio || {}; },
    get categories() {
      const c = [{ id: 'languages' }, { id: 'inference' }];
      if (this.advanced) c.push({ id: 'capture' });
      c.push({ id: 'overlays' }, { id: 'integrations' }, { id: 'diagnostics' });
      return c;
    },

    get providerOpts() { return [{ v: 'openrouter', t: 'OpenRouter' }, { v: 'openai', t: 'OpenAI' }]; },
    get pipelineOpts() { return [{ v: 'split', t: tt('prefs.inf.modeSplit') }, { v: 'multimodal', t: tt('prefs.inf.modeMultimodal') }]; },
    get backOpts() { return [{ v: 'none', t: tt('prefs.inf.backNone') }, { v: 'local', t: tt('prefs.inf.backLocal') }, { v: 'openai', t: tt('prefs.inf.backCloud') }]; },
    get detectOpts() { return [{ v: 'streaming', t: tt('prefs.cap.detectStreaming') }, { v: 'filter', t: tt('prefs.cap.detectFilter') }, { v: 'rms', t: tt('prefs.cap.detectRms') }]; },
    get alignOpts() { return [{ v: 'left', t: tt('prefs.ov.alignLeft') }, { v: 'center', t: tt('prefs.ov.alignCenter') }, { v: 'right', t: tt('prefs.ov.alignRight') }]; },
    get gpuOpts() { return this.cuda.length ? this.cuda.map((d) => ({ v: d.id, t: d.label || d.id })) : [{ v: '', t: tt('prefs.inf.noGpus') }]; },

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
      try { const r = await fetch('/api/settings'); this.settings = r.ok ? await r.json() : {}; } catch (_) { this.settings = {}; }
      try { const r = await fetch('/api/languages'); this.catalog = r.ok ? (await r.json()).catalog || [] : []; } catch (_) { this.catalog = []; }
      try { const r = await fetch('/api/cuda-devices'); this.cuda = r.ok ? (await r.json()).devices || [] : []; } catch (_) { this.cuda = []; }
      this.syncOscDraft();
      this.loaded = true;
    },
    syncOscDraft() {
      const o = (this.practice.plugins || {}).vrchat_osc || {};
      this.oscDraft = { enabled: !!o.enabled, host: o.host || '127.0.0.1', port: Number(o.port) || 9000, include_original: !!o.include_original, notification: o.notification !== false };
    },

    async show(id) { this.open = true; if (!this.loaded) await this.loadAll(); this.select(id || this.cat || 'languages'); },
    close() { this.open = false; stopMeter(); },
    select(id) {
      stopMeter();
      if (id && this.categories.some((c) => c.id === id)) this.cat = id;
      else if (!this.categories.some((c) => c.id === this.cat)) this.cat = 'languages';
      this.status = ''; this.statusErr = false;
      if (this.cat === 'integrations') this.syncOscDraft();
      if (this.cat === 'diagnostics') this.refreshDiag();
      if (this.cat === 'capture') Alpine.nextTick(() => this.startMeter());
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
    removeKey(which) { this.save(which === 'openai' ? { remove_openai_key: true } : { remove_openrouter_key: true }); },

    saveOsc() {
      // The backend replaces the whole plugin config on save, so send it whole.
      const cfg = { ...this.oscDraft, port: Number(this.oscDraft.port) || 9000, host: this.oscDraft.host || '127.0.0.1' };
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
