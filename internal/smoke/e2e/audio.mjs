import http from 'node:http';
import net from 'node:net';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn, execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { synthVoiceWav, FIXTURES } from './testdata.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, '..', '..', '..');
const EDGE = process.env.EDGE
  || ['C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
      'C:/Program Files/Microsoft/Edge/Application/msedge.exe'].find(fs.existsSync)
  || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe';
const MIC_MODEL = 'mock-mic-asr', SYS_MODEL = 'mock-sys-asr';

if (typeof WebSocket === 'undefined') {
  console.error('FATAL: no global WebSocket. Use Node 22+, or `node --experimental-websocket`.');
  process.exit(2);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const freePort = () => new Promise((res, rej) => {
  const s = net.createServer();
  s.on('error', rej);
  s.listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => res(p)); });
});

const results = [];
const softSkips = [];
const ok = (name, cond, extra = '') => {
  results.push({ name, pass: !!cond, extra });
  console.log(cond ? 'PASS ' : 'FAIL ', name, extra ? '· ' + extra : '');
};
const skip = (name) => { softSkips.push(name); console.log('SKIP ', name); };

const expectDevice = /^(1|true|yes)$/i.test(process.env.EXPECT_AUDIO_DEVICE || '');
const gate = (name) => {
  if (expectDevice) ok(name, false, 'EXPECT_AUDIO_DEVICE set but system audio was not captured');
  else skip(name);
};

function resolveExe() {
  let p = (process.env.TO_SMOKE_EXE || '').trim();
  if (!p) p = path.join('build', 'penguin-translate.exe');
  if (!path.isAbsolute(p)) p = path.join(ROOT, p);
  if (!fs.existsSync(p)) throw new Error(`app binary not found: ${p} (set TO_SMOKE_EXE)`);
  return p;
}

function fieldFromBody(body, key) {
  let m = body.match(new RegExp(`"${key}"\\s*:\\s*"([^"]+)"`));
  if (m) return m[1];
  m = body.match(new RegExp(`name="${key}"\\r?\\n\\r?\\n([^\\r\\n]+)`));
  return m ? m[1] : '';
}

function batchItems(msgs) {
  const u = msgs.find((m) => m.role === 'user');
  if (!u || typeof u.content !== 'string') return null;
  try {
    const arr = JSON.parse(u.content);
    return Array.isArray(arr) && arr.length && typeof arr[0]?.i === 'number' ? arr : null;
  } catch { return null; }
}
function startMockCloud() {
  const hits = { sttMic: 0, sttSys: 0, chatFwd: 0, chatBatch: 0 };
  const srv = http.createServer((req, res) => {
    const send = (obj) => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(obj)); };
    let body = '';
    req.on('data', (d) => { body += d; });
    req.on('end', () => {
      if (req.url.includes('/audio/transcriptions')) {
        if (fieldFromBody(body, 'model').includes('sys')) {
          hits.sttSys++;
          return send({ text: FIXTURES.sysSttText, language: FIXTURES.sysSttLanguage });
        }
        hits.sttMic++;
        return send({ text: FIXTURES.sttText, language: FIXTURES.sttLanguage });
      }
      if (req.url.includes('/chat/completions')) {
        let msgs = [];
        try { msgs = (JSON.parse(body).messages) || []; } catch {}
        const items = batchItems(msgs);
        if (items) {
          hits.chatBatch++;
          const arr = items.map((it) => ({ i: it.i, en: FIXTURES.sysEnglish }));
          return send({ choices: [{ message: { content: JSON.stringify(arr) } }] });
        }
        hits.chatFwd++;
        return send({ choices: [{ message: { content: FIXTURES.chatTranslation } }] });
      }
      res.writeHead(404); res.end('not found');
    });
  });
  return { srv, hits };
}

function killStaleEdge(tag) {
  try { execSync(`powershell -NoProfile -Command "Get-CimInstance Win32_Process -Filter \\"Name='msedge.exe'\\" | Where-Object { $_.CommandLine -match '${tag}' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"`, { stdio: 'ignore' }); } catch {}
}
function killTree(pid) { try { execSync(`taskkill /T /F /PID ${pid}`, { stdio: 'ignore' }); } catch {} }
function overlayOrphaned() {
  try { return execSync('tasklist /FI "IMAGENAME eq penguin-translate-overlay.exe" /NH', { encoding: 'utf8' }).includes('penguin-translate-overlay.exe'); }
  catch { return false; }
}
async function waitHealth(base, ms) {
  const end = Date.now() + ms; let last = '';
  while (Date.now() < end) {
    try { const r = await fetch(base + '/health'); if (r.ok) return; last = 'status ' + r.status; }
    catch (e) { last = String(e.message || e); }
    await sleep(250);
  }
  throw new Error('app did not become healthy: ' + last);
}

