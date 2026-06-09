import Alpine from '/ui/shared/alpine.esm.js';
import * as i18n from '/ui/i18n/index.js';
import { createConversationStore } from '/ui/features/conversation.js';
import { createOverlayStore } from '/ui/features/overlay.js';
import { getJSON, postJSON } from '/ui/shared/http.js';
import { createPrefsStore } from '/ui/prefs.js';

Alpine.store('i18n', {
  locale: 'en',
  set(id) { this.locale = i18n.setLocale(id); },
  t(key, params) { return i18n.t(key, params, this.locale); },
  langName(id, fallback) { return i18n.langName(id, fallback, this.locale); },
});

Alpine.store('toasts', {
  items: [],
  _id: 0,
  push({ severity = 'error', title = '', msg = '', actions = [], timeout = 0 } = {}) {
    const id = ++this._id;
    this.items.push({ id, severity: severity === 'warn' ? 'warn' : 'err', title, msg, actions });
    if (timeout > 0) setTimeout(() => this.dismiss(id), timeout);
    return id;
  },
  dismiss(id) { const i = this.items.findIndex((t) => t.id === id); if (i >= 0) this.items.splice(i, 1); },
  clear() { this.items.splice(0, this.items.length); },
  runAction(tt, a) { try { a.onClick?.(); } finally { if (a.dismiss !== false) this.dismiss(tt.id); } },
});

const ENGINE_INSTALL_PHASES = new Set(['create_venv', 'pip_upgrade', 'pip_install', 'sync_engine']);
let engSubs = new Set();
let engTimer = null;
let engStopped = false;
const engHooks = { retry: null, openLog: null };
Alpine.store('engine', {
  phase: 'ready', label: '', percent: 100, error: '', logTail: [], health: null,
  get installing() { return ENGINE_INSTALL_PHASES.has(this.phase); },
  get failed() { return this.phase === 'failed'; },
  get hidden() { return this.phase === 'ready' || !this.phase; },
  setPhase(next) {
    Object.assign(this, next);
    const snap = this.get();
    engSubs.forEach((cb) => { try { cb(snap); } catch (_) {} });
  },
  async refresh() {
    const [ls, hs] = await Promise.allSettled([
      fetch('/api/launcher-status').then((r) => (r.ok ? r.json() : null)),
      fetch('/api/health-summary').then((r) => (r.ok ? r.json() : null)),
    ]);
    if (ls.status === 'fulfilled' && ls.value) {
      const v = ls.value;
      this.setPhase({ phase: v.phase, label: v.label, percent: v.percent, error: v.error || '', logTail: v.log_tail || [] });
    }
    if (hs.status === 'fulfilled' && hs.value) this.health = hs.value;
  },
  _loop() {
    if (engStopped) return;
    this.refresh().catch(() => {});
    const ms = this.phase === 'ready' ? 5000 : 1000;
    engTimer = setTimeout(() => this._loop(), ms);
  },
  start() { engStopped = false; clearTimeout(engTimer); this._loop(); },
  stop() { engStopped = true; clearTimeout(engTimer); },
  get() { return { phase: this.phase, label: this.label, percent: this.percent, error: this.error, logTail: this.logTail, health: this.health }; },
  subscribe(cb) { engSubs.add(cb); return () => engSubs.delete(cb); },
  onRetry(cb) { engHooks.retry = cb; },
  onOpenLog(cb) { engHooks.openLog = cb; },
  retry() { engHooks.retry?.(); },
  openLog() { engHooks.openLog?.(); },
});

Alpine.store('langs', {
  my: 'en', others: ['ja'], catalog: [],
  byId(id) { return this.catalog.find((l) => l.id === id) || { id, label: id, flag: '🏳️', short_label: (id || '').toUpperCase() }; },
  abbr(id) { const L = this.byId(id); return L.short_label || (L.id || '').toUpperCase(); },
  flag(id) { return this.byId(id).flag || ''; },
  label(id) { const L = this.byId(id); return L.label || id; },
  broadcast() { document.dispatchEvent(new CustomEvent('langschange', { detail: { my: this.my, others: this.others.slice() } })); },
  async persist() {
    try { await postJSON('/api/settings', { practice: { my_language: this.my, other_languages: this.others } }, 'POST /api/settings'); }
    catch (e) { Alpine.store('toasts').push({ title: i18n.t('toast.saveLangsFailed'), msg: String(e?.message || e) }); }
  },
  pickMy(id) {
    if (id === this.my) return;
    this.my = id; this.others = this.others.filter((o) => o !== id);
    if (!this.others.length) { const alt = this.catalog.find((l) => l.id !== id); if (alt) this.others = [alt.id]; }
    Alpine.store('i18n').set(this.my);
    this.persist(); this.broadcast();
  },
  toggleOther(id) {
    if (id === this.my) return;
    if (this.others.includes(id)) { if (this.others.length > 1) this.others = this.others.filter((o) => o !== id); }
    else this.others = [...this.others, id];
    this.persist(); this.broadcast();
  },
  removeOther(id) { if (this.others.length <= 1) return; this.others = this.others.filter((o) => o !== id); this.persist(); this.broadcast(); },
  async load() {
    try { const j = await getJSON('/api/languages'); this.catalog = Array.isArray(j.catalog) ? j.catalog : []; } catch (_) { this.catalog = []; }
    try {
      const p = (await getJSON('/api/settings')).practice || {};
      if (p.my_language) this.my = p.my_language;
      if (Array.isArray(p.other_languages) && p.other_languages.length) this.others = p.other_languages;
    } catch (_) {}
    Alpine.store('i18n').set(this.my);
    this.broadcast();
  },
  getLangs() { return { my: this.my, others: this.others.slice() }; },
});

