"""API providers (OpenAI / OpenRouter), multimodal caption, zh pinyin."""

from __future__ import annotations

import base64
import json
import logging
import re
import socket
import time
import urllib.error
import urllib.request
from contextlib import contextmanager
from typing import Any, Iterator

_engine_log = logging.getLogger("engine")


@contextmanager
def log_provider_request(*, step: str, provider: str, model: str) -> Iterator[None]:
    """Log wall time for one provider HTTP round-trip (success or failure)."""
    t0 = time.perf_counter()
    try:
        yield
    finally:
        ms = (time.perf_counter() - t0) * 1000
        _engine_log.info(
            "provider %s %s model=%s took %.0fms",
            provider,
            step,
            (model or "").strip() or "?",
            ms,
        )


def is_timeout_exc(exc: BaseException) -> bool:
    if isinstance(exc, (TimeoutError, socket.timeout)):
        return True
    if isinstance(exc, urllib.error.URLError):
        reason = getattr(exc, "reason", None)
        if isinstance(reason, (TimeoutError, socket.timeout)):
            return True
        if reason is not None and "timed out" in str(reason).lower():
            return True
    return "timed out" in str(exc).lower()


def step_timeout_error(step: str, timeout: float, *, model: str = "") -> RuntimeError:
    suffix = f" (model: {model.strip()})" if (model or "").strip() else ""
    return RuntimeError(f"{step} timed out after {int(timeout)}s{suffix}")

OPENROUTER_DEFAULT_BASE = "https://openrouter.ai/api/v1"
OPENAI_DEFAULT_BASE = "https://api.openai.com/v1"

MULTIMODAL_MODEL_PRESETS = [
    "xiaomi/mimo-v2-flash",
    "google/gemini-2.5-flash",
    "google/gemini-2.0-flash-lite-001",
    "openai/gpt-4o-mini",
]

SPLIT_TRANSCRIBE_PRESETS_OPENAI = [
    "gpt-4o-mini-transcribe",
    "gpt-4o-transcribe",
]

# OpenRouter STT uses JSON + base64 (not OpenAI multipart). Whisper models on OR.
SPLIT_TRANSCRIBE_PRESETS_OPENROUTER = [
    "qwen/qwen3-asr-flash-2026-02-10",
    "openai/whisper-large-v3",
    "openai/whisper-1",
]

SPLIT_TRANSLATE_PRESETS_OPENROUTER = [
    "google/gemini-2.0-flash-lite-001",
    "openai/gpt-4o-mini",
    "xiaomi/mimo-v2-flash",
]


def resolve_api(
    *,
    api_provider: str,
    openai_api_key: str,
    openai_base_url: str,
    openrouter_api_key: str,
    openrouter_base_url: str,
) -> tuple[str, str, str]:
    """Returns (api_key, base_url, provider_label)."""
    prov = (api_provider or "openai").strip().lower()
    if prov == "openrouter":
        key = (openrouter_api_key or "").strip() or (openai_api_key or "").strip()
        base = (openrouter_base_url or "").strip() or OPENROUTER_DEFAULT_BASE
        if not key:
            raise ValueError("OpenRouter API key required (settings)")
        return key, base.rstrip("/"), "openrouter"
    key = (openai_api_key or "").strip()
    base = (openai_base_url or "").strip() or OPENAI_DEFAULT_BASE
    if not key:
        raise ValueError("OpenAI API key required (settings)")
    return key, base.rstrip("/"), "openai"


def request_headers(api_key: str, provider: str) -> dict[str, str]:
    h = {"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}
    if provider == "openrouter":
        h["HTTP-Referer"] = "https://github.com/system-audio-transcribe"
        h["X-Title"] = "System Audio Transcribe"
    return h


def openrouter_transcribe_wav(
    wav_bytes: bytes,
    *,
    api_key: str,
    base_url: str,
    model: str,
    language: str | None,
    timeout: float = 5.0,
) -> dict[str, Any]:
    """OpenRouter /audio/transcriptions — JSON body with base64 audio (not multipart)."""
    if len(wav_bytes) < 800:
        return {"text": "", "segments": [], "raw": {}}
    lang = (language or "").strip().lower()
    b64 = base64.standard_b64encode(wav_bytes).decode("ascii")
    payload: dict[str, Any] = {
        "model": model.strip(),
        "input_audio": {"data": b64, "format": "wav"},
    }
    if len(lang) >= 2:
        payload["language"] = lang[:2]
    url = base_url.rstrip("/") + "/audio/transcriptions"
    body = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=body, headers=request_headers(api_key, "openrouter"), method="POST")
    try:
        with log_provider_request(step="Transcription (STT)", provider="openrouter", model=model):
            with urllib.request.urlopen(req, timeout=timeout) as r:
                data = json.load(r)
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Transcription (STT) HTTP {e.code}: {err_body[:4000]}") from e
    except Exception as e:
        if is_timeout_exc(e):
            raise step_timeout_error("Transcription (STT)", timeout, model=model) from e
        raise RuntimeError(f"Transcription (STT) failed: {type(e).__name__}: {e}") from e
    if not isinstance(data, dict):
        raise RuntimeError("OpenRouter STT unexpected response")
    text = (data.get("text") or "").strip()
    return {"text": text, "segments": [], "raw": data}


