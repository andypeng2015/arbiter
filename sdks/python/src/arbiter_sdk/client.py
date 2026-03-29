from __future__ import annotations

import time
from collections.abc import Mapping, Sequence
from typing import Any

import grpc
from google.protobuf import struct_pb2, wrappers_pb2

from arbiter.v1 import service_pb2, service_pb2_grpc

_DEFAULT_RETRYABLE_CODES = (
    grpc.StatusCode.UNAVAILABLE,
    grpc.StatusCode.DEADLINE_EXCEEDED,
)


def _to_struct(data: Mapping[str, Any] | None) -> struct_pb2.Struct:
    message = struct_pb2.Struct()
    if data:
        message.update(dict(data))
    return message


def _normalize_target(target: str, secure: bool | None) -> tuple[str, bool]:
    if target.startswith("https://"):
        return target.removeprefix("https://"), True
    if target.startswith("grpcs://"):
        return target.removeprefix("grpcs://"), True
    if target.startswith("http://"):
        return target.removeprefix("http://"), False
    if target.startswith("grpc://"):
        return target.removeprefix("grpc://"), False
    return target, bool(secure)


def _normalize_metadata(
    metadata: Mapping[str, Any] | Sequence[tuple[str, Any]] | None,
    token: str | None,
) -> tuple[tuple[str, str], ...]:
    items: list[tuple[str, str]] = []
    if token:
        items.append(("authorization", f"Bearer {token}"))
    if metadata is None:
        return tuple(items)
    iterable = metadata.items() if isinstance(metadata, Mapping) else metadata
    for key, value in iterable:
        items.append((str(key), str(value)))
    return tuple(items)


