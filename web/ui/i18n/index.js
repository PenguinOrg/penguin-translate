import en from '/ui/i18n/en.js';
import ja from '/ui/i18n/ja.js';
import zh from '/ui/i18n/zh.js';
import ko from '/ui/i18n/ko.js';

export const DICTS = { en, ja, zh, ko };

// Keep in sync with internal/feature/mictranslate/infra/languages CanonicalID;
// anything not listed falls back to English.
const ALIASES = {
  jp: 'ja', jpn: 'ja', japanese: 'ja',
  cn: 'zh', zho: 'zh', chinese: 'zh', 'zh-cn': 'zh', zh_hans: 'zh',
  kr: 'ko', kor: 'ko', korean: 'ko',
  eng: 'en', english: 'en',
};

export function baseCode(id) {
  const k = String(id || '').trim().toLowerCase();
  return ALIASES[k] || k;
}

export function resolveLocale(id) {
  const canon = baseCode(id);
  return DICTS[canon] ? canon : 'en';
}

let current = 'en';
export function setLocale(id) { current = resolveLocale(id); return current; }
export function getLocale() { return current; }

function lookup(locale, key) {
  const d = DICTS[locale];
  if (d && d[key] != null) return d[key];
  if (en[key] != null) return en[key];
  return null;
}

export function t(key, params, locale = current) {
  let s = lookup(locale, key);
  if (s == null) return key;
  if (params) s = s.replace(/\{(\w+)\}/g, (m, p) => (p in params ? String(params[p]) : m));
  return s;
}

const _dn = {};
function displayNames(locale) {
  if (!(locale in _dn)) {
    try { _dn[locale] = new Intl.DisplayNames([locale], { type: 'language' }); }
    catch { _dn[locale] = null; }
  }
  return _dn[locale];
}
export function langName(id, fallbackLabel, locale = current) {
  const base = baseCode(id);
  const dn = displayNames(locale);
  if (dn && base) {
    try { const n = dn.of(base); if (n && n.toLowerCase() !== base) return n; } catch { /* empty */ }
  }
  return fallbackLabel || String(id || '').toUpperCase();
}
