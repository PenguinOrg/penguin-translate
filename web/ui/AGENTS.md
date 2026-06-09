# web/ui — agent notes

Build-less **single-document app** rendered with **Alpine.js** (vendored ESM, no bundler,
no npm). One `index.html` shell; reactive state lives in Alpine *stores* and the markup
binds to them with `x-data`/`x-text`/`x-for`/`x-if`/`:attr`/`@event`. The UI talks to the
Go server over `fetch` (`/api/*`); no Wails JS bindings (which is what makes headless
`-http` mode work). The whole UI is `//go:embed`-ed (`embed.go`), so any change here needs a
rebuild to show in the packaged app.

## Alpine + i18n

- `shared/alpine.esm.js` — vendored Alpine v3 ESM (NOT via npm; offline/loopback app, so no
  CDN). `app.js` imports it as the default export, registers every store/component/magic,
  builds the `window.App` facade, then calls `Alpine.start()` **last**. No CSP exists, so the
  standard build (uses `Function()` for expressions) is fine.
- **Alpine auto-calls a store's `init()` synchronously at registration** (`Alpine.store(name,
  value)`), and `$store.x` is a reactive *proxy* of the object you passed — NOT the object
  itself. Module-local code that mutates state must go through the proxy (`Alpine.store(...)`),
  or reactivity won't fire (see `conversation.js` `S()`).
- **i18n** (`i18n/`): the interface localizes to the user's **Speak language**
  (`practice.my_language`); ships **en + zh/ja/ko**, English is the canonical key set and the
  fallback. `i18n/index.js` has `t(key,params,locale)` (dotted-key lookup + `{param}`) and
  `langName(id)` (browser `Intl.DisplayNames` for localized language *names*). Strings bind via
  `x-text="$store.i18n.t('key')"`; runtime-composed strings (status/error lines) call the
  `i18n` store's `t()`. There is **no** separate interface-language control — picking your
  Speak language re-localizes the whole UI live (the `i18n` store's `locale` is reactive).
  Add a key to `i18n/en.js` first, then the three CJK files (missing keys fall back to en).

## Structure

The app is **one conversation surface** + a **launchable overlay utility** — no per-feature
tabs, one run indicator. Your language + the set you hear/reply in live in an app-bar
languages bar.

- `index.html` — the shell + **all** declarative markup (app bar + langbar + menu, engine
  banner, toast host, the conversation `<section>` + transcript, the run bar with capture
  lanes/composer/Start-Stop, the overlay toolbar row, the Preferences modal with its panes).
  Shell chrome CSS is inline here; tokens from `shared/ui.css` (keep the `[x-cloak]` rule).
- `app.js` — registers the stores + the two scope components and boots Alpine:
  - `i18n` store — `{ locale, t(), langName() }` (reactive locale → live re-localization).
  - `toasts` store — `{ items[], push(), dismiss(), clear() }`; host renders via `x-for`.
  - `engine` store — the *only* poller of `/api/launcher-status` + `/api/health-summary`;
    drives the shared banner. `subscribe/refresh/setPhase/start/stop`. Banner binds `:hidden`
    (NOT `x-show`) so `el.hidden` stays correct.
  - `langs` store — reads/writes `practice.my_language` + `other_languages`; setting `my`
    also sets `i18n.locale`; broadcasts the `langschange` document event. `getLangs()`/`load()`.
  - `prefs` store — Preferences state + load/save (`createPrefsStore`, in `prefs.js`).
  - `overlay` store — overlay-an-app state + logic (`createOverlayStore`, in `features/overlay.js`).
  - `conversation` store — the transcript surface (`createConversationStore`, in
    `features/conversation.js`).
  - Components (`Alpine.data`): `langbar()` (scopes the whole `.appbar`; owns the menu's
    open/mode/position) and `prefsModal()` (short aliases onto `$store.prefs` for the panes).
  - `Api` — `get/post` over `shared/http.js` that route failures to a toast; auth/key errors
    get an "Open Preferences › Inference" recovery action.
  - `window.App` — thin facade for cross-module use + the harness: `{ Toasts, EngineStatus,
    RunController (compat shim), Preferences (open/select/close), Api, setView (no-op),
    getLangs, reloadLangs, views:[conversation store], i18n, Overlay }`.
- `features/conversation.js` — `createConversationStore(ctx)`. Reactive VIEW state (transcript
  `turns[]`, lane `enabled`/`devices`/`device`, `listening`, `status`, `composer`) on the
  store; the markup (conversation section + run-bar controls) is in `index.html`. The audio
  **engine** (capture, shared VAD segmenter, Silero gate/stream workers, loopback WS,
  `/api/transcribe` + `/api/translate-text`) stays module-local JS and mutates the store via
  `S() = () => Alpine.store('conversation')`. Live level **meters are imperative**
  (`.style.width` on the `.lane-meter > i`, looked up once), never reactive. Reads languages
  from `/api/settings` + the `langschange` event; capture tuning live-applies via
  `audioprefschange`. Test seams: `_simulateIncoming(text,detected)`, `_langs()`.
- `features/overlay.js` — `createOverlayStore(ctx)`. The overlay-an-app utility (NOT a view):
  `#overlayBar` is an embedded toolbar row under the app bar with a *setup* state (window
  `<select>` + ↻ + ▶ Start + ✕) and a *running* strip (`● Overlaying {app}` · F9 · Change
  window · ■ Stop · ▸ Recognized text → inline dual-pane). The app-bar `#btnOverlay` toggles
  it and reflects running state. Keeps `/api/config` · `/api/start|stop|state` · `/api/events`
  SSE; F9 pause is a Go hotkey. OCR translates into `practice.my_language` (no own picker).