class ArbiterClient:
    def __init__(
        self,
        target: str,
        *,
        channel: grpc.Channel | None = None,
        options: Sequence[tuple[str, Any]] = (),
        token: str | None = None,
        metadata: Mapping[str, Any] | Sequence[tuple[str, Any]] | None = None,
        secure: bool | None = None,
        root_certificates: bytes | None = None,
        private_key: bytes | None = None,
        certificate_chain: bytes | None = None,
        server_name_override: str | None = None,
        retry_attempts: int = 3,
        initial_backoff: float = 0.1,
        max_backoff: float = 1.0,
        backoff_multiplier: float = 2.0,
        retryable_status_codes: Sequence[grpc.StatusCode] = _DEFAULT_RETRYABLE_CODES,
    ) -> None:
        self._metadata = _normalize_metadata(metadata, token)
        self._retry_attempts = max(1, retry_attempts)
        self._initial_backoff = max(0.0, initial_backoff)
        self._max_backoff = max(self._initial_backoff, max_backoff)
        self._backoff_multiplier = max(1.0, backoff_multiplier)
        self._retryable_status_codes = tuple(retryable_status_codes)
        if channel is None:
            secure = secure if secure is not None else bool(
                root_certificates or private_key or certificate_chain or server_name_override
            )
            normalized_target, use_tls = _normalize_target(target, secure)
            channel_options = list(options)
            if server_name_override:
                channel_options.append(("grpc.ssl_target_name_override", server_name_override))
                channel_options.append(("grpc.default_authority", server_name_override))
            if use_tls:
                credentials = grpc.ssl_channel_credentials(
                    root_certificates=root_certificates,
                    private_key=private_key,
                    certificate_chain=certificate_chain,
                )
                self._channel = grpc.secure_channel(normalized_target, credentials, options=tuple(channel_options))
            else:
                self._channel = grpc.insecure_channel(normalized_target, options=tuple(channel_options))
        else:
            self._channel = channel
        self._owns_channel = channel is None
        self.stub = service_pb2_grpc.ArbiterServiceStub(self._channel)

    def close(self) -> None:
        if self._owns_channel:
            self._channel.close()

    def __enter__(self) -> "ArbiterClient":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def _invoke(self, rpc: Any, request: Any) -> Any:
        backoff = self._initial_backoff
        for attempt in range(1, self._retry_attempts + 1):
            try:
                return rpc(request, metadata=self._metadata or None)
            except grpc.RpcError as exc:
                if attempt >= self._retry_attempts or exc.code() not in self._retryable_status_codes:
                    raise
                if backoff > 0:
                    time.sleep(backoff)
                backoff = min(self._max_backoff, backoff * self._backoff_multiplier)
        raise RuntimeError("exhausted retries")

    def publish_bundle(self, name: str, source: str | bytes) -> service_pb2.PublishBundleResponse:
        payload = source.encode("utf-8") if isinstance(source, str) else source
        return self._invoke(self.stub.PublishBundle, service_pb2.PublishBundleRequest(name=name, source=payload))

    def list_bundles(self, *, name: str = "") -> service_pb2.ListBundlesResponse:
        return self._invoke(self.stub.ListBundles, service_pb2.ListBundlesRequest(name=name))

    def activate_bundle(self, name: str, bundle_id: str) -> service_pb2.ActivateBundleResponse:
        return self._invoke(
            self.stub.ActivateBundle,
            service_pb2.ActivateBundleRequest(name=name, bundle_id=bundle_id),
        )

    def rollback_bundle(self, name: str) -> service_pb2.RollbackBundleResponse:
        return self._invoke(self.stub.RollbackBundle, service_pb2.RollbackBundleRequest(name=name))

    def get_bundle(self, *, bundle_id: str = "", bundle_name: str = "") -> service_pb2.GetBundleResponse:
        return self._invoke(
            self.stub.GetBundle,
            service_pb2.GetBundleRequest(bundle_id=bundle_id, bundle_name=bundle_name),
        )

    def watch_bundles(self, *, names: Sequence[str] = (), active_only: bool = False):
        return self.stub.WatchBundles(
            service_pb2.WatchBundlesRequest(names=list(names), active_only=active_only),
            metadata=self._metadata or None,
        )

    def get_overrides(self, *, bundle_id: str = "", bundle_name: str = "") -> service_pb2.GetOverridesResponse:
        return self._invoke(
            self.stub.GetOverrides,
            service_pb2.GetOverridesRequest(bundle_id=bundle_id, bundle_name=bundle_name),
        )

    def watch_overrides(self, *, bundle_id: str):
        return self.stub.WatchOverrides(
            service_pb2.WatchOverridesRequest(bundle_id=bundle_id),
            metadata=self._metadata or None,
        )

    def evaluate_rules(
        self,
        *,
        bundle_id: str = "",
        bundle_name: str = "",
        context: Mapping[str, Any] | None = None,
        request_id: str = "",
    ) -> service_pb2.EvaluateRulesResponse:
        return self._invoke(
            self.stub.EvaluateRules,
            service_pb2.EvaluateRulesRequest(
                bundle_id=bundle_id,
                bundle_name=bundle_name,
                context=_to_struct(context),
                request_id=request_id,
            ),
        )

    def resolve_flag(
        self,
        flag_key: str,
        *,
        bundle_id: str = "",
        bundle_name: str = "",
        context: Mapping[str, Any] | None = None,
        request_id: str = "",
    ) -> service_pb2.ResolveFlagResponse:
        return self._invoke(
            self.stub.ResolveFlag,
            service_pb2.ResolveFlagRequest(
                bundle_id=bundle_id,
                bundle_name=bundle_name,
                flag_key=flag_key,
                context=_to_struct(context),
                request_id=request_id,
            ),
        )

    def evaluate_strategy(
        self,
        strategy_name: str,
        *,
        bundle_id: str = "",
        bundle_name: str = "",
        context: Mapping[str, Any] | None = None,
        request_id: str = "",
    ) -> service_pb2.EvaluateStrategyResponse:
        return self._invoke(
            self.stub.EvaluateStrategy,
            service_pb2.EvaluateStrategyRequest(
                bundle_id=bundle_id,
                bundle_name=bundle_name,
                strategy_name=strategy_name,
                context=_to_struct(context),
                request_id=request_id,
            ),
        )

    def start_session(
        self,
        *,
        bundle_id: str = "",
        bundle_name: str = "",
        envelope: Mapping[str, Any] | None = None,
        facts: Sequence[Mapping[str, Any]] | None = None,
    ) -> service_pb2.StartSessionResponse:
        items = [
            service_pb2.ExpertFact(
                type=str(fact["type"]),
                key=str(fact["key"]),
                fields=_to_struct(fact.get("fields")),
            )
            for fact in (facts or [])
        ]
        return self._invoke(
            self.stub.StartSession,
            service_pb2.StartSessionRequest(
                bundle_id=bundle_id,
                bundle_name=bundle_name,
                envelope=_to_struct(envelope),
                facts=items,
            ),
        )

    def run_session(self, session_id: str, *, request_id: str = "") -> service_pb2.RunSessionResponse:
        return self._invoke(
            self.stub.RunSession,
            service_pb2.RunSessionRequest(session_id=session_id, request_id=request_id),
        )

    def assert_facts(self, session_id: str, facts: Sequence[Mapping[str, Any]]) -> service_pb2.AssertFactsResponse:
        items = [
            service_pb2.ExpertFact(
                type=str(fact["type"]),
                key=str(fact["key"]),
                fields=_to_struct(fact.get("fields")),
            )
            for fact in facts
        ]
        return self._invoke(
            self.stub.AssertFacts,
            service_pb2.AssertFactsRequest(session_id=session_id, facts=items),
        )

    def retract_facts(self, session_id: str, facts: Sequence[Mapping[str, Any]]) -> service_pb2.RetractFactsResponse:
        items = [service_pb2.FactRef(type=str(fact["type"]), key=str(fact["key"])) for fact in facts]
        return self._invoke(
            self.stub.RetractFacts,
            service_pb2.RetractFactsRequest(session_id=session_id, facts=items),
        )

    def get_session_trace(self, session_id: str) -> service_pb2.GetSessionTraceResponse:
        return self._invoke(
            self.stub.GetSessionTrace,
            service_pb2.GetSessionTraceRequest(session_id=session_id),
        )

    def close_session(self, session_id: str) -> service_pb2.CloseSessionResponse:
        return self._invoke(self.stub.CloseSession, service_pb2.CloseSessionRequest(session_id=session_id))

    def set_rule_override(
        self,
        bundle_id: str,
        rule_name: str,
        *,
        kill_switch: bool | None = None,
        rollout: int | None = None,
    ) -> service_pb2.SetRuleOverrideResponse:
        request = service_pb2.SetRuleOverrideRequest(bundle_id=bundle_id, rule_name=rule_name)
        if kill_switch is not None:
            request.kill_switch.CopyFrom(wrappers_pb2.BoolValue(value=kill_switch))
        if rollout is not None:
            request.rollout.CopyFrom(wrappers_pb2.UInt32Value(value=rollout))
        return self._invoke(self.stub.SetRuleOverride, request)

    def set_flag_override(
        self,
        bundle_id: str,
        flag_key: str,
        *,
        kill_switch: bool | None = None,
    ) -> service_pb2.SetFlagOverrideResponse:
        request = service_pb2.SetFlagOverrideRequest(bundle_id=bundle_id, flag_key=flag_key)
        if kill_switch is not None:
            request.kill_switch.CopyFrom(wrappers_pb2.BoolValue(value=kill_switch))
        return self._invoke(self.stub.SetFlagOverride, request)

    def set_flag_rule_override(
        self,
        bundle_id: str,
        flag_key: str,
        rule_index: int,
        *,
        rollout: int | None = None,
    ) -> service_pb2.SetFlagRuleOverrideResponse:
        request = service_pb2.SetFlagRuleOverrideRequest(
            bundle_id=bundle_id,
            flag_key=flag_key,
            rule_index=rule_index,
        )
        if rollout is not None:
            request.rollout.CopyFrom(wrappers_pb2.UInt32Value(value=rollout))
        return self._invoke(self.stub.SetFlagRuleOverride, request)

    def set_strategy_override(
        self,
        bundle_id: str,
        strategy_name: str,
        candidate_label: str,
        *,
        kill_switch: bool | None = None,
        rollout: int | None = None,
    ) -> service_pb2.SetStrategyOverrideResponse:
        request = service_pb2.SetStrategyOverrideRequest(
            bundle_id=bundle_id,
            strategy_name=strategy_name,
            candidate_label=candidate_label,
        )
        if kill_switch is not None:
            request.kill_switch.CopyFrom(wrappers_pb2.BoolValue(value=kill_switch))
        if rollout is not None:
            request.rollout.CopyFrom(wrappers_pb2.UInt32Value(value=rollout))
        return self._invoke(self.stub.SetStrategyOverride, request)


