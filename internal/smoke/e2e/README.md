# User-flow E2E

Drives the **real** application the way a user does: the real web UI in headless
Edge, talking to the real `penguin-translate.exe` (`-http` mode), with only the
paid cloud (LLM/ASR) replaced by an in-process mock.

```
real browser → real backend → nativecloud → cloudapi → mock cloud → back to DOM
```

Where the Go smoke (`internal/smoke`) asserts backend HTTP/WS contracts, this
asserts the end-to-end experience: type a message and the translation renders;
let a fake mic speak and a transcribed, translated turn appears — furigana and
all. It found a real crash the contract smoke missed (the OpenAI multipart ASR
path panicked on every normal language code).

Two harnesses:

- **`e2e.mjs`** — mic + typed reply (outgoing only). Runs anywhere with Edge.
- **`audio.mjs`** — both capture directions, hardware in the loop: the mic
  (Edge fake device → outgoing) **and** system audio (the app plays the fixture
  to the default output; the native WASAPI loopback captures it back → incoming).
  Needs a real loopback-capable audio device, so it **self-skips** (exit 0) on a
  device-less CI runner. The mock tells the two directions apart by the ASR model
  name and the translate prompt shape. Note: it briefly plays audible tones.

## Run

```sh
# Node 22+ has a global WebSocket; on Node 20 add --experimental-websocket.
TO_SMOKE_EXE=build/penguin-translate.exe \
  node --experimental-websocket internal/smoke/e2e/e2e.mjs
TO_SMOKE_EXE=build/penguin-translate.exe \
  node --experimental-websocket internal/smoke/e2e/audio.mjs
```

Needs a built binary (`go build -o build/penguin-translate.exe ./cmd/app`) and
Microsoft Edge. Screenshots + the captured app log go to `$E2E_ARTIFACT_DIR`
(default: a temp dir, printed as `ARTIFACTS` at the end).

## Test data

[`testdata.mjs`](testdata.mjs) synthesizes everything — nothing is recorded or
copyrighted. The fake-mic clip is a deterministic sum of speech-band sine
partials (loud enough to trip the capture worker's RMS gate); the cloud fixtures
are neutral placeholder strings. Inspect/regenerate the clip standalone:

```sh
node internal/smoke/e2e/testdata.mjs voice.wav
```
