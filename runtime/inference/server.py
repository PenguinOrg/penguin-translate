"""Penguin Translate inference engine: FastAPI service spawned by the Go shell and proxied over localhost. Heavy deps load lazily; endpoint reference is in the repo README."""

from __future__ import annotations

import asyncio
import hashlib
import logging
import sys

# Windows engine.log is often cp1252; avoid UnicodeEncodeError on progress lines.
for _stream in (sys.stdout, sys.stderr):
    if hasattr(_stream, "reconfigure"):
        try:
            _stream.reconfigure(encoding="utf-8", errors="replace")
        except Exception:
            pass
import io
import base64
import json
import os
from collections import OrderedDict

# Align ctranslate2 CUDA indices with nvidia-smi order (must be set before CUDA init).
os.environ.setdefault("CUDA_DEVICE_ORDER", "PCI_BUS_ID")

import re
import subprocess
import threading
import time
import unicodedata
import urllib.error
import urllib.request
import wave
from difflib import SequenceMatcher
from pathlib import Path
from typing import Any

from fastapi import FastAPI, File, Form, HTTPException, Request, UploadFile
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field
import uvicorn

app = FastAPI(title="translation-overlay-engine", version="0.1.0")
_engine_log = logging.getLogger("engine")

_whisper: Any = None
_translator: Any = None
_tok_cache: dict[tuple[str, str], Any] = {}
_katsu: Any = None
_load_lock = threading.Lock()
_ENGINE_STARTED_AT = time.monotonic()
print(f"translation-overlay-engine: process start pid={os.getpid()}", flush=True)


def _cold_load(label: str, fn) -> None:
    t0 = time.perf_counter()
    fn()
    ms = (time.perf_counter() - t0) * 1000.0
    print(f"translation-overlay-engine: cold_load {label}={ms:.0f}ms", flush=True)


class _StepTimer:
    """Per-request step timings (ms). Logs to stdout → engine.log."""

    __slots__ = ("_label", "_step", "_steps", "_t0")

    def __init__(self, label: str) -> None:
        self._label = label
        self._t0 = time.perf_counter()
        self._step = self._t0
        self._steps: list[tuple[str, float]] = []

    def mark(self, name: str) -> None:
        now = time.perf_counter()
        self._steps.append((name, (now - self._step) * 1000.0))
        self._step = now

    def timings_ms(self) -> dict[str, float]:
        out = {name: round(ms, 1) for name, ms in self._steps}
        out["total_ms"] = round((time.perf_counter() - self._t0) * 1000.0, 1)
        return out

    def log(self, **extra: Any) -> dict[str, float]:
        timings = self.timings_ms()
        parts = " ".join(f"{name}={timings[name]:.0f}ms" for name, _ in self._steps)
        msg = f"translation-overlay-engine: {self._label} {parts} total={timings['total_ms']:.0f}ms"
        if extra:
            msg += " " + " ".join(f"{k}={v}" for k, v in extra.items())
        print(msg, flush=True)
        return timings


@app.exception_handler(Exception)
async def _unhandled_exception_handler(request: Request, exc: Exception) -> JSONResponse:
    if isinstance(exc, HTTPException):
        return JSONResponse(status_code=exc.status_code, content={"detail": exc.detail})
    _engine_log.exception("unhandled %s %s", request.method, request.url.path)
    detail = f"{type(exc).__name__}: {exc}"
    print(
        f"translation-overlay-engine: unhandled {request.method} {request.url.path}: {detail}",
        flush=True,
    )
    return JSONResponse(status_code=500, content={"detail": detail})


@app.middleware("http")
async def _log_request_timing(request: Request, call_next):
    path = request.url.path
    skip_log = path in ("/health", "/openapi.json", "/docs", "/redoc")
    t0 = time.perf_counter()
    try:
        response = await call_next(request)
    except Exception as exc:
        if not skip_log:
            ms = (time.perf_counter() - t0) * 1000.0
            print(
                f"translation-overlay-engine: {request.method} {path} -> error {ms:.0f}ms: {type(exc).__name__}: {exc}",
                flush=True,
            )
        raise
    if not skip_log:
        ms = (time.perf_counter() - t0) * 1000.0
        print(
            f"translation-overlay-engine: {request.method} {path} -> {response.status_code} {ms:.0f}ms",
            flush=True,
        )
    return response




_WHISPER_VARIANT = "large-v3-turbo"
_NLLB_VARIANT = "nllb-200-distilled-1.3B-ct2-int8"


def _env_first(*names: str) -> str:
    """First non-empty env value among names. Current TO_* names come first; LP_* are legacy aliases kept for back-compat."""
    for name in names:
        if v := os.environ.get(name, "").strip():
            return v
    return ""


def _weights_root_candidates() -> list[Path]:
    out: list[Path] = []
    for key in ("TO_WEIGHTS_ROOT", "LP_WEIGHTS_ROOT", "VRCT_WEIGHTS_ROOT"):
        if v := os.environ.get(key, "").strip():
            out.append(Path(v))
    local = os.environ.get("LOCALAPPDATA")
    if local:
        out.extend(
            [
                Path(local) / "translation-overlay" / "weights",
                Path(local) / "VRCT" / "weights",
            ]
        )
    out.append(Path.home() / ".cache" / "translation-overlay" / "weights")
    seen: set[str] = set()
    unique: list[Path] = []
    for p in out:
        s = str(p)
        if s not in seen:
            seen.add(s)
            unique.append(p)
    return unique


def _weights_root() -> Path:
    """Pick a weights root that already has model files, else the app default."""
    whisper_rel = Path("whisper") / _WHISPER_VARIANT / "model.bin"
    nllb_rel = Path("ctranslate2") / _NLLB_VARIANT / "model.bin"
    for root in _weights_root_candidates():
        if (root / whisper_rel).is_file() or (root / nllb_rel).is_file():
            return root
    for key in ("TO_WEIGHTS_ROOT", "LP_WEIGHTS_ROOT"):
        if v := os.environ.get(key, "").strip():
            return Path(v)
    cands = _weights_root_candidates()
    return cands[0] if cands else Path.home() / ".cache" / "translation-overlay" / "weights"


def _whisper_dir() -> Path:
    if env := _env_first("TO_WHISPER_DIR", "LP_WHISPER_DIR"):
        return Path(env)
    return _weights_root() / "whisper" / _WHISPER_VARIANT


def _nllb_dir() -> Path:
    if env := _env_first("TO_NLLB_DIR", "LP_NLLB_DIR"):
        return Path(env)
    return _weights_root() / "ctranslate2" / _NLLB_VARIANT


def _nllb_tokenizer_try_sources() -> list[str]:
    env = _env_first("TO_NLLB_TOKENIZER", "LP_NLLB_TOKENIZER")
    if env:
        return [env]
    return [
        "facebook/nllb-200-distilled-1.3B",
        str(_nllb_dir()),
    ]


def _truthy_env(name: str) -> bool:
    return os.environ.get(name, "").strip().lower() in ("1", "true", "yes")


def _ctranslate2_cuda_device_count() -> int:
    try:
        import ctranslate2

        return max(0, int(ctranslate2.get_cuda_device_count()))
    except Exception:
        return 0


def _gpu_memory_mib() -> dict[int, int]:
    try:
        out = subprocess.check_output(
            ["nvidia-smi", "--query-gpu=index,memory.used", "--format=csv,noheader,nounits"],
            text=True,
            timeout=10,
        )
    except Exception:
        return {}
    mem: dict[int, int] = {}
    for line in out.strip().splitlines():
        parts = [p.strip() for p in line.split(",")]
        if len(parts) >= 2:
            mem[int(parts[0])] = int(parts[1])
    return mem


def _physical_gpu_name(mem_before: dict[int, int], mem_after: dict[int, int]) -> str:
    best_idx = -1
    best_delta = 0
    for idx, after in mem_after.items():
        delta = after - mem_before.get(idx, 0)
        if delta > best_delta:
            best_delta = delta
            best_idx = idx
    if best_idx < 0 or best_delta < 32:
        return ""
    names = _nvidia_smi_names()
    return names.get(best_idx, f"nvidia:{best_idx}")


def _nvidia_smi_names() -> dict[int, str]:
    try:
        out = subprocess.check_output(
            ["nvidia-smi", "--query-gpu=index,name", "--format=csv,noheader"],
            text=True,
            timeout=10,
        )
    except Exception:
        return {}
    names: dict[int, str] = {}
    for line in out.strip().splitlines():
        parts = [p.strip() for p in line.split(",", 1)]
        if len(parts) >= 2:
            names[int(parts[0])] = parts[1]
    return names


def _torch_cuda_available() -> bool:
    try:
        import torch

        return bool(torch.cuda.is_available())
    except Exception:
        return False


_model_devices: dict[str, dict[str, Any]] = {}
_gpu_name_to_ct2: dict[str, int] | None = None
_gpu_chip_re = re.compile(r"rtx\s*(\d+)", re.I)


def _log_gpu_env() -> None:
    print(
        f"translation-overlay-engine: GPU env "
        f"TO_WHISPER_GPU={_env_first('TO_WHISPER_GPU', 'LP_WHISPER_GPU')!r} "
        f"TO_NLLB_GPU={_env_first('TO_NLLB_GPU', 'LP_NLLB_GPU')!r} "
        f"CUDA_DEVICE_ORDER={os.environ.get('CUDA_DEVICE_ORDER', '')!r}",
        flush=True,
    )
    for idx, name in sorted(_nvidia_smi_names().items()):
        print(f"translation-overlay-engine: nvidia-smi:{idx} {name}", flush=True)
    n = _ctranslate2_cuda_device_count()
    print(f"translation-overlay-engine: ctranslate2 reports {n} CUDA device(s)", flush=True)


def _gpu_env_name(model: str) -> str:
    names = {
        "whisper": ("TO_WHISPER_GPU", "LP_WHISPER_GPU"),
        "nllb": ("TO_NLLB_GPU", "LP_NLLB_GPU"),
    }.get(model, ())
    return _env_first(*names)


def _match_gpu_name(want: str, mapping: dict[str, int]) -> int | None:
    want = want.strip()
    if not want or not mapping:
        return None
    if want in mapping:
        return mapping[want]
    wl = want.lower()
    for name, idx in mapping.items():
        if wl == name.lower():
            return idx
    want_chip = _gpu_chip_re.search(wl)
    if want_chip:
        chip = want_chip.group(1)
        want_laptop = "laptop" in wl
        candidates: list[tuple[str, int]] = []
        for name, idx in mapping.items():
            nl = name.lower()
            if chip not in nl.replace(" ", ""):
                continue
            if want_laptop and "laptop" not in nl:
                continue
            if not want_laptop and "laptop" in nl and "laptop" not in wl:
                continue
            candidates.append((name, idx))
        if len(candidates) == 1:
            return candidates[0][1]
        if candidates:
            return candidates[0][1]
    for name, idx in mapping.items():
        nl = name.lower()
        if wl in nl or nl in wl:
            return idx
    return None


