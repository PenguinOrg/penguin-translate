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
const ok = (name, cond, extra = '') => {
  results.push({ name, pass: !!cond, extra });
  console.log(cond ? 'PASS ' : 'FAIL ', name, extra ? '· ' + extra : '');
};

function resolveExe() {
  let p = (process.env.TO_SMOKE_EXE || '').trim();
  if (!p) p = path.join('build', 'penguin-translate.exe');
  if (!path.isAbsolute(p)) p = path.join(ROOT, p);
  if (!fs.existsSync(p)) throw new Error(`app binary not found: ${p} (set TO_SMOKE_EXE)`);
  return p;
}

function startMockCloud(srv = http.createServer()) {
  const hits = { chat: 0, stt: 0 };
  srv.on('request', (req, res) => {
    const send = (obj) => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(obj)); };
    let body = '';
    req.on('data', (d) => { body += d; });
    req.on('end', () => {
      if (req.url.includes('/chat/completions')) {
        hits.chat++;
        return send({ choices: [{ message: { content: FIXTURES.chatTranslation } }] });
      }
      if (req.url.includes('/audio/transcriptions')) {
        hits.stt++;
        return send({ text: FIXTURES.sttText, language: FIXTURES.sttLanguage });
      }
      res.writeHead(404); res.end('not found');
    });
  });
  return { srv, hits };
}

function killStaleEdge(profileTag) {
  try {
    execSync(`powershell -NoProfile -Command "Get-CimInstance Win32_Process -Filter \\"Name='msedge.exe'\\" | Where-Object { $_.CommandLine -match '${profileTag}' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"`, { stdio: 'ignore' });
  } catch {}
}

function killTree(pid) {
  try { execSync(`taskkill /T /F /PID ${pid}`, { stdio: 'ignore' }); } catch {}
}

function overlayOrphaned() {
  try {
    const out = execSync('tasklist /FI "IMAGENAME eq penguin-translate-overlay.exe" /NH', { encoding: 'utf8' });
    return out.includes('penguin-translate-overlay.exe');
  } catch { return false; }
}

async function waitHealth(base, ms) {
  const deadline = Date.now() + ms;
  let last = '';
  while (Date.now() < deadline) {
    try { const r = await fetch(base + '/health'); if (r.ok) return; last = 'status ' + r.status; }
    catch (e) { last = String(e.message || e); }
    await sleep(250);
  }
  throw new Error('app did not become healthy: ' + last);
}

