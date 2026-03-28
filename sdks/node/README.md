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

See [src/index.js](/home/draco/work/arbiter/sdks/node/src/index.js) and [examples/smoke.js](/home/draco/work/arbiter/sdks/node/examples/smoke.js).
