import Alpine from '/ui/shared/alpine.esm.js';
import { httpErrorMessage } from '/ui/shared/http.js';

let es = null, esTimer = null, stateTimer = null, inited = false;
const normExe = (name) => (name || '').trim().toLowerCase().replace(/^.*[\\/]/, '');

export function createOverlayStore(ctx) {
  const tt = (key, params) => Alpine.store('i18n').t(key, params);

  const store = {
    open: false,
    running: false,
    showText: false,
    status: '',
    savedConfig: {},
    windows: [],
    selectedHwnd: '',
    ocr: '',
    translation: '',

    targetLabel() {
      const w = this.windows.find((x) => String(x.hwnd) === String(this.selectedHwnd));
      if (w) return w.process || w.title || w.label || tt('overlay.targetFallback');
      return this.savedConfig.window_process_name || this.savedConfig.window_title || tt('overlay.targetFallback');
    },
    setStatus(msg, isErr = false) { this.status = msg; if (isErr) ctx.Toasts.push({ title: tt('overlay.title'), msg }); },

    toggle() { this.open = !this.open; if (this.open) this.refreshSetup(); },
    close() { this.open = false; },
    async refreshSetup() {
      if (this.running) return;
      try { await this.loadWindows(); this.pickWindowFromConfig(); }
      catch (e) { this.setStatus(String(e?.message || e), true); }
    },

    async loadConfig() { try { const r = await fetch('/api/config'); this.savedConfig = await r.json(); } catch (_) {} },
    async loadWindows() { const r = await fetch('/api/windows'); const list = await r.json(); this.windows = Array.isArray(list) ? list : []; },
    pickWindowFromConfig() {
      if (!this.windows.length) { this.selectedHwnd = ''; return; }
      const wantExe = normExe(this.savedConfig.window_process_name);
      const wantHwnd = this.savedConfig.window_hwnd ? String(this.savedConfig.window_hwnd) : '';
      const wantTitle = (this.savedConfig.window_title || '').trim().toLowerCase();
      if (wantExe) for (const w of this.windows) if (normExe(w.process) === wantExe) { this.selectedHwnd = String(w.hwnd); return; }
      if (wantHwnd) for (const w of this.windows) if (String(w.hwnd) === wantHwnd) { this.selectedHwnd = String(w.hwnd); return; }
      if (wantTitle) for (const w of this.windows) { const t = (w.title || w.label || '').toLowerCase(); if (t.includes(wantTitle) || wantTitle.includes(t)) { this.selectedHwnd = String(w.hwnd); return; } }
      if (!this.windows.some((w) => String(w.hwnd) === String(this.selectedHwnd))) this.selectedHwnd = String(this.windows[0].hwnd);
    },
    async saveConfig() {
      const w = this.windows.find((x) => String(x.hwnd) === String(this.selectedHwnd));
      const armedExe = (!w && this.savedConfig.window_process_name) ? this.savedConfig.window_process_name : '';
      if (!w && !armedExe) { this.setStatus(tt('overlay.selectWindow'), true); return false; }
      const body = { ...this.savedConfig,
        window_hwnd: w ? (parseInt(this.selectedHwnd, 10) || 0) : 0,
        window_title: w ? (w.title || '') : (this.savedConfig.window_title || ''),
        window_process_name: w ? (w.process || '') : armedExe };
      const r = await fetch('/api/config', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      if (!r.ok) { this.setStatus(await httpErrorMessage(r, 'POST /api/config'), true); return false; }
      this.savedConfig = body; return true;
    },

    async start() {
      if (!(await this.saveConfig())) return;
      const r = await fetch('/api/start', { method: 'POST' });
      if (!r.ok) { this.setStatus(await httpErrorMessage(r, 'POST /api/start'), true); return; }
      this.running = true; this.startStatePoll(); this.setStatus(tt('overlay.statusRunning'));
    },
    async stop() {
      await fetch('/api/stop', { method: 'POST' }).catch(() => {});
      this.stopStatePoll(); this.running = false; this.setStatus(tt('overlay.statusStopped'));
    },
    async change() { await this.stop(); this.open = true; this.refreshSetup(); },

    setRunning(next) { const was = this.running; this.running = !!next; if (!was && this.running) this.open = true; },
    startStatePoll() { if (stateTimer) return; stateTimer = setInterval(() => this.pollState(false), 600); this.pollState(false); },
    stopStatePoll() { if (stateTimer) { clearInterval(stateTimer); stateTimer = null; } },
    async pollState(manage = true) {
      try {
        const r = await fetch('/api/state'); if (!r.ok) return; const j = await r.json();
        if (typeof j.running === 'boolean') { this.setRunning(j.running); if (manage) { if (j.running) this.startStatePoll(); else this.stopStatePoll(); } }
        this.applyUpdate(j);
      } catch (_) {}
    },
    applyUpdate(u) {
      if (!u || typeof u !== 'object') return;
      if (u.err) this.setStatus(u.err);
      else if (u.status) this.setStatus(u.status);
      if ('ocr' in u) this.ocr = u.ocr || '';
      if ('translation' in u) { this.translation = u.translation || ''; if (!u.err && u.translation) this.setStatus(u.cached ? (u.status || '') + ' (cached)' : (u.status || 'Translated')); }
    },
    connectEvents() {
      if (es) { es.close(); es = null; }
      es = new EventSource('/api/events');
      es.addEventListener('update', (e) => { try { this.applyUpdate(JSON.parse(e.data)); } catch (_) {} });
      es.addEventListener('status', (e) => { try { const j = JSON.parse(e.data); if (typeof j.running === 'boolean') { this.setRunning(j.running); if (j.running) this.setStatus(tt('overlay.statusRunning')); } } catch (_) {} });
      es.onopen = () => { if (esTimer) { clearTimeout(esTimer); esTimer = null; } this.pollState(true); };
      es.onerror = () => { es.close(); es = null; if (!esTimer) esTimer = setTimeout(() => { esTimer = null; this.connectEvents(); }, 1000); };
    },
    async resumeIfNeeded() {
      if (!this.savedConfig?.session_active) return;
      for (let i = 0; i < 120; i++) {
        const r = await fetch('/api/start', { method: 'POST' });
        if (r.ok) { this.setRunning(true); this.startStatePoll(); this.setStatus(tt('overlay.statusRunning')); return; }
        if (r.status !== 503) { const m = await r.text(); if (m) this.setStatus(m, true); return; }
        await new Promise((res) => setTimeout(res, 500));
      }
    },
    async init() {
      if (inited) return; inited = true;
      await this.loadConfig();
      this.connectEvents();
      await this.pollState(true);
      document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') this.pollState(true); });
      if (this.savedConfig.session_active) this.resumeIfNeeded();
    },
  };

  return store;
}
