export const VAD_FULL_SCALE = 0.15;
export const rmsToScale = (rms) => Math.max(0, Math.min(100, (rms / VAD_FULL_SCALE) * 100));
export const scaleToRms = (pos) => (Math.max(0, Math.min(100, pos)) / 100) * VAD_FULL_SCALE;

export function rmsInt16(arr, off, len) {
  if (len < 1) return 0;
  let s = 0;
  for (let i = 0; i < len; i++) {
    const v = arr[off + i] / 32768;
    s += v * v;
  }
  return Math.sqrt(s / len);
}

export function floatTo16kMono(input, sampleRate) {
  const ratio = sampleRate / 16000;
  const outLen = Math.floor(input.length / ratio);
  const out = new Int16Array(outLen);
  for (let i = 0; i < outLen; i++) {
    const x = input[Math.floor(i * ratio)] || 0;
    const s = Math.max(-1, Math.min(1, x));
    out[i] = s < 0 ? s * 0x8000 : s * 0x7fff;
  }
  return out;
}

export function buildWav(int16) {
  const samples = int16.length;
  const buffer = new ArrayBuffer(44 + samples * 2);
  const view = new DataView(buffer);
  const wstr = (o, s) => { for (let i = 0; i < s.length; i++) view.setUint8(o + i, s.charCodeAt(i)); };
  wstr(0, 'RIFF');
  view.setUint32(4, 36 + samples * 2, true);
  wstr(8, 'WAVE'); wstr(12, 'fmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, 16000, true);
  view.setUint32(28, 16000 * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  wstr(36, 'data');
  view.setUint32(40, samples * 2, true);
  new Int16Array(buffer, 44).set(int16);
  return new Blob([buffer], { type: 'audio/wav' });
}

export function createSpeechSegmenter({ params, onMeter, onSpeechStart, onQuietWhileIdle, onSegment }) {
  let pcm16 = new Int16Array(0);
  let speechOn = false;
  let quietCnt = 0;
  let loudCnt = 0;

  function append(arr, off, len) {
    if (len < 1) return;
    const slice = arr.subarray(off, off + len);
    const next = new Int16Array(pcm16.length + slice.length);
    next.set(pcm16);
    next.set(slice, pcm16.length);
    pcm16 = next;
  }

  function reset() {
    pcm16 = new Int16Array(0);
    speechOn = false;
    quietCnt = 0;
    loudCnt = 0;
  }

  function cut() {
    const clip = pcm16;
    pcm16 = new Int16Array(0);
    speechOn = false;
    quietCnt = 0;
    loudCnt = 0;
    onSegment(clip);
  }

  function dropBufferIfIdle() {
    if (!speechOn && pcm16.length > 0) pcm16 = new Int16Array(0);
  }

  function feed(chunk) {
    if (!chunk || chunk.length < 1) return;
    const p = params();
    const frame = p.frameSamples > 0 ? p.frameSamples : chunk.length;
    let peak = 0;

    for (let off = 0; off < chunk.length; off += frame) {
      const len = Math.min(frame, chunk.length - off);
      const r = rmsInt16(chunk, off, len);
      if (r > peak) peak = r;
      const isLoud = r >= p.rmsSpeech;
      const isQuiet = r <= p.rmsQuiet;

      if (isLoud) {
        loudCnt++;
        quietCnt = 0;
        if (loudCnt >= p.loudFrames) {
          if (!speechOn && onSpeechStart) onSpeechStart();
          speechOn = true;
        }
      } else if (isQuiet) {
        loudCnt = 0;
      }

      if (speechOn || loudCnt > 0) append(chunk, off, len);

      if (speechOn) {
        if (isQuiet) {
          quietCnt++;
          const qNeed = pcm16.length >= p.longAfterSamples ? p.silenceLongFrames : p.silenceFrames;
          const cutNeed = pcm16.length < p.fragmentSamples ? qNeed + p.fragmentExtraSilenceFrames : qNeed;
          if (quietCnt >= cutNeed && pcm16.length >= p.minSamples) cut();
        } else if (isLoud) {
          quietCnt = 0;
        }
        if (pcm16.length >= p.maxSamples) cut();
      } else if (isQuiet && onQuietWhileIdle) {
        onQuietWhileIdle();
      }
    }

    if (!speechOn && loudCnt === 0 && pcm16.length > 0) pcm16 = new Int16Array(0);

    if (onMeter) onMeter(peak);
  }

  return {
    feed,
    reset,
    dropBufferIfIdle,
    get inSpeech() { return speechOn; },
    get bufferLength() { return pcm16.length; },
  };
}
