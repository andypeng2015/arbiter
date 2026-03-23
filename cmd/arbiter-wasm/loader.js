// Arbiter WASM Loader
//
// Usage (Node.js):
//   const arbiter = require('./loader.js');
//   await arbiter.init('./arbiter.wasm');
//   arbiter.compile('rule X { when { a > 1 } then Y { z: 1 } }');
//   const result = arbiter.eval('{"a": 5}');
//
// Usage (Browser):
//   <script src="wasm_exec.js"></script>
//   <script src="loader.js"></script>
//   await arbiter.init('arbiter.wasm');
//   arbiter.compile('rule X { when { a > 1 } then Y { z: 1 } }');

(function (exports) {
  "use strict";

  let _ready = false;

  exports.init = async function init(wasmPath) {
    if (_ready) return;

    // Node.js: load wasm_exec.js if Go global isn't present.
    if (typeof Go === "undefined" && typeof require !== "undefined") {
      require("./wasm_exec.js");
    }

    const go = new Go();
    let wasm;

    if (typeof fetch === "function") {
      // Browser / Deno
      const resp = await fetch(wasmPath);
      const result = await WebAssembly.instantiateStreaming(resp, go.importObject);
      wasm = result.instance;
    } else {
      // Node.js
      const fs = require("fs");
      const buf = fs.readFileSync(wasmPath);
      const result = await WebAssembly.instantiate(buf, go.importObject);
      wasm = result.instance;
    }

    go.run(wasm); // Starts the Go runtime; blocks via select{}.
    _ready = true;
  };

  exports.compile = function compile(source) {
    if (!_ready) throw new Error("arbiter: call init() first");
    return globalThis.arbiterCompile(source);
  };

  exports.eval = function eval(jsonContext) {
    if (!_ready) throw new Error("arbiter: call init() first");
    return globalThis.arbiterEval(jsonContext);
  };

  exports.evalGoverned = function evalGoverned(jsonContext) {
    if (!_ready) throw new Error("arbiter: call init() first");
    return globalThis.arbiterEvalGoverned(jsonContext);
  };

  exports.evalStrategy = function evalStrategy(name, jsonContext) {
    if (!_ready) throw new Error("arbiter: call init() first");
    return globalThis.arbiterEvalStrategy(name, jsonContext);
  };

  // CommonJS / ESM / Browser global
  if (typeof module !== "undefined" && module.exports) {
    module.exports = exports;
  } else {
    globalThis.arbiter = exports;
  }
})({});
