// Arbiter WASM SDK
//
// All four evaluation modes in 3.6MB gzipped.
//
// Usage (Node.js):
//   const arbiter = require('./loader.js');
//   await arbiter.init('./arbiter.wasm');
//
//   // Stateless rules
//   arbiter.compile('rule X { when { a > 1 } then Y { z: 1 } }');
//   arbiter.eval('{"a": 5}');
//
//   // Expert inference
//   const sid = arbiter.startSession('{"temp": 30}');
//   arbiter.assertFact(sid, '{"type":"Reading","key":"r1","fields":{"value":30}}');
//   const result = arbiter.runSession(sid);
//   arbiter.closeSession(sid);
//
//   // Workflows
//   arbiter.compileWorkflow(source);
//   arbiter.setSourceFacts('transaction', '[...]');
//   arbiter.runWorkflow();

(function (exports) {
  "use strict";

  let _ready = false;

  exports.init = async function init(wasmPath) {
    if (_ready) return;

    if (typeof Go === "undefined" && typeof require !== "undefined") {
      require("./wasm_exec.js");
    }

    const go = new Go();
    let wasm;

    // Node (incl. v21+, which has a global fetch that cannot load local file
    // paths) reads the module from disk; browsers stream it over fetch.
    const isNode =
      typeof process !== "undefined" &&
      process.versions != null &&
      process.versions.node != null;
    if (!isNode && typeof fetch === "function") {
      const resp = await fetch(wasmPath);
      const result = await WebAssembly.instantiateStreaming(resp, go.importObject);
      wasm = result.instance;
    } else {
      const fs = require("fs");
      const buf = fs.readFileSync(wasmPath);
      const result = await WebAssembly.instantiate(buf, go.importObject);
      wasm = result.instance;
    }

    go.run(wasm);
    _ready = true;
  };

  function check() {
    if (!_ready) throw new Error("arbiter: call init() first");
  }

  // --- Compilation ---

  exports.compile = function (source) {
    check();
    return globalThis.arbiterCompile(source);
  };

  /** Load a pre-compiled bundle (base64 string). No .arb source needed. */
  exports.loadBundle = function (base64Bundle) {
    check();
    return globalThis.arbiterLoadBundle(base64Bundle);
  };

  // --- Stateless Evaluation ---

  exports.eval = function (jsonContext) {
    check();
    return globalThis.arbiterEval(jsonContext);
  };

  exports.evalGoverned = function (jsonContext) {
    check();
    return globalThis.arbiterEvalGoverned(jsonContext);
  };

  exports.evalStrategy = function (name, jsonContext) {
    check();
    return globalThis.arbiterEvalStrategy(name, jsonContext);
  };

  // --- Expert Sessions ---

  exports.startSession = function (jsonEnvelope, jsonFacts) {
    check();
    if (jsonFacts) {
      return globalThis.arbiterStartSession(jsonEnvelope, jsonFacts);
    }
    return globalThis.arbiterStartSession(jsonEnvelope);
  };

  exports.assertFact = function (sessionId, jsonFact) {
    check();
    return globalThis.arbiterAssertFact(sessionId, jsonFact);
  };

  exports.retractFact = function (sessionId, factType, factKey) {
    check();
    return globalThis.arbiterRetractFact(sessionId, factType, factKey);
  };

  exports.runSession = function (sessionId) {
    check();
    return globalThis.arbiterRunSession(sessionId);
  };

  exports.closeSession = function (sessionId) {
    check();
    return globalThis.arbiterCloseSession(sessionId);
  };

  // --- Workflows ---

  exports.compileWorkflow = function (source) {
    check();
    return globalThis.arbiterCompileWorkflow(source);
  };

  exports.setSourceFacts = function (target, jsonFacts) {
    check();
    return globalThis.arbiterSetSourceFacts(target, jsonFacts);
  };

  exports.runWorkflow = function () {
    check();
    return globalThis.arbiterRunWorkflow();
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = exports;
  } else {
    globalThis.arbiter = exports;
  }
})({});