class RuntimeClient:
    def __init__(
        self,
        target: str,
        *,
        channel: grpc.Channel | None = None,
        options: Sequence[tuple[str, Any]] = (),
        token: str | None = None,
        metadata: Mapping[str, Any] | Sequence[tuple[str, Any]] | None = None,
        secure: bool | None = None,
        root_certificates: bytes | None = None,
        private_key: bytes | None = None,
        certificate_chain: bytes | None = None,
        server_name_override: str | None = None,
        retry_attempts: int = 3,
        initial_backoff: float = 0.1,
        max_backoff: float = 1.0,
        backoff_multiplier: float = 2.0,
        retryable_status_codes: Sequence[grpc.StatusCode] = _DEFAULT_RETRYABLE_CODES,
    ) -> None:
        self._metadata = _normalize_metadata(metadata, token)
        self._retry_attempts = max(1, retry_attempts)
        self._initial_backoff = max(0.0, initial_backoff)
        self._max_backoff = max(self._initial_backoff, max_backoff)
        self._backoff_multiplier = max(1.0, backoff_multiplier)
        self._retryable_status_codes = tuple(retryable_status_codes)
        if channel is None:
            secure = secure if secure is not None else bool(
                root_certificates or private_key or certificate_chain or server_name_override
            )
            normalized_target, use_tls = _normalize_target(target, secure)
            channel_options = list(options)
            if server_name_override:
                channel_options.append(("grpc.ssl_target_name_override", server_name_override))
                channel_options.append(("grpc.default_authority", server_name_override))
            if use_tls:
                credentials = grpc.ssl_channel_credentials(
                    root_certificates=root_certificates,
                    private_key=private_key,
                    certificate_chain=certificate_chain,
                )
                self._channel = grpc.secure_channel(normalized_target, credentials, options=tuple(channel_options))
            else:
                self._channel = grpc.insecure_channel(normalized_target, options=tuple(channel_options))
        else:
            self._channel = channel
        self._owns_channel = channel is None
        self.stub = service_pb2_grpc.RuntimeServiceStub(self._channel)

    def close(self) -> None:
        if self._owns_channel:
            self._channel.close()

    def __enter__(self) -> "RuntimeClient":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def _invoke(self, rpc: Any, request: Any) -> Any:
        backoff = self._initial_backoff
        for attempt in range(1, self._retry_attempts + 1):
            try:
                return rpc(request, metadata=self._metadata or None)
            except grpc.RpcError as exc:
                if attempt >= self._retry_attempts or exc.code() not in self._retryable_status_codes:
                    raise
                if backoff > 0:
                    time.sleep(backoff)
                backoff = min(self._max_backoff, backoff * self._backoff_multiplier)
        raise RuntimeError("exhausted retries")

    def get_runtime_capabilities(self) -> service_pb2.GetRuntimeCapabilitiesResponse:
        return self._invoke(
            self.stub.GetRuntimeCapabilities,
            service_pb2.GetRuntimeCapabilitiesRequest(),
        )

    def get_runtime_status(self) -> service_pb2.GetRuntimeStatusResponse:
        return self._invoke(
            self.stub.GetRuntimeStatus,
            service_pb2.GetRuntimeStatusRequest(),
        )