- `prefs.js` — `createPrefsStore(ctx)`: the `prefs` store + a Basic⇄Advanced toggle (persisted
  in `localStorage` `pt.prefs.advanced`). Panes (declarative `x-if` in `index.html`, so only
  the active pane is in the DOM): **Languages** (a view onto the `langs` store), **Inference
  source** (provider + API keys + curated model `<select>`s — no free-text model fields; adv:
  pipeline, GPUs, load models), **Capture & audio** (advanced-only; VAD/clip tuning + a live
  mic-activation meter, an imperative WebAudio island), **Overlays & VR**, **Integrations**
  (VRChat OSC), **Diagnostics** (engine info + log tail; adv: the pipeline-latency iframe).
  Writes `/api/settings`; secret handling unchanged (keys POST, stored 0600, re-injected,
  never rendered beyond the input value).
- `shared/` — `dsp.js` (DSP + `createSpeechSegmenter`), `vad/*` (Silero gate/stream),
  `http.js` (`getJSON`/`postJSON`/`postForm`/`httpErrorMessage`), `ui.css`, `alpine.esm.js`.
  Never re-inline DSP/fetch/VAD — call into these modules.
- `features/mic.{js,css}` — **legacy, unreferenced** (old Mic-translate view; still the only
  home of the `/api/score` practice UI). NOT migrated to Alpine, not imported by the shell.

### Any↔any translation
`/api/languages` returns `{languages:[legacy profiles], catalog:[full registry]}`; the UI reads
`catalog` (`{id,label,short_label,flag,asr_code,nllb_code,reading_aid}`). `/api/translate-text`
accepts `{text, source_language, target_languages[]}` and returns `{results:[{language,label,
flag,text,reading_aid,reading_aid_tokens}]}`. The HTTP contract and both sidecar IPC protocols
are the stable seam — don't change them from the front end.

## Testing the UI in a real browser (zero installs)

Headless Edge + DevTools Protocol; no Playwright/Puppeteer. The harness `_uitest/runner.mjs`
is the gate (52 assertions): it serves this dir at `/ui/` with a Node mock backend (mocking
`/api/*` with correct JSON **shapes**), a fake-mic WAV, and headless Edge/CDP, then asserts the
chrome, the languages bar (add/remove persists), the conversation two-way flow (mic→VAD→
`/api/transcribe`→`/api/translate-text`→incoming/outgoing turns with furigana ruby), the overlay
utility, the banner, the Preferences panes (5 basic / 6 advanced, control counts), error→toast,
and **i18n re-localization** (switch Speak language → chrome switches to Chinese and back). Run:
`node --experimental-websocket _uitest/runner.mjs`.

- **Alpine renders async** (effects flush on a microtask), so the harness yields (`await
  sleep`) after interactions before re-querying the DOM — it can't assume a click updates the
  DOM synchronously within one `evaluate`. The post-load wait (~1.8s) covers hydration.
- Backend: the built-in mock, **or** the real API headless: `TO_DATA_DIR=<scratch> app -http
  :PORT` (real persistence; engine/inference endpoints still need the Python sidecar).
- Edge here is Chromium ≈ the WebView2 engine
  (`C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe`). Drive via
  `node --experimental-websocket` (global `WebSocket` needs the flag).

Not covered: Python engine (inference), WASAPI **loopback** VAD (mic VAD can be faked with
`--use-file-for-fake-audio-capture`), the real WebView2 shell — for that attach to the real app
via `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=9332`.