async function main() {
  const exe = resolveExe();
  const profileTag = 'penguin-e2e-' + process.pid;
  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'penguin-e2e-'));
  const dataDir = path.join(work, 'data');
  const profile = path.join(work, 'edge-profile-' + profileTag);
  const wavPath = path.join(work, 'voice.wav');
  const artifactDir = process.env.E2E_ARTIFACT_DIR ? path.resolve(process.env.E2E_ARTIFACT_DIR) : work;
  fs.mkdirSync(dataDir, { recursive: true });
  fs.mkdirSync(artifactDir, { recursive: true });
  fs.writeFileSync(wavPath, synthVoiceWav());

  const { srv: mockSrv, hits } = startMockCloud();
  const mockPort = await freePort();
  await new Promise((r) => mockSrv.listen(mockPort, '127.0.0.1', r));
  const mockBase = `http://127.0.0.1:${mockPort}/v1`;

  const appPort = await freePort();
  const base = `http://127.0.0.1:${appPort}`;
  const appLog = [];
  const app = spawn(exe, ['-http', `:${appPort}`], {
    cwd: ROOT,
    env: { ...process.env, TO_DATA_DIR: dataDir },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  app.stdout.on('data', (d) => appLog.push(d.toString()));
  app.stderr.on('data', (d) => appLog.push(d.toString()));

  let edge;
  let exitCode = 1;
  try {
    await waitHealth(base, 30000);

    const settings = {
      openai_api_key: 'sk-test', openai_base_url: mockBase,
      openrouter_api_key: 'sk-test', openrouter_base_url: mockBase,
      practice: {
        forward_translator: 'openai', api_provider: 'openai', english_asr_engine: 'openai',
        my_language: 'en', other_languages: ['ja'], target_language: 'jp', practice_enabled: false,
      },
      audio: { speech_detection: 'rms', vad_sensitivity: 70 },
    };
    const sresp = await fetch(base + '/api/settings', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settings),
    });
    ok('settings POST accepted (backend pointed at mock cloud)', sresp.ok, 'status ' + sresp.status);
    const applied = await (await fetch(base + '/api/settings')).json();
    ok('output language persisted as Japanese',
      Array.isArray(applied?.practice?.other_languages) && applied.practice.other_languages.includes('ja'),
      JSON.stringify(applied?.practice?.other_languages));

    killStaleEdge(profileTag);
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
      try {
        const list = await fetch(`http://127.0.0.1:${cdpPort}/json`).then((r) => r.json());
        target = list.find((t) => t.type === 'page' && t.url.includes('/ui'));
      } catch {}
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
        if (m.id && pending.has(m.id)) {
          const pr = pending.get(m.id); pending.delete(m.id);
          m.error ? pr.reject(new Error(m.error.message)) : pr.resolve(m.result);
        } else if (m.method === 'Runtime.exceptionThrown') {
          const d = m.params.exceptionDetails;
          exceptions.push(d.exception?.description || d.text || '');
        }
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
    const newTurnWithFixture = (n) => `(()=>{const ts=[...document.querySelectorAll('.view-conversation .turn.out')]; if(ts.length<=${n}) return false; const r=ts[ts.length-1].querySelector('.trans-row'); return !!r && /${FIXTURES.chatTranslation}/.test(r.textContent);})()`;
    const shot = async (name) => {
      try { const r = await send('Page.captureScreenshot', { format: 'png' });
        fs.writeFileSync(path.join(artifactDir, name), Buffer.from(r.data, 'base64')); } catch {}
    };
    await send('Page.enable'); await send('Runtime.enable'); await send('Log.enable');

    const booted = await waitFor(`!!(window.App && window.App.RunController && window.App.getLangs)`, 30000);
    ok('UI shell booted (window.App present)', booted);
    ok('no module/runtime errors on boot',
      exceptions.filter((e) => /SyntaxError|ReferenceError|is not defined|Failed to fetch|TypeError/.test(e)).length === 0,
      exceptions[0] || '');
    const chipReady = await waitFor(`document.querySelectorAll('#langSet .lchip').length===1 && /JA/.test(document.querySelector('#langSet .lchip')?.textContent||'')`, 15000);
    ok('output set shows one Japanese chip (from real /api/settings)', chipReady,
      await evaluate(`[...document.querySelectorAll('#langSet .lchip')].map(c=>c.textContent.trim()).join('|')`));
    await shot('e2e-01-boot.png');

    await waitFor(`!!document.querySelector('.runbar .conv-composer input')`, 10000);
    const chatBefore = hits.chat;
    const outBefore = await evaluate(`document.querySelectorAll('.view-conversation .turn.out').length`);
    await evaluate(`(()=>{const i=document.querySelector('.runbar .conv-composer input'); i.value='good morning everyone'; i.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true}));})()`);
    const typedOk = await waitFor(newTurnWithFixture(outBefore), 15000);
    await shot('e2e-02-typed.png');
    ok('typed reply hit the REAL mock cloud (chat/completions)', hits.chat > chatBefore, `chat hits=${hits.chat}`);
    ok('typed reply rendered the mock translation in a new outgoing turn (full round-trip)', typedOk,
      await evaluate(`(document.querySelector('.view-conversation .turn.out:last-child .trans-row')?.textContent||'').trim()`));
    ok('server-side furigana rendered as ruby on the Japanese row',
      await evaluate(`!!document.querySelector('.view-conversation .turn.out:last-child .trans-row ruby rt')`));

    await evaluate(`document.querySelector('.runbar .lane-sys .lane-toggle')?.click()`); await sleep(200);
    const sttBefore = hits.stt, chatBefore2 = hits.chat;
    const outBefore2 = await evaluate(`document.querySelectorAll('.view-conversation .turn.out').length`);
    await evaluate(`document.getElementById('btnRun').click()`);
    const gotStt = await waitHits(() => hits.stt > sttBefore, 25000);
    const micOk = await waitFor(newTurnWithFixture(outBefore2), 12000);
    await shot('e2e-03-mic.png');
    await evaluate(`document.getElementById('btnRun').click()`);
    ok('fake mic drove a real transcribe to the mock cloud (audio/transcriptions)', gotStt, `stt hits=${hits.stt}`);
    ok('recognized speech fanned out to a real translate', hits.chat > chatBefore2, `chat hits=${hits.chat}`);
    ok('spoken turn rendered the translation in a NEW outgoing turn', micOk);

    ok('no runtime exceptions across the run',
      exceptions.filter((e) => /SyntaxError|ReferenceError|is not defined|TypeError/.test(e)).length === 0,
      exceptions.find((e) => /SyntaxError|ReferenceError|is not defined|TypeError/.test(e)) || '');

    exitCode = results.some((r) => !r.pass) ? 1 : 0;
  } catch (e) {
    console.error('HARNESS ERROR', e);
    exitCode = 2;
  } finally {
    try { edge && edge.kill(); } catch {}
    killStaleEdge(profileTag);
    killTree(app.pid);
    await new Promise((r) => mockSrv.close(r));
    await sleep(500);

    if (overlayOrphaned()) ok('no orphaned overlay subprocess after shutdown', false, 'overlay still running');
    const log = appLog.join('');
    try { fs.writeFileSync(path.join(artifactDir, 'app.log'), log); } catch {}
    if (/panic:|panic\(/i.test(log)) {
      ok('no panic in app log', false);
      console.error('--- app log tail ---\n' + log.slice(-2000));
    }
    if (results.some((r) => !r.pass)) exitCode = exitCode || 1;
  }

  const pass = results.filter((r) => r.pass).length;
  const fail = results.length - pass;
  console.log(`\nRESULT ${JSON.stringify({ pass, fail })}`);
  console.log(`CLOUD  ${JSON.stringify(hits)}`);
  console.log(`ARTIFACTS ${artifactDir}`);
  process.exit(fail ? 1 : exitCode);
}

main().catch((e) => { console.error('FATAL', e); process.exit(2); });