def caption_presets() -> dict[str, Any]:
    return {
        "openai": {
            "split_transcribe": SPLIT_TRANSCRIBE_PRESETS_OPENAI,
            "split_translate": ["gpt-4o-mini", "gpt-4o"],
            "split_diarize": ["gpt-4o-transcribe-diarize"],
            "multimodal": MULTIMODAL_MODEL_PRESETS,
        },
        "openrouter": {
            "split_transcribe": SPLIT_TRANSCRIBE_PRESETS_OPENROUTER,
            "split_translate": SPLIT_TRANSLATE_PRESETS_OPENROUTER,
            "split_diarize": [],
            "multimodal": MULTIMODAL_MODEL_PRESETS,
        },
    }


def chat_completion(
    *,
    api_key: str,
    base_url: str,
    provider: str,
    model: str,
    system: str | None,
    user_text: str,
    temperature: float = 0.1,
    timeout: int = 180,
    step: str = "Chat",
) -> str:
    url = base_url.rstrip("/") + "/chat/completions"
    messages: list[dict[str, Any]] = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": user_text})
    body = json.dumps({"model": model, "messages": messages, "temperature": temperature}).encode()
    req = urllib.request.Request(url, data=body, headers=request_headers(api_key, provider), method="POST")
    try:
        with log_provider_request(step=step, provider=provider, model=model):
            with urllib.request.urlopen(req, timeout=timeout) as r:
                data = json.load(r)
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{step} HTTP {e.code}: {err_body[:4000]}") from e
    except Exception as e:
        if is_timeout_exc(e):
            raise step_timeout_error(step, float(timeout), model=model) from e
        raise RuntimeError(f"{step} failed: {type(e).__name__}: {e}") from e
    try:
        return (data["choices"][0]["message"]["content"] or "").strip()
    except (KeyError, IndexError, TypeError) as e:
        raise RuntimeError("Unexpected chat response") from e


