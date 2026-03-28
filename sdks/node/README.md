# Arbiter Node SDK

Node client for the Arbiter gRPC API with bearer-token metadata and bounded retries on transient unary failures.

## Install

```bash
npm install
```

## Example

```js
const { ArbiterClient } = require("./src");

async function main() {
  const client = new ArbiterClient("http://127.0.0.1:8081");
  const publish = await client.publishBundle({
    name: "checkout",
    source: 'rule Approve { when { true } then Ok {} }',
  });
  const result = await client.evaluateRules({
    bundleName: "checkout",
    context: { user: { score: 720 } },
  });
  console.log(publish.bundleId, result.matched.length);
  client.close();
}
```

For a managed or TLS-terminated endpoint, pass an `https://` target and a token:

```js
const client = new ArbiterClient("https://arbiter.internal:443", {
  token: process.env.ARBITER_TOKEN,
});
```

The runtime control surface is separate from the bundle/eval API. Use `RuntimeClient`
to inspect one `arbiter-runtime` instance:

```js
const { RuntimeClient } = require("./src");

async function main() {
  const runtime = new RuntimeClient("http://127.0.0.1:7081");
  const caps = await runtime.getRuntimeCapabilities();
  console.log(caps.sources.map(item => `${item.scheme}:${item.owner}`));
  runtime.close();
}
```

## Capability Plugins

```js
const grpc = require("@grpc/grpc-js");
const { CapabilityServer } = require("./src");

const plugin = new CapabilityServer({ name: "ops-plugin", version: "1.0.0" })
  .registerSource("kafka", target => [{
    type: "OrderEvent",
    key: "evt-1",
    fields: { topic: target, status: "new" },
  }], { description: "load facts from kafka topics" })
  .registerSink("discord", delivery => {
    console.log("deliver", delivery.outcome.name, "to", delivery.handler_target);
  }, { description: "post governed outcomes to discord" })
  .registerWorker("python", ({ worker, delivery }) => ({
    facts: [{
      type: worker.output,
      key: delivery.outcome.params.key,
      fields: { status: "sent" },
    }],
  }), { description: "delegate worker execution to python" });

plugin.listen("127.0.0.1:7090", grpc.ServerCredentials.createInsecure());
```

See [src/index.js](/home/draco/work/arbiter/sdks/node/src/index.js) and [examples/smoke.js](/home/draco/work/arbiter/sdks/node/examples/smoke.js).