class AgentClient:
    def __init__(
        self,
        target: str,
        *,
        channel: grpc.Channel | None = None,
        options: Sequence[tuple[str, Any]] = (),
        token: str | None = None,
        metadata: Mapping[str, Any] | Sequence[tuple[str, Any]] | None = None,
        secure: bool | None = None,
        root_certificates: bytes | None = None,
        private_key: bytes | None = None,
        certificate_chain: bytes | None = None,
        server_name_override: str | None = None,
        retry_attempts: int = 3,
        initial_backoff: float = 0.1,
        max_backoff: float = 1.0,
        backoff_multiplier: float = 2.0,
        retryable_status_codes: Sequence[grpc.StatusCode] = _DEFAULT_RETRYABLE_CODES,
    ) -> None:
        self._metadata = _normalize_metadata(metadata, token)
        self._retry_attempts = max(1, retry_attempts)
        self._initial_backoff = max(0.0, initial_backoff)
        self._max_backoff = max(self._initial_backoff, max_backoff)
        self._backoff_multiplier = max(1.0, backoff_multiplier)
        self._retryable_status_codes = tuple(retryable_status_codes)
        if channel is None:
            secure = secure if secure is not None else bool(
                root_certificates or private_key or certificate_chain or server_name_override
            )
            normalized_target, use_tls = _normalize_target(target, secure)
            channel_options = list(options)
            if server_name_override:
                channel_options.append(("grpc.ssl_target_name_override", server_name_override))
                channel_options.append(("grpc.default_authority", server_name_override))
            if use_tls:
                credentials = grpc.ssl_channel_credentials(
                    root_certificates=root_certificates,
                    private_key=private_key,
                    certificate_chain=certificate_chain,
                )
                self._channel = grpc.secure_channel(normalized_target, credentials, options=tuple(channel_options))
            else:
                self._channel = grpc.insecure_channel(normalized_target, options=tuple(channel_options))
        else:
            self._channel = channel
        self._owns_channel = channel is None
        self.stub = service_pb2_grpc.AgentServiceStub(self._channel)

    def close(self) -> None:
        if self._owns_channel:
            self._channel.close()

    def __enter__(self) -> "AgentClient":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def _invoke(self, rpc: Any, request: Any) -> Any:
        backoff = self._initial_backoff
        for attempt in range(1, self._retry_attempts + 1):
            try:
                return rpc(request, metadata=self._metadata or None)
            except grpc.RpcError as exc:
                if attempt >= self._retry_attempts or exc.code() not in self._retryable_status_codes:
                    raise
                if backoff > 0:
                    time.sleep(backoff)
                backoff = min(self._max_backoff, backoff * self._backoff_multiplier)
        raise RuntimeError("exhausted retries")

    def get_agent_status(self) -> service_pb2.GetAgentStatusResponse:
        return self._invoke(
            self.stub.GetAgentStatus,
            service_pb2.GetAgentStatusRequest(),
        )