def multimodal_caption_wav(
    wav_bytes: bytes,
    *,
    api_key: str,
    base_url: str,
    provider: str,
    model: str,
    language: str,
    want_translate: bool,
    timeout: float = 5.0,
) -> dict[str, str]:
    if len(wav_bytes) < 800:
        return {"text": "", "english": "", "detected_lang": language}
    lang = (language or "ja")[:2]
    b64 = base64.standard_b64encode(wav_bytes).decode("ascii")
    tr_note = (
        'Include "english" with a natural English translation.'
        if want_translate
        else '"english" must be an empty string.'
    )
    sys_prompt = (
        "You transcribe short speech audio. Focus on Chinese (zh), Japanese (ja), or English (en). "
        f"Language hint: {lang}. "
        f"{tr_note} "
        'Reply with ONLY JSON: {"text":"…","english":"…","detected_lang":"zh|ja|en"} — no markdown.'
    )
    user_content: list[dict[str, Any]] = [
        {"type": "text", "text": sys_prompt},
        {"type": "input_audio", "input_audio": {"data": b64, "format": "wav"}},
    ]
    url = base_url.rstrip("/") + "/chat/completions"
    body = json.dumps(
        {
            "model": model.strip(),
            "messages": [{"role": "user", "content": user_content}],
            "temperature": 0.1,
        }
    ).encode()
    req = urllib.request.Request(url, data=body, headers=request_headers(api_key, provider), method="POST")
    try:
        with log_provider_request(step="Multimodal (STT+translate)", provider=provider, model=model):
            with urllib.request.urlopen(req, timeout=timeout) as r:
                raw_resp = r.read()
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Multimodal HTTP {e.code}: {err_body[:4000]}") from e
    except urllib.error.URLError as e:
        reason = getattr(e, "reason", e)
        raise RuntimeError(f"Multimodal network error: {reason}") from e
    except Exception as e:
        if is_timeout_exc(e):
            raise step_timeout_error("Multimodal (STT+translate)", timeout, model=model) from e
        raise RuntimeError(f"Multimodal request failed: {type(e).__name__}: {e}") from e

    try:
        data = json.loads(raw_resp.decode("utf-8"))
    except json.JSONDecodeError as e:
        sample = raw_resp[:800].decode("utf-8", errors="replace")
        raise RuntimeError(f"Multimodal non-JSON response: {sample[:400]}") from e

    if not isinstance(data, dict):
        raise RuntimeError(f"Multimodal unexpected response type: {type(data).__name__}")

    if err := data.get("error"):
        if isinstance(err, dict):
            msg = str(err.get("message") or err.get("code") or json.dumps(err, ensure_ascii=False)[:500])
        else:
            msg = str(err)
        raise RuntimeError(f"Multimodal API error: {msg}")

    choices = data.get("choices")
    if not isinstance(choices, list) or not choices:
        snippet = json.dumps(data, ensure_ascii=False)[:600]
        raise RuntimeError(f"Multimodal missing choices in response: {snippet}")

    first = choices[0] if isinstance(choices[0], dict) else {}
    msg_obj = first.get("message") if isinstance(first.get("message"), dict) else {}
    raw = (msg_obj.get("content") or "").strip()
    try:
        obj = json.loads(raw)
    except json.JSONDecodeError:
        m = re.search(r"\{[\s\S]*\}", raw)
        if not m:
            return {"text": raw, "english": "", "detected_lang": lang}
        obj = json.loads(m.group(0))
    if not isinstance(obj, dict):
        return {"text": raw, "english": "", "detected_lang": lang}
    return {
        "text": str(obj.get("text") or "").strip(),
        "english": str(obj.get("english") or "").strip(),
        "detected_lang": str(obj.get("detected_lang") or lang)[:2],
    }


def _multimodal_audio_chat_json(
    wav_bytes: bytes,
    *,
    api_key: str,
    base_url: str,
    provider: str,
    model: str,
    system_prompt: str,
    timeout: float = 5.0,
) -> dict[str, Any]:
    """One chat/completions call with wav input; parse JSON object from model content."""
    if len(wav_bytes) < 800:
        return {}
    b64 = base64.standard_b64encode(wav_bytes).decode("ascii")
    user_content: list[dict[str, Any]] = [
        {"type": "text", "text": system_prompt},
        {"type": "input_audio", "input_audio": {"data": b64, "format": "wav"}},
    ]
    url = base_url.rstrip("/") + "/chat/completions"
    body = json.dumps(
        {
            "model": model.strip(),
            "messages": [{"role": "user", "content": user_content}],
            "temperature": 0.1,
        }
    ).encode()
    req = urllib.request.Request(url, data=body, headers=request_headers(api_key, provider), method="POST")
    try:
        with log_provider_request(step="Multimodal (STT+translate)", provider=provider, model=model):
            with urllib.request.urlopen(req, timeout=timeout) as r:
                raw_resp = r.read()
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Multimodal HTTP {e.code}: {err_body[:4000]}") from e
    except urllib.error.URLError as e:
        reason = getattr(e, "reason", e)
        raise RuntimeError(f"Multimodal network error: {reason}") from e
    except Exception as e:
        if is_timeout_exc(e):
            raise step_timeout_error("Multimodal (STT+translate)", timeout, model=model) from e
        raise RuntimeError(f"Multimodal request failed: {type(e).__name__}: {e}") from e

    try:
        data = json.loads(raw_resp.decode("utf-8"))
    except json.JSONDecodeError as e:
        sample = raw_resp[:800].decode("utf-8", errors="replace")
        raise RuntimeError(f"Multimodal non-JSON response: {sample[:400]}") from e

    if not isinstance(data, dict):
        raise RuntimeError(f"Multimodal unexpected response type: {type(data).__name__}")

    if err := data.get("error"):
        if isinstance(err, dict):
            msg = str(err.get("message") or err.get("code") or json.dumps(err, ensure_ascii=False)[:500])
        else:
            msg = str(err)
        raise RuntimeError(f"Multimodal API error: {msg}")

    choices = data.get("choices")
    if not isinstance(choices, list) or not choices:
        snippet = json.dumps(data, ensure_ascii=False)[:600]
        raise RuntimeError(f"Multimodal missing choices in response: {snippet}")

    first = choices[0] if isinstance(choices[0], dict) else {}
    msg_obj = first.get("message") if isinstance(first.get("message"), dict) else {}
    raw = (msg_obj.get("content") or "").strip()
    try:
        obj = json.loads(raw)
    except json.JSONDecodeError:
        m = re.search(r"\{[\s\S]*\}", raw)
        if not m:
            return {"raw": raw}
        obj = json.loads(m.group(0))
    if not isinstance(obj, dict):
        return {"raw": raw}
    return obj


