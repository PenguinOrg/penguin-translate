import Alpine from '/ui/shared/alpine.esm.js';
import { floatTo16kMono, buildWav, createSpeechSegmenter, scaleToRms, rmsToScale } from '/ui/shared/dsp.js';
import { getSileroGate } from '/ui/shared/vad/silero.js';
import { getSileroStream } from '/ui/shared/vad/silero-stream.js';
import { httpErrorMessage } from '/ui/shared/http.js';

let styleInjected = false;
function ensureStyle() {
  if (styleInjected) return;
  styleInjected = true;
  const l = document.createElement('link');
  l.rel = 'stylesheet';
  l.href = '/ui/features/conversation.css';
  document.head.appendChild(l);
}

export function createConversationStore(ctx) {
  ensureStyle();
  const tt = (k, p) => Alpine.store('i18n').t(k, p);
  const langName = (id, fb) => Alpine.store('i18n').langName(id, fb);
  // The module-local engine MUST read/write state through S() (the Alpine reactive
  // proxy); mutating the raw returned object directly would skip reactivity.
  const S = () => Alpine.store('conversation');

  let myLang = 'en', otherLangs = ['ja'];
  const catalog = new Map();
  const lang = (id) => catalog.get(id) || (id
    ? { id, label: id, short_label: id.toUpperCase(), flag: '🏳️', asr_code: id, reading_aid: 'none' }
    : { id: '', label: 'Detected', short_label: '··', flag: '🌐', asr_code: '', reading_aid: 'none' });

  async function loadCatalog() { try { const r = await fetch('/api/languages'); if (!r.ok) return; const j = await r.json(); (j.catalog || []).forEach((l) => catalog.set(l.id, l)); } catch (_) {} }
  async function loadLangsFromSettings() { try { const r = await fetch('/api/settings'); if (!r.ok) return; const p = (await r.json()).practice || {}; if (p.my_language) myLang = p.my_language; if (Array.isArray(p.other_languages) && p.other_languages.length) otherLangs = p.other_languages; } catch (_) {} }

  const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  const clock = () => { const d = new Date(); const z = (n) => (n < 10 ? '0' : '') + n; return z(d.getHours()) + ':' + z(d.getMinutes()) + ':' + z(d.getSeconds()); };
  function rubyHTML(text, tokens) {
    if (!Array.isArray(tokens) || !tokens.length) return esc(text);
    let out = '';
    for (const t of tokens) { if (!t || !t.surface) continue; out += t.reading ? '<ruby>' + esc(t.surface) + '<rt>' + esc(t.reading) + '</rt></ruby>' : esc(t.surface); }
    return out || esc(text);
  }
  let turnId = 0;
  const scrollAfter = () => Alpine.nextTick(() => { const t = document.getElementById('convThread'); if (t) t.scrollTop = t.scrollHeight; });
  const setStatus = (m) => { S().status = m; };

  function appendIncoming({ original, detectedId, translation, tokens }) {
    const L = lang(detectedId);
    S().turns.push({ id: ++turnId, kind: 'in', who: { flag: L.flag, label: langName(detectedId, L.label) }, original, transHTML: translation ? rubyHTML(translation, tokens) : esc(original), time: clock() });
    scrollAfter();
  }
  function appendOutgoing(original, results) {
    const rows = (results || []).map((r) => { const L = lang(r.language); return { flag: L.flag, label: r.label || L.label, textHTML: rubyHTML(r.text, r.reading_aid_tokens) }; });
    S().turns.push({ id: ++turnId, kind: 'out', srcShort: lang(myLang).short_label, original, rows, pending: false, time: clock() });
    scrollAfter();
  }

  async function transcribe(blob, langHint) {
    const fd = new FormData(); fd.append('file', blob, 'clip.wav'); if (langHint) fd.append('language', langHint);
    const r = await fetch('/api/transcribe', { method: 'POST', body: fd });
    if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/transcribe'));
    return r.json();
  }
  async function translateMulti(text, sourceLanguage, targets, fromSelf) {
    const r = await fetch('/api/translate-text', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text, source_language: sourceLanguage, target_languages: targets, from_self: !!fromSelf }) });
    if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/translate-text'));
    return r.json();
  }
  const incomingHint = () => (otherLangs.length === 1 ? lang(otherLangs[0]).asr_code : '');

  const cfg = { desktopOverlay: false, openvr: false, diarize: false, denoise: true };
  async function refreshCfg() {
    try {
      const a = (await (await fetch('/api/settings')).json()).audio || {};
      cfg.desktopOverlay = !!a.desktop_overlay_enabled; cfg.openvr = !!a.openvr_overlay_enabled;
      cfg.diarize = !!a.diarize_by_default; cfg.denoise = a.denoise_enabled !== false;
      detMode = a.speech_detection || 'streaming';
      if (Number(a.vad_sensitivity)) vadPct = Number(a.vad_sensitivity);
      if (Number(a.clip_max_sec) > 0) clipMaxSec = Number(a.clip_max_sec);
    } catch (_) {}
  }
  async function syncOverlaysFromSettings() {
    try { await fetch('/api/overlay/' + (cfg.openvr ? 'start' : 'stop'), { method: 'POST' }); } catch (_) {}
    try { await fetch('/api/desktop-overlay/' + (cfg.desktopOverlay ? 'start' : 'stop'), { method: 'POST' }); } catch (_) {}
    try { await fetch('/api/audio/apply-overlay-layout', { method: 'POST' }); } catch (_) {}
  }

  async function renderIncoming(text, detected, preset) {
    text = (text || '').trim(); if (!text) return;
    detected = (detected || incomingHint() || '').toLowerCase();
    if (typeof preset === 'string' && preset.trim()) { appendIncoming({ original: text, detectedId: detected, translation: preset.trim(), tokens: [] }); return; }
    if (detected && detected === myLang) { appendIncoming({ original: text, detectedId: detected, translation: '', tokens: [] }); return; }
    try {
      const out = await translateMulti(text, detected, [myLang]); const res = (out.results || [])[0] || {};
      appendIncoming({ original: text, detectedId: detected || out.source_language, translation: res.text || '', tokens: res.reading_aid_tokens || [] });
    } catch (e) { appendIncoming({ original: text, detectedId: detected, translation: '', tokens: [] }); setStatus(tt('run.status.Listening') + ' · ' + (e?.message || e)); }
  }
  async function handleIncomingClip(pcm) {
    if (!pcm || !pcm.length) return;
    setStatus(tt('conv.statusTranscribing'));
    const fd = new FormData(); fd.append('file', buildWav(pcm), 'clip.wav');
    fd.append('diarize', cfg.diarize ? '1' : '0'); fd.append('translate_to_en', '1');
    fd.append('language', incomingHint() || 'auto'); fd.append('openvr_overlay', cfg.openvr ? '1' : '0');
    let j; try { const r = await fetch('/api/transcribe-segment', { method: 'POST', body: fd }); if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/transcribe-segment')); j = await r.json(); }
    catch (e) { setStatus(tt('run.status.Listening') + ' · ' + (e?.message || e)); return; }
    if (!j.filtered) for (const seg of (j.segments || [])) { const t = (seg.text || '').trim(); if (t) await renderIncoming(t, j.language || incomingHint(), myLang === 'en' ? (seg.english || '') : ''); }
    if (S().listening) setStatus(tt('run.status.Listening'));
  }

  async function sendOutgoing(text) {
    text = (text || '').trim(); if (!text) return;
    const id = ++turnId;
    S().turns.push({ id, kind: 'out', srcShort: lang(myLang).short_label, original: text, rows: [], pending: true, time: clock() });
    scrollAfter();
    try {
      const out = await translateMulti(text, myLang, otherLangs, true);
      const rows = (out.results || []).map((r) => { const L = lang(r.language); return { flag: L.flag, label: r.label || L.label, textHTML: rubyHTML(r.text, r.reading_aid_tokens) }; });
      const idx = S().turns.findIndex((t) => t.id === id);
      if (idx >= 0) { S().turns[idx].rows = rows; S().turns[idx].pending = false; }
      scrollAfter();
    } catch (e) {
      const idx = S().turns.findIndex((t) => t.id === id); if (idx >= 0) S().turns.splice(idx, 1);
      ctx.Toasts?.push?.({ title: tt('conv.replyFailed'), msg: e?.message || String(e) });
      setStatus(tt('conv.replyFailedStatus', { err: e?.message || e }));
    }
  }
  async function handleMicClip(pcm) {
    if (!pcm || !pcm.length) return;
    setStatus(tt('conv.statusTranscribingSpeech'));
    let tr; try { tr = await transcribe(buildWav(pcm), lang(myLang).asr_code); } catch (e) { setStatus(tt('run.status.Listening') + ' · ' + (e?.message || e)); return; }
    const text = (tr.text || '').trim(); if (text) await sendOutgoing(text);
    if (S().listening) setStatus(tt('run.status.Listening'));
  }

  const VAD_FRAME_SAMPLES = 320, VAD_FRAME_MS = 20;
  let vadPct = 15, clipMaxSec = 3.5;
  let detMode = 'streaming';
  const speechGate = getSileroGate();
  const speechStream = getSileroStream();
  function chunkParams() {
    const rmsSpeech = Math.max(0.006, scaleToRms(vadPct)), rmsQuiet = rmsSpeech * 0.65;
    const minSamples = Math.floor(16000 * 0.2), maxSamples = Math.floor(16000 * clipMaxSec);
    const longAfterSamples = Math.floor(16000 * 2.0);
    return { rmsSpeech, rmsQuiet, minSamples, maxSamples, longAfterSamples, silenceFrames: Math.round(450 / VAD_FRAME_MS), silenceLongFrames: Math.round(180 / VAD_FRAME_MS), fragmentSamples: Math.floor(16000 * 1.0), fragmentExtraSilenceFrames: Math.round(500 / VAD_FRAME_MS), loudFrames: 2, frameSamples: VAD_FRAME_SAMPLES };
  }

  function makeLane(kind, onClip) {
    const st = () => S().lanes[kind];
    const lane = { kind, audioCtx: null, stream: null, proc: null, silent: null, ws: null, runtime: null, meter: null, queue: [], draining: false };
    const seg = createSpeechSegmenter({
      params: () => chunkParams(),
      onMeter: (rms) => { if (lane.meter) lane.meter.style.width = rmsToScale(rms) + '%'; },
      onSegment: (clip) => { if (st().enabled) { lane.queue.push(clip); void drain(); } },
    });
    lane.seg = seg;
    lane.sink = (frames) => { if (detMode === 'streaming') speechStream.feed(kind, frames); else seg.feed(frames); };
    async function drain() {
      if (lane.draining) return; lane.draining = true;
      try {
        while (S().listening && st().enabled && lane.queue.length) {
          const clip = lane.queue.shift();
          if (detMode === 'filter' && !(await speechGate.hasSpeech(clip))) continue;
          await onClip(clip);
        }
      } finally { lane.draining = false; }
    }
    lane.drain = drain;

    lane.startMic = async () => {
      lane.audioCtx = new (window.AudioContext || window.webkitAudioContext)();
      const dev = st().device;
      lane.stream = await navigator.mediaDevices.getUserMedia({ audio: { deviceId: dev ? { exact: dev } : undefined, channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
      const src = lane.audioCtx.createMediaStreamSource(lane.stream);
      lane.proc = lane.audioCtx.createScriptProcessor(4096, 1, 1);
      lane.proc.onaudioprocess = (e) => { if (!(S().listening && st().enabled)) return; lane.sink(floatTo16kMono(e.inputBuffer.getChannelData(0), e.inputBuffer.sampleRate)); };
      lane.silent = lane.audioCtx.createGain(); lane.silent.gain.value = 0; src.connect(lane.proc); lane.proc.connect(lane.silent); lane.silent.connect(lane.audioCtx.destination);
    };
    lane.startSys = () => new Promise((resolve, reject) => {
      const open = () => {
        const url = lane.runtime?.loopback_ws_url || 'ws://127.0.0.1:8746/ws/loopback'; const ws = new WebSocket(url); lane.ws = ws; ws.binaryType = 'arraybuffer';
        let settled = false; const fail = (m) => { if (!settled) { settled = true; reject(new Error(m)); } }; const okk = () => { if (!settled) { settled = true; resolve(); } };
        const timer = setTimeout(() => fail('System audio start timed out — is the audio sidecar ready?'), 12000);
        ws.onopen = () => ws.send(JSON.stringify({ cmd: 'start', device_id: st().device || null }));
        ws.onmessage = (ev) => { if (typeof ev.data === 'string') { try { const j = JSON.parse(ev.data); if (j.error) { clearTimeout(timer); fail(j.error); return; } if (j.status === 'capturing') { clearTimeout(timer); okk(); } } catch (_) {} return; } clearTimeout(timer); okk(); if (st().enabled) lane.sink(new Int16Array(ev.data)); };
        ws.onerror = () => { clearTimeout(timer); fail('System-audio WebSocket failed — is the Go audio sidecar on :8746?'); };
        ws.onclose = () => { clearTimeout(timer); if (S().listening && lane.ws === ws) lane.ws = null; };
      };
      fetch('/api/audio/runtime').then((r) => r.ok ? r.json() : null).then((j) => { lane.runtime = j; open(); }).catch(open);
    });
    lane.start = async () => { seg.reset(); lane.queue = []; if (detMode === 'streaming') speechStream.reset(kind); if (kind === 'mic') await lane.startMic(); else await lane.startSys(); };
    lane.stop = () => {
      if (lane.ws) { try { lane.ws.send(JSON.stringify({ cmd: 'stop' })); lane.ws.close(); } catch (_) {} lane.ws = null; }
      try { if (lane.proc) lane.proc.disconnect(); if (lane.silent) lane.silent.disconnect(); } catch (_) {}
      lane.proc = null; lane.silent = null;
      if (lane.stream) { lane.stream.getTracks().forEach((t) => t.stop()); lane.stream = null; }
      if (lane.audioCtx) { lane.audioCtx.close().catch(() => {}); lane.audioCtx = null; }
      lane.queue = []; if (lane.meter) lane.meter.style.width = '0';
    };
    return lane;
  }

  const micLane = makeLane('mic', handleMicClip);
  const sysLane = makeLane('sys', handleIncomingClip);

  speechStream.onClip((laneId, pcm) => { const ln = laneId === 'mic' ? micLane : sysLane; if (S().listening && S().lanes[laneId].enabled) { ln.queue.push(pcm); void ln.drain(); } });
  speechStream.onProb((laneId, prob) => { const ln = laneId === 'mic' ? micLane : sysLane; if (ln.meter) ln.meter.style.width = Math.min(100, Math.round(prob * 100)) + '%'; });
  speechStream.onBroken(() => { if (detMode === 'streaming') { detMode = 'filter'; setStatus(tt('run.status.Listening') + ' · speech detection fell back to RMS + filter'); if (S().listening) restartLanes(); } });

  async function restartLanes() {
    for (const ln of [micLane, sysLane]) if (S().lanes[ln.kind].enabled) ln.stop();
    if (detMode === 'streaming') { const ok = await speechStream.warm(clipMaxSec); if (!ok) detMode = 'filter'; }
    if (detMode === 'filter') speechGate.warm();
    for (const ln of [micLane, sysLane]) if (S().lanes[ln.kind].enabled) { try { await ln.start(); } catch (e) { S().lanes[ln.kind].enabled = false; } }
  }

  async function populateMics() {
    if (!navigator.mediaDevices?.enumerateDevices) return;
    const prev = S().lanes.mic.device;
    const inputs = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === 'audioinput');
    S().lanes.mic.devices = inputs.map((d, i) => ({ id: d.deviceId, label: d.label || ('Microphone ' + (i + 1)) }));
    if (prev && S().lanes.mic.devices.some((o) => o.id === prev)) S().lanes.mic.device = prev;
    else if (S().lanes.mic.devices.length && !S().lanes.mic.device) S().lanes.mic.device = S().lanes.mic.devices[0].id;
  }
  async function populateOutputs() {
    try {
      const r = await fetch('/api/loopback/devices'); const j = r.ok ? await r.json() : { devices: [] }; const devs = j.devices || [];
      if (!devs.length) { S().lanes.sys.devices = [{ id: '', label: '(no outputs found)' }]; return; }
      S().lanes.sys.devices = devs.map((d) => ({ id: d.id || '', label: (d.name || d.id || 'Output') + (d.is_default ? ' · default' : '') + (d.loopback_ok === false ? ' · no loopback' : ''), disabled: d.loopback_ok === false }));
      const def = devs.find((d) => d.is_default && d.loopback_ok !== false);
      if (def && !S().lanes.sys.device) S().lanes.sys.device = def.id || '';
      else if (!S().lanes.sys.device) S().lanes.sys.device = S().lanes.sys.devices[0].id;
    } catch (_) { S().lanes.sys.devices = [{ id: '', label: '(outputs unavailable)' }]; }
  }

  async function startListen() {
    await refreshCfg();
    if (detMode === 'streaming') { const ok = await speechStream.warm(clipMaxSec); if (!ok) detMode = 'filter'; }
    if (detMode === 'filter') speechGate.warm();
    S().listening = true;
    const errs = [];
    for (const lane of [micLane, sysLane]) {
      if (!S().lanes[lane.kind].enabled) continue;
      try { await lane.start(); } catch (e) { S().lanes[lane.kind].enabled = false; errs.push((lane.kind === 'mic' ? tt('conv.mic') : tt('conv.sys')) + ': ' + (e?.message || e)); }
    }
    if (!micLane.audioCtx && !sysLane.ws && errs.length) { S().listening = false; throw new Error(errs.join(' · ')); }
    if (errs.length) ctx.Toasts?.push?.({ title: tt('conv.oneInputFailed'), msg: errs.join(' · ') });
    if (cfg.desktopOverlay) { try { await fetch('/api/desktop-overlay/start', { method: 'POST' }); await fetch('/api/audio/apply-overlay-layout', { method: 'POST' }); } catch (_) {} }
    if (cfg.openvr) { try { await fetch('/api/overlay/start', { method: 'POST' }); } catch (_) {} }
    setStatus(tt('run.status.Listening'));
    if (S().lanes.mic.enabled) populateMics().catch(() => {});
  }
  function stopListen() {
    S().listening = false; micLane.stop(); sysLane.stop();
    fetch('/api/desktop-overlay/clear', { method: 'POST' }).catch(() => {});
    fetch('/api/overlay/clear', { method: 'POST' }).catch(() => {});
    setStatus(tt('conv.statusStopped'));
  }

  let busy = false;
  function bindMeters() {
    micLane.meter = document.querySelector('.runbar .lane-mic .lane-meter > i');
    sysLane.meter = document.querySelector('.runbar .lane-sys .lane-meter > i');
  }

  const store = {
    id: 'conversation',
    turns: [],
    listening: false,
    status: '',
    composer: '',
    lanes: { mic: { enabled: true, devices: [], device: '' }, sys: { enabled: true, devices: [], device: '' } },
    get empty() { return this.turns.length === 0; },
    get statusText() { return this.status || (this.listening ? tt('run.status.Listening') : tt('run.status.Idle')); },

    async toggleListen() {
      if (busy) return; busy = true;
      const wasRunning = this.listening;
      try { if (wasRunning) stopListen(); else await startListen(); }
      catch (e) { ctx.Toasts?.push?.({ title: tt(wasRunning ? 'run.couldNotStop' : 'run.couldNotStart', { name: tt('conv.title') || 'Conversation' }), msg: String(e?.message || e) }); }
      finally { busy = false; }
    },
    async toggleLane(kind) {
      const stl = this.lanes[kind]; stl.enabled = !stl.enabled;
      if (!this.listening) return;
      const lane = kind === 'mic' ? micLane : sysLane;
      if (stl.enabled) { try { await lane.start(); } catch (e) { stl.enabled = false; ctx.Toasts?.push?.({ title: tt('conv.laneStartFailed', { lane: kind === 'mic' ? tt('conv.mic') : tt('conv.sys') }), msg: e?.message || String(e) }); } }
      else lane.stop();
    },
    changeDevice(kind) {
      if (!this.listening || !this.lanes[kind].enabled) return;
      const lane = kind === 'mic' ? micLane : sysLane;
      lane.stop(); lane.start().catch((e) => setStatus(String(e?.message || e)));
    },
    sendComposer(text) { this.composer = ''; sendOutgoing(text || ''); },

    _simulateIncoming(text, detected) { return renderIncoming(text, detected); },
    _langs() { return { my: myLang, others: otherLangs.slice(), incomingHint: incomingHint() }; },

    async init() {
      // Register before any await: the langs store broadcasts `langschange` while
      // the fetches below are in flight; missing it would leave us on the default
      // language (the "captions come back in Japanese" bug).
      document.addEventListener('langschange', (e) => { const d = e.detail || {}; if (d.my) myLang = d.my; if (Array.isArray(d.others)) otherLangs = d.others; });
      bindMeters();
      await loadCatalog();
      await loadLangsFromSettings();
      await refreshCfg();
      syncOverlaysFromSettings().catch(() => {});
      await populateMics().catch(() => {});
      await populateOutputs().catch(() => {});
      document.addEventListener('audioprefschange', (e) => { const d = e.detail || {}; if (Number(d.vad_sensitivity) > 0) vadPct = Number(d.vad_sensitivity); if (typeof d.denoise_enabled === 'boolean') cfg.denoise = d.denoise_enabled; if (typeof d.speech_detection === 'string' && d.speech_detection !== detMode) { detMode = d.speech_detection; if (S().listening) restartLanes(); } if (Number(d.clip_max_sec) > 0) clipMaxSec = Number(d.clip_max_sec); });
      setStatus(tt('conv.statusIdle'));
    },
  };

  return store;
}
