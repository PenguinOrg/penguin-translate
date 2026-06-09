"""Download CTranslate2 / faster-whisper weights from Hugging Face (standalone, no VRCT)."""

from __future__ import annotations

import json
import os
import threading
import urllib.error
import urllib.request
from pathlib import Path
from typing import Callable

_WHISPER_REPOS: dict[str, str] = {
    "large-v3-turbo": "deepdml/faster-whisper-large-v3-turbo-ct2",
    "large-v3-turbo-int8": "Zoont/faster-whisper-large-v3-turbo-int8-ct2",
    "large-v3": "Systran/faster-whisper-large-v3",
    "large-v2": "Systran/faster-whisper-large-v2",
    "large-v1": "Systran/faster-whisper-large-v1",
    "medium": "Systran/faster-whisper-medium",
    "small": "Systran/faster-whisper-small",
    "base": "Systran/faster-whisper-base",
    "tiny": "Systran/faster-whisper-tiny",
}

_WHISPER_SKIP_FILES = frozenset({".gitattributes", "README.md"})

_NLLB_REPOS: dict[str, str] = {
    "nllb-200-distilled-1.3B-ct2-int8": "OpenNMT/nllb-200-distilled-1.3B-ct2-int8",
}

_download_lock = threading.Lock()


def hf_resolve_url(repo_id: str, filename: str) -> str:
    return f"https://huggingface.co/{repo_id}/resolve/main/{filename}"


def list_hf_repo_files(repo_id: str, *, timeout: float = 120.0) -> list[str]:
    url = f"https://huggingface.co/api/models/{repo_id}/tree/main?recursive=1"
    req = urllib.request.Request(url, headers={"User-Agent": "translation-overlay-engine/1.0"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        data = json.load(resp)
    if not isinstance(data, list):
        raise RuntimeError(f"unexpected Hugging Face API response for {repo_id}")
    return [str(item["path"]) for item in data if isinstance(item, dict) and item.get("type") == "file"]


def _download_file(
    url: str,
    dest: Path,
    *,
    progress: Callable[[str, float], None] | None = None,
    label: str = "",
) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    tmp = dest.with_suffix(dest.suffix + ".part")
    req = urllib.request.Request(url, headers={"User-Agent": "translation-overlay-engine/1.0"})
    try:
        with urllib.request.urlopen(req, timeout=600) as resp:
            total = int(resp.headers.get("Content-Length") or 0)
            done = 0
            chunk = 1024 * 256
            with open(tmp, "wb") as out:
                while True:
                    block = resp.read(chunk)
                    if not block:
                        break
                    out.write(block)
                    done += len(block)
                    if progress and total > 0:
                        progress(label or dest.name, done / total)
    except urllib.error.HTTPError as e:
        if tmp.exists():
            tmp.unlink(missing_ok=True)
        raise RuntimeError(f"download failed {url}: HTTP {e.code}") from e
    except Exception:
        if tmp.exists():
            tmp.unlink(missing_ok=True)
        raise
    os.replace(tmp, dest)


def _log_progress(name: str, frac: float) -> None:
    pct = int(frac * 100)
    if pct >= 100 or pct % 5 == 0:
        print(f"translation-overlay-engine: download {name} {pct}%", flush=True)


def whisper_weights_complete(target_dir: Path) -> bool:
    """faster-whisper needs model.bin plus a vocabulary file (json or txt)."""
    if not (target_dir / "model.bin").is_file():
        return False
    if (target_dir / "vocabulary.json").is_file() and (target_dir / "vocabulary.json").stat().st_size > 0:
        return True
    if (target_dir / "vocabulary.txt").is_file() and (target_dir / "vocabulary.txt").stat().st_size > 0:
        return True
    return False


def download_whisper_weights(target_dir: Path, *, variant: str = "large-v3-turbo") -> None:
    repo = _WHISPER_REPOS.get(variant)
    if not repo:
        raise ValueError(f"unknown whisper variant {variant!r}")
    target_dir.mkdir(parents=True, exist_ok=True)
    print(
        f"translation-overlay-engine: downloading whisper {variant} from {repo} -> {target_dir}",
        flush=True,
    )
    files = list_hf_repo_files(repo)
    if not files:
        raise RuntimeError(f"no files listed for {repo}")
    for filename in files:
        if filename in _WHISPER_SKIP_FILES or filename.startswith("."):
            continue
        dest = target_dir / filename
        if dest.is_file() and dest.stat().st_size > 0:
            continue
        url = hf_resolve_url(repo, filename)
        prog = _log_progress if filename == "model.bin" else None
        _download_file(url, dest, progress=prog, label=filename)
    if not whisper_weights_complete(target_dir):
        raise RuntimeError(f"whisper download incomplete under {target_dir}")


def nllb_weights_complete(target_dir: Path) -> bool:
    return (target_dir / "model.bin").is_file() and (target_dir / "model.bin").stat().st_size > 0


def download_nllb_weights(target_dir: Path, *, variant: str = "nllb-200-distilled-1.3B-ct2-int8") -> None:
    repo = _NLLB_REPOS.get(variant)
    if not repo:
        raise ValueError(f"unknown NLLB variant {variant!r}")
    target_dir.mkdir(parents=True, exist_ok=True)
    print(
        f"translation-overlay-engine: downloading NLLB {variant} from {repo} -> {target_dir}",
        flush=True,
    )
    files = list_hf_repo_files(repo)
    if not files:
        raise RuntimeError(f"no files listed for {repo}")
    for filename in files:
        if filename in _WHISPER_SKIP_FILES or filename.startswith("."):
            continue
        dest = target_dir / filename
        if dest.is_file() and dest.stat().st_size > 0:
            continue
        url = hf_resolve_url(repo, filename)
        prog = _log_progress if filename == "model.bin" else None
        _download_file(url, dest, progress=prog, label=filename)
    if not nllb_weights_complete(target_dir):
        raise RuntimeError(f"NLLB download incomplete under {target_dir}")


def ensure_whisper_weights(target_dir: Path, *, variant: str = "large-v3-turbo") -> None:
    if whisper_weights_complete(target_dir):
        return
    with _download_lock:
        if whisper_weights_complete(target_dir):
            return
        download_whisper_weights(target_dir, variant=variant)


def ensure_nllb_weights(target_dir: Path, *, variant: str = "nllb-200-distilled-1.3B-ct2-int8") -> None:
    if nllb_weights_complete(target_dir):
        return
    with _download_lock:
        if nllb_weights_complete(target_dir):
            return
        download_nllb_weights(target_dir, variant=variant)