def multimodal_practice_wav(
    wav_bytes: bytes,
    *,
    api_key: str,
    base_url: str,
    provider: str,
    model: str,
    target_kind: str,
) -> dict[str, str]:
    """English speech → {english, target} in one multimodal call (practice mode)."""
    kind = (target_kind or "jp").strip().lower()
    if kind in ("ja",):
        kind = "jp"
    if kind not in ("jp", "zh", "ko"):
        kind = "jp"
    lang_name = {"jp": "Japanese", "zh": "Chinese", "ko": "Korean"}.get(kind, "Japanese")
    prompt = (
        f"You help with English-to-{lang_name} language practice. "
        f"Transcribe the speaker's English accurately, then translate to natural {lang_name}. "
        'Reply with ONLY JSON: {"english":"…","target":"…"} — no markdown.'
    )
    obj = _multimodal_audio_chat_json(
        wav_bytes,
        api_key=api_key,
        base_url=base_url,
        provider=provider,
        model=model,
        system_prompt=prompt,
    )
    if not obj:
        return {"english": "", "target": ""}
    if "raw" in obj and len(obj) == 1:
        return {"english": str(obj.get("raw") or "").strip(), "target": ""}
    target = str(obj.get("target") or obj.get("japanese") or obj.get("chinese") or obj.get("korean") or "").strip()
    return {
        "english": str(obj.get("english") or "").strip(),
        "target": target,
    }


def _strip_markdown_fences(text: str) -> str:
    t = (text or "").strip()
    if t.startswith("```"):
        t = re.sub(r"^```(?:json)?\s*", "", t, flags=re.IGNORECASE)
        t = re.sub(r"\s*```$", "", t)
    return t.strip()


def _fix_trailing_commas(text: str) -> str:
    return re.sub(r",\s*([}\]])", r"\1", text)


def _translations_from_array(arr: list[Any], n: int) -> list[str]:
    out_map: dict[int, str] = {}
    for item in arr:
        if not isinstance(item, dict):
            continue
        idx = item.get("i")
        if isinstance(idx, bool):
            continue
        if isinstance(idx, str) and idx.isdigit():
            idx = int(idx)
        if not isinstance(idx, int):
            continue
        en = item.get("en")
        if en is None:
            en = item.get("english")
        out_map[idx] = str(en or "").strip()
    return [out_map.get(i, "") for i in range(n)]


def _parse_translation_batch(raw: str, n: int) -> list[str] | None:
    """Best-effort parse of a batch translation model reply."""
    text = _strip_markdown_fences(raw)
    if not text:
        return None

    candidates: list[str] = [text, _fix_trailing_commas(text)]
    m = re.search(r"\[[\s\S]*\]", text)
    if m:
        frag = m.group(0)
        candidates.extend([frag, _fix_trailing_commas(frag)])

    for cand in candidates:
        try:
            parsed = json.loads(cand)
        except json.JSONDecodeError:
            continue
        if isinstance(parsed, dict):
            for key in ("items", "translations", "results", "data"):
                inner = parsed.get(key)
                if isinstance(inner, list):
                    return _translations_from_array(inner, n)
        if isinstance(parsed, list):
            return _translations_from_array(parsed, n)

    scraped: list[dict[str, Any]] = []
    for mobj in re.finditer(
        r'\{\s*"i"\s*:\s*(\d+)\s*,\s*"en"\s*:\s*"((?:[^"\\]|\\.)*)"\s*\}',
        text,
    ):
        scraped.append({"i": int(mobj.group(1)), "en": mobj.group(2)})
    if scraped:
        return _translations_from_array(scraped, n)

    numbered = [""] * n
    got = 0
    for line in text.splitlines():
        mobj = re.match(r"^\s*(\d+)\s*[:.)-]\s*(.+?)\s*$", line)
        if not mobj:
            continue
        idx = int(mobj.group(1))
        if 0 <= idx < n:
            numbered[idx] = mobj.group(2).strip()
            got += 1
    if got > 0:
        return numbered

    if n == 1 and text and not text.lstrip().startswith(("[", "{")):
        return [text.strip()]
    return None