def _build_gpu_name_to_ct2() -> dict[str, int]:
    global _gpu_name_to_ct2
    if _gpu_name_to_ct2 is not None:
        return _gpu_name_to_ct2

    import ctranslate2

    mapping: dict[str, int] = {}
    n = _ctranslate2_cuda_device_count()
    nd = _nllb_dir()
    has_nllb = (nd / "model.bin").is_file()

    for ct2_idx in range(n):
        phys = ""
        if has_nllb:
            mem_before = _gpu_memory_mib()
            try:
                t = ctranslate2.Translator(
                    str(nd),
                    device="cuda",
                    device_index=ct2_idx,
                    compute_type="int8",
                    inter_threads=1,
                    intra_threads=1,
                )
                del t
            except Exception as e:
                print(
                    f"translation-overlay-engine: GPU probe cuda:{ct2_idx} NLLB load failed: {e}",
                    flush=True,
                )
                continue
            time.sleep(0.25)
            phys = _physical_gpu_name(mem_before, _gpu_memory_mib())
        else:
            nv = _nvidia_smi_names()
            phys = nv.get(ct2_idx, "")

        if phys:
            mapping[phys] = ct2_idx
            print(
                f"translation-overlay-engine: GPU map {phys!r} -> ctranslate2:{ct2_idx}",
                flush=True,
            )
        else:
            print(
                f"translation-overlay-engine: GPU probe cuda:{ct2_idx} could not detect physical GPU",
                flush=True,
            )

    if not mapping and n > 0:
        print(
            "translation-overlay-engine: GPU probe produced empty map; "
            "assuming ctranslate2 index matches nvidia-smi (CUDA_DEVICE_ORDER=PCI_BUS_ID)",
            flush=True,
        )
        for idx, name in _nvidia_smi_names().items():
            if idx < n:
                mapping[name] = idx

    _gpu_name_to_ct2 = mapping
    return mapping


def _cuda_index_for(model: str) -> int:
    model = (model or "").strip().lower()
    gpu_name = _gpu_env_name(model)
    if gpu_name and not gpu_name.isdigit():
        mapping = _build_gpu_name_to_ct2()
        hit = _match_gpu_name(gpu_name, mapping)
        if hit is not None:
            print(
                f"translation-overlay-engine: {model} wants {gpu_name!r} -> ctranslate2 cuda:{hit}",
                flush=True,
            )
            return hit
        print(
            f"translation-overlay-engine: {model} GPU {gpu_name!r} not in map {list(mapping.keys())!r}; "
            "using cuda:0",
            flush=True,
        )
        return 0
    env_keys = {
        "whisper": ("TO_WHISPER_CUDA_DEVICE", "LP_WHISPER_CUDA_DEVICE"),
        "nllb": ("TO_NLLB_CUDA_DEVICE", "LP_NLLB_CUDA_DEVICE"),
    }.get(model, ())
    raw = gpu_name or _env_first(*env_keys, "TO_CUDA_DEVICE", "LP_CUDA_DEVICE") or "0"
    try:
        idx = int(raw)
    except ValueError:
        idx = 0
    print(f"translation-overlay-engine: {model} legacy cuda index {idx}", flush=True)
    return idx


def _inference_device_for(model: str) -> tuple[str, int, str]:
    idx = _cuda_index_for(model)
    gpu_want = _gpu_env_name(model)
    if _truthy_env("TO_FORCE_CPU") or _truthy_env("LP_FORCE_CPU"):
        return "cpu", 0, "TO_FORCE_CPU=1"

    ct2_gpus = _ctranslate2_cuda_device_count()
    torch_cuda = _torch_cuda_available()

    if ct2_gpus > 0:
        dev_idx = min(max(0, idx), ct2_gpus - 1)
        if gpu_want and not gpu_want.isdigit():
            note = f"{model} {gpu_want} -> cuda:{dev_idx}"
        else:
            note = (
                f"{model} cuda:{dev_idx} (ctranslate2 sees {ct2_gpus} GPU(s); "
                f"torch.cuda.is_available()={torch_cuda})"
            )
        return "cuda", dev_idx, note

    if _truthy_env("TO_REQUIRE_CUDA") or _truthy_env("LP_REQUIRE_CUDA"):
        raise RuntimeError(
            "TO_REQUIRE_CUDA=1 but ctranslate2.get_cuda_device_count() is 0. "
            "Install a CUDA build of ctranslate2 + NVIDIA drivers + (recommended) CUDA PyTorch."
        )
    return (
        "cpu",
        0,
        f"{model} cpu: ctranslate2 reports no CUDA GPUs. torch.cuda.is_available()={torch_cuda}.",
    )


def _inference_device() -> tuple[str, int, str]:
    """Default device (legacy / health fallback)."""
    return _inference_device_for("nllb")


def _ctranslate2_compute_type(dev: str, dev_idx: int) -> str:
    import ctranslate2

    try:
        sup = set(ctranslate2.get_supported_compute_types(dev, dev_idx))
    except Exception:
        sup = set()
    if dev == "cuda":
        for p in ("int8_float16", "int8_bfloat16", "int8", "float16", "bfloat16", "float32"):
            if p in sup:
                return p
        return "float16"
    for p in ("int8", "float32"):
        if p in sup:
            return p
    return "int8"




def ensure_whisper() -> None:
    global _whisper
    if _whisper is not None:
        return

    def _load() -> None:
        global _whisper
        from faster_whisper import WhisperModel
        from weights_download import ensure_whisper_weights

        wd = _whisper_dir()
        ensure_whisper_weights(wd, variant=_WHISPER_VARIANT)
        if not (wd / "model.bin").is_file():
            raise FileNotFoundError(f"Missing whisper model.bin under {wd}")
        dev, dev_idx, dev_note = _inference_device_for("whisper")
        wcomp = "float16" if dev == "cuda" else "int8"
        mem_before = _gpu_memory_mib()
        print(f"translation-overlay-engine: loading whisper on {dev_note}", flush=True)
        _whisper = WhisperModel(str(wd), device=dev, device_index=dev_idx, compute_type=wcomp)
        physical = _physical_gpu_name(mem_before, _gpu_memory_mib())
        _model_devices["whisper"] = {
            "ctranslate2_index": dev_idx,
            "device": f"cuda:{dev_idx}" if dev == "cuda" else "cpu",
            "physical_gpu": physical,
            "detail": dev_note,
        }
        if physical:
            print(
                f"translation-overlay-engine: whisper cuda:{dev_idx} -> {physical}",
                flush=True,
            )

    _cold_load("whisper", _load)


def ensure_cutlet() -> None:
    global _katsu
    if _katsu is not None:
        return

    def _load() -> None:
        global _katsu
        import cutlet

        _katsu = cutlet.Cutlet()

    _cold_load("cutlet", _load)


def ensure_nllb_translator() -> None:
    global _translator
    if _translator is not None:
        return

    def _load() -> None:
        global _translator
        import ctranslate2
        from weights_download import ensure_nllb_weights

        nd = _nllb_dir()
        ensure_nllb_weights(nd, variant=_NLLB_VARIANT)
        if not (nd / "model.bin").is_file():
            raise FileNotFoundError(f"Missing NLLB model.bin under {nd}")
        dev, dev_idx, dev_note = _inference_device_for("nllb")
        tcomp = _ctranslate2_compute_type(dev, dev_idx)
        intra = 1 if dev == "cuda" else 4
        mem_before = _gpu_memory_mib()
        print(f"translation-overlay-engine: loading nllb on {dev_note}", flush=True)
        _translator = ctranslate2.Translator(
            str(nd), device=dev, device_index=dev_idx, compute_type=tcomp, inter_threads=1, intra_threads=intra
        )
        physical = _physical_gpu_name(mem_before, _gpu_memory_mib())
        _model_devices["nllb"] = {
            "ctranslate2_index": dev_idx,
            "device": f"cuda:{dev_idx}" if dev == "cuda" else "cpu",
            "physical_gpu": physical,
            "detail": dev_note,
        }
        if physical:
            print(
                f"translation-overlay-engine: nllb cuda:{dev_idx} -> {physical}",
                flush=True,
            )

    _cold_load("nllb", _load)


def _load_nllb_tokenizer(src_code: str, tgt_code: str) -> Any:
    """Must be NllbTokenizerFast: AutoTokenizer on a CT2 folder yields a generic
    fast tokenizer without NLLB's language special tokens, which silently
    produces garbage translations."""
    from transformers import NllbTokenizerFast

    errs: list[str] = []
    for source in _nllb_tokenizer_try_sources():
        try:
            t = NllbTokenizerFast.from_pretrained(source, src_lang=src_code, tgt_lang=tgt_code)
            t.src_lang = src_code
            _ = t.encode("x")
            return t
        except Exception as e:
            errs.append(f"{source!r}: {type(e).__name__}: {e}")
            continue
    raise RuntimeError("NLLB tokenizer: " + "; ".join(errs))


def ensure_models(*, whisper: bool = True, nllb: bool = True, cutlet: bool = True) -> None:
    """Whisper + cutlet + NLLB — each optional for selective warm-up."""
    with _load_lock:
        if whisper:
            ensure_whisper()
        if cutlet:
            ensure_cutlet()
        if nllb:
            try:
                ensure_nllb_translator()
            except FileNotFoundError:
                print(
                    "translation-overlay-engine: NLLB weights not found - translations will use cloud when configured",
                    flush=True,
                )




def _is_kanji_char(ch: str) -> bool:
    if not ch:
        return False
    o = ord(ch)
    return (
        0x4E00 <= o <= 0x9FFF
        or 0x3400 <= o <= 0x4DBF
        or o in (0x3005, 0x3006, 0x30FB)
    )


def furigana_tokens(japanese: str) -> list[dict]:
    """Returns word-level [{surface, reading}] tokens. `reading` is empty when
    the surface contains no kanji (so the renderer can skip the ruby <rt>
    entirely). For v1 we overlay the reading on the whole token; splitting
    okurigana off can come later without changing the wire format."""
    text = (japanese or "").strip()
    if not text:
        return []
    try:
        from cutlet.cutlet import normalize_text
        import jaconv
    except Exception:
        return [{"surface": text, "reading": ""}]
    if _katsu is None:
        return [{"surface": text, "reading": ""}]

    out: list[dict] = []
    n = normalize_text(text)
    for w in _katsu.tagger(n):
        surf = w.surface
        if not surf:
            continue
        if surf.isascii() or not any(_is_kanji_char(c) for c in surf):
            out.append({"surface": surf, "reading": ""})
            continue
        feat = w.feature
        kana = (getattr(feat, "kana", None) or "").strip()
        if not kana:
            out.append({"surface": surf, "reading": ""})
            continue
        out.append({"surface": surf, "reading": jaconv.kata2hira(kana)})
    if not out:
        return [{"surface": text, "reading": ""}]
    return out


def expected_kana_from_furigana(tokens: list[dict] | None) -> str:
    """Rebuild a kana-only expected line from pipeline furigana tokens for fair ASR scoring."""
    if not tokens:
        return ""
    try:
        import jaconv
    except Exception:
        return ""
    parts: list[str] = []
    for t in tokens:
        if not isinstance(t, dict):
            continue
        surf = (t.get("surface") or "").strip()
        read = (t.get("reading") or "").strip()
        if read:
            parts.append(read)
        elif surf:
            parts.append(jaconv.kata2hira(unicodedata.normalize("NFKC", surf)))
    return "".join(parts)




_PUNCT_SCORE_RE = re.compile(r"[、。「」『』【】〈〉《》〔〕（）()！!？\?\.,\s・:;\"']+")


def normalize_ja(s: str) -> str:
    """Strip whitespace + punctuation, NFKC, then katakana→hiragana so that
    transcripts that vary only in script (e.g. Whisper occasionally emits
    katakana) score the same as the canonical hiragana form."""
    if not s:
        return ""
    try:
        import jaconv
    except Exception:
        return s
    s = unicodedata.normalize("NFKC", s)
    s = jaconv.kata2hira(s)
    return _PUNCT_SCORE_RE.sub("", s)


def score_ja(expected: str, spoken: str) -> float:
    a = normalize_ja(expected)
    b = normalize_ja(spoken)
    if not a or not b:
        return 0.0
    return round(SequenceMatcher(None, a, b).ratio() * 100, 1)


