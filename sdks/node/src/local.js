// Local in-process evaluation for the Arbiter Node SDK.
//
// Unlike the gRPC client (./index.js), this evaluates compiled .arb rules
// directly in-process via WebAssembly — no control-plane round-trip. Run
// `./build-wasm.sh` first to produce ./wasm/{arbiter.wasm,wasm_exec.js,loader.js}.
//
//   const arbiter = require("@arbiter/sdk-node/local");
//   await arbiter.init();
//   arbiter.compile(`rule Big { when { score > 10 } then Flag {} }`);
//   arbiter.evalGoverned(JSON.stringify({ score: 20 }));
"use strict";

const path = require("path");

const wasmDir = path.join(__dirname, "..", "wasm");
const loader = require(path.join(wasmDir, "loader.js"));

let ready = false;

// init loads the embedded WASM module. wasmPath defaults to the bundled
// ./wasm/arbiter.wasm produced by build-wasm.sh.
async function init(wasmPath) {
  if (ready) return;
  await loader.init(wasmPath || path.join(wasmDir, "arbiter.wasm"));
  ready = true;
}

module.exports = {
  init,
  compile: (source) => loader.compile(source),
  loadBundle: (base64Bundle) => loader.loadBundle(base64Bundle),
  eval: (jsonContext) => loader.eval(jsonContext),
  evalGoverned: (jsonContext) => loader.evalGoverned(jsonContext),
  evalStrategy: (name, jsonContext) => loader.evalStrategy(name, jsonContext),
  startSession: (...args) => loader.startSession(...args),
  assertFact: (...args) => loader.assertFact(...args),
  retractFact: (...args) => loader.retractFact(...args),
  runSession: (...args) => loader.runSession(...args),
  closeSession: (...args) => loader.closeSession(...args),
};