const Toasts = Alpine.store('toasts');
const EngineStatus = Alpine.store('engine');
const Langs = Alpine.store('langs');
const I18n = Alpine.store('i18n');

Alpine.data('langbar', () => ({
  menuOpen: false,
  menuMode: null,
  menuItems() {
    const L = this.$store.langs;
    return L.catalog.filter((l) => (this.menuMode === 'others' ? l.id !== L.my : true));
  },
  isOn(id) {
    const L = this.$store.langs;
    return this.menuMode === 'my' ? id === L.my : L.others.includes(id);
  },
  openMenu(mode, ev) {
    if (this.menuMode === mode && this.menuOpen) { this.closeMenu(); return; }
    this.menuMode = mode; this.menuOpen = true;
    const anchor = ev && ev.currentTarget;
    this.$nextTick(() => this.position(anchor));
  },
  closeMenu() { this.menuOpen = false; this.menuMode = null; },
  pick(id) {
    if (this.menuMode === 'my') { this.$store.langs.pickMy(id); this.closeMenu(); }
    else this.$store.langs.toggleOther(id);
  },
  position(anchor) {
    const menu = this.$refs.menu; if (!menu || !anchor) return;
    const barRect = this.$el.getBoundingClientRect();
    const a = anchor.getBoundingClientRect();
    let left = a.left - barRect.left;
    left = Math.max(8, Math.min(left, barRect.width - 272));
    menu.style.left = left + 'px';
  },
}));

Alpine.store('conversation', createConversationStore({ Toasts }));
const RunController = { repaint() {}, get running() { return Alpine.store('conversation').listening; } };

Alpine.store('prefs', createPrefsStore({ Toasts, EngineStatus }));
const Preferences = {
  open: (id) => Alpine.store('prefs').show(id),
  select: (id) => Alpine.store('prefs').select(id),
  close: () => Alpine.store('prefs').close(),
  isOpen: () => Alpine.store('prefs').open,
};

Alpine.data('prefsModal', () => ({
  get s() { return Alpine.store('prefs'); },
  get practice() { return this.s.practice; },
  get audio() { return this.s.audio; },
  get settings() { return this.s.settings; },
  get osc() { return this.s.oscDraft; },
}));

Alpine.store('overlay', createOverlayStore({ Toasts }));

const AUTH_RE = /\b40[13]\b|unauthorized|forbidden|api[\s_-]?key|no valid[^.]*key|invalid[^.]*key|missing[^.]*key/i;

function reportApiError(err, { title, retry } = {}) {
  const msg = String(err?.message || err);
  const actions = [];
  if (AUTH_RE.test(msg)) {
    actions.push({ label: i18n.t('toast.openInference'), primary: true, onClick: () => Preferences.open('inference') });
  }
  if (typeof retry === 'function') {
    actions.push({ label: i18n.t('common.retry'), onClick: () => { retry(); }, dismiss: true });
  }
  Toasts.push({ title: title || i18n.t('toast.requestFailed'), msg, actions });
  return err;
}

const Api = {
  async get(url, { label, title, retry } = {}) {
    try { return await getJSON(url, label); }
    catch (e) { throw reportApiError(e, { title, retry: retry || (() => Api.get(url, { label, title })) }); }
  },
  async post(url, body, { label, title, retry } = {}) {
    try { return await postJSON(url, body, label); }
    catch (e) { throw reportApiError(e, { title, retry: retry || (() => Api.post(url, body, { label, title })) }); }
  },
  report: reportApiError,
};

const setView = () => {};

EngineStatus.onRetry(async () => {
  try { await Api.post('/api/engine-load', {}, { title: i18n.t('banner.failed') }); } catch (_) {}
  EngineStatus.refresh();
});
EngineStatus.onOpenLog(() => Preferences.open('diagnostics'));

window.App = {
  Toasts, EngineStatus, RunController, Preferences, Api,
  setView,
  getLangs: () => Langs.getLangs(),
  reloadLangs: () => Langs.load(),
  views: [Alpine.store('conversation')],
  i18n: I18n,
  Overlay: Alpine.store('overlay'),
};

Langs.load();

EngineStatus.start();

// Alpine.start() must run last: every store, component and window.App is
// registered above so any x-data init() can read them on hydration.
window.Alpine = Alpine;
Alpine.start();
