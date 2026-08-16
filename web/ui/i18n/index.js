import en from '/ui/i18n/en.js';
import es from '/ui/i18n/es.js';
import fr from '/ui/i18n/fr.js';
import ja from '/ui/i18n/ja.js';
import zh from '/ui/i18n/zh.js';
import zhTW from '/ui/i18n/zh-tw.js';
import ko from '/ui/i18n/ko.js';
import pt from '/ui/i18n/pt.js';

export const DICTS = { en, es, fr, ja, zh, 'zh-tw': zhTW, ko, pt };

// Keep in sync with internal/platform/lang/languages CanonicalID;
// anything not listed falls back to English.
const ALIASES = {
  jp: 'ja', jpn: 'ja', japanese: 'ja',
  es: 'es', spa: 'es', spanish: 'es',
  fr: 'fr', fra: 'fr', french: 'fr',
  cn: 'zh', zho: 'zh', chinese: 'zh', 'zh-cn': 'zh', zh_hans: 'zh',
  tw: 'zh-tw', 'zh-tw': 'zh-tw', zh_tw: 'zh-tw', 'zh-hant': 'zh-tw', zh_hant: 'zh-tw', 'zh-hant-tw': 'zh-tw', zh_hant_tw: 'zh-tw', zho_hant: 'zh-tw', traditional: 'zh-tw', 'traditional chinese': 'zh-tw',
  yue: 'yue', 'yue-hant': 'yue', yue_hant: 'yue', 'yue-hant-hk': 'yue', yue_hant_hk: 'yue', 'yue-hk': 'yue', yue_hk: 'yue', 'yue-cn': 'yue', yue_cn: 'yue', canton: 'yue', cantonese: 'yue', 'yue chinese': 'yue', 'chinese cantonese': 'yue', 'chinese (cantonese)': 'yue', 'zh-yue': 'yue', zh_yue: 'yue',
  wuu: 'wuu', 'wuu-hani': 'wuu', wuu_hani: 'wuu', 'wuu-hani-cn': 'wuu', wuu_hani_cn: 'wuu', 'wuu-cn': 'wuu', wuu_cn: 'wuu', wu: 'wuu', 'wu chinese': 'wuu', 'chinese wu': 'wuu', 'chinese (wu)': 'wuu', 'zh-wuu': 'wuu', zh_wuu: 'wuu',
  kr: 'ko', kor: 'ko', korean: 'ko',
  eng: 'en', english: 'en',
  pt: 'pt', por: 'pt', portuguese: 'pt',
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