def batch_translate_to_en(
    lines: list[str],
    *,
    source_lang: str,
    api_key: str,
    base_url: str,
    provider: str,
    model: str,
    timeout: float = 5.0,
) -> list[str]:
    cleaned = [(t or "").strip() for t in lines]
    if not any(cleaned):
        return [""] * len(lines)
    lang = (source_lang or "ja")[:2]
    lang_name = {"ja": "Japanese", "zh": "Chinese", "en": "English"}.get(lang, lang)
    payload = json.dumps([{"i": i, "t": t} for i, t in enumerate(cleaned)], ensure_ascii=False)
    sys_prompt = (
        f"You translate {lang_name} lines to natural English. "
        'Input is JSON [{"i":number,"t":string}]. '
        'Output ONLY a JSON array [{"i":number,"en":string}] in the same order. '
        "Escape double quotes inside en strings. No markdown, no commentary."
    )
    raw = chat_completion(
        api_key=api_key,
        base_url=base_url,
        provider=provider,
        model=model,
        system=sys_prompt,
        user_text=payload,
        timeout=int(timeout),
        step="Translation",
    )
    parsed = _parse_translation_batch(raw, len(lines))
    if parsed is None:
        _engine_log.warning(
            "Translation batch parse failed (%d lines, model=%s): %r",
            len(lines),
            model,
            (raw or "")[:500],
        )
        return [""] * len(lines)
    return parsed


def zh_pinyin_pairs(text: str) -> list[dict[str, str]]:
    from pypinyin import Style, pinyin

    t = (text or "").strip()
    if not t:
        return []
    pairs: list[dict[str, str]] = []
    for ch in t:
        if "\u4e00" <= ch <= "\u9fff":
            py = pinyin(ch, style=Style.TONE, errors="ignore")
            ro = py[0][0] if py and py[0] else ""
            pairs.append({"jp": ch, "ro": ro})
        elif ch.isspace():
            pairs.append({"jp": "\u00a0", "ro": ""})
        else:
            pairs.append({"jp": ch, "ro": ""})
    return pairs


def enrich_chinese_line(text: str) -> dict[str, Any]:
    t = (text or "").strip()
    if not t:
        return {"zh_pinyin": [], "jp_romaji": [], "ko_roman": []}
    try:
        zp = zh_pinyin_pairs(t)
    except Exception:
        zp = [{"jp": t, "ro": ""}]
    return {"zh_pinyin": zp, "jp_romaji": [], "ko_roman": []}


def _is_hangul_syllable(ch: str) -> bool:
    o = ord(ch)
    return 0xAC00 <= o <= 0xD7AF


def text_mostly_hangul(text: str) -> bool:
    hangul = 0
    other_letters = 0
    for ch in text or "":
        if _is_hangul_syllable(ch):
            hangul += 1
        elif ch.isalpha():
            other_letters += 1
    return hangul > 0 and hangul >= other_letters


def ko_roman_pairs(text: str) -> list[dict[str, str]]:
    """Per-syllable Revised Romanization (reading line / ruby), like zh_pinyin."""
    from hangul_romanize import Transliter
    from hangul_romanize.rule import academic

    t = (text or "").strip()
    if not t:
        return []
    tr = Transliter(academic)
    pairs: list[dict[str, str]] = []
    for ch in t:
        if _is_hangul_syllable(ch):
            pairs.append({"jp": ch, "ro": (tr.translit(ch) or "").strip()})
        elif ch.isspace():
            pairs.append({"jp": "\u00a0", "ro": ""})
        else:
            pairs.append({"jp": ch, "ro": ""})
    return pairs


def enrich_korean_line(text: str) -> dict[str, Any]:
    t = (text or "").strip()
    if not t:
        return {"ko_roman": [], "zh_pinyin": [], "jp_romaji": []}
    try:
        kr = ko_roman_pairs(t)
    except Exception as e:
        import logging

        logging.getLogger("engine").warning("KO romanization failed: %s", e)
        kr = [{"jp": t, "ro": ""}]
    return {"ko_roman": kr, "zh_pinyin": [], "jp_romaji": []}