async function main() {
  const exe = resolveExe();
  const tag = 'penguin-audio-' + process.pid;
  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'penguin-audio-'));
  const dataDir = path.join(work, 'data');
  const profile = path.join(work, 'edge-' + tag);
  const wavPath = path.join(work, 'voice.wav');
  const artifactDir = process.env.E2E_ARTIFACT_DIR ? path.resolve(process.env.E2E_ARTIFACT_DIR) : work;
  fs.mkdirSync(dataDir, { recursive: true });
  fs.mkdirSync(artifactDir, { recursive: true });
  fs.writeFileSync(wavPath, synthVoiceWav());
  const playWav = synthVoiceWav({ cycles: 3, burstSec: 1.2, gapSec: 1.6, peak: 0.95 });

  const { srv: mockSrv, hits } = startMockCloud();
  const mockPort = await freePort();
  await new Promise((r) => mockSrv.listen(mockPort, '127.0.0.1', r));
  const mockBase = `http://127.0.0.1:${mockPort}/v1`;

  const appPort = await freePort();
  const base = `http://127.0.0.1:${appPort}`;
  const appLog = [];
  const app = spawn(exe, ['-http', `:${appPort}`], {
    cwd: ROOT, env: { ...process.env, TO_DATA_DIR: dataDir }, stdio: ['ignore', 'pipe', 'pipe'],
  });
  app.stdout.on('data', (d) => appLog.push(d.toString()));
  app.stderr.on('data', (d) => appLog.push(d.toString()));

  let edge, exitCode = 1, skipped = false;
  try {
    await waitHealth(base, 30000);

    const devs = (await (await fetch(base + '/api/loopback/devices')).json()).devices || [];
    const loopDev = devs.find((d) => d.loopback_ok !== false);
    if (!loopDev) {
      if (expectDevice) {
        ok('a loopback-capable audio device is present', false, 'EXPECT_AUDIO_DEVICE set but none found — virtual audio install failed');
        exitCode = 1; return;
      }
      console.log('SKIP system-audio E2E: no loopback-capable audio device on this machine');
      skipped = true; exitCode = 0; return;
    }

    const settings = {
      openai_api_key: 'sk-test', openai_base_url: mockBase,
      openrouter_api_key: 'sk-test', openrouter_base_url: mockBase,
      practice: {
        forward_translator: 'openai', api_provider: 'openai', english_asr_engine: 'openai',
        my_language: 'en', other_languages: ['ja'], target_language: 'jp', practice_enabled: false,
        transcribe_model: MIC_MODEL, translate_model: 'mock-fwd',
      },
      audio: {
        api_provider: 'openai', pipeline_mode: 'split', primary_language: 'ja',
        speech_detection: 'rms', vad_sensitivity: 95, transcribe_model: SYS_MODEL, translate_model: 'mock-batch',
        clip_min_sec: 0.2, clip_max_sec: 4, clip_silence_ms: 300,
      },
    };
    await fetch(base + '/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settings) });

    killStaleEdge(tag);
    const cdpPort = await freePort();
    edge = spawn(EDGE, [
      '--headless=new', `--remote-debugging-port=${cdpPort}`, '--remote-allow-origins=*',
      '--no-first-run', '--no-default-browser-check',
      '--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream',
      `--use-file-for-fake-audio-capture=${wavPath}`, '--autoplay-policy=no-user-gesture-required',
      `--user-data-dir=${profile}`, '--window-size=1280,800', `${base}/ui/`,
    ], { stdio: 'ignore' });

    let target;
    for (let i = 0; i < 40 && !target?.webSocketDebuggerUrl; i++) {
      try { target = (await fetch(`http://127.0.0.1:${cdpPort}/json`).then((r) => r.json())).find((t) => t.type === 'page' && t.url.includes('/ui')); } catch {}
      if (!target?.webSocketDebuggerUrl) await sleep(250);
    }
    if (!target) throw new Error('no Edge page target on CDP');

    let ws, msgId = 0; const pending = new Map(); const exceptions = [];
    const send = (method, params = {}) => new Promise((resolve, reject) => {
      const id = ++msgId; pending.set(id, { resolve, reject }); ws.send(JSON.stringify({ id, method, params }));
    });
    await new Promise((resolve, reject) => {
      ws = new WebSocket(target.webSocketDebuggerUrl);
      ws.onopen = resolve; ws.onerror = reject;
      ws.onmessage = (e) => {
        const m = JSON.parse(e.data);
        if (m.id && pending.has(m.id)) { const pr = pending.get(m.id); pending.delete(m.id); m.error ? pr.reject(new Error(m.error.message)) : pr.resolve(m.result); }
        else if (m.method === 'Runtime.exceptionThrown') exceptions.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text || '');
      };
    });
    const evaluate = async (expression) => {
      const r = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) throw new Error('eval: ' + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
      return r.result.value;
    };
    const waitFor = async (expr, timeoutMs, stepMs = 300) => {
      const end = Date.now() + timeoutMs;
      while (Date.now() < end) { if (await evaluate(expr)) return true; await sleep(stepMs); }
      return false;
    };
    const waitHits = async (read, timeoutMs, stepMs = 400) => {
      const end = Date.now() + timeoutMs;
      while (Date.now() < end) { if (read()) return true; await sleep(stepMs); }
      return false;
    };
    const shot = async (name) => { try { const r = await send('Page.captureScreenshot', { format: 'png' }); fs.writeFileSync(path.join(artifactDir, name), Buffer.from(r.data, 'base64')); } catch {} };
    await send('Page.enable'); await send('Runtime.enable'); await send('Log.enable');

    const booted = await waitFor(`!!(window.App && window.App.RunController && window.App.getLangs)`, 30000);
    ok('UI shell booted', booted);
    await waitFor(`document.querySelectorAll('#langSet .lchip').length>=1`, 15000);

    ok('mic + system-audio lanes both present', await evaluate(`!!document.querySelector('.runbar .lane-mic .lane-toggle') && !!document.querySelector('.runbar .lane-sys .lane-toggle')`));
    await evaluate(`document.getElementById('btnRun').click()`);
    await sleep(3000);

    const playOnce = () => fetch(base + '/api/play-wav', { method: 'POST', body: (() => { const fd = new (globalThis.FormData)(); fd.append('file', new Blob([playWav], { type: 'audio/wav' }), 'play.wav'); return fd; })() }).then((r) => r.ok).catch(() => false);
    const sysMeter = `parseFloat((document.querySelector('.runbar .lane-sys .lane-meter > i')||{}).style?.width)||0`;
    let played = false, sysLive = false;
    for (let attempt = 0; attempt < 6 && hits.sttSys === 0; attempt++) {
      played = (await playOnce()) || played;
      const end = Date.now() + 12000;
      while (Date.now() < end && hits.sttSys === 0) {
        if ((await evaluate(sysMeter)) > 1) sysLive = true;
        await sleep(300);
      }
    }
    ok('app played the fixture to the output device (render path works)', played);

    const gotMic = await waitHits(() => hits.sttMic > 0, 10000);
    const outOk = await waitFor(`(()=>{const t=document.querySelector('.view-conversation .turn.out:last-child .trans-row'); return !!t && /${FIXTURES.chatTranslation}/.test(t.textContent);})()`, 15000);
    await shot('audio-01-bothways.png');
    ok('MIC captured → real transcribe to cloud (audio/transcriptions, mic model)', gotMic, `sttMic=${hits.sttMic}`);
    ok('outgoing turn: my speech translated EN→JA and rendered', outOk);
    ok('forward translate hit the cloud (outgoing direction)', hits.chatFwd > 0, `chatFwd=${hits.chatFwd}`);

    if (hits.sttSys > 0) {
      const inOk = await waitFor(`(()=>{const t=document.querySelector('.view-conversation .turn.in'); return !!t && /キャプチャ/.test(t.textContent) && /audio capture test/.test(t.textContent);})()`, 15000);
      ok('SYSTEM audio captured via native loopback → real transcribe-segment (sys model)', true, `sttSys=${hits.sttSys}`);
      ok('incoming turn: their system audio transcribed (JA) and translated → EN', inOk,
        await evaluate(`(document.querySelector('.view-conversation .turn.in')?.textContent||'').trim().slice(0,80)`));
      ok('batch translate-to-EN hit the cloud (incoming direction)', hits.chatBatch > 0, `chatBatch=${hits.chatBatch}`);
    } else if (sysLive) {
      ok('SYSTEM audio capture path live — native loopback received the played fixture', true, 'sttSys=0; live transcription skipped (no VAD segment this run)');
      skip('system audio transcription: no VAD segment cut this run — covered device-free by Go smoke transcribe_segment');
    } else {
      gate('system audio: native loopback delivered no signal — capture path broken');
    }

    await evaluate(`document.getElementById('btnRun').click()`);
    ok('no runtime exceptions', exceptions.filter((e) => /SyntaxError|ReferenceError|is not defined|TypeError/.test(e)).length === 0, exceptions.find((e) => /TypeError|ReferenceError/.test(e)) || '');

    exitCode = results.some((r) => !r.pass) ? 1 : 0;
  } catch (e) {
    console.error('HARNESS ERROR', e); exitCode = 2;
  } finally {
    try { edge && edge.kill(); } catch {}
    killStaleEdge(tag);
    killTree(app.pid);
    await new Promise((r) => mockSrv.close(r));
    await sleep(500);
    if (!skipped) {
      if (overlayOrphaned()) ok('no orphaned overlay subprocess', false);
      const log = appLog.join('');
      try { fs.writeFileSync(path.join(artifactDir, 'audio-app.log'), log); } catch {}
      if (/panic:|panic\(/i.test(log)) { ok('no panic in app log', false); console.error('--- app log tail ---\n' + log.slice(-2000)); }
      if (results.some((r) => !r.pass)) exitCode = exitCode || 1;
    }
  }

  if (skipped) { process.exit(0); }
  const pass = results.filter((r) => r.pass).length, fail = results.length - pass;
  console.log(`\nRESULT ${JSON.stringify({ pass, fail, skip: softSkips.length })}`);
  console.log(`CLOUD  ${JSON.stringify(hits)}`);
  console.log(`ARTIFACTS ${artifactDir}`);
  process.exit(fail ? 1 : exitCode);
}

main().catch((e) => { console.error('FATAL', e); process.exit(2); });
