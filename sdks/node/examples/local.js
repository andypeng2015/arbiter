// Local in-process evaluation — no server. Run `../build-wasm.sh` first.
//
//   node examples/local.js
const arbiter = require("../src/local.js");

(async () => {
  await arbiter.init();

  const compiled = arbiter.compile(
    `rule BigOrder { when { total >= 100 } then Flag { tier: "vip" } }`,
  );
  if (compiled.error) {
    console.error("compile error:", compiled.error);
    process.exit(1);
  }

  const matched = arbiter.evalGoverned(JSON.stringify({ total: 250 }));
  console.log("matched:", JSON.stringify(matched));
})();
