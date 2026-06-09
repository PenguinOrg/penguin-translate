const ORT_URL = '/ui/shared/vad/ort.wasm.min.mjs';
const MODEL_URL = '/ui/shared/vad/silero_vad.onnx';

const WINDOW = 512;            // new samples per frame @16k (32 ms) — required by Silero v5
const CONTEXT = 64;            // samples of the previous frame Silero v5 prepends
const INPUT = CONTEXT + WINDOW; // 576: the model scores context+window, advancing by WINDOW
const SAMPLE_RATE = 16000;
const PROB_THRESHOLD = 0.5;
const SPEECH_FRACTION = 0.05;

let gate = null;

export function getSileroGate() {
  if (!gate) gate = createSileroGate();
  return gate;
}

function createSileroGate() {
  let session = null;
  let Tensor = null;
  let loadPromise = null;
  let broken = false;
  let chain = Promise.resolve();

  async function load() {
    const ort = await import(ORT_URL);
    Tensor = ort.Tensor;
    ort.env.wasm.wasmPaths = '/ui/shared/vad/';
    ort.env.wasm.numThreads = 1;
    ort.env.wasm.proxy = false;
    session = await ort.InferenceSession.create(MODEL_URL);
  }

  function warm() {
    if (!loadPromise) {
      loadPromise = load().catch((e) => {
        broken = true;
        console.warn('[silero] disabled (load failed) — capture continues unfiltered:', e?.message || e);
      });
    }
    return loadPromise;
  }

  async function step(win, state, sr) {
    const out = await session.run({ input: win, state, sr });
    return [out.output.data[0], out.stateN];
  }

  // Fails open: returns true (has speech) whenever the gate is unavailable, so a
  // VAD problem never blocks capture.
  async function hasSpeech(int16) {
    if (broken) return true;
    await warm();
    if (broken || !session) return true;

    const total = int16.length >= WINDOW ? Math.floor((int16.length - WINDOW) / WINDOW) + 1 : 0;
    if (total < 1) return true;

    chain = chain.then(() => runClip(int16, total, true).then((r) => r.hasSpeech)).catch((e) => {
      broken = true;
      console.warn('[silero] disabled (inference failed) — capture continues unfiltered:', e?.message || e);
      return true;
    });
    return chain;
  }

  async function runClip(int16, total, earlyStop) {
    const needed = Math.max(1, Math.ceil(total * SPEECH_FRACTION));
    const sr = new Tensor('int64', [BigInt(SAMPLE_RATE)]);
    let state = new Tensor('float32', new Float32Array(2 * 1 * 128), [2, 1, 128]);
    // Each inference scores [64-sample context | 512-sample window]. Feeding a
    // bare 512 (no context) runs without error but returns near-zero probabilities
    // for everything — speech included — so the gate would drop real speech.
    const buf = new Float32Array(INPUT);
    let speech = 0, max = 0, sum = 0;
    for (let i = 0; i + WINDOW <= int16.length; i += WINDOW) {
      buf.copyWithin(0, WINDOW, INPUT);
      for (let j = 0; j < WINDOW; j++) buf[CONTEXT + j] = int16[i + j] / 32768;
      const win = new Tensor('float32', buf.slice(), [1, INPUT]);
      const [prob, next] = await step(win, state, sr);
      state = next;
      sum += prob; if (prob > max) max = prob;
      if (prob >= PROB_THRESHOLD && ++speech >= needed && earlyStop) {
        return { hasSpeech: true, total, speech, needed, max, mean: sum / total };
      }
    }
    return { hasSpeech: speech >= needed, total, speech, needed, max, mean: sum / total };
  }

  async function stats(int16) {
    await warm();
    if (broken || !session) return { unavailable: true };
    const total = int16.length >= WINDOW ? Math.floor((int16.length - WINDOW) / WINDOW) + 1 : 0;
    if (total < 1) return { total: 0 };
    chain = chain.then(() => runClip(int16, total, false));
    return chain;
  }

  return { warm, hasSpeech, stats, get ready() { return !!session; } };
}