def _normalize_ja_mapped(text: str) -> tuple[str, list[int]]:
    """Same string as normalize_ja(text); idx[j] is the index in NFKC(text) (= pre-strip t) for norm char j.

    Must mirror normalize_ja exactly (full-string punct strip), otherwise SequenceMatcher
    opcodes won't line up with the score and highlight ranges can be empty/wrong.
    """
    if not (text or "").strip():
        return "", []
    try:
        import jaconv
    except Exception:
        return "", []
    s0 = unicodedata.normalize("NFKC", (text or "").strip())
    t = jaconv.kata2hira(s0)
    out: list[str] = []
    idxs: list[int] = []
    i = 0
    n = len(t)
    while i < n:
        m = _PUNCT_SCORE_RE.match(t, i)
        if m:
            i = m.end()
            continue
        out.append(t[i])
        idxs.append(i)
        i += 1
    return "".join(out), idxs


def spoken_match_ranges_for_highlight(expected: str, spoken: str) -> tuple[str, list[list[int]]]:
    """Return (nfkc_spoken, [[start,end), ...]) marking spans in nfkc_spoken that align to expected (SequenceMatcher on normalized forms)."""
    s = unicodedata.normalize("NFKC", (spoken or "").strip())
    norm_exp, _ = _normalize_ja_mapped(expected)
    norm_sp, sp_idx = _normalize_ja_mapped(s)
    if not norm_exp or not norm_sp:
        return s, []
    matched_norm = [False] * len(norm_sp)
    sm = SequenceMatcher(None, norm_exp, norm_sp, autojunk=False)
    for tag, _i1, _i2, j1, j2 in sm.get_opcodes():
        if tag != "equal":
            continue
        for k in range(j1, j2):
            matched_norm[k] = True
    matched_s = [False] * len(s)
    for k, is_m in enumerate(matched_norm):
        if is_m:
            matched_s[sp_idx[k]] = True
    ranges: list[list[int]] = []
    i = 0
    n = len(matched_s)
    while i < n:
        if not matched_s[i]:
            i += 1
            continue
        j = i + 1
        while j < n and matched_s[j]:
            j += 1
        ranges.append([i, j])
        i = j
    # Fallback: if index projection produced no visible spans, still return
    # normalized spoken with normalized ranges so the UI can render highlights.
    if not ranges:
        norm_ranges: list[list[int]] = []
        i = 0
        n2 = len(matched_norm)
        while i < n2:
            if not matched_norm[i]:
                i += 1
                continue
            j = i + 1
            while j < n2 and matched_norm[j]:
                j += 1
            norm_ranges.append([i, j])
            i = j
        if norm_ranges:
            return norm_sp, norm_ranges
    return s, ranges



# CTranslate2 defaults max_decoding_length=256, which cuts off long speech translations
# while Whisper still returns the full English transcript.
_NLLB_INPUT_CHUNK = 900
_NLLB_DECODE_MIN = 256
_NLLB_DECODE_MAX = 1024


def _nllb_decode_budget(source_token_count: int) -> int:
    if source_token_count <= 0:
        return _NLLB_DECODE_MIN
    return min(_NLLB_DECODE_MAX, max(_NLLB_DECODE_MIN, int(source_token_count * 1.5) + 48))


def _translation_units(text: str) -> list[str]:
    """Split into sentence/clause units so translators don't drop lead-in text before ':' etc."""
    text = (text or "").strip()
    if not text:
        return []
    units: list[str] = []
    for para in re.split(r"\n\s*\n", text):
        para = para.strip()
        if not para:
            continue
        for sent in re.split(r"(?<=[.!?…])\s+", para):
            sent = sent.strip()
            if not sent:
                continue
            for sub in re.split(r"(?<=[;:])\s+(?=\S)", sent):
                sub = sub.strip()
                if sub:
                    units.append(sub)
    return units or [text]


