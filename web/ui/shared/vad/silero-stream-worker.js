// Silero v5 scores [64-sample context | 512-sample window] = 576 samples; feeding
// a bare 512 (no context) returns ≈0 for everything (see memory note
// silero-v5-context-window). State is kept CONTINUOUS across cuts (canonical
// streaming VADIterator behaviour) and reset only on an explicit per-lane reset.
const WINDOW = 512;
const CONTEXT = 64;
const INPUT = CONTEXT + WINDOW; // 576
const SAMPLE_RATE = 16000;

let THRESH_ON = 0.5;
let THRESH_OFF = 0.35;           // hysteresis: prob must drop below this to count toward release
const PRE_WINDOWS = 8;           // pre-roll ring (~256 ms) kept before onset
const SILENCE_WINDOWS = 15;
const MIN_SAMPLES = 3200;
let maxSamples = Math.floor(SAMPLE_RATE * 3.5);
const PROB_EVERY = 2;

let ort = null;
let Tensor = null;
let session = null;
let chain = Promise.resolve();

const lanes = new Map();

function newLane() {
  return {
    state: new Tensor('float32', new Float32Array(2 * 1 * 128), [2, 1, 128]),
    ctx: new Float32Array(INPUT),
    pending: new Int16Array(0),
    preChunks: [],
    clipChunks: null,
    clipLen: 0,
    speechOn: false,
    quiet: 0,
    probTick: 0,
  };
}

function lane(id) {
  let l = lanes.get(id);
  if (!l) { l = newLane(); lanes.set(id, l); }
  return l;
}

function concatI16(list) {
  let n = 0;
  for (const a of list) n += a.length;
  const out = new Int16Array(n);
  let o = 0;
  for (const a of list) { out.set(a, o); o += a.length; }
  return out;
}

async function init(msg) {
  if (Number(msg?.clipMaxSec) > 0) maxSamples = Math.floor(SAMPLE_RATE * Number(msg.clipMaxSec));
  if (Number(msg?.threshOn) > 0) THRESH_ON = Number(msg.threshOn);
  if (Number(msg?.threshOff) > 0) THRESH_OFF = Number(msg.threshOff);
  try {
    ort = await import('./ort.wasm.min.mjs');
    Tensor = ort.Tensor;
    ort.env.wasm.wasmPaths = '/ui/shared/vad/';
    ort.env.wasm.numThreads = 1;
    ort.env.wasm.proxy = false;
    session = await ort.InferenceSession.create('/ui/shared/vad/silero_vad.onnx');
    self.postMessage({ type: 'ready' });
  } catch (e) {
    self.postMessage({ type: 'error', message: e?.message || String(e) });
  }
}

async function infer(l, win) {
  l.ctx.copyWithin(0, WINDOW, INPUT);
  for (let j = 0; j < WINDOW; j++) l.ctx[CONTEXT + j] = win[j] / 32768;
  const input = new Tensor('float32', l.ctx.slice(), [1, INPUT]);
  const sr = new Tensor('int64', [BigInt(SAMPLE_RATE)]);
  const out = await session.run({ input, state: l.state, sr });
  l.state = out.stateN;
  return out.output.data[0];
}

function cut(id, l) {
  const clip = concatI16(l.clipChunks);
  l.clipChunks = null;
  l.clipLen = 0;
  l.speechOn = false;
  l.quiet = 0;
  self.postMessage({ type: 'clip', laneId: id, pcm: clip }, [clip.buffer]);
}

async function onFrame(id, pcm) {
  if (!session || !pcm || !pcm.length) return;
  const l = lane(id);
  l.pending = l.pending.length ? concatI16([l.pending, pcm]) : pcm;

  let off = 0;
  while (l.pending.length - off >= WINDOW) {
    const view = l.pending.subarray(off, off + WINDOW);
    off += WINDOW;
    const win = view.slice();
    const wasSpeech = l.speechOn;
    const prob = await infer(l, view);

    l.preChunks.push(win);
    if (l.preChunks.length > PRE_WINDOWS) l.preChunks.shift();

    if (!wasSpeech) {
      if (prob >= THRESH_ON) {
        l.speechOn = true;
        l.quiet = 0;
        l.clipChunks = l.preChunks.slice();
        l.clipLen = l.clipChunks.reduce((n, c) => n + c.length, 0);
      }
    } else {
      l.clipChunks.push(win);
      l.clipLen += win.length;
      if (prob < THRESH_OFF) l.quiet++; else l.quiet = 0;
      if ((l.quiet >= SILENCE_WINDOWS && l.clipLen >= MIN_SAMPLES) || l.clipLen >= maxSamples) {
        cut(id, l);
      }
    }

    if (++l.probTick >= PROB_EVERY) { l.probTick = 0; self.postMessage({ type: 'prob', laneId: id, prob }); }
  }
  l.pending = off ? l.pending.slice(off) : l.pending;
}

function resetLane(id) {
  if (!Tensor) { lanes.delete(id); return; }
  lanes.set(id, newLane());
}

function stopAll() {
  lanes.clear();
}

self.onmessage = (e) => {
  const msg = e.data || {};
  switch (msg.type) {
    case 'init': chain = chain.then(() => init(msg)); break;
    case 'frame': chain = chain.then(() => onFrame(msg.laneId, msg.pcm)).catch((err) => self.postMessage({ type: 'error', message: err?.message || String(err) })); break;
    case 'reset': chain = chain.then(() => resetLane(msg.laneId)); break;
    case 'stop': chain = chain.then(stopAll); break;
  }
};
