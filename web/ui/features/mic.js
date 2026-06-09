import { floatTo16kMono, buildWav, createSpeechSegmenter } from '/ui/shared/dsp.js';
import { httpErrorMessage } from '/ui/shared/http.js';

let styleInjected = false;
function ensureStyle() {
  if (styleInjected) return;
  styleInjected = true;
  const l = document.createElement('link');
  l.rel = 'stylesheet';
  l.href = '/ui/features/mic.css';
  document.head.appendChild(l);
}

export function createMicView(ctx) {
  ensureStyle();

  let root = null, elControls = null, elLang = null, elMic = null, elMicLevelBar = null;
  const q = (sel) => (root ? root.querySelector(sel) : null);

  const Phase = { Idle: 'Idle', ENRec: 'ENRec', Translating: 'Translating', Review: 'Review', JARec: 'JARec', Verifying: 'Verifying', Passed: 'Passed', RetryPrompt: 'RetryPrompt', Done: 'Done' };
  let currentPhase = Phase.Idle, lastMsg = 'Type or speak an English sentence.';
  const runSubs = new Set();
  const notifyRun = () => runSubs.forEach((cb) => { try { cb(); } catch (_) {} });
  const isRunning = () => listening || pipelineBusy || textTranslateBusy;

  let listening = false, recordingMode = 'en', continuousSession = false, stopRequested = false,
    pipelineBusy = false, segQueue = [], textTranslateBusy = false, textTranslateTimer = null, textTranslateSeq = 0,
    autoTtsTimer = null, autoTtsSeq = 0, autoTtsUtteranceId = 0, ttsSpeakBusy = false, lastSpeechEndedAt = 0,
    currentSegmentSpeechStartAt = 0, audioCtx = null, stream = null, proc = null, silent = null,
    currentSentence = null, history = [], currentAttemptBlob = null, settings = null, langProfiles = [],
    sessionResumeAttempted = false, engineReady = false;

  async function persistSessionActive(active) { try { await fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ practice: { session_active: active } }) }); if (settings) settings.session_active = active; } catch (_) {} }
  const basicMode = () => !!window.UIMode?.isBasic();
  const practiceEnabled = () => !basicMode() && !!settings?.practice_enabled;

  async function ensureCloudInBasic() {
    if (!basicMode() || !settings) return;
    const prov = settings.api_provider === 'openai' ? 'openai' : 'openrouter';
    const localAsr = (settings.english_asr_engine || 'whisper') === 'whisper';
    const localFwd = (settings.forward_translator || 'nllb') === 'nllb';
    if (!localAsr && !localFwd) return;
    try {
      const r = await fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ practice: { asr_engine: prov, english_asr_engine: prov, ja_repeat_asr_engine: prov, forward_translator: 'openai' } }) });
      if (r.ok) { settings = (await r.json()).practice; applySettingsToUI(); }
    } catch (_) {}
  }

  function syncPracticeUI() {
    const on = practiceEnabled();
    for (const id of ['practiceMainUI', 'practiceBottomUI']) { const el = q('#' + id); if (el) el.style.display = on ? '' : 'none'; }
    const bp = q('#btnBeginPractice'); if (bp) bp.style.display = on ? '' : 'none';
    const rep = q('#btnSpeakRef'); if (rep) rep.style.display = on ? '' : 'none';
    const ps = q('#phaseScore'); if (ps && !on) { ps.hidden = true; ps.textContent = ''; ps.className = 'score-chip'; }
    if (!on) { clearScore(); clearSpokenJa(); const be = q('#backEn'); if (be) be.textContent = ''; }
  }

  const autoTtsEnabled = () => !!settings?.continuous_tts_repeat;
  const autoTtsIdleMs = () => { const v = Number(settings?.tts_repeat_debounce_ms); return Number.isFinite(v) && v >= 500 ? v : 1500; };
  function cancelAutoTtsSchedule() { clearTimeout(autoTtsTimer); autoTtsTimer = null; autoTtsSeq++; }
  function scheduleAutoTts() {
    if (!autoTtsEnabled()) return;
    const t = sentenceTarget(currentSentence); if (!t) return;
    cancelAutoTtsSchedule();
    const seq = ++autoTtsSeq, utterId = autoTtsUtteranceId, ms = autoTtsIdleMs();
    autoTtsTimer = setTimeout(() => {
      if (seq !== autoTtsSeq || utterId !== autoTtsUtteranceId) return;
      if (!sentenceTarget(currentSentence)) return;
      speakReference({ auto: true }).catch((e) => setPhase(currentPhase, 'TTS: ' + (e?.message || String(e))));
    }, ms);
  }

  const showSubmittedEnglish = (text) => { const el = q('#enInput'); if (!el) return; el.value = ''; el.placeholder = (text || '').trim() || 'Type English here…'; };
  const syncEnInputFromSentence = () => showSubmittedEnglish(currentSentence?.english || '');

  function applyPipelineResult(pr, { merge = false } = {}) {
    const chunkEn = (pr.english || '').trim(), chunkTarget = (pr.target || pr.japanese || '').trim(),
      chunkBack = practiceEnabled() ? (pr.back_english || '').trim() : '', chunkFuri = Array.isArray(pr.furigana) ? pr.furigana : [];
    if (merge && currentSentence) {
      const english = [currentSentence.english, chunkEn].filter(Boolean).join(' '), target = (currentSentence.target || '') + chunkTarget,
        furigana = currentProfile().has_furigana ? [...(currentSentence.furigana || []), ...chunkFuri] : [],
        backEnglish = [currentSentence.backEnglish, chunkBack].filter(Boolean).join(' ');
      currentSentence = { ...currentSentence, english, target, japanese: target, backEnglish, furigana };
    } else {
      currentSentence = { ts: Date.now(), english: chunkEn, target: chunkTarget, japanese: chunkTarget, backEnglish: chunkBack, furigana: chunkFuri, attempts: [], passedAttemptIdx: null };
    }
    syncEnInputFromSentence();
    renderFurigana(currentSentence.furigana);
    if (practiceEnabled()) q('#backEn').textContent = currentSentence.backEnglish || '';
    clearSpokenJa(); clearScore(); renderAttemptChips();
    q('#btnBeginPractice').disabled = !practiceEnabled() || !sentenceTarget(currentSentence);
    q('#btnTryAgain').disabled = true;
    autoTtsUtteranceId++; scheduleAutoTts();
  }

  function currentProfile() { const id = settings?.target_language || 'jp'; return langProfiles.find((p) => p.id === id) || langProfiles[0] || { id: 'jp', label: 'Japanese', short_label: 'JP', target_asr_lang: 'ja', tts_lang: 'ja-JP', has_furigana: true }; }
  const sentenceTarget = (s) => (s?.target || s?.japanese || '').trim();

  function applyLanguageUI() {
    const p = currentProfile();
    const sub = q('#micSub'); if (sub) sub.textContent = `EN → ${p.label || 'target'}` + (practiceEnabled() ? ' · practice' : '');
    const bt = q('#backTransLabel'); if (bt) bt.textContent = `Back translation (${p.short_label || '?'} → EN)`;
    const sl = q('#spokenLabel'); if (sl) sl.textContent = `What you said (${p.label || 'target'})`;
    const tf = q('#btnToggleFuri'); if (tf) tf.style.display = p.has_furigana ? 'inline-block' : 'none';
    syncPracticeUI();
  }

  const loudFrames = 2, minSamples = Math.floor(16000 * .2), maxSamples = Math.floor(16000 * 30), utteranceGapMs = 4500,
    fragmentSamples = Math.floor(16000 * 1.0), fragmentExtraQuietFrames = 6;
  const rmsSpeechThreshold = () => { const v = Number(settings?.mic_sensitivity); return (Number.isFinite(v) && v > 0 ? v : 18) / 1000; };
  const rmsQuietThreshold = () => rmsSpeechThreshold() * 0.444;
  const vadFrameMs = () => { const sr = audioCtx?.sampleRate || 48000, chunkLen = Math.max(1, Math.floor(4096 / (sr / 16000))); return (chunkLen / 16000) * 1000; };
  function silenceMsNeeded() { const def = continuousSession ? 2700 : 1500; if (!settings) return def; const v = continuousSession ? settings.vad_silence_continuous_ms : settings.vad_silence_ms; return Number.isFinite(Number(v)) && Number(v) > 0 ? Number(v) : def; }
  const quietFramesNeeded = () => Math.max(1, Math.round(silenceMsNeeded() / Math.max(1, vadFrameMs())));
  const shouldMergeContinuous = (speechStartAt) => !!(continuousSession && currentSentence && lastSpeechEndedAt && speechStartAt && (speechStartAt - lastSpeechEndedAt) < utteranceGapMs);
  function resetContinuousTiming() { lastSpeechEndedAt = 0; currentSegmentSpeechStartAt = 0; }
  function finalizeUtteranceIfIdle() { if (!continuousSession || !listening || pipelineBusy || micSegmenter.inSpeech || !currentSentence || !lastSpeechEndedAt) return; if ((Date.now() - lastSpeechEndedAt) < utteranceGapMs) return; setPhase(Phase.ENRec, 'Continuous — speak or type English (Stop to end)…'); }

  const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  function clearSpokenJa() { const el = q('#spokenJa'); if (!el) return; el.textContent = ''; el.classList.remove('has-hit'); }
  function setSpokenJaPlain(t) { const el = q('#spokenJa'); if (!el) return; el.textContent = t || ''; el.classList.remove('has-hit'); }
  function setSpokenJaHighlighted(base, ranges) {
    const el = q('#spokenJa'); if (!el) return;
    if (!base || !Array.isArray(ranges) || !ranges.length) { setSpokenJaPlain(base || ''); return; }
    const parts = []; let cur = 0;
    const sorted = ranges.filter((r) => Array.isArray(r) && r.length >= 2).map((r) => [Math.max(0, r[0] | 0), Math.max(0, r[1] | 0)]).sort((a, b) => a[0] - b[0]);
    for (const [a, b] of sorted) { if (b <= a) continue; const aa = Math.min(a, base.length), ee = Math.min(b, base.length); if (ee <= aa) continue; if (aa > cur) parts.push(esc(base.slice(cur, aa))); parts.push('<span class="hit">' + esc(base.slice(aa, ee)) + '</span>'); cur = Math.max(cur, ee); }
    if (cur < base.length) parts.push(esc(base.slice(cur)));
    el.innerHTML = parts.join(''); el.classList.add('has-hit');
  }
  function rangesFromNormalized(expectedNorm, spokenNorm) {
    if (!expectedNorm || !spokenNorm) return [];
    const a = [...expectedNorm], b = [...spokenNorm], dp = Array.from({ length: a.length + 1 }, () => Array(b.length + 1).fill(0));
    for (let i = a.length - 1; i >= 0; i--) for (let j = b.length - 1; j >= 0; j--) dp[i][j] = a[i] === b[j] ? 1 + dp[i + 1][j + 1] : Math.max(dp[i + 1][j], dp[i][j + 1]);
    const matched = new Array(b.length).fill(false); let i = 0, j = 0;
    while (i < a.length && j < b.length) { if (a[i] === b[j]) { matched[j] = true; i++; j++; } else if (dp[i + 1][j] >= dp[i][j + 1]) i++; else j++; }
    const out = []; let s = -1;
    for (let k = 0; k < matched.length; k++) { if (matched[k] && s < 0) s = k; else if (!matched[k] && s >= 0) { out.push([s, k]); s = -1; } }
    if (s >= 0) out.push([s, matched.length]);
    return out;
  }
  function applyHighlightFromScore(sc, spokenRaw) {
    const hiBase = typeof sc?.spoken_highlight_base === 'string' ? sc.spoken_highlight_base : '', hiRanges = Array.isArray(sc?.spoken_match_ranges) ? sc.spoken_match_ranges : [];
    if (hiBase && hiRanges.length) { setSpokenJaHighlighted(hiBase, hiRanges); return { base: hiBase, ranges: hiRanges }; }
    const sn = typeof sc?.normalized_spoken === 'string' ? sc.normalized_spoken : '', en = typeof sc?.normalized_expected_reading === 'string' && sc.normalized_expected_reading ? sc.normalized_expected_reading : (typeof sc?.normalized_expected === 'string' ? sc.normalized_expected : '');
    if (sn && en) { const rr = rangesFromNormalized(en, sn); if (rr.length) { setSpokenJaHighlighted(sn, rr); return { base: sn, ranges: rr }; } }
    setSpokenJaPlain(spokenRaw || '(no transcript)'); return { base: '', ranges: [] };
  }
  function renderSpokenFromAttempt(a) { if (!a) { clearSpokenJa(); return; } if (Array.isArray(a.spoken_match_ranges) && a.spoken_match_ranges.length && typeof a.spoken_highlight_base === 'string' && a.spoken_highlight_base.length) setSpokenJaHighlighted(a.spoken_highlight_base, a.spoken_match_ranges); else setSpokenJaPlain(a.transcript || '(empty)'); }
  function syncNextButton() { const busy = [Phase.ENRec, Phase.JARec, Phase.Translating, Phase.Verifying, Phase.Passed]; const bn = q('#btnNext'); if (bn) bn.disabled = !currentSentence || busy.includes(currentPhase); }

  function setPhase(p, m = '') { currentPhase = p; if (m) lastMsg = m; syncNextButton(); notifyRun(); }

  function renderFurigana(tokens) {
    const w = q('#jpRubyWrap'); if (!w) return; const p = currentProfile();
    w.classList.toggle('lang-zh', p.id === 'zh'); w.classList.toggle('lang-ko', p.id === 'ko');
    w.innerHTML = ''; w.classList.remove('jp-placeholder');
    const target = sentenceTarget(currentSentence);
    if (!p.has_furigana) { if (target) { w.textContent = target; return; } w.classList.add('jp-placeholder'); w.textContent = 'Target language text appears here after you capture an English sentence.'; return; }
    const a = Array.isArray(tokens) ? tokens : [];
    if (!a.length) { if (target) { w.textContent = target; return; } w.classList.add('jp-placeholder'); w.textContent = 'Target text appears here after you capture an English sentence.'; return; }
    for (const t of a) { if (!t || !t.surface) continue; if (!t.reading) { w.appendChild(document.createTextNode(t.surface)); continue; } const rb = document.createElement('ruby'); rb.appendChild(document.createTextNode(t.surface)); const rt = document.createElement('rt'); rt.textContent = t.reading; rb.appendChild(rt); w.appendChild(rb); }
  }
  function setScore(s, ok) { const c = q('#phaseScore'); if (!c) return; c.hidden = false; c.textContent = Number(s).toFixed(1) + '%'; c.className = 'score-chip ' + (ok ? 'ok' : 'bad'); }
  function clearScore() { const c = q('#phaseScore'); if (!c) return; c.hidden = true; c.textContent = ''; c.className = 'score-chip'; }

  async function postPipeline(blob) { const fd = new FormData(); fd.append('file', blob, 'clip.wav'); fd.append('speech_language', 'en'); fd.append('backtranslate', practiceEnabled() ? (settings?.backtranslate || 'local') : 'none'); const r = await fetch('/api/pipeline', { method: 'POST', body: fd }); if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/pipeline')); return r.json(); }
  async function postTranslateText(english) { const r = await fetch('/api/translate-text', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ english }) }); if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/translate-text')); return r.json(); }
  const reviewPhaseMsg = () => practiceEnabled() ? `Review ${currentProfile().label || 'target'} then begin practice.` : 'Translation ready.';
  function clearEnInput() { textTranslateSeq++; clearTimeout(textTranslateTimer); cancelAutoTtsSchedule(); const el = q('#enInput'); if (el) { el.value = ''; el.placeholder = 'Type English here…'; } }
  function scheduleTextTranslate() { cancelAutoTtsSchedule(); clearTimeout(textTranslateTimer); }
  async function runTextTranslate({ englishOverride = '', clearOnSuccess = false, autoSpeak = false } = {}) {
    const english = (englishOverride || q('#enInput').value || '').trim(); const seq = ++textTranslateSeq; if (!english) return;
    textTranslateBusy = true; notifyRun();
    const wasEnListen = listening && recordingMode === 'en'; const cont = continuousSession && wasEnListen && !stopRequested;
    if (!wasEnListen) setPhase(Phase.Translating, 'Translating typed English…');
    try {
      const pr = await postTranslateText(english); if (seq !== textTranslateSeq) return;
      applyPipelineResult(pr, { merge: false });
      if (clearOnSuccess) { q('#enInput').value = ''; q('#enInput').placeholder = english; }
      if (autoSpeak && sentenceTarget(currentSentence)) { cancelAutoTtsSchedule(); await speakReference({ auto: true }); }
      if (wasEnListen || cont) setPhase(Phase.ENRec, cont ? 'Continuous — speak or type English (Stop to end)…' : 'Listening for English — type or speak…');
      else setPhase(Phase.Review, sentenceTarget(currentSentence) ? reviewPhaseMsg() : 'Could not produce translation.');
    } finally { textTranslateBusy = false; notifyRun(); }
  }
  async function postTranscribeTarget(blob) { const p = currentProfile(); const fd = new FormData(); fd.append('language', p.target_asr_lang || 'ja'); fd.append('file', blob, 'clip.wav'); const r = await fetch('/api/transcribe', { method: 'POST', body: fd }); if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/transcribe')); return r.json(); }
  async function postScore(expected, spoken, furigana) { const body = { expected, spoken, threshold: settings?.score_threshold || 100 }; if (Array.isArray(furigana) && furigana.length && currentProfile().has_furigana) body.furigana = furigana; const r = await fetch('/api/score', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/score')); return r.json(); }
  async function postPlayWav(blob) { const fd = new FormData(); fd.append('file', blob, 'attempt.wav'); const r = await fetch('/api/play-wav', { method: 'POST', body: fd }); if (!r.ok) throw new Error(await httpErrorMessage(r)); return r.json(); }

  async function refreshMics() { const sel = elMic; if (!sel) return; sel.innerHTML = ''; if (!navigator.mediaDevices?.enumerateDevices) return; const list = await navigator.mediaDevices.enumerateDevices(), inputs = list.filter((d) => d.kind === 'audioinput'); for (const [i, d] of inputs.entries()) { const o = document.createElement('option'); o.value = d.deviceId; o.textContent = d.label || ('Mic ' + (i + 1)); sel.appendChild(o); } }
  function teardownAudio() {
    listening = false; continuousSession = false; stopRequested = false; resetContinuousTiming();
    try { if (proc) proc.disconnect(); if (silent) silent.disconnect(); } catch (_) {}
    proc = null; silent = null;
    if (stream) { stream.getTracks().forEach((t) => t.stop()); stream = null; }
    if (audioCtx) { audioCtx.close().catch(() => {}); audioCtx = null; }
    updateMicLevel(0); notifyRun();
  }
  async function startListen() {
    await refreshMics().catch(() => {}); const id = elMic?.value;
    if (!id) { setPhase(currentPhase, 'Pick a microphone first.'); return; }
    recordingMode = recordingMode || 'en'; continuousSession = recordingMode === 'en'; stopRequested = false; resetContinuousTiming();
    const p = currentProfile();
    audioCtx = new (window.AudioContext || window.webkitAudioContext)();
    stream = await navigator.mediaDevices.getUserMedia({ audio: { deviceId: { exact: id }, channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
    const src = audioCtx.createMediaStreamSource(stream);
    proc = audioCtx.createScriptProcessor(4096, 1, 1); proc.onaudioprocess = onAudioProcess;
    silent = audioCtx.createGain(); silent.gain.value = 0;
    src.connect(proc); proc.connect(silent); silent.connect(audioCtx.destination);
    micSegmenter.reset(); listening = true; notifyRun();
    if (recordingMode === 'en') setPhase(Phase.ENRec, continuousSession ? 'Continuous — speak or type English (Stop to end)…' : 'Listening — speak or type English…');
    else setPhase(Phase.JARec, `Listening for ${p.label || 'target'} repeat…`);
    persistSessionActive(true).catch(() => {});
  }
  function stopListen() {
    stopRequested = true; if (continuousSession && currentSentence) commitSentenceToHistory();
    teardownAudio(); persistSessionActive(false).catch(() => {});
    if (currentPhase === Phase.ENRec || currentPhase === Phase.JARec || currentPhase === Phase.Translating) setPhase(Phase.Idle, 'Stopped. Press Start to continue.');
  }

  const micSegmenter = createSpeechSegmenter({
    params: () => { const qf = quietFramesNeeded(); return { rmsSpeech: rmsSpeechThreshold(), rmsQuiet: rmsQuietThreshold(), loudFrames, minSamples, maxSamples, fragmentSamples, fragmentExtraSilenceFrames: fragmentExtraQuietFrames, longAfterSamples: Infinity, silenceLongFrames: qf, silenceFrames: qf, frameSamples: 0 }; },
    onMeter: (r) => updateMicLevel(r),
    onSpeechStart: () => { cancelAutoTtsSchedule(); currentSegmentSpeechStartAt = Date.now(); },
    onQuietWhileIdle: () => finalizeUtteranceIfIdle(),
    onSegment: (pcm) => { if (!pcm.length) return; segQueue.push({ mode: recordingMode, pcm, speechStartAt: currentSegmentSpeechStartAt }); currentSegmentSpeechStartAt = 0; void drainSegQueue(); },
  });
  async function drainSegQueue() {
    if (pipelineBusy) return; pipelineBusy = true; notifyRun();
    try { while (segQueue.length > 0) { const { mode, pcm, speechStartAt } = segQueue.shift(); await processClip(mode, pcm, speechStartAt); } }
    finally { pipelineBusy = false; notifyRun(); }
  }
  async function processClip(mode, pcm, speechStartAt) {
    const wavBlob = buildWav(pcm);
    if (mode === 'ja' && !practiceEnabled()) return;
    if (mode === 'en') {
      const cont = continuousSession && !stopRequested;
      setPhase(Phase.Translating, (settings?.pipeline_mode === 'multimodal') ? 'Multimodal transcribe + translate…' : 'Transcribing and translating…');
      try {
        const pr = await postPipeline(wavBlob);
        if (cont && !stopRequested) { const merge = shouldMergeContinuous(speechStartAt); if (!merge && currentSentence) commitSentenceToHistory(); applyPipelineResult(pr, { merge }); lastSpeechEndedAt = Date.now(); setPhase(Phase.ENRec, 'Continuous — speak or type English (Stop to end)…'); return; }
        applyPipelineResult(pr); setPhase(Phase.Review, sentenceTarget(currentSentence) ? reviewPhaseMsg() : 'Could not produce translation.'); teardownAudio();
      } catch (e) {
        if (cont && !stopRequested) { setPhase(Phase.ENRec, 'Error: ' + (e?.message || String(e)) + ' — still listening…'); }
        else { ctx.Toasts.push({ title: 'Translation failed', msg: e?.message || String(e) }); setPhase(Phase.Idle, 'Error: ' + (e?.message || String(e))); teardownAudio(); }
      }
      return;
    }
    const p = currentProfile(); setPhase(Phase.Verifying, `Transcribing ${p.label || 'target'} and scoring…`); currentAttemptBlob = wavBlob;
    try {
      const tr = await postTranscribeTarget(wavBlob), spoken = (tr.text || '').trim(); setSpokenJaPlain(spoken || '(no transcript)');
      const expected = sentenceTarget(currentSentence);
      if (!expected) { setPhase(Phase.RetryPrompt, 'No target sentence loaded.'); teardownAudio(); return; }
      const sc = await postScore(expected, spoken, currentSentence.furigana), score = Number(sc.score || 0), scoreStrict = Number.isFinite(Number(sc.score_strict)) ? Number(sc.score_strict) : score, ok = !!sc.accepted, wavUrl = URL.createObjectURL(wavBlob), hl = applyHighlightFromScore(sc, spoken);
      currentSentence.attempts.push({ ts: Date.now(), transcript: spoken, score, score_strict: scoreStrict, accepted: ok, wavUrl, spoken_highlight_base: hl.base, spoken_match_ranges: hl.ranges });
      setScore(score, ok); renderAttemptChips();
      if (ok) { currentSentence.passedAttemptIdx = currentSentence.attempts.length - 1; setPhase(Phase.Passed, 'Great. Playing your successful attempt…'); try { await postPlayWav(wavBlob); } catch (_) {} setPhase(Phase.Done, 'Done. Click Next sentence.'); q('#btnTryAgain').disabled = true; }
      else { const thr = settings?.score_threshold || 100; setPhase(Phase.RetryPrompt, `Need ${thr}% or higher to pass. Press Start to try again.`); q('#btnTryAgain').disabled = false; }
      teardownAudio();
    } catch (e) { setPhase(Phase.RetryPrompt, 'Error: ' + (e?.message || String(e))); teardownAudio(); }
  }
  function updateMicLevel(r) { const bar = elMicLevelBar; if (!bar) return; const pct = Math.max(0, Math.min(100, Math.round((r / 0.3) * 100))); bar.style.width = pct + '%'; bar.classList.toggle('speech', r >= rmsSpeechThreshold()); }
  function onAudioProcess(e) { if (!listening) return; micSegmenter.feed(floatTo16kMono(e.inputBuffer.getChannelData(0), e.inputBuffer.sampleRate)); }

  async function speakReference({ auto = false } = {}) {
    const t = sentenceTarget(currentSentence); if (!t) { if (!auto) setPhase(currentPhase, 'No target text to speak.'); return; }
    if (auto && ttsSpeakBusy) return; if (!auto) cancelAutoTtsSchedule();
    const btn = q('#btnSpeakRef'); if (btn?.disabled && !auto) return; const prev = btn?.textContent; ttsSpeakBusy = true;
    try {
      if (btn && !auto) { btn.disabled = true; btn.textContent = '🔊 …'; }
      if (!auto) setPhase(currentPhase, 'Synthesizing…');
      const r = await fetch('/api/speak-tts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: t }) });
      if (!r.ok) throw new Error(await httpErrorMessage(r, 'POST /api/speak-tts'));
      const j = await r.json(); const dev = j.device_name || 'output'; const cacheNote = j.cached ? ' (cached)' : '';
      const msg = auto ? `Spoke on ${dev}${cacheNote}.` : `Played on ${dev}${cacheNote}.`;
      if (!listening || recordingMode === 'en' || currentPhase === Phase.Review) setPhase(currentPhase, msg);
    } catch (e) { if (!auto) setPhase(currentPhase, 'TTS: ' + (e?.message || String(e))); }
    finally { ttsSpeakBusy = false; if (btn && !auto) { btn.disabled = false; if (prev) btn.textContent = prev; } }
  }

  function renderAttemptChips() {
    const box = q('#attemptChips'), sum = q('#attemptSummary'); if (!box) return; box.innerHTML = '';
    if (!currentSentence) { if (sum) sum.hidden = true; return; }
    const n = currentSentence.attempts.length; if (sum) { if (n) { sum.textContent = n + '×'; sum.hidden = false; } else sum.hidden = true; }
    currentSentence.attempts.forEach((a, i) => { const c = document.createElement('button'); c.className = 'chip ' + (a.accepted ? 'ok' : 'bad') + (i === currentSentence.attempts.length - 1 ? ' current' : ''); const ss = a.score_strict, showStrict = Number.isFinite(ss) && Math.abs(ss - a.score) > .2; c.title = showStrict ? `Attempt ${i + 1}: ${a.score.toFixed(1)}% (strict ${ss.toFixed(1)}%)` : `Attempt ${i + 1}: ${a.score.toFixed(1)}%`; c.onclick = () => { renderSpokenFromAttempt(a); setScore(a.score, a.accepted); try { new Audio(a.wavUrl).play(); } catch (_) {} }; box.appendChild(c); });
  }
  function commitSentenceToHistory() { if (!currentSentence) return; history.unshift(currentSentence); currentSentence = null; autoTtsUtteranceId++; cancelAutoTtsSchedule(); renderHistoryPanel(); clearEnInput(); const be = q('#backEn'); if (be) be.textContent = ''; renderFurigana([]); q('#btnBeginPractice').disabled = true; q('#attemptChips').innerHTML = ''; q('#attemptSummary').hidden = true; syncNextButton(); }
  function renderHistoryPanel() {
    const hp = q('#historyPanel'); if (!hp) return; hp.innerHTML = '';
    if (!history.length) { hp.innerHTML = '<div class="mini">No history yet.</div>'; return; }
    for (const [idx, s] of history.entries()) { const wrap = document.createElement('div'); wrap.className = 'hist-item'; const ok = s.passedAttemptIdx != null, title = `<div class="hist-title">${ok ? '✅' : '🟠'} #${history.length - idx} · ${esc(s.english || '(no EN)')}</div>`, jp = `<div class="mini" style="margin-top:4px">${esc(sentenceTarget(s) || '(no target)')}</div>`, attempts = s.attempts.map((a, i) => { const ss = a.score_strict, extra = Number.isFinite(ss) && Math.abs(ss - a.score) > .2 ? ` strict ${ss.toFixed(1)}%` : ''; return `<div class="attempt"><span>${a.accepted ? 'pass' : 'retry'} · ${a.score.toFixed(1)}%${extra}</span><span>${esc(a.transcript || '(empty)')}</span><button data-hidx="${idx}" data-aidx="${i}">▶</button></div>`; }).join(''); wrap.innerHTML = title + jp + attempts; hp.appendChild(wrap); }
    hp.querySelectorAll('button[data-hidx]').forEach((btn) => { btn.onclick = () => { const h = history[Number(btn.dataset.hidx)], a = h?.attempts?.[Number(btn.dataset.aidx)]; if (!a) return; try { new Audio(a.wavUrl).play(); } catch (_) {} }; });
  }

  async function fetchLanguages() {
    try {
      const r = await fetch('/api/languages'); if (!r.ok) return; const j = await r.json(); langProfiles = Array.isArray(j.languages) ? j.languages : [];
      if (elLang) { elLang.innerHTML = ''; for (const p of langProfiles) { const o = document.createElement('option'); o.value = p.id; o.textContent = p.label; elLang.appendChild(o); } }
      if (settings) applySettingsToUI();
    } catch (_) { langProfiles = [{ id: 'jp', label: 'Japanese', short_label: 'JP', target_asr_lang: 'ja', tts_lang: 'ja-JP', has_furigana: true }, { id: 'zh', label: 'Chinese (Simplified)', short_label: 'ZH', target_asr_lang: 'zh', tts_lang: 'zh-CN', has_furigana: false }, { id: 'ko', label: 'Korean', short_label: 'KO', target_asr_lang: 'ko', tts_lang: 'ko-KR', has_furigana: false }]; if (settings) applySettingsToUI(); }
  }
  async function fetchSettings() { const r = await fetch('/api/settings'); if (!r.ok) throw new Error(await httpErrorMessage(r)); settings = (await r.json()).practice; applySettingsToUI(); }

  function applySettingsToUI() { if (!settings) return; if (elLang && elLang.options.length) elLang.value = settings.target_language || 'jp'; applyLanguageUI(); }

  async function persistTargetLanguage() { const v = elLang?.value || 'jp'; const r = await fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ practice: { target_language: v } }) }); if (!r.ok) throw new Error(await httpErrorMessage(r)); settings = (await r.json()).practice; }

  function resetForNextSentence() { teardownAudio(); textTranslateSeq++; clearTimeout(textTranslateTimer); cancelAutoTtsSchedule(); autoTtsUtteranceId++; if (currentSentence) commitSentenceToHistory(); currentSentence = null; currentAttemptBlob = null; recordingMode = 'en'; clearEnInput(); const be = q('#backEn'); if (be) be.textContent = ''; clearSpokenJa(); q('#btnBeginPractice').disabled = true; q('#btnTryAgain').disabled = true; clearScore(); renderFurigana([]); q('#attemptChips').innerHTML = ''; q('#attemptSummary').hidden = true; setPhase(Phase.Idle, 'Type or speak an English sentence.'); }

  function wireWorkArea() {
    q('#enInput').oninput = () => scheduleTextTranslate();
    q('#enInput').onkeydown = (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); if (textTranslateBusy) return; runTextTranslate({ englishOverride: q('#enInput').value, clearOnSuccess: true, autoSpeak: true }).catch((err) => setPhase(currentPhase, 'Type error: ' + (err?.message || String(err)))); } };
    q('#btnBeginPractice').onclick = () => { if (!sentenceTarget(currentSentence)) return; recordingMode = 'ja'; q('#btnBeginPractice').disabled = true; setPhase(Phase.JARec, `Press Start and repeat the ${currentProfile().label || 'target'} sentence.`); };
    q('#btnTryAgain').onclick = () => { recordingMode = 'ja'; q('#btnTryAgain').disabled = true; setPhase(Phase.JARec, 'Try again. Press Start.'); };
    q('#btnNext').onclick = resetForNextSentence;
    q('#btnSpeakRef').onclick = () => speakReference().catch((e) => setPhase(currentPhase, e?.message || String(e)));
    q('#btnToggleFuri').onclick = () => q('#jpRubyWrap').classList.toggle('hide-rt');
    q('#btnHistory').onclick = () => q('#historyPanel').classList.toggle('show');
    elLang.onchange = () => { if (settings) settings.target_language = elLang.value; applyLanguageUI(); persistTargetLanguage().catch((e) => setPhase(currentPhase, 'Language: ' + (e?.message || String(e)))); };
  }

  async function init() {
    wireWorkArea();
    await fetchLanguages();
    await refreshMics().catch(() => {});
    await fetchSettings().catch(() => {});
    await ensureCloudInBasic();
    renderHistoryPanel(); renderFurigana([]); applyLanguageUI();
    setPhase(Phase.Idle, 'Type or speak an English sentence.');
    document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') refreshMics().catch(() => {}); });
    document.addEventListener('uimodechange', () => { if (settings) applyLanguageUI(); ensureCloudInBasic(); });
    ctx.EngineStatus.subscribe((s) => {
      const ready = s.phase === 'ready';
      if (ready && !engineReady) { engineReady = true; if (settings?.session_active && !listening && !sessionResumeAttempted) { sessionResumeAttempted = true; startListen().catch((e) => setPhase(currentPhase, e?.message || String(e))); } }
      else if (!ready) engineReady = false;
    });
    if (ctx.EngineStatus.get().phase === 'ready') { engineReady = true; if (settings?.session_active && !sessionResumeAttempted) { sessionResumeAttempted = true; startListen().catch(() => {}); } }
  }

  function buildControls() {
    const wrap = document.createElement('span');
    wrap.className = 'mic-controls';
    wrap.innerHTML = `
      <label class="lang-ctl" title="Target language — English is translated into this"><span class="lang-ctl-tag">EN→</span><select class="mic-lang"></select></label>
      <select class="mic-pick" title="Microphone"></select>
      <span class="mic-level" title="Mic input level"><span class="mic-level-bar"></span></span>`;
    elLang = wrap.querySelector('.mic-lang');
    elMic = wrap.querySelector('.mic-pick');
    elMicLevelBar = wrap.querySelector('.mic-level-bar');
    return wrap;
  }

  function mount(r) {
    root = r;
    r.classList.add('view-mic-mounted');
    r.innerHTML = `
      <div class="area-head" style="display:flex;align-items:baseline;gap:10px;padding:10px 16px 0">
        <h1 style="margin:0;font-size:0.95rem;font-weight:600;color:var(--text-bright)">Mic translate</h1>
        <span class="sub" id="micSub" style="font-size:0.75rem;color:var(--muted)"></span>
      </div>
      <div class="mic-stage">
        <div class="mic-card">
          <div class="en-input-wrap">
            <div class="mini" id="enMini">Type or speak English, then press Enter to translate and auto-play TTS.</div>
            <textarea id="enInput" class="en-input" rows="2" placeholder="Type English here…" autocomplete="off" spellcheck="true"></textarea>
          </div>
          <div class="jp-section">
            <div class="jp-scroll"><div class="big-jp jp-placeholder" id="jpRubyWrap">Loading…</div></div>
            <div class="jp-actions">
              <button id="btnSpeakRef">🔊 Repeat</button>
              <button id="btnToggleFuri">Hide furigana</button>
              <button id="btnBeginPractice" class="warn" disabled>Begin practice</button>
              <span class="score-chip" id="phaseScore" hidden aria-live="polite"></span>
            </div>
          </div>
          <div class="card-footer" id="practiceMainUI">
            <div class="mini" id="backTransLabel">Back translation</div><div class="back-en" id="backEn"></div>
            <div class="mini" id="spokenLabel">What you said</div><div class="spoken-ja" id="spokenJa"></div>
            <div class="feedback-actions row"><button id="btnTryAgain" disabled>Try again</button><button id="btnNext" disabled>Next sentence</button></div>
          </div>
        </div>
        <div class="mic-bottom" id="practiceBottomUI"><div class="chips-track"><span class="attempt-summary" id="attemptSummary" hidden></span><div class="chips" id="attemptChips"></div></div><button class="history-btn" id="btnHistory">History</button></div>
        <div class="history-panel" id="historyPanel"></div>
      </div>`;
    elControls = buildControls();
    init();
  }

  return {
    id: 'mic',
    label: 'Mic',
    mount,
    controlsNode: () => elControls,
    start: () => startListen(),
    stop: () => { stopListen(); },
    getRunState: () => ({ running: isRunning(), statusText: lastMsg, badge: currentPhase }),
    onRunStateChange: (cb) => { runSubs.add(cb); return () => runSubs.delete(cb); },
    refreshDevices: () => refreshMics(),
    prefsCategories: () => [{ id: 'mic', label: 'Mic translate', placeholder: 'Pipeline, ASR/translation models, TTS, GPUs, plugins (built in P4).' }],
  };
}
