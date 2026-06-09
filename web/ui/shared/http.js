export function formatApiDetail(detail) {
  if (typeof detail === 'string') return detail;
  if (Array.isArray(detail)) {
    return detail.map((item) => {
      if (item && typeof item.msg === 'string') return item.msg;
      return JSON.stringify(item);
    }).join('; ');
  }
  if (detail && typeof detail === 'object') {
    if (typeof detail.message === 'string') return detail.message;
    return JSON.stringify(detail);
  }
  return String(detail || '');
}

export async function httpErrorMessage(r, label) {
  const t = await r.text();
  const where = label || r.url || 'request';
  let msg = t || r.statusText || 'error';
  try {
    const j = JSON.parse(t);
    if (j.detail != null) msg = formatApiDetail(j.detail);
    else if (typeof j.message === 'string') msg = j.message;
    else if (typeof j.error === 'string') msg = j.error;
  } catch (_) {}
  if (/timed out|translation|transcri|multimodal|diariz/i.test(msg)) return msg;
  return `${where} [HTTP ${r.status}]: ${msg}`;
}

export async function getJSON(url, label) {
  const r = await fetch(url);
  if (!r.ok) throw new Error(await httpErrorMessage(r, label || `GET ${url}`));
  return r.json();
}

export async function postJSON(url, body, label) {
  const r = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(await httpErrorMessage(r, label || `POST ${url}`));
  return r.json();
}

export async function postForm(url, formData, label) {
  const r = await fetch(url, { method: 'POST', body: formData });
  if (!r.ok) throw new Error(await httpErrorMessage(r, label || `POST ${url}`));
  return r.json();
}
