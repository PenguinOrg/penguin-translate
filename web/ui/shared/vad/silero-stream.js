const WORKER_URL = '/ui/shared/vad/silero-stream-worker.js';

let inst = null;

export function getSileroStream() {
  if (!inst) inst = create();
  return inst;
}

function create() {
  let worker = null;
  let readyPromise = null;
  let broken = false;
  let onClipCb = null, onProbCb = null, onBrokenCb = null;

  function ensure(clipMaxSec) {
    if (readyPromise) return readyPromise;
    readyPromise = new Promise((resolve) => {
      try {
        worker = new Worker(WORKER_URL, { type: 'module' });
      } catch (e) {
        broken = true;
        console.warn('[silero-stream] worker create failed — falling back:', e?.message || e);
        resolve(false);
        return;
      }
      let settled = false;
      const fail = (msg) => {
        console.warn('[silero-stream] disabled:', msg);
        broken = true;
        if (!settled) { settled = true; resolve(false); }
        else if (onBrokenCb) onBrokenCb();
      };
      worker.onmessage = (ev) => {
        const m = ev.data || {};
        if (m.type === 'ready') { if (!settled) { settled = true; resolve(true); } return; }
        if (m.type === 'error') { fail(m.message); return; }
        if (m.type === 'clip') { if (onClipCb) onClipCb(m.laneId, m.pcm); return; }
        if (m.type === 'prob') { if (onProbCb) onProbCb(m.laneId, m.prob); return; }
      };
      worker.onerror = (e) => fail(e?.message || 'worker onerror');
      worker.postMessage({ type: 'init', clipMaxSec });
    });
    return readyPromise;
  }

  return {
    get broken() { return broken; },
    warm(clipMaxSec) { return ensure(clipMaxSec); },
    onClip(cb) { onClipCb = cb; },
    onProb(cb) { onProbCb = cb; },
    onBroken(cb) { onBrokenCb = cb; },
    feed(laneId, int16) {
      if (broken || !worker || !int16 || !int16.length) return;
      const copy = int16.slice();
      worker.postMessage({ type: 'frame', laneId, pcm: copy }, [copy.buffer]);
    },
    reset(laneId) { if (worker && !broken) worker.postMessage({ type: 'reset', laneId }); },
    stop() { if (worker && !broken) worker.postMessage({ type: 'stop' }); },
  };
}