def _nllb_split_text(text: str, tok: Any, max_tokens: int = _NLLB_INPUT_CHUNK) -> list[str]:
    text = (text or "").strip()
    if not text:
        return []

    def flush(buf: str, out: list[str]) -> str:
        b = buf.strip()
        if b:
            out.append(b)
        return ""

    def pack_units(units: list[str], out: list[str]) -> None:
        buf = ""
        for unit in units:
            u = unit.strip()
            if not u:
                continue
            candidate = f"{buf} {u}".strip() if buf else u
            if len(tok.encode(candidate)) <= max_tokens:
                buf = candidate
                continue
            buf = flush(buf, out)
            if len(tok.encode(u)) <= max_tokens:
                buf = u
                continue
            step = max(8, max_tokens // 2)
            words = u.split()
            chunk_words: list[str] = []
            for w in words:
                chunk_words.append(w)
                if len(tok.encode(" ".join(chunk_words))) >= step:
                    out.append(" ".join(chunk_words))
                    chunk_words = []
            if chunk_words:
                out.append(" ".join(chunk_words))
        flush(buf, out)

    units = _translation_units(text)
    out: list[str] = []
    pack_units(units, out)
    return out or units or [text]


def _nllb_join_parts(parts: list[str], tgt_code: str) -> str:
    cleaned = [p.strip() for p in parts if (p or "").strip()]
    if not cleaned:
        return ""
    if tgt_code == "eng_Latn":
        return " ".join(cleaned)
    return "".join(cleaned)


def nllb_translate(text: str, src_code: str, tgt_code: str) -> str:
    assert _translator is not None
    text = text.strip()
    if not text:
        return ""
    key = (src_code, tgt_code)
    tok = _tok_cache.get(key)
    if tok is None:
        tok = _load_nllb_tokenizer(src_code, tgt_code)
        _tok_cache[key] = tok
    tok.src_lang = src_code

    chunks = _nllb_split_text(text, tok)
    sources = [tok.convert_ids_to_tokens(tok.encode(c)) for c in chunks]
    budgets = [_nllb_decode_budget(len(s)) for s in sources]
    max_dec = max(budgets) if budgets else _NLLB_DECODE_MIN
    target_prefix = [[tgt_code] for _ in sources]
    res = _translator.translate_batch(
        sources,
        target_prefix=target_prefix,
        max_decoding_length=max_dec,
        max_input_length=1024,
    )
    parts: list[str] = []
    for r in res:
        target = r.hypotheses[0][1:]
        ids = tok.convert_tokens_to_ids(target)
        parts.append(tok.decode(ids, skip_special_tokens=True).strip())
    return _nllb_join_parts(parts, tgt_code).strip()


def _detect_ui_source_lang(text: str, hint: str) -> str:
    h = (hint or "auto").strip().lower()
    if h in ("ja", "jp", "japanese"):
        return "ja"
    if h in ("zh", "cn", "chinese"):
        return "zh"
    if h in ("en", "english"):
        return "en"
    jp = zh = 0
    for ch in text:
        o = ord(ch)
        if 0x3040 <= o <= 0x309F or 0x30A0 <= o <= 0x30FF:
            jp += 1
        elif 0x4E00 <= o <= 0x9FFF:
            zh += 1
    if jp > zh and jp > 0:
        return "ja"
    if zh > jp and zh > 0:
        return "zh"
    if zh > 0 or jp > 0:
        return "ja" if jp >= zh else "zh"
    return "ja"


def _nllb_src_for_lang(lang: str) -> str:
    return {
        "ja": "jpn_Jpan",
        "zh": "zho_Hans",
        "ko": "kor_Hang",
        "en": "eng_Latn",
    }.get(lang, "jpn_Jpan")


def _nllb_code_to_lang2(code: str) -> str:
    """Inverse of _nllb_src_for_lang: flores code → the 2-letter id that
    _detect_ui_source_lang emits, so a source already in the target language can
    be passed through. "" for codes we don't source-detect."""
    return {
        "jpn_Jpan": "ja",
        "zho_Hans": "zh",
        "kor_Hang": "ko",
        "eng_Latn": "en",
    }.get((code or "").strip(), "")


def _romanize_ui_source(text: str, lang: str) -> str:
    text = (text or "").strip()
    if not text or lang != "ja":
        return ""
    try:
        ensure_cutlet()
        if _katsu is not None:
            return (_katsu.romaji(text) or "").strip()
    except Exception:
        pass
    return ""




def _default_openai_base_url() -> str:
    return os.environ.get("OPENAI_BASE_URL", "https://api.openai.com/v1").strip().rstrip("/")


def _resolve_openai_credentials(openai_api_key: str, openai_base_url: str) -> tuple[str, str]:
    key = (openai_api_key or "").strip() or os.environ.get("OPENAI_API_KEY", "").strip()
    base_raw = (openai_base_url or "").strip() or _default_openai_base_url()
    return key, base_raw.rstrip("/")


OPENROUTER_TTS_PCM_SAMPLE_RATE = 24000
# WASAPI shared mode on Windows (incl. VB-Audio virtual devices) is most reliable at 48 kHz stereo.
PLAYBACK_SAMPLE_RATE = 48000
# Only one WASAPI play at a time (overlapping plays on VB-Cable → AUDCLNT_E_DEVICE_IN_USE).
_playback_lock = threading.Lock()

# COINIT_MULTITHREADED — soundcard/WASAPI from asyncio.to_thread needs COM on that thread.
_COINIT_MULTITHREADED = 0
_RPC_E_CHANGED_MODE = -2147417850


def _com_init_on_playback_thread() -> int | None:
    """Initialize COM on the current thread. Returns HRESULT, -1 if already in another mode, None on non-Windows."""
    if os.name != "nt":
        return None
    import ctypes

    hr = ctypes.windll.ole32.CoInitializeEx(None, _COINIT_MULTITHREADED)
    if hr >= 0:
        return hr
    if hr == _RPC_E_CHANGED_MODE:
        return -1
    raise RuntimeError(f"CoInitializeEx failed: {hex(hr & 0xFFFFFFFF)}")


def _com_uninit_on_playback_thread(hr: int | None) -> None:
    if os.name == "nt" and hr == 0:
        import ctypes

        ctypes.windll.ole32.CoUninitialize()


def _audio_speech_request(
    *,
    api_key: str,
    base_url: str,
    payload: dict[str, Any],
    headers: dict[str, str],
    timeout: int = 120,
    label: str = "TTS",
) -> bytes:
    url = base_url.rstrip("/") + "/audio/speech"
    body = json.dumps(payload).encode("utf-8")
    hdrs = {"Content-Type": "application/json", **headers}
    req = urllib.request.Request(url, data=body, headers=hdrs, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.read()
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise HTTPException(status_code=502, detail=f"{label} HTTP {e.code}: {err_body[:4000]}") from e
    except Exception as e:
        raise HTTPException(status_code=502, detail=f"{label} failed: {type(e).__name__}: {e}") from e


def _pcm16le_to_float32(pcm: bytes, sample_rate: int = OPENROUTER_TTS_PCM_SAMPLE_RATE) -> tuple[Any, int]:
    import numpy as np

    if len(pcm) < 2:
        raise ValueError("empty PCM audio")
    if len(pcm) % 2:
        pcm = pcm[: len(pcm) - 1]
    samples = np.frombuffer(pcm, dtype=np.int16).astype(np.float32) / 32768.0
    return samples, sample_rate


def _pcm16_to_wav_bytes(pcm: bytes, sample_rate: int = OPENROUTER_TTS_PCM_SAMPLE_RATE) -> bytes:
    if len(pcm) % 2:
        pcm = pcm[: len(pcm) - 1]
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(sample_rate)
        w.writeframes(pcm)
    return buf.getvalue()


def _guess_audio_format(raw: bytes) -> str:
    if len(raw) >= 4 and raw[:4] == b"RIFF":
        return "wav"
    if len(raw) >= 3 and raw[:3] == b"ID3":
        return "mp3"
    if len(raw) >= 2 and raw[0] == 0xFF and (raw[1] & 0xE0) == 0xE0:
        return "mp3"
    return "pcm"


def _decode_mp3_to_float32(mp3: bytes) -> tuple[Any, int]:
    import numpy as np

    try:
        from pydub import AudioSegment

        seg = AudioSegment.from_file(io.BytesIO(mp3), format="mp3")
        samples = np.array(seg.get_array_of_samples(), dtype=np.float32)
        if seg.sample_width > 0:
            samples = samples / float(1 << (8 * seg.sample_width - 1))
        if seg.channels > 1:
            samples = samples.reshape(-1, seg.channels).mean(axis=1)
        return samples, int(seg.frame_rate)
    except ImportError:
        pass
    import shutil
    import subprocess

    ffmpeg = shutil.which("ffmpeg")
    if not ffmpeg:
        raise ValueError(
            "MP3 TTS needs ffmpeg on PATH (or pip install pydub). "
            "Install ffmpeg or switch TTS provider to OpenAI direct (WAV)."
        )
    proc = subprocess.run(
        [ffmpeg, "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-f", "wav", "pipe:1"],
        input=mp3,
        capture_output=True,
        timeout=120,
    )
    if proc.returncode != 0:
        err = (proc.stderr or b"").decode("utf-8", errors="replace").strip()
        raise ValueError(f"ffmpeg mp3 decode failed: {err[:500]}")
    return read_wav_native(proc.stdout)


def decode_speech_audio(raw: bytes, *, response_format: str) -> tuple[Any, int]:
    """Turn /audio/speech bytes into float32 samples + sample rate for soundcard."""
    fmt = (response_format or "").strip().lower() or _guess_audio_format(raw)
    if fmt == "wav" or (len(raw) >= 4 and raw[:4] == b"RIFF"):
        return read_wav_native(raw)
    if fmt == "mp3":
        return _decode_mp3_to_float32(raw)
    if fmt == "pcm":
        try:
            return read_wav_native(_pcm16_to_wav_bytes(raw))
        except Exception:
            return _pcm16le_to_float32(raw)
    try:
        return read_wav_native(raw)
    except Exception:
        return _pcm16le_to_float32(raw)


def openrouter_speech(
    *,
    text: str,
    api_key: str,
    base_url: str,
    model: str,
    voice: str,
    instructions: str = "",
    timeout: int = 120,
) -> tuple[bytes, str]:
    """OpenRouter POST /audio/speech -> (audio bytes, response_format)."""
    text = (text or "").strip()
    if not text:
        raise HTTPException(status_code=400, detail="empty text")
    if len(text) > 4096:
        text = text[:4096]
    model = (model or "").strip() or "openai/gpt-4o-mini-tts-2025-12-15"
    voice = (voice or "").strip() or "coral"
    payload: dict[str, Any] = {
        "model": model,
        "input": text,
        "voice": voice,
        # PCM avoids MP3 decode (no ffmpeg/pydub required on Windows).
        "response_format": "pcm",
    }
    instr = (instructions or "").strip()
    if instr:
        payload["provider"] = {"options": {"openai": {"instructions": instr}}}
    raw = _audio_speech_request(
        api_key=api_key,
        base_url=base_url,
        payload=payload,
        headers=_openrouter_request_headers(api_key),
        timeout=timeout,
        label="OpenRouter TTS",
    )
    return raw, "pcm"


def openai_speech_wav(
    *,
    text: str,
    api_key: str,
    base_url: str,
    model: str,
    voice: str,
    instructions: str = "",
    timeout: int = 120,
) -> tuple[bytes, str]:
    """OpenAI POST /audio/speech -> (WAV bytes, response_format)."""
    text = (text or "").strip()
    if not text:
        raise HTTPException(status_code=400, detail="empty text")
    if len(text) > 4096:
        text = text[:4096]
    model = (model or "").strip() or "gpt-4o-mini-tts-2025-12-15"
    voice = (voice or "").strip() or "coral"
    payload: dict[str, Any] = {
        "model": model,
        "input": text,
        "voice": voice,
        "response_format": "wav",
    }
    instr = (instructions or "").strip()
    if instr:
        payload["instructions"] = instr
    raw = _audio_speech_request(
        api_key=api_key,
        base_url=base_url,
        payload=payload,
        headers={"Authorization": f"Bearer {api_key}"},
        timeout=timeout,
        label="OpenAI TTS",
    )
    return raw, "wav"


_TTS_RAW_CACHE: OrderedDict[str, tuple[bytes, str, str]] = OrderedDict()
_TTS_CACHE_MAX_ENTRIES = 96
_tts_cache_lock = threading.Lock()


def _tts_cache_key(
    *,
    tts_engine: str,
    text: str,
    model: str,
    voice: str,
    instructions: str,
) -> str:
    payload = json.dumps(
        {
            "engine": (tts_engine or "openrouter").strip().lower(),
            "text": (text or "").strip(),
            "model": (model or "").strip(),
            "voice": (voice or "").strip(),
            "instructions": (instructions or "").strip(),
        },
        sort_keys=True,
        ensure_ascii=False,
    )
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def synthesize_speech_audio(
    *,
    tts_engine: str,
    text: str,
    model: str,
    voice: str,
    instructions: str,
    openai_api_key: str,
    openai_base_url: str,
    openrouter_api_key: str,
    openrouter_base_url: str,
) -> tuple[Any, int, str, bool]:
    """Returns (samples, sample_rate, backend_label, cache_hit)."""
    text = (text or "").strip()
    eng = (tts_engine or "openrouter").strip().lower()
    cache_key = _tts_cache_key(
        tts_engine=eng,
        text=text,
        model=model,
        voice=voice,
        instructions=instructions,
    )
    with _tts_cache_lock:
        hit = _TTS_RAW_CACHE.get(cache_key)
        if hit is not None:
            _TTS_RAW_CACHE.move_to_end(cache_key)
            raw, fmt, backend = hit
            samples, sr = decode_speech_audio(raw, response_format=fmt)
            return samples, sr, backend, True

    if eng == "openai":
        key, base = _resolve_openai_credentials(openai_api_key, openai_base_url)
        if not key:
            raise HTTPException(status_code=400, detail="OpenAI TTS requires an API key.")
        raw, fmt = openai_speech_wav(
            text=text,
            api_key=key,
            base_url=base,
            model=model,
            voice=voice,
            instructions=instructions,
        )
        backend = "openai"
    else:
        key, base = _resolve_openrouter_credentials(openrouter_api_key, openrouter_base_url)
        if not key:
            raise HTTPException(
                status_code=400,
                detail="OpenRouter TTS requires an API key (Settings → OpenRouter key).",
            )
        raw, fmt = openrouter_speech(
            text=text,
            api_key=key,
            base_url=base,
            model=model,
            voice=voice,
            instructions=instructions,
        )
        backend = "openrouter"

    with _tts_cache_lock:
        _TTS_RAW_CACHE[cache_key] = (raw, fmt, backend)
        while len(_TTS_RAW_CACHE) > _TTS_CACHE_MAX_ENTRIES:
            _TTS_RAW_CACHE.popitem(last=False)

    samples, sr = decode_speech_audio(raw, response_format=fmt)
    return samples, sr, backend, False


def openai_chat_completion(
    *,
    api_key: str,
    base_url: str,
    model: str,
    system: str | None,
    user_text: str,
    temperature: float = 0.2,
    timeout: int = 120,
) -> str:
    url = base_url.rstrip("/") + "/chat/completions"
    messages: list[dict[str, str]] = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": user_text})
    body = json.dumps(
        {
            "model": model,
            "messages": messages,
            "temperature": temperature,
            "max_tokens": max(512, min(4096, len(user_text) * 3 + 128)),
        }
    ).encode()
    req = urllib.request.Request(
        url,
        data=body,
        headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            data = json.load(r)
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise HTTPException(status_code=502, detail=f"OpenAI HTTP {e.code}: {err_body[:4000]}") from e
    except Exception as e:
        raise HTTPException(status_code=502, detail=f"OpenAI request failed: {type(e).__name__}: {e}") from e
    try:
        return (data["choices"][0]["message"]["content"] or "").strip()
    except (KeyError, IndexError, TypeError) as e:
        raise HTTPException(status_code=502, detail="OpenAI unexpected response shape") from e


def _openai_translate_units(text: str, translate_one, tgt_code: str) -> str:
    units = _translation_units(text)
    if len(units) <= 1:
        return translate_one(text)
    parts = [translate_one(u) for u in units if (u or "").strip()]
    return _nllb_join_parts(parts, tgt_code)


def openai_en_to_jp(text: str, api_key: str, base_url: str, model: str) -> str:
    t = (text or "").strip()
    if not t:
        return ""

    def one(chunk: str) -> str:
        return openai_chat_completion(
            api_key=api_key,
            base_url=base_url,
            model=model,
            system=(
                "You translate English into natural Japanese for spoken dialogue. "
                "Translate the entire input completely; do not summarize, omit, or shorten any part. "
                "Output only the Japanese translation, no romaji, no explanations, no quotes."
            ),
            user_text=chunk,
            temperature=0.2,
        )

    return _openai_translate_units(t, one, "jpn_Jpan")


def openai_en_to_ko(text: str, api_key: str, base_url: str, model: str) -> str:
    t = (text or "").strip()
    if not t:
        return ""

    def one(chunk: str) -> str:
        return openai_chat_completion(
            api_key=api_key,
            base_url=base_url,
            model=model,
            system=(
                "You translate English into natural Korean for spoken dialogue. "
                "Translate the entire input completely; do not summarize, omit, or shorten any part. "
                "Output only the Korean translation, no romanization, no explanations, no quotes."
            ),
            user_text=chunk,
            temperature=0.2,
        )

    return _openai_translate_units(t, one, "kor_Hang")


def openai_en_to_zh(text: str, api_key: str, base_url: str, model: str) -> str:
    t = (text or "").strip()
    if not t:
        return ""

    def one(chunk: str) -> str:
        return openai_chat_completion(
            api_key=api_key,
            base_url=base_url,
            model=model,
            system=(
                "You translate English into natural Simplified Chinese for spoken dialogue. "
                "Translate the entire input completely; do not summarize, omit, or shorten any part. "
                "Output only the Chinese translation, no pinyin, no explanations, no quotes."
            ),
            user_text=chunk,
            temperature=0.2,
        )

    return _openai_translate_units(t, one, "zho_Hans")


def openai_target_to_en(text: str, api_key: str, base_url: str, model: str, *, lang_label: str = "Japanese") -> str:
    t = (text or "").strip()
    if not t:
        return ""
    return openai_chat_completion(
        api_key=api_key,
        base_url=base_url,
        model=model,
        system=None,
        user_text=f"Translate this {lang_label} to natural English only, no commentary:\n\n{t}",
        temperature=0.2,
    )


def openai_ja_to_en(text: str, api_key: str, base_url: str, model: str) -> str:
    return openai_target_to_en(text, api_key, base_url, model, lang_label="Japanese")


def openai_zh_to_en(text: str, api_key: str, base_url: str, model: str) -> str:
    return openai_target_to_en(text, api_key, base_url, model, lang_label="Chinese")


def openai_ko_to_en(text: str, api_key: str, base_url: str, model: str) -> str:
    return openai_target_to_en(text, api_key, base_url, model, lang_label="Korean")


def _multipart_form_data_file(
    fields: list[tuple[str, str]],
    file_field: str,
    filename: str,
    file_bytes: bytes,
    file_content_type: str,
) -> tuple[bytes, str]:
    token = "lp" + os.urandom(12).hex()
    boundary = f"----WebKitFormBoundary{token}"
    crlf = b"\r\n"
    parts: list[bytes] = []
    for name, value in fields:
        parts.append(f"--{boundary}".encode() + crlf)
        parts.append(f'Content-Disposition: form-data; name="{name}"'.encode() + crlf + crlf)
        parts.append(value.encode("utf-8") + crlf)
    parts.append(f"--{boundary}".encode() + crlf)
    disp = (
        f'Content-Disposition: form-data; name="{file_field}"; filename="{filename}"'.encode()
        + crlf
        + f"Content-Type: {file_content_type}".encode()
        + crlf
        + crlf
    )
    parts.append(disp)
    parts.append(file_bytes + crlf)
    parts.append(f"--{boundary}--".encode() + crlf)
    return b"".join(parts), boundary


def resolve_asr(
    asr_engine: str,
    *,
    openai_transcribe_model: str,
    openai_whisper_model: str,
    openrouter_transcribe_model: str,
    transcribe_model: str = "",
) -> tuple[str, str | None]:
    """Map UI ASR engine to cloud/local backend and model id."""
    eng = (asr_engine or "whisper").strip().lower()
    tm = (transcribe_model or "").strip()
    if eng == "openrouter":
        m = tm or (openrouter_transcribe_model or "").strip() or os.environ.get(
            "OPENROUTER_TRANSCRIBE_MODEL", "qwen/qwen3-asr-flash-2026-02-10"
        )
        return "openrouter", m
    if eng == "openai_whisper":
        wm = tm or (openai_whisper_model or "").strip() or os.environ.get("OPENAI_WHISPER_MODEL", "whisper-1")
        return "openai", wm
    if eng == "openai":
        tm2 = tm or (openai_transcribe_model or "").strip() or os.environ.get(
            "OPENAI_TRANSCRIBE_MODEL", "gpt-4o-mini-transcribe"
        )
        return "openai", tm2
    return "local", None


def resolve_openai_asr(
    asr_engine: str,
    *,
    openai_transcribe_model: str,
    openai_whisper_model: str,
    openrouter_transcribe_model: str = "",
) -> tuple[str, str | None]:
    backend, model = resolve_asr(
        asr_engine,
        openai_transcribe_model=openai_transcribe_model,
        openai_whisper_model=openai_whisper_model,
        openrouter_transcribe_model=openrouter_transcribe_model,
    )
    if backend == "local":
        return "whisper", None
    return backend, model


OPENROUTER_DEFAULT_BASE = "https://openrouter.ai/api/v1"


def _resolve_openrouter_credentials(openrouter_api_key: str, openrouter_base_url: str) -> tuple[str, str]:
    key = (openrouter_api_key or "").strip() or os.environ.get("OPENROUTER_API_KEY", "").strip()
    base = (openrouter_base_url or "").strip() or os.environ.get("OPENROUTER_BASE_URL", OPENROUTER_DEFAULT_BASE)
    return key, base.rstrip("/")


def _openrouter_request_headers(api_key: str) -> dict[str, str]:
    return {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
        "HTTP-Referer": "https://github.com/translation-overlay",
        "X-Title": "Penguin Translate",
    }


def openrouter_transcribe_wav(
    wav_bytes: bytes,
    *,
    api_key: str,
    base_url: str,
    model: str,
    language: str | None,
    timeout: float = 180.0,
) -> tuple[str, str | None]:
    """OpenRouter /audio/transcriptions — JSON body with base64 audio."""
    if len(wav_bytes) < 800:
        return "", None
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
    req = urllib.request.Request(
        url,
        data=body,
        headers=_openrouter_request_headers(api_key),
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            data = json.load(r)
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise HTTPException(status_code=502, detail=f"OpenRouter STT HTTP {e.code}: {err_body[:4000]}") from e
    except Exception as e:
        raise HTTPException(status_code=502, detail=f"OpenRouter STT failed: {type(e).__name__}: {e}") from e
    if not isinstance(data, dict):
        raise HTTPException(status_code=502, detail="OpenRouter STT unexpected response")
    text = (data.get("text") or "").strip()
    det = data.get("language")
    if isinstance(det, str) and det.strip():
        return text, det.strip()
    return text, None


def cloud_transcribe_wav(
    raw: bytes,
    *,
    backend: str,
    model: str | None,
    language: str | None,
    openai_api_key: str,
    openai_base_url: str,
    openrouter_api_key: str,
    openrouter_base_url: str,
) -> tuple[str, str | None, str]:
    """Returns (text, detected_language, timer_step)."""
    if backend == "openrouter":
        key, base = _resolve_openrouter_credentials(openrouter_api_key, openrouter_base_url)
        if not key:
            raise HTTPException(
                status_code=400,
                detail="OpenRouter ASR requires an API key (Settings → OpenRouter key).",
            )
        text, det = openrouter_transcribe_wav(
            raw,
            api_key=key,
            base_url=base,
            model=model or "openai/whisper-large-v3-turbo",
            language=language,
        )
        return text, det, "asr_openrouter"
    if backend == "openai":
        key, base = _resolve_openai_credentials(openai_api_key, openai_base_url)
        if not key:
            raise HTTPException(
                status_code=400,
                detail="OpenAI ASR requires an API key (Settings → OpenAI key).",
            )
        text, det = openai_transcribe_wav(
            raw,
            api_key=key,
            base_url=base,
            model=model or "whisper-1",
            language=language,
        )
        step = "asr_openai_whisper" if (model or "").startswith("whisper") else "asr_openai_gpt"
        return text, det, step
    raise HTTPException(status_code=400, detail=f"unsupported cloud ASR backend: {backend}")


def openai_transcribe_wav(
    wav_bytes: bytes,
    *,
    api_key: str,
    base_url: str,
    model: str,
    language: str | None,
    timeout: float = 120.0,
) -> tuple[str, str | None]:
    if len(wav_bytes) < 800:
        return "", None
    m = model.strip()
    fields: list[tuple[str, str]] = [("model", m)]
    if m.startswith("gpt-4o") or m.startswith("gpt-4-turbo") or m.startswith("whisper"):
        fields.append(("response_format", "json"))
    lang = (language or "").strip().lower()
    if len(lang) >= 2:
        fields.append(("language", lang[:16]))
    body, boundary = _multipart_form_data_file(
        fields,
        file_field="file",
        filename="clip.wav",
        file_bytes=wav_bytes,
        file_content_type="audio/wav",
    )
    url = base_url.rstrip("/") + "/audio/transcriptions"
    req = urllib.request.Request(
        url,
        data=body,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": f"multipart/form-data; boundary={boundary}",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw_body = r.read()
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="replace")
        raise HTTPException(status_code=502, detail=f"OpenAI transcription HTTP {e.code}: {err_body[:4000]}") from e
    except Exception as e:
        raise HTTPException(status_code=502, detail=f"OpenAI transcription failed: {type(e).__name__}: {e}") from e
    try:
        data = json.loads(raw_body.decode("utf-8"))
    except json.JSONDecodeError as e:
        sample = raw_body[:800].decode("utf-8", errors="replace")
        raise HTTPException(
            status_code=502,
            detail=f"OpenAI transcription returned non-JSON: {sample[:400]}",
        ) from e
    text = (data.get("text") or "").strip() if isinstance(data, dict) else ""
    det = None
    if isinstance(data, dict):
        d = data.get("language")
        if isinstance(d, str) and d.strip():
            det = d.strip()
    return text, det




def read_wav_mono_f32(raw: bytes) -> Any:
    import numpy as np

    with wave.open(io.BytesIO(raw), "rb") as w:
        ch, sw, sr = w.getnchannels(), w.getsampwidth(), w.getframerate()
        n = w.getnframes()
        frames = w.readframes(n)
    if sw != 2:
        raise ValueError("WAV must be 16-bit PCM")
    x = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
    if ch == 2:
        x = x.reshape(-1, 2).mean(axis=1)
    if sr != 16000:
        new_len = int(len(x) * 16000 / sr)
        xi = np.linspace(0, len(x) - 1, new_len).astype(np.float32)
        x = np.interp(xi, np.arange(len(x), dtype=np.float32), x).astype(np.float32)
    return x


def read_wav_native(raw: bytes) -> tuple[Any, int]:
    """Decode WAV at its original sample rate (no resample) for direct
    playback through the user's chosen output device."""
    import numpy as np

    with wave.open(io.BytesIO(raw), "rb") as w:
        ch, sw, sr = w.getnchannels(), w.getsampwidth(), w.getframerate()
        n = w.getnframes()
        frames = w.readframes(n)
    if sw != 2:
        raise ValueError("WAV must be 16-bit PCM")
    x = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
    if ch == 2:
        x = x.reshape(-1, 2)
    return x, sr




def _list_output_speakers() -> dict:
    """Return the same shape as Web's enumerateDevices: a list of
    {id, name, is_default} for each WASAPI output. We surface the soundcard id
    (its WASAPI moniker) as the stable lookup key; the human-friendly name is
    what the UI shows."""
    try:
        import soundcard as sc
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"soundcard unavailable: {type(e).__name__}: {e}") from e
    try:
        speakers = sc.all_speakers()
        default = sc.default_speaker()
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"soundcard list failed: {type(e).__name__}: {e}") from e
    default_id = getattr(default, "id", None)
    out = []
    for sp in speakers:
        sid = getattr(sp, "id", None) or getattr(sp, "name", "")
        sname = getattr(sp, "name", str(sp))
        out.append({"id": str(sid), "name": str(sname), "is_default": (default_id is not None and sid == default_id)})
    return {"devices": out}


def _resolve_speaker(device_id: str, device_name: str) -> Any:
    """Pick a soundcard speaker by id, then by exact name, then by case-fold
    name, falling back to the system default."""
    import soundcard as sc

    did = (device_id or "").strip()
    dname = (device_name or "").strip()
    if did:
        try:
            return sc.get_speaker(did)
        except Exception:
            pass
    if dname:
        try:
            speakers = sc.all_speakers()
        except Exception:
            speakers = []
        for sp in speakers:
            if str(getattr(sp, "name", "")) == dname:
                return sp
        for sp in speakers:
            if str(getattr(sp, "name", "")).lower() == dname.lower():
                return sp
        dlow = dname.lower()
        for sp in speakers:
            n = str(getattr(sp, "name", ""))
            if dlow in n.lower() or n.lower() in dlow:
                return sp
    return sc.default_speaker()


def _prepare_playback_samples(samples: Any, samplerate: int) -> tuple[Any, int]:
    """Resample to 48 kHz stereo float32 for WASAPI / virtual cable compatibility."""
    import numpy as np

    s = np.asarray(samples, dtype=np.float32)
    if s.size == 0:
        raise ValueError("empty audio buffer")
    if s.ndim == 1:
        s = np.column_stack([s, s])
    elif s.ndim == 2:
        if s.shape[1] == 1:
            s = np.repeat(s, 2, axis=1)
        elif s.shape[1] > 2:
            s = s[:, :2]
    else:
        raise ValueError("unexpected audio shape")
    if samplerate != PLAYBACK_SAMPLE_RATE:
        n_out = max(1, int(s.shape[0] * PLAYBACK_SAMPLE_RATE / samplerate))
        xi = np.linspace(0, s.shape[0] - 1, n_out)
        out = np.zeros((n_out, s.shape[1]), dtype=np.float32)
        for ch in range(s.shape[1]):
            out[:, ch] = np.interp(xi, np.arange(s.shape[0]), s[:, ch])
        s = out
        samplerate = PLAYBACK_SAMPLE_RATE
    np.clip(s, -1.0, 1.0, out=s)
    return s, samplerate


def _play_prepared_blocking(samples: Any, samplerate: int, speaker: Any) -> None:
    """Play already-prepared float32 (frames, channels). Raises on failure."""
    import soundcard as sc

    with _playback_lock:
        try:
            speaker.play(samples, samplerate=samplerate)
        except RuntimeError as e:
            err = str(e).lower()
            # AUDCLNT_E_DEVICE_IN_USE — try system default speaker once.
            if "8889000a" in err:
                default = sc.default_speaker()
                if getattr(default, "id", None) != getattr(speaker, "id", None):
                    try:
                        default.play(samples, samplerate=samplerate)
                        return
                    except RuntimeError:
                        pass
            raise RuntimeError(
                f"{e} — device may be busy (close apps using exclusive audio on this output)"
            ) from e


def _play_on_device_blocking(
    samples: Any, samplerate: int, device_id: str, device_name: str
) -> str:
    """Resolve output + play on the current thread (COM must be initialized here)."""
    com_hr = _com_init_on_playback_thread()
    try:
        speaker = _resolve_speaker(device_id, device_name)
        _play_prepared_blocking(samples, samplerate, speaker)
        return str(getattr(speaker, "name", "speaker"))
    finally:
        _com_uninit_on_playback_thread(com_hr)


def _play_wav_blocking(samples: Any, samplerate: int, device_id: str, device_name: str) -> None:
    """Blocking playback after resampling to 48 kHz stereo."""
    s, sr = _prepare_playback_samples(samples, samplerate)
    _play_on_device_blocking(s, sr, device_id, device_name)




class TranslateItemIn(BaseModel):
    id: int = 0
    text: str = ""


class TranslateBody(BaseModel):
    items: list[TranslateItemIn] = Field(default_factory=list)
    source_lang: str = "auto"
    # NLLB flores target code (e.g. "zho_Hans"). English keeps the legacy
    # behavior so older callers that omit it are unaffected.
    target_lang: str = "eng_Latn"


@app.post("/translate")
async def translate_ui(body: TranslateBody) -> dict:
    """Batch UI text from its detected language into target_lang (default English)."""
    timer = _StepTimer("POST /translate")
    try:
        ensure_nllb_translator()
    except FileNotFoundError as e:
        raise HTTPException(
            503,
            detail=f"NLLB model not loaded. Start Penguin Translate or POST /load first. {e}",
        ) from e

    tgt = (body.target_lang or "eng_Latn").strip() or "eng_Latn"
    tgt2 = _nllb_code_to_lang2(tgt)
    out: list[dict[str, Any]] = []
    for item in body.items:
        text = (item.text or "").strip()
        if not text:
            out.append({"id": item.id, "en": "", "roman": ""})
            continue
        lang = _detect_ui_source_lang(text, body.source_lang)
        if lang == tgt2:
            out.append({"id": item.id, "en": text, "roman": ""})
            continue
        src = _nllb_src_for_lang(lang)
        en = nllb_translate(text, src, tgt)
        # The romanization reading aid only helps an English reader.
        roman = _romanize_ui_source(text, lang) if tgt == "eng_Latn" else ""
        out.append({"id": item.id, "en": en, "roman": roman})
    timer.log(lines=len(body.items))
    return {"items": out}


@app.get("/devices/cuda")
def devices_cuda() -> dict:
    """List NVIDIA GPUs by name (same order as nvidia-smi)."""
    try:
        out = subprocess.check_output(
            ["nvidia-smi", "--query-gpu=index,name,memory.total", "--format=csv,noheader,nounits"],
            text=True,
            timeout=10,
        )
    except Exception:
        return {"devices": [], "index_kind": "nvidia_name"}
    rows: list[dict[str, Any]] = []
    for line in out.strip().splitlines():
        parts = [p.strip() for p in line.split(",")]
        if len(parts) < 2:
            continue
        name = parts[1]
        mem = int(parts[2]) if len(parts) >= 3 and parts[2].isdigit() else 0
        short = name
        for token in ("RTX", "GTX"):
            if token in name.upper():
                idx = name.upper().find(token)
                short = name[idx:].strip()
                break
        label = f"{short} ({mem} MiB)" if mem else short
        rows.append({"id": name, "label": label, "memory_total_mib": mem})
    return {"devices": rows, "index_kind": "nvidia_name"}


def release_gpu_models() -> None:
    """Drop model references so CUDA/VRAM is freed before process exit."""
    global _whisper, _translator, _katsu, _tok_cache
    with _load_lock:
        _whisper = None
        _translator = None
        _katsu = None
        _tok_cache.clear()
    import gc

    gc.collect()
    try:
        import torch

        if torch.cuda.is_available():
            torch.cuda.empty_cache()
            torch.cuda.synchronize()
    except Exception:
        pass


@app.post("/shutdown")
def shutdown_engine() -> dict:
    """Ask the engine to release GPU memory and exit (called by the Go host on app close)."""

    def _exit() -> None:
        time.sleep(0.15)
        release_gpu_models()
        os._exit(0)

    threading.Thread(target=_exit, daemon=True).start()
    return {"ok": True}


@app.get("/health")
def health() -> dict:
    out: dict[str, Any] = {
        "ok": "true",
        "pid": os.getpid(),
        "uptime_sec": round(time.monotonic() - _ENGINE_STARTED_AT, 1),
        "models_loaded": {
            "whisper": _whisper is not None,
            "nllb": _translator is not None,
            "cutlet": _katsu is not None,
        },
        "features": {"translate": True},
        "model_devices": dict(_model_devices),
    }
    try:
        if _whisper is not None and _katsu is not None:
            out["status"] = "ready"
        if _model_devices:
            w = _model_devices.get("whisper", {})
            n = _model_devices.get("nllb", {})
            if w:
                out["device"] = w.get("device", out.get("device"))
                out["device_detail"] = w.get("detail", "")
            if w and n:
                out["device_detail"] = (
                    f"whisper {w.get('device')} ({w.get('physical_gpu') or '?'}) | "
                    f"nllb {n.get('device')} ({n.get('physical_gpu') or '?'})"
                )
        elif _whisper is not None:
            dev, idx, note = _inference_device_for("whisper")
            out["device"] = f"cuda:{idx}" if dev == "cuda" else "cpu"
            out["device_detail"] = note
    except Exception:
        pass
    return out


class LoadIn(BaseModel):
    whisper: bool = True
    nllb: bool = True
    cutlet: bool = True


@app.post("/load")
def load(body: LoadIn | None = None) -> dict:
    timer = _StepTimer("POST /load")
    opts = body or LoadIn()
    try:
        ensure_models(whisper=opts.whisper, nllb=opts.nllb, cutlet=opts.cutlet)
        timer.mark("ensure_models")
        dev, idx, note = _inference_device()
        label = f"cuda:{idx}" if dev == "cuda" else "cpu"
        if not opts.whisper and not opts.nllb:
            label = "cloud"
            note = "Selective load — Whisper/NLLB skipped"
        timings = timer.log(device=label)
        return {"status": "ready", "device": label, "device_detail": note, "timings_ms": timings}
    except HTTPException:
        raise
    except FileNotFoundError as e:
        raise HTTPException(503, detail=str(e)) from e
    except Exception as e:
        raise HTTPException(500, detail=f"{type(e).__name__}: {e}") from e


class TranslateTextIn(BaseModel):
    english: str = ""
    src_nllb: str = "eng_Latn"
    tgt_nllb: str = "jpn_Jpan"
    back_src_nllb: str = "jpn_Jpan"
    target_lang: str = "jp"
    backtranslate: str = "local"
    forward_translate: str = "nllb"
    api_provider: str = "openrouter"
    translate_model: str = ""
    openai_api_key: str = ""
    openai_base_url: str = ""
    openrouter_api_key: str = ""
    openrouter_base_url: str = ""
    openai_forward_model: str = ""
    openai_backtrans_model: str = ""


def _practice_target_kind(tgt_id: str) -> str:
    """Normalize practice target id to jp | zh | ko."""
    t = (tgt_id or "jp").strip().lower()
    if t in ("zh", "cn", "chinese"):
        return "zh"
    if t in ("ko", "kr", "korean"):
        return "ko"
    return "jp"


def _cloud_en_to_target(
    english: str,
    kind: str,
    *,
    api_provider: str,
    translate_model: str,
    openai_forward_model: str,
    openai_api_key: str,
    openai_base_url: str,
    openrouter_api_key: str,
    openrouter_base_url: str,
) -> str:
    import sys
    from pathlib import Path

    audio_dir = Path(__file__).resolve().parent / "audio"
    if str(audio_dir) not in sys.path:
        sys.path.insert(0, str(audio_dir))
    from caption_api import chat_completion, resolve_api

    try:
        key, base, provider = resolve_api(
            api_provider=api_provider,
            openai_api_key=openai_api_key,
            openai_base_url=openai_base_url,
            openrouter_api_key=openrouter_api_key,
            openrouter_base_url=openrouter_base_url,
        )
    except ValueError as e:
        raise HTTPException(status_code=400, detail=str(e)) from e
    model = (translate_model or openai_forward_model or "").strip()
    if not model:
        model = "openai/gpt-4o-mini" if provider == "openrouter" else "gpt-4o-mini"
    prompts = {
        "jp": (
            "You translate English into natural Japanese for spoken dialogue. "
            "Translate the entire input completely; do not summarize, omit, or shorten any part. "
            "Output only the Japanese translation, no romaji, no explanations, no quotes."
        ),
        "zh": (
            "You translate English into natural Simplified Chinese for spoken dialogue. "
            "Translate the entire input completely; do not summarize, omit, or shorten any part. "
            "Output only the Chinese translation, no pinyin, no explanations, no quotes."
        ),
        "ko": (
            "You translate English into natural Korean for spoken dialogue. "
            "Translate the entire input completely; do not summarize, omit, or shorten any part. "
            "Output only the Korean translation, no romanization, no explanations, no quotes."
        ),
    }
    system = prompts.get(kind, prompts["jp"])
    tgt_code = {"jp": "jpn_Jpan", "zh": "zho_Hans", "ko": "kor_Hang"}.get(kind, "jpn_Jpan")

    def one(chunk: str) -> str:
        return chat_completion(
            api_key=key,
            base_url=base,
            provider=provider,
            model=model,
            system=system,
            user_text=chunk,
            temperature=0.2,
            step="Forward translation",
        )

    return _openai_translate_units(english, one, tgt_code)


def _translate_english_to_target(
    english: str,
    *,
    tgt_id: str,
    src_nllb: str,
    tgt_nllb: str,
    back_src_nllb: str,
    backtranslate: str,
    forward_translate: str,
    api_provider: str = "openrouter",
    translate_model: str = "",
    openai_api_key: str,
    openai_base_url: str,
    openrouter_api_key: str = "",
    openrouter_base_url: str = "",
    openai_forward_model: str,
    openai_backtrans_model: str,
    timer: "_StepTimer",
    target_override: str | None = None,
) -> dict[str, Any]:
    """English text -> target language (+ furigana for JP, + back-translation)."""
    english = (english or "").strip()
    kind = _practice_target_kind(tgt_id)
    tgt_id = kind
    if kind == "jp":
        ensure_cutlet()

    fwd = (forward_translate or "nllb").strip().lower()
    target = ""
    if target_override is not None:
        target = (target_override or "").strip()
        timer.mark("forward_multimodal")
    elif english:
        if fwd == "openai":
            target = _cloud_en_to_target(
                english,
                kind,
                api_provider=api_provider,
                translate_model=translate_model,
                openai_forward_model=openai_forward_model,
                openai_api_key=openai_api_key,
                openai_base_url=openai_base_url,
                openrouter_api_key=openrouter_api_key,
                openrouter_base_url=openrouter_base_url,
            )
            timer.mark("forward_cloud")
        else:
            ensure_nllb_translator()
            target = nllb_translate(english, src_nllb, tgt_nllb)
            timer.mark("forward_nllb")
    else:
        timer.mark("forward_skip")

    furigana: list[dict] = []
    if target and kind == "jp":
        try:
            furigana = furigana_tokens(target)
        except Exception:
            furigana = [{"surface": target, "reading": ""}]
        timer.mark("furigana")
    elif kind == "jp":
        timer.mark("furigana_skip")

    back_english = ""
    if target:
        mode = (backtranslate or "local").strip().lower()
        if mode == "local":
            try:
                ensure_nllb_translator()
                back_english = nllb_translate(target, back_src_nllb, "eng_Latn")
                timer.mark("back_nllb")
            except FileNotFoundError:
                back_english = ""
                timer.mark("back_skip")
        elif mode == "openai":
            key, base = _resolve_openai_credentials(openai_api_key, openai_base_url)
            if key:
                bm = (openai_backtrans_model or "").strip() or os.environ.get("OPENAI_BACKTRANS_MODEL", "gpt-4o-mini")
                if kind == "jp":
                    back_english = openai_ja_to_en(target, key, base, bm)
                elif kind == "zh":
                    back_english = openai_zh_to_en(target, key, base, bm)
                else:
                    back_english = openai_ko_to_en(target, key, base, bm)
                timer.mark("back_openai")
            else:
                timer.mark("back_skip")
        else:
            timer.mark("back_skip")
    else:
        timer.mark("back_skip")

    return {
        "english": english,
        "japanese": target,
        "target": target,
        "target_lang": tgt_id,
        "furigana": furigana,
        "back_english": back_english,
        "detected_language": "en",
        "english_asr_engine": "manual",
        "english_asr_model": "typed",
    }


@app.post("/translate-text")
async def translate_text(body: TranslateTextIn) -> dict:
    """Typed English -> target language (same shape as /pipeline, no ASR)."""
    timer = _StepTimer("POST /translate-text")
    try:
        out = _translate_english_to_target(
            body.english,
            tgt_id=body.target_lang,
            src_nllb=body.src_nllb,
            tgt_nllb=body.tgt_nllb,
            back_src_nllb=body.back_src_nllb,
            backtranslate=body.backtranslate,
            forward_translate=body.forward_translate,
            api_provider=body.api_provider,
            translate_model=body.translate_model,
            openai_api_key=body.openai_api_key,
            openai_base_url=body.openai_base_url,
            openrouter_api_key=body.openrouter_api_key,
            openrouter_base_url=body.openrouter_base_url,
            openai_forward_model=body.openai_forward_model,
            openai_backtrans_model=body.openai_backtrans_model,
            timer=timer,
        )
        timings = timer.log(
            en_chars=len(out["english"]),
            tgt_chars=len(out["target"]),
            fwd=(body.forward_translate or "nllb").strip().lower(),
            tgt=(body.target_lang or "jp").strip().lower(),
        )
        out["timings_ms"] = timings
        return out
    except HTTPException:
        raise
    except FileNotFoundError as e:
        raise HTTPException(
            503,
            detail=f"Model files missing or download failed. Weights dir: {_weights_root()}. {e}",
        ) from e
    except Exception as e:
        raise HTTPException(500, detail=f"{type(e).__name__}: {e}") from e


@app.post("/pipeline")
async def pipeline(
    file: UploadFile = File(...),
    speech_language: str = Form("en"),
    src_nllb: str = Form("eng_Latn"),
    tgt_nllb: str = Form("jpn_Jpan"),
    back_src_nllb: str = Form("jpn_Jpan"),
    target_lang: str = Form("jp"),
    backtranslate: str = Form("local"),
    forward_translate: str = Form("nllb"),
    english_asr_engine: str = Form("whisper"),
    openai_transcribe_model: str = Form(""),
    openai_whisper_model: str = Form(""),
    openrouter_transcribe_model: str = Form(""),
    transcribe_model: str = Form(""),
    translate_model: str = Form(""),
    openai_api_key: str = Form(""),
    openai_base_url: str = Form(""),
    openrouter_api_key: str = Form(""),
    openrouter_base_url: str = Form(""),
    openai_forward_model: str = Form(""),
    openai_backtrans_model: str = Form(""),
    pipeline_mode: str = Form("split"),
    api_provider: str = Form("openrouter"),
    multimodal_model: str = Form(""),
) -> dict:
    """English speech → target language (+furigana for JP, +back-translation)."""
    timer = _StepTimer("POST /pipeline")
    try:
        tgt_id = _practice_target_kind(target_lang)
        raw = await file.read()
        timer.mark("read_wav")
        pipe = (pipeline_mode or "split").strip().lower()
        if pipe not in ("split", "multimodal"):
            pipe = "split"

        if pipe == "multimodal":
            import sys
            from pathlib import Path

            audio_dir = Path(__file__).resolve().parent / "audio"
            if str(audio_dir) not in sys.path:
                sys.path.insert(0, str(audio_dir))
            from caption_api import multimodal_practice_wav, resolve_api

            try:
                key, base, provider = resolve_api(
                    api_provider=api_provider,
                    openai_api_key=openai_api_key,
                    openai_base_url=openai_base_url,
                    openrouter_api_key=openrouter_api_key,
                    openrouter_base_url=openrouter_base_url,
                )
            except ValueError as e:
                raise HTTPException(400, detail=str(e)) from e
            mm = (multimodal_model or "").strip() or "xiaomi/mimo-v2-flash"
            try:
                cap = multimodal_practice_wav(
                    raw,
                    api_key=key,
                    base_url=base,
                    provider=provider,
                    model=mm,
                    target_kind=tgt_id,
                )
            except RuntimeError as e:
                raise HTTPException(502, detail=str(e)) from e
            english = (cap.get("english") or "").strip()
            target_prebuilt = (cap.get("target") or "").strip()
            timer.mark("multimodal")
            out = _translate_english_to_target(
                english,
                tgt_id=tgt_id,
                src_nllb=src_nllb,
                tgt_nllb=tgt_nllb,
                back_src_nllb=back_src_nllb,
                backtranslate=backtranslate,
                forward_translate=forward_translate,
                api_provider=api_provider,
                translate_model=translate_model,
                openai_api_key=openai_api_key,
                openai_base_url=openai_base_url,
                openrouter_api_key=openrouter_api_key,
                openrouter_base_url=openrouter_base_url,
                openai_forward_model=openai_forward_model,
                openai_backtrans_model=openai_backtrans_model,
                timer=timer,
                target_override=target_prebuilt,
            )
            out["detected_language"] = "en"
            out["english_asr_engine"] = "multimodal"
            out["english_asr_model"] = mm
            out["pipeline"] = "multimodal"
            timings = timer.log(
                wav_bytes=len(raw),
                en_chars=len(english),
                tgt_chars=len(out["target"]),
                asr="multimodal",
                asr_model=mm,
                fwd="multimodal",
                tgt=tgt_id,
            )
            out["timings_ms"] = timings
            return out

        backend, asr_model = resolve_asr(
            english_asr_engine,
            openai_transcribe_model=openai_transcribe_model,
            openai_whisper_model=openai_whisper_model,
            openrouter_transcribe_model=openrouter_transcribe_model,
            transcribe_model=transcribe_model,
        )
        detected_language: str | None = None

        if backend in ("openai", "openrouter"):
            speech_lang = (speech_language or "").strip() or "en"
            english, det, step = cloud_transcribe_wav(
                raw,
                backend=backend,
                model=asr_model,
                language=speech_lang,
                openai_api_key=openai_api_key,
                openai_base_url=openai_base_url,
                openrouter_api_key=openrouter_api_key,
                openrouter_base_url=openrouter_base_url,
            )
            detected_language = det or speech_lang
            timer.mark(step)
        else:
            ensure_whisper()
            audio = read_wav_mono_f32(raw)
            assert _whisper is not None
            segments, info = _whisper.transcribe(audio, language=speech_language or None, vad_filter=True)
            english = "".join(s.text for s in segments).strip()
            detected_language = getattr(info, "language", None) if info is not None else None
            timer.mark("asr_whisper")

        out = _translate_english_to_target(
            english,
            tgt_id=tgt_id,
            src_nllb=src_nllb,
            tgt_nllb=tgt_nllb,
            back_src_nllb=back_src_nllb,
            backtranslate=backtranslate,
            forward_translate=forward_translate,
            api_provider=api_provider,
            translate_model=translate_model,
            openai_api_key=openai_api_key,
            openai_base_url=openai_base_url,
            openrouter_api_key=openrouter_api_key,
            openrouter_base_url=openrouter_base_url,
            openai_forward_model=openai_forward_model,
            openai_backtrans_model=openai_backtrans_model,
            timer=timer,
        )
        out["detected_language"] = detected_language
        out["english_asr_engine"] = english_asr_engine
        out["english_asr_model"] = asr_model or "local-whisper"
        out["pipeline"] = "split"
        timings = timer.log(
            wav_bytes=len(raw),
            en_chars=len(english),
            tgt_chars=len(out["target"]),
            asr=english_asr_engine,
            asr_model=asr_model or "local-whisper",
            fwd=(forward_translate or "nllb").strip().lower(),
            tgt=tgt_id,
        )
        out["timings_ms"] = timings
        return out
    except HTTPException:
        raise
    except FileNotFoundError as e:
        raise HTTPException(
            503,
            detail=f"Model files missing or download failed. Weights dir: {_weights_root()}. {e}",
        ) from e
    except Exception as e:
        raise HTTPException(500, detail=f"{type(e).__name__}: {e}") from e


@app.post("/transcribe")
async def transcribe(
    request: Request,
    language: str = Form("ja"),
    asr_engine: str = Form("whisper"),
    openai_transcribe_model: str = Form(""),
    openai_whisper_model: str = Form(""),
    openrouter_transcribe_model: str = Form(""),
    transcribe_model: str = Form(""),
    openai_api_key: str = Form(""),
    openai_base_url: str = Form(""),
    openrouter_api_key: str = Form(""),
    openrouter_base_url: str = Form(""),
    file: UploadFile = File(...),
) -> dict:
    """Used by the Japanese-repeat path: returns just {text} for scoring."""
    timer = _StepTimer("POST /transcribe")
    try:
        raw = await file.read()
        timer.mark("read_wav")
        backend, asr_model = resolve_asr(
            asr_engine,
            openai_transcribe_model=openai_transcribe_model,
            openai_whisper_model=openai_whisper_model,
            openrouter_transcribe_model=openrouter_transcribe_model,
            transcribe_model=transcribe_model,
        )
        if backend in ("openai", "openrouter"):
            text, det, step = cloud_transcribe_wav(
                raw,
                backend=backend,
                model=asr_model,
                language=(language or "ja"),
                openai_api_key=openai_api_key,
                openai_base_url=openai_base_url,
                openrouter_api_key=openrouter_api_key,
                openrouter_base_url=openrouter_base_url,
            )
            timer.mark(step)
            timings = timer.log(
                wav_bytes=len(raw),
                asr=asr_engine,
                asr_model=asr_model,
                lang=language,
                text_chars=len(text),
            )
            return {
                "text": text,
                "asr_engine": asr_engine,
                "asr_model": asr_model,
                "detected_language": det or language,
                "timings_ms": timings,
            }
        try:
            audio = read_wav_mono_f32(raw)
        except Exception as e:
            raise HTTPException(400, f"bad wav: {e}") from e
        ensure_whisper()
        assert _whisper is not None
        segments, info = _whisper.transcribe(audio, language=language or None, vad_filter=True)
        text = "".join(s.text for s in segments).strip()
        timer.mark("asr_whisper")
        timings = timer.log(wav_bytes=len(raw), asr="whisper", lang=language, text_chars=len(text))
        return {
            "text": text,
            "asr_engine": "whisper",
            "detected_language": getattr(info, "language", None) or language,
            "timings_ms": timings,
        }
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(500, detail=f"{type(e).__name__}: {e}") from e


class FuriganaTokenIn(BaseModel):
    surface: str = ""
    reading: str = ""


class ScoreJaIn(BaseModel):
    expected: str = Field(default="", max_length=8000)
    spoken: str = Field(default="", max_length=8000)
    threshold: int = Field(default=100, ge=50, le=100)
    furigana: list[FuriganaTokenIn] | None = None


@app.post("/score-ja")
def score_ja_endpoint(body: ScoreJaIn) -> dict:
    timer = _StepTimer("POST /score-ja")
    exp = (body.expected or "").strip()
    spk = (body.spoken or "").strip()
    score_strict = score_ja(exp, spk)
    timer.mark("score_strict")
    tok_list = [{"surface": t.surface, "reading": t.reading} for t in body.furigana] if body.furigana else []
    expected_reading = expected_kana_from_furigana(tok_list)
    use_reading = bool(expected_reading and normalize_ja(expected_reading))
    if use_reading:
        score_primary = score_ja(expected_reading, spk)
        highlight_expected = expected_reading
        timer.mark("score_reading")
    else:
        score_primary = score_strict
        highlight_expected = exp
    accepted = score_primary >= float(body.threshold)
    base, ranges = spoken_match_ranges_for_highlight(highlight_expected, spk)
    timer.mark("highlight")
    timings = timer.log(exp_chars=len(exp), spk_chars=len(spk), accepted=accepted)
    out: dict[str, Any] = {
        "score": score_primary,
        "accepted": bool(accepted),
        "threshold_used": int(body.threshold),
        "score_strict": score_strict,
        "normalized_expected": normalize_ja(exp),
        "normalized_spoken": normalize_ja(spk),
        "spoken_highlight_base": base,
        "spoken_match_ranges": ranges,
        "timings_ms": timings,
    }
    if use_reading:
        out["expected_reading"] = expected_reading
        out["normalized_expected_reading"] = normalize_ja(expected_reading)
    return out


_PUNCT_ZH_RE = re.compile(r"[，。！？、．…\s\"'「」『』（）()\[\]{}:;,.!?]+")


def normalize_zh(s: str) -> str:
    if not s:
        return ""
    s = unicodedata.normalize("NFKC", s)
    return _PUNCT_ZH_RE.sub("", s)


def score_zh(expected: str, spoken: str) -> float:
    a = normalize_zh(expected)
    b = normalize_zh(spoken)
    if not a or not b:
        return 0.0
    return round(SequenceMatcher(None, a, b).ratio() * 100, 1)


def _normalize_zh_mapped(text: str) -> tuple[str, list[int]]:
    if not (text or "").strip():
        return "", []
    s0 = unicodedata.normalize("NFKC", (text or "").strip())
    out: list[str] = []
    idxs: list[int] = []
    i = 0
    n = len(s0)
    while i < n:
        m = _PUNCT_ZH_RE.match(s0, i)
        if m:
            i = m.end()
            continue
        out.append(s0[i])
        idxs.append(i)
        i += 1
    return "".join(out), idxs


def spoken_match_ranges_zh(expected: str, spoken: str) -> tuple[str, list[list[int]]]:
    s = unicodedata.normalize("NFKC", (spoken or "").strip())
    norm_exp, _ = _normalize_zh_mapped(expected)
    norm_sp, sp_idx = _normalize_zh_mapped(s)
    if not norm_exp or not norm_sp:
        return s, []
    matched_norm = [False] * len(norm_sp)
    sm = SequenceMatcher(None, norm_exp, norm_sp, autojunk=False)
    for tag, _i1, _i2, j1, j2 in sm.get_opcodes():
        if tag != "equal":
            continue
        for k in range(j1, j2):
            matched_norm[k] = True
    matched_s = [False] * len(s)
    for k, is_m in enumerate(matched_norm):
        if is_m:
            matched_s[sp_idx[k]] = True
    ranges: list[list[int]] = []
    i = 0
    n = len(matched_s)
    while i < n:
        if not matched_s[i]:
            i += 1
            continue
        j = i + 1
        while j < n and matched_s[j]:
            j += 1
        ranges.append([i, j])
        i = j
    if not ranges and norm_sp:
        return norm_sp, [[0, len(norm_sp)]] if norm_exp == norm_sp else []
    return s, ranges


class ScoreZhIn(BaseModel):
    expected: str = Field(default="", max_length=8000)
    spoken: str = Field(default="", max_length=8000)
    threshold: int = Field(default=100, ge=50, le=100)


@app.post("/score-zh")
def score_zh_endpoint(body: ScoreZhIn) -> dict:
    timer = _StepTimer("POST /score-zh")
    exp = (body.expected or "").strip()
    spk = (body.spoken or "").strip()
    score_primary = score_zh(exp, spk)
    timer.mark("score")
    accepted = score_primary >= float(body.threshold)
    base, ranges = spoken_match_ranges_zh(exp, spk)
    timer.mark("highlight")
    timings = timer.log(exp_chars=len(exp), spk_chars=len(spk), accepted=accepted)
    return {
        "score": score_primary,
        "accepted": bool(accepted),
        "threshold_used": int(body.threshold),
        "score_strict": score_primary,
        "normalized_expected": normalize_zh(exp),
        "normalized_spoken": normalize_zh(spk),
        "spoken_highlight_base": base,
        "spoken_match_ranges": ranges,
        "timings_ms": timings,
    }


_PUNCT_KO_RE = re.compile(
    r"[，。！？、．…\s\"'「」『』（）()\[\]{}:;,.!?·―～~\-]+"
)


def normalize_ko(s: str) -> str:
    if not s:
        return ""
    s = unicodedata.normalize("NFKC", s)
    return _PUNCT_KO_RE.sub("", s)


def score_ko(expected: str, spoken: str) -> float:
    a = normalize_ko(expected)
    b = normalize_ko(spoken)
    if not a or not b:
        return 0.0
    return round(SequenceMatcher(None, a, b).ratio() * 100, 1)


def _normalize_ko_mapped(text: str) -> tuple[str, list[int]]:
    if not (text or "").strip():
        return "", []
    s0 = unicodedata.normalize("NFKC", (text or "").strip())
    out: list[str] = []
    idxs: list[int] = []
    i = 0
    n = len(s0)
    while i < n:
        m = _PUNCT_KO_RE.match(s0, i)
        if m:
            i = m.end()
            continue
        out.append(s0[i])
        idxs.append(i)
        i += 1
    return "".join(out), idxs


def spoken_match_ranges_ko(expected: str, spoken: str) -> tuple[str, list[list[int]]]:
    s = unicodedata.normalize("NFKC", (spoken or "").strip())
    norm_exp, _ = _normalize_ko_mapped(expected)
    norm_sp, sp_idx = _normalize_ko_mapped(s)
    if not norm_exp or not norm_sp:
        return s, []
    matched_norm = [False] * len(norm_sp)
    sm = SequenceMatcher(None, norm_exp, norm_sp, autojunk=False)
    for tag, _i1, _i2, j1, j2 in sm.get_opcodes():
        if tag != "equal":
            continue
        for k in range(j1, j2):
            matched_norm[k] = True
    matched_s = [False] * len(s)
    for k, is_m in enumerate(matched_norm):
        if is_m:
            matched_s[sp_idx[k]] = True
    ranges: list[list[int]] = []
    i = 0
    n = len(matched_s)
    while i < n:
        if not matched_s[i]:
            i += 1
            continue
        j = i + 1
        while j < n and matched_s[j]:
            j += 1
        ranges.append([i, j])
        i = j
    if not ranges and norm_sp:
        return norm_sp, [[0, len(norm_sp)]] if norm_exp == norm_sp else []
    return s, ranges


class ScoreKoIn(BaseModel):
    expected: str = Field(default="", max_length=8000)
    spoken: str = Field(default="", max_length=8000)
    threshold: int = Field(default=100, ge=50, le=100)


@app.post("/score-ko")
def score_ko_endpoint(body: ScoreKoIn) -> dict:
    timer = _StepTimer("POST /score-ko")
    exp = (body.expected or "").strip()
    spk = (body.spoken or "").strip()
    score_primary = score_ko(exp, spk)
    timer.mark("score")
    accepted = score_primary >= float(body.threshold)
    base, ranges = spoken_match_ranges_ko(exp, spk)
    timer.mark("highlight")
    timings = timer.log(exp_chars=len(exp), spk_chars=len(spk), accepted=accepted)
    return {
        "score": score_primary,
        "accepted": bool(accepted),
        "threshold_used": int(body.threshold),
        "score_strict": score_primary,
        "normalized_expected": normalize_ko(exp),
        "normalized_spoken": normalize_ko(spk),
        "spoken_highlight_base": base,
        "spoken_match_ranges": ranges,
        "timings_ms": timings,
    }


@app.get("/devices/output")
def devices_output() -> dict:
    return _list_output_speakers()


class SpeakTTSIn(BaseModel):
    text: str = Field(default="", max_length=4096)
    tts_engine: str = "openrouter"
    model: str = "openai/gpt-4o-mini-tts-2025-12-15"
    voice: str = "coral"
    instructions: str = ""
    openai_api_key: str = ""
    openai_base_url: str = ""
    openrouter_api_key: str = ""
    openrouter_base_url: str = ""
    device_id: str = ""
    device_name: str = ""


@app.post("/speak-tts")
async def speak_tts(body: SpeakTTSIn) -> dict:
    """Synthesize target text (OpenRouter or OpenAI TTS) and play on a system output device."""
    timer = _StepTimer("POST /speak-tts")
    text = (body.text or "").strip()
    if not text:
        raise HTTPException(status_code=400, detail="empty text")
    eng = (body.tts_engine or "openrouter").strip().lower()
    if eng == "openai":
        model = (body.model or "").strip() or os.environ.get("OPENAI_TTS_MODEL", "gpt-4o-mini-tts-2025-12-15")
    else:
        eng = "openrouter"
        model = (body.model or "").strip() or os.environ.get(
            "OPENROUTER_TTS_MODEL", "openai/gpt-4o-mini-tts-2025-12-15"
        )
    voice = (body.voice or "").strip() or os.environ.get("OPENAI_TTS_VOICE", "coral")
    try:
        samples, sr, backend, cached = synthesize_speech_audio(
            tts_engine=eng,
            text=text,
            model=model,
            voice=voice,
            instructions=body.instructions or "",
            openai_api_key=body.openai_api_key,
            openai_base_url=body.openai_base_url,
            openrouter_api_key=body.openrouter_api_key,
            openrouter_base_url=body.openrouter_base_url,
        )
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"{type(e).__name__}: {e}") from e

    try:
        prep, play_sr = _prepare_playback_samples(samples, sr)
        name = await asyncio.to_thread(
            _play_on_device_blocking,
            prep,
            play_sr,
            body.device_id,
            body.device_name,
        )
        frame_count = int(prep.shape[0])
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(
            status_code=500,
            detail=f"TTS playback failed: {type(e).__name__}: {e}",
        ) from e
    timer.log(chars=len(text), cached=bool(cached), device=str(name))
    return {
        "ok": True,
        "device_name": str(name),
        "samplerate": play_sr,
        "samples": frame_count,
        "model": model,
        "voice": voice,
        "tts_engine": backend,
        "cached": bool(cached),
    }


@app.post("/play-wav")
async def play_wav(
    file: UploadFile = File(...),
    device_id: str = Form(""),
    device_name: str = Form(""),
) -> dict:
    raw = await file.read()
    try:
        samples, sr = read_wav_native(raw)
    except Exception as e:
        raise HTTPException(400, f"bad wav: {e}") from e
    # Run the blocking play on a daemon thread so the HTTP response returns
    # right away — the UI shows a Passed→Done transition without waiting for
    # the playback to finish.
    t = threading.Thread(
        target=_play_wav_blocking,
        args=(samples, sr, device_id, device_name),
        daemon=True,
    )
    t.start()
    name = (device_name or "").strip() or "speaker"
    return {"ok": True, "device_name": name, "samplerate": sr, "samples": int(len(samples))}




def main() -> None:
    _log_gpu_env()
    host = os.environ.get("TO_ENGINE_HOST", "127.0.0.1")
    port = int(os.environ.get("TO_ENGINE_PORT", "8745"))
    uvicorn.run(app, host=host, port=port, log_level="info", access_log=False)


if __name__ == "__main__":
    main()
