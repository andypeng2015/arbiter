# Arbiter Playground

Compiled `.arb` decision rules, evaluated entirely in the browser via
WebAssembly — nothing leaves the page. A small top-of-funnel demo and a
GopherCon-friendly "try it live" surface.

## Run

```bash
./build.sh                 # builds arbiter.wasm + copies wasm_exec.js, loader.js
python3 -m http.server 8080
# open http://localhost:8080
```

`build.sh` produces three **gitignored** build artifacts next to `index.html`:

- `arbiter.wasm` — the compiled WASM module (`cmd/arbiter-wasm`)
- `wasm_exec.js` — Go's WASM runtime glue (copied from `$GOROOT`)
- `loader.js` — the Arbiter WASM SDK loader (copied from `cmd/arbiter-wasm`)

WASM must be served over HTTP (`file://` won't instantiate it).

## How it works

`index.html` loads the module via `arbiter.init("arbiter.wasm")`, then on each
Evaluate it calls `arbiter.compile(source)` and `arbiter.evalGoverned(context)`
(the same API documented in `cmd/arbiter-wasm/arbiter.d.ts`). The eval path is
smoke-tested under Node against this exact API.
