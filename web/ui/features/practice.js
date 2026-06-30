import Alpine from '/ui/shared/alpine.esm.js';
import { floatTo16kMono, buildWav } from '/ui/shared/dsp.js';
import { httpErrorMessage, postJSON } from '/ui/shared/http.js';

let styleInjected = false;
function ensureStyle() {
  if (styleInjected) return;
  styleInjected = true;
  const l = document.createElement('link');
  l.rel = 'stylesheet';
  l.href = '/ui/features/practice.css';
  document.head.appendChild(l);
}

// Catalog language id -> server score profile id. Only these three have a
// pronunciation scorer + reading aid; everything else cannot be practiced.
const SCOREABLE = { ja: 'jp', zh: 'zh', ko: 'ko' };
const MAX_REC_MS = 10000;

export function createPracticeStore(ctx) {
  ensureStyle();
  const tt = (k, p) => Alpine.store('i18n').t(k, p);
  const S = () => Alpine.store('practice');

  const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  function rubyHTML(text, tokens) {
    if (!Array.isArray(tokens) || !tokens.length) return esc(text);
    let out = '';
    for (const t of tokens) { if (!t || !t.surface) continue; out += t.reading ? '<ruby>' + esc(t.surface) + '<rt>' + esc(t.reading) + '</rt></ruby>' : esc(t.surface); }
    return out || esc(text);
  }
  // Azure scores per word: render the recognized text colouring each word green
  // (good) or red (mispronounced / wrong) from its accuracy + error type.
  function azureWordsHTML(words, fallback) {
    if (!Array.isArray(words) || !words.length) return esc(fallback || '');
    return words.map((w) => {
      if (!w || !w.word) return '';
      const acc = Number(w.accuracy || 0);
      const bad = (w.error_type && w.error_type !== 'None') || acc < 60;
      return '<span class="' + (bad ? 'miss' : 'hit') + '" title="' + esc(String(Math.round(acc))) + '%">' + esc(w.word) + '</span>';
    }).join('');
  }
  // Render the spoken transcript with the server's matched character ranges wrapped in <span class="hit">.
  function highlightedSpoken(base, ranges) {
    const chars = [...String(base || '')];
    if (!Array.isArray(ranges) || !ranges.length) return esc(base || '');
    const on = new Array(chars.length).fill(false);
    for (const r of ranges) { if (!Array.isArray(r)) continue; for (let i = r[0]; i < r[1] && i < chars.length; i++) if (i >= 0) on[i] = true; }
    let out = '', run = '', hot = false;
    const flush = () => { if (!run) return; out += hot ? '<span class="hit">' + esc(run) + '</span>' : esc(run); run = ''; };
    for (let i = 0; i < chars.length; i++) { if (on[i] !== hot) { flush(); hot = on[i]; } run += chars[i]; }
    flush();
    return out;
  }

  // ---- audio capture (own stream; push-to-record) ----
  let audioCtx = null, stream = null, proc = null, silent = null, chunks = [], recTimer = null;
  function concatInt16(parts) {
    let n = 0; for (const c of parts) n += c.length;
    const out = new Int16Array(n); let o = 0;
    for (const c of parts) { out.set(c, o); o += c.length; }
    return out;
  }
  async function startCapture() {
    const dev = Alpine.store('conversation')?.lanes?.mic?.device;
    audioCtx = new (window.AudioContext || window.webkitAudioContext)();
    stream = await navigator.mediaDevices.getUserMedia({ audio: { deviceId: dev ? { exact: dev } : undefined, channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
    const src = audioCtx.createMediaStreamSource(stream);
    proc = audioCtx.createScriptProcessor(4096, 1, 1);
    chunks = [];
    proc.onaudioprocess = (e) => { if (!S().recording) return; chunks.push(floatTo16kMono(e.inputBuffer.getChannelData(0), e.inputBuffer.sampleRate)); };
    silent = audioCtx.createGain(); silent.gain.value = 0;
    src.connect(proc); proc.connect(silent); silent.connect(audioCtx.destination);
    // A context created outside a gesture can start suspended; without this the
    // ScriptProcessor never fires and the clip is empty (a silent 0% score).
    try { await audioCtx.resume(); } catch (_) {}
  }
  function stopCapture() {
    try { if (proc) proc.disconnect(); if (silent) silent.disconnect(); } catch (_) {}
    proc = null; silent = null;
    if (stream) { stream.getTracks().forEach((t) => t.stop()); stream = null; }
    if (audioCtx) { audioCtx.close().catch(() => {}); audioCtx = null; }
    const pcm = concatInt16(chunks); chunks = [];
    return pcm;
  }

  async function transcribe(blob, asrCode) {
    const fd = new FormData(); fd.append('file', blob, 'clip.wav'); if (asrCode) fd.append('language', asrCode);
    const r = await fetch('/api/transcribe', { method: 'POST', body: fd });
    if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/transcribe'));
    return r.json();
  }
  async function translateOne(english, targetId) {
    const r = await fetch('/api/translate-text', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: english, source_language: 'en', target_languages: [targetId] }) });
    if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/translate-text'));
    const j = await r.json(); return (j.results || [])[0] || null;
  }
  async function postScore(expected, spoken, tokens) {
    const body = { expected, spoken, threshold: S().threshold || 0 };
    if (Array.isArray(tokens) && tokens.length) body.furigana = tokens;
    const r = await fetch('/api/score', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/score'));
    return r.json();
  }

  // Descriptor for a practiced language by catalog id (jp/zh/ko only).
  function resolveTargetById(id) {
    if (!id || !SCOREABLE[id]) return null;
    const langs = Alpine.store('langs');
    const L = langs.byId(id);
    return { id, profile: SCOREABLE[id], label: langs.label(id), flag: langs.flag(id), asr: L.asr_code || id, furigana: L.reading_aid === 'furigana' };
  }
  // The default practiced language = first scoreable output language.
  function firstScoreable() { return (Alpine.store('langs')?.others || []).find((o) => SCOREABLE[o]) || ''; }

  // Arm practice mode for a target and refresh the threshold + assessment mode.
  // The server scores by practice.target_language (not the request body), so this
  // must run before scoring; it's cheap and idempotent, so it runs each attempt.
  async function ensureReady(target) {
    if (!target) return false;
    try {
      const s = await postJSON('/api/settings', { practice: { practice_enabled: true, target_language: target.profile } }, 'POST /api/settings');
      const p = s.practice || {};
      if (Number(p.score_threshold) > 0) S().threshold = Number(p.score_threshold);
      S().assessmentMode = (p.assessment_mode === 'azure') ? 'azure' : 'basic';
      return true;
    } catch (e) { ctx.Toasts?.push?.({ title: tt('practice.title'), msg: e?.message || String(e) }); return false; }
  }

  const store = {
    open: false,
    threshold: 80,
    english: '',
    recording: false,
    busy: false,
    ttsBusy: false,
    status: '',
    activeTargetId: '', // set when practicing a specific conversation turn; else first scoreable
    assessmentMode: 'basic', // 'basic' (local match) | 'azure' (pronunciation assessment)
    // current phrase
    target: '',
    tokens: [],
    // last score
    score: { has: false, value: 0, accepted: false, sub: null },
    spokenHTML: '',
    attempts: [],
    heard: { text: '', detected: '', engine: '' }, // what the ASR returned + which backend

    THRESHOLDS: [50, 60, 70, 80, 90, 100],

    get tgt() { return resolveTargetById(this.activeTargetId || firstScoreable()); },
    get targetReady() { return !!this.tgt; },
    get hasPhrase() { return !!this.target; },
    get dirLabel() { const t = this.tgt; return t ? 'EN → ' + (Alpine.store('langs').abbr(t.id)) : ''; },
    get targetLabel() { const t = this.tgt; return t ? (t.flag + ' ' + t.label) : ''; },
    get targetCls() { const t = this.tgt; return t ? ('lang-' + t.profile) : ''; },
    get targetHTML() { return this.target ? rubyHTML(this.target, this.tokens) : ''; },
    get recBtnLabel() { return this.recording ? tt('practice.release') : (this.score.has ? tt('practice.recordAgain') : tt('practice.record')); },
    get verdict() { return this.score.accepted ? tt('practice.passed') : tt('practice.keepTrying'); },
    get thresholdLabel() { return tt('practice.threshold', { n: this.threshold }); },
    get hasHeard() { return !!(this.heard.engine || this.heard.text); },
    get heardLine() { const h = this.heard; const parts = []; if (h.engine) parts.push(h.engine); if (h.detected) parts.push(h.detected); return parts.join(' · '); },
    get isAzure() { return this.assessmentMode === 'azure'; },
    get canHear() { return this.hasPhrase && !this.ttsBusy && !this.recording; },
    // Per-dimension bars for the Azure "proper" result (prosody only when present).
    get subBars() {
      const s = this.score.sub; if (!s) return [];
      const out = [
        { key: 'accuracy', label: tt('practice.subAccuracy'), value: Math.round(Number(s.accuracy || 0)) },
        { key: 'fluency', label: tt('practice.subFluency'), value: Math.round(Number(s.fluency || 0)) },
        { key: 'completeness', label: tt('practice.subCompleteness'), value: Math.round(Number(s.completeness || 0)) },
      ];
      if (Number(s.prosody) > 0) out.push({ key: 'prosody', label: tt('practice.subProsody'), value: Math.round(Number(s.prosody)) });
      return out;
    },
    ringStyle() {
      const v = Math.max(0, Math.min(100, this.score.value));
      const col = this.score.accepted ? 'var(--ok)' : 'var(--warn)';
      return `background: conic-gradient(${col} ${v * 3.6}deg, var(--surface-3) 0);`;
    },

    toggle() { this.open = !this.open; if (this.open) this.onOpen(); },
    close() { this.cancelHold(); this.open = false; },
    async onOpen() {
      const t = this.tgt;
      if (!t) { this.status = tt('practice.noTarget'); return; }
      this.status = this.hasPhrase ? '' : tt('practice.statusReady');
      await ensureReady(t);
    },

    async loadPhrase() {
      const en = (this.english || '').trim();
      const t = this.tgt;
      if (!en || !t || this.busy) return;
      this.busy = true; this.status = tt('practice.statusTranslating');
      this._clearScore();
      try {
        if (!(await ensureReady(t))) return;
        const row = await translateOne(en, t.id);
        const text = (row && row.text || '').trim();
        if (!text) { this.status = tt('practice.translateEmpty'); return; }
        this.target = text;
        this.tokens = (row.reading_aid_tokens) || [];
        this.attempts = [];
        this.status = tt('practice.statusSayIt');
      } catch (e) {
        ctx.Toasts?.push?.({ title: tt('practice.translateFailed'), msg: e?.message || String(e) });
        this.status = e?.message || String(e);
      } finally { this.busy = false; }
    },

    // Hear the target phrase spoken by a native TTS voice. Plays in-page (the
    // user's own output) via /api/tts — NOT the server-side Speak output route.
    async playTarget() {
      if (!this.hasPhrase || this.ttsBusy || this.recording) return;
      this.ttsBusy = true;
      try {
        const r = await fetch('/api/tts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: this.target }) });
        if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/tts'));
        const buf = await r.arrayBuffer();
        const url = URL.createObjectURL(new Blob([buf], { type: r.headers.get('Content-Type') || 'audio/wav' }));
        const audio = new Audio(url);
        audio.onended = audio.onerror = () => URL.revokeObjectURL(url);
        await audio.play();
      } catch (e) {
        ctx.Toasts?.push?.({ title: tt('practice.ttsFailed'), msg: e?.message || String(e) });
      } finally { this.ttsBusy = false; }
    },

    // Hold-to-record (press the mic, speak, release to score). Uses its OWN mic
    // stream and pauses the conversation mic so the attempt never reaches the
    // main translate flow.
    async holdStart() {
      if (this.busy || this.recording) return;
      const t = this.tgt;
      if (!t || !this.hasPhrase) return;
      if (!(await ensureReady(t))) return;
      try {
        const conv = Alpine.store('conversation');
        if (conv?.listening) await conv.suspendMic();
        await startCapture();
      } catch (e) { ctx.Toasts?.push?.({ title: tt('practice.micFailed'), msg: e?.message || String(e) }); return; }
      this.recording = true; this.status = tt('practice.statusRecording');
      recTimer = setTimeout(() => { if (S().recording) S().holdEnd(); }, MAX_REC_MS);
    },
    async holdEnd() {
      if (!this.recording) return;
      clearTimeout(recTimer); recTimer = null;
      this.recording = false;
      const pcm = stopCapture();
      Alpine.store('conversation')?.resumeMic?.();
      const t = this.tgt;
      if (!t) { this.status = tt('practice.statusSayIt'); return; }
      if (pcm.length < 16000 * 0.3) { this.status = tt('practice.tooShort'); return; } // <0.3s captured
      this.busy = true; this.status = tt('practice.statusScoring');
      try {
        const wav = buildWav(pcm);
        if (this.isAzure) await this._assessAzure(wav, t);
        else await this._scoreBasic(wav, t);
      } catch (e) {
        ctx.Toasts?.push?.({ title: tt('practice.scoreFailed'), msg: e?.message || String(e) });
        this.status = e?.message || String(e);
      } finally { this.busy = false; }
    },
    // Basic scorer: cloud ASR → local string-match score.
    async _scoreBasic(wav, t) {
      const tr = await transcribe(wav, t.asr);
      const spoken = (tr.text || '').trim();
      this.heard = { text: spoken, detected: tr.detected_language || '', engine: tr.asr_engine || '' };
      if (!spoken) {
        // ASR returned nothing — the most common real cause of a 0%. Say so
        // instead of scoring an empty string against the target.
        this.score = { has: false, value: 0, accepted: false, sub: null }; this.spokenHTML = '';
        this.status = tt('practice.noSpeech', { engine: this.heard.engine || tt('practice.unknownEngine') });
        return;
      }
      const sc = await postScore(this.target, spoken, t.furigana ? this.tokens : []);
      const value = Number(sc.score || 0), accepted = !!sc.accepted;
      const base = typeof sc.spoken_highlight_base === 'string' && sc.spoken_highlight_base ? sc.spoken_highlight_base : spoken;
      this.spokenHTML = highlightedSpoken(base, sc.spoken_match_ranges) || esc(spoken || '');
      this.score = { has: true, value, accepted, sub: null };
      this.attempts = [...this.attempts, { value, accepted }];
      this.status = accepted ? tt('practice.passed') : tt('practice.statusRetry', { n: this.threshold });
    },
    // Azure scorer: one call does ASR + phoneme-level pronunciation assessment,
    // returning per-dimension sub-scores and per-word accuracy/error types.
    async _assessAzure(wav, t) {
      const fd = new FormData();
      fd.append('file', wav, 'clip.wav');
      fd.append('language', t.asr);
      fd.append('expected', this.target);
      fd.append('threshold', String(this.threshold || 0));
      const r = await fetch('/api/assess', { method: 'POST', body: fd });
      if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/assess'));
      const a = await r.json();
      const spoken = (a.recognized_text || '').trim();
      this.heard = { text: spoken, detected: t.id, engine: 'Azure' };
      if (!spoken) {
        this.score = { has: false, value: 0, accepted: false, sub: null }; this.spokenHTML = '';
        this.status = tt('practice.noSpeech', { engine: 'Azure' });
        return;
      }
      const value = Math.round(Number(a.score || 0)), accepted = !!a.accepted;
      this.spokenHTML = azureWordsHTML(a.words || [], spoken);
      this.score = { has: true, value, accepted, sub: a.sub || null };
      this.attempts = [...this.attempts, { value, accepted }];
      this.status = accepted ? tt('practice.passed') : tt('practice.statusRetry', { n: this.threshold });
    },
    setThreshold(v) {
      const n = Math.max(1, Math.min(100, Number(v) || 0));
      this.threshold = n;
      postJSON('/api/settings', { practice: { score_threshold: n } }, 'POST /api/settings')
        .catch((e) => ctx.Toasts?.push?.({ title: tt('practice.title'), msg: e?.message || String(e) }));
    },
    cancelHold() {
      if (!this.recording) return;
      clearTimeout(recTimer); recTimer = null;
      this.recording = false; stopCapture();
      Alpine.store('conversation')?.resumeMic?.();
      this.status = tt('practice.statusSayIt');
    },
    retry() { this._clearScore(); this.status = tt('practice.statusSayIt'); },

    // Seed the panel from a translated conversation turn ("Practice this phrase").
    scoreableRow(turn) {
      if (!turn || turn.kind !== 'out' || !Array.isArray(turn.rows)) return null;
      return turn.rows.find((r) => r && SCOREABLE[r.language]) || null;
    },
    async practiceTurn(turn) {
      const row = this.scoreableRow(turn);
      if (!row) return;
      this.activeTargetId = row.language;
      this.open = true;
      this.english = turn.original || '';
      this.target = row.text || '';
      this.tokens = row.tokens || [];
      this.attempts = []; this._clearScore();
      const t = this.tgt;
      this.status = t ? tt('practice.statusSayIt') : tt('practice.noTarget');
      if (t) await ensureReady(t);
    },

    nextPhrase() {
      this.cancelHold();
      this.activeTargetId = '';
      this.english = ''; this.target = ''; this.tokens = []; this.attempts = [];
      this._clearScore(); this.status = tt('practice.statusReady');
      Alpine.nextTick(() => { const el = document.getElementById('ppEnglish'); if (el) el.focus(); });
    },
    _clearScore() { this.score = { has: false, value: 0, accepted: false, sub: null }; this.spokenHTML = ''; this.heard = { text: '', detected: '', engine: '' }; },

    init() {
      // Re-resolving target is reactive via getters; threshold + assessment mode
      // are refreshed by ensureReady on every open/target-change/attempt.
    },
  };
  return store;
}
