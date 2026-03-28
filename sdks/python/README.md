# Arbiter Python SDK

Python client for the Arbiter gRPC API with bearer-token metadata and bounded retries on transient unary failures.

## Install

```bash
pip install -e .
```

## Example

```python
from arbiter_sdk import ArbiterClient

with ArbiterClient("http://127.0.0.1:8081") as client:
    publish = client.publish_bundle("checkout", b'rule Approve { when { true } then Ok {} }')
    result = client.evaluate_rules(
        bundle_name="checkout",
        context={"user": {"score": 720}},
    )
    print(publish.bundle_id, len(result.matched))
```

For TLS endpoints, use an `https://` target and pass a token:

```python
with ArbiterClient("https://arbiter.internal:443", token="...", secure=True) as client:
    ...
```

See [examples/smoke.py](/home/draco/work/arbiter/sdks/python/examples/smoke.py) for a runnable end-to-end example.
