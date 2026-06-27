import crypto from 'node:crypto';
import { pathToFileURL } from 'node:url';

export const FIXTURES = {
  sttText: 'this is a generic smoke test utterance',
  sttLanguage: 'en',
  chatTranslation: 'こんにちは世界',
  furiganaSurface: '世界',

  sysSttText: 'これはおんせいキャプチャのテストです',
  sysSttLanguage: 'ja',
  sysEnglish: 'this is an audio capture test',
};

export function synthVoiceWav({ sampleRate = 48000, cycles = 5, burstSec = 1.3, gapSec = 1.7, peak = 0.5 } = {}) {
  const parts = [];
  const f0s = [180, 200, 165, 220, 190];
  for (let c = 0; c < cycles; c++) {
    parts.push(voicedBurst(sampleRate, burstSec, f0s[c % f0s.length], peak));
    parts.push(new Int16Array(Math.round(gapSec * sampleRate)));
  }
  let total = 0;
  for (const p of parts) total += p.length;
  const pcm = new Int16Array(total);
  let o = 0;
  for (const p of parts) { pcm.set(p, o); o += p.length; }
  return wrapWav(pcm, sampleRate);
}

function voicedBurst(sr, sec, f0, peak = 0.5) {
  const n = Math.round(sec * sr);
  const a = new Int16Array(n);
  const partials = [[1, 0.6], [2, 0.3], [3, 0.15]];
  const ramp = Math.round(0.04 * sr);
  for (let i = 0; i < n; i++) {
    let s = 0;
    for (const [h, amp] of partials) s += amp * Math.sin((2 * Math.PI * f0 * h * i) / sr);
    const trem = 0.9 + 0.1 * Math.sin((2 * Math.PI * 5 * i) / sr);
    let env = 1;
    if (i < ramp) env = 0.5 - 0.5 * Math.cos((Math.PI * i) / ramp);
    else if (i > n - ramp) env = 0.5 - 0.5 * Math.cos((Math.PI * (n - i)) / ramp);
    a[i] = Math.round(s * trem * env * peak * 32767);
  }
  return a;
}

function wrapWav(pcm, sr) {
  const buf = Buffer.alloc(44 + pcm.length * 2);
  buf.write('RIFF', 0);
  buf.writeUInt32LE(36 + pcm.length * 2, 4);
  buf.write('WAVE', 8);
  buf.write('fmt ', 12);
  buf.writeUInt32LE(16, 16);
  buf.writeUInt16LE(1, 20);
  buf.writeUInt16LE(1, 22);
  buf.writeUInt32LE(sr, 24);
  buf.writeUInt32LE(sr * 2, 28);
  buf.writeUInt16LE(2, 32);
  buf.writeUInt16LE(16, 34);
  buf.write('data', 36);
  buf.writeUInt32LE(pcm.length * 2, 40);
  Buffer.from(pcm.buffer).copy(buf, 44);
  return buf;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const fs = await import('node:fs');
  const out = process.argv[2] || 'voice.wav';
  const wav = synthVoiceWav();
  fs.writeFileSync(out, wav);
  const sr = wav.readUInt32LE(24);
  const samples = wav.readUInt32LE(40) / 2;
  console.log(JSON.stringify({
    file: out,
    bytes: wav.length,
    sample_rate: sr,
    channels: 1,
    bits: 16,
    duration_sec: +(samples / sr).toFixed(2),
    sha256: crypto.createHash('sha256').update(wav).digest('hex'),
    source: 'pure synthetic sine partials — no recorded or copyrighted audio',
    fixtures: FIXTURES,
  }, null, 2));
}
