from __future__ import annotations

from collections.abc import Mapping, Sequence
from concurrent import futures
from datetime import datetime, timezone
from typing import Any

import grpc
from google.protobuf import struct_pb2

from arbiter.v1 import capability_pb2, capability_pb2_grpc, service_pb2

_RESERVED_SOURCE_SCHEMES = {"chain", "worker"}
_RESERVED_HANDLER_KINDS = {"chain", "worker"}


def _to_struct(data: Mapping[str, Any] | None) -> struct_pb2.Struct:
    message = struct_pb2.Struct()
    if data:
        message.update(dict(data))
    return message


def _normalize_identifier(value: str, label: str, *, lower: bool = False) -> str:
    normalized = value.strip()
    if not normalized:
        raise ValueError(f"{label} must be non-empty")
    return normalized.lower() if lower else normalized


def _source_scheme(target: str) -> str:
    index = target.find("://")
    if index <= 0:
        return ""
    return target[:index].lower()


def _proto_timestamp_to_datetime(value: Any) -> datetime | None:
    if value is None:
        return None
    if hasattr(value, "seconds") and value.seconds == 0 and value.nanos == 0:
        return None
    dt = value.ToDatetime()
    if dt.tzinfo is None:
        return dt.replace(tzinfo=timezone.utc)
    return dt


def _outcome_to_dict(item: service_pb2.ExpertOutcome | None) -> dict[str, Any] | None:
    if item is None:
        return None
    return {
        "rule": item.rule,
        "name": item.name,
        "params": dict(item.params) if item.HasField("params") else {},
    }


def _delivery_to_dict(item: capability_pb2.DeliveryContext | None) -> dict[str, Any] | None:
    if item is None:
        return None
    return {
        "delivery_id": item.delivery_id,
        "arbiter_name": item.arbiter_name,
        "worker_name": item.worker_name,
        "handler_kind": item.handler_kind,
        "handler_target": item.handler_target,
        "outcome": _outcome_to_dict(item.outcome if item.HasField("outcome") else None),
        "attempt": item.attempt,
        "enqueued_at": _proto_timestamp_to_datetime(item.enqueued_at),
        "last_attempt_at": _proto_timestamp_to_datetime(item.last_attempt_at),
        "next_attempt_at": _proto_timestamp_to_datetime(item.next_attempt_at),
        "last_error": item.last_error,
    }


def _worker_to_dict(item: capability_pb2.WorkerSpec | None) -> dict[str, Any] | None:
    if item is None:
        return None
    return {
        "name": item.name,
        "input": item.input,
        "output": item.output,
        "output_kind": capability_pb2.WorkerOutputKind.Name(item.output_kind),
        "kind": item.kind,
        "target": item.target,
    }


def _fact_message(item: service_pb2.ExpertFact | Mapping[str, Any]) -> service_pb2.ExpertFact:
    if isinstance(item, service_pb2.ExpertFact):
        return item
    return service_pb2.ExpertFact(
        type=str(item["type"]),
        key=str(item["key"]),
        fields=_to_struct(item.get("fields")),
    )


def _outcome_message(item: service_pb2.ExpertOutcome | Mapping[str, Any]) -> service_pb2.ExpertOutcome:
    if isinstance(item, service_pb2.ExpertOutcome):
        return item
    return service_pb2.ExpertOutcome(
        rule=str(item.get("rule", "")),
        name=str(item["name"]),
        params=_to_struct(item.get("params")),
    )


def _coerce_fact_result(result: Any) -> list[service_pb2.ExpertFact]:
    if result is None:
        return []
    if isinstance(result, capability_pb2.LoadSourceResponse):
        return list(result.facts)
    if isinstance(result, Mapping) and "facts" in result:
        result = result["facts"]
    if not isinstance(result, Sequence) or isinstance(result, (str, bytes, bytearray)):
        raise TypeError("source handler must return a sequence of facts or a response with facts")
    return [_fact_message(item) for item in result]


def _coerce_worker_result(result: Any) -> tuple[list[service_pb2.ExpertFact], list[service_pb2.ExpertOutcome]]:
    if result is None:
        return [], []
    if isinstance(result, capability_pb2.ExecuteWorkerResponse):
        return list(result.facts), list(result.outcomes)
    if not isinstance(result, Mapping):
        raise TypeError("worker handler must return a mapping with optional facts/outcomes or an ExecuteWorkerResponse")
    facts = result.get("facts", ())
    outcomes = result.get("outcomes", ())
    if not isinstance(facts, Sequence) or isinstance(facts, (str, bytes, bytearray)):
        raise TypeError("worker result facts must be a sequence")
    if not isinstance(outcomes, Sequence) or isinstance(outcomes, (str, bytes, bytearray)):
        raise TypeError("worker result outcomes must be a sequence")
    return [_fact_message(item) for item in facts], [_outcome_message(item) for item in outcomes]


class CapabilityServer(capability_pb2_grpc.CapabilityServiceServicer):
    def __init__(self, *, name: str = "", version: str = "") -> None:
        self.name = str(name)
        self.version = str(version)
        self._sources: dict[str, dict[str, Any]] = {}
        self._sinks: dict[str, dict[str, Any]] = {}
        self._workers: dict[str, dict[str, Any]] = {}

    def register_source(self, scheme: str, handler: Any, *, description: str = "") -> "CapabilityServer":
        normalized = _normalize_identifier(scheme, "source scheme", lower=True)
        if normalized in _RESERVED_SOURCE_SCHEMES:
            raise ValueError(f"source scheme {normalized} is reserved")
        if not callable(handler):
            raise TypeError("source handler must be callable")
        self._sources[normalized] = {"description": str(description), "handler": handler}
        return self

    def register_sink(self, kind: str, handler: Any, *, description: str = "") -> "CapabilityServer":
        normalized = _normalize_identifier(kind, "sink kind")
        if normalized in _RESERVED_HANDLER_KINDS:
            raise ValueError(f"sink kind {normalized} is reserved")
        if not callable(handler):
            raise TypeError("sink handler must be callable")
        self._sinks[normalized] = {"description": str(description), "handler": handler}
        return self

    def register_worker(self, kind: str, handler: Any, *, description: str = "") -> "CapabilityServer":
        normalized = _normalize_identifier(kind, "worker kind")
        if normalized in _RESERVED_HANDLER_KINDS:
            raise ValueError(f"worker kind {normalized} is reserved")
        if not callable(handler):
            raise TypeError("worker handler must be callable")
        self._workers[normalized] = {"description": str(description), "handler": handler}
        return self

    def add_to_server(self, server: grpc.Server) -> grpc.Server:
        capability_pb2_grpc.add_CapabilityServiceServicer_to_server(self, server)
        return server

    def serve(self, target: str, *, max_workers: int = 10, server: grpc.Server | None = None) -> grpc.Server:
        bound = server or grpc.server(futures.ThreadPoolExecutor(max_workers=max_workers))
        self.add_to_server(bound)
        bound.add_insecure_port(target)
        bound.start()
        return bound

    def manifest(self) -> capability_pb2.GetCapabilitiesResponse:
        return capability_pb2.GetCapabilitiesResponse(
            name=self.name,
            version=self.version,
            sources=[
                capability_pb2.SourceCapability(scheme=scheme, description=item["description"])
                for scheme, item in sorted(self._sources.items())
            ],
            sinks=[
                capability_pb2.SinkCapability(kind=kind, description=item["description"])
                for kind, item in sorted(self._sinks.items())
            ],
            workers=[
                capability_pb2.WorkerCapability(kind=kind, description=item["description"])
                for kind, item in sorted(self._workers.items())
            ],
        )

    def GetCapabilities(
        self, _request: capability_pb2.GetCapabilitiesRequest, _context: grpc.ServicerContext
    ) -> capability_pb2.GetCapabilitiesResponse:
        return self.manifest()

    def LoadSource(
        self, request: capability_pb2.LoadSourceRequest, context: grpc.ServicerContext
    ) -> capability_pb2.LoadSourceResponse:
        scheme = _source_scheme(request.target)
        item = self._sources.get(scheme)
        if item is None:
            context.abort(grpc.StatusCode.UNIMPLEMENTED, f"no source handler registered for scheme {scheme or '<none>'}")
        facts = _coerce_fact_result(item["handler"](request.target, context))
        return capability_pb2.LoadSourceResponse(facts=facts)

    def DeliverOutcome(
        self, request: capability_pb2.DeliverOutcomeRequest, context: grpc.ServicerContext
    ) -> capability_pb2.DeliverOutcomeResponse:
        kind = request.delivery.handler_kind
        item = self._sinks.get(kind)
        if item is None:
            context.abort(grpc.StatusCode.UNIMPLEMENTED, f"no sink handler registered for kind {kind or '<none>'}")
        item["handler"](_delivery_to_dict(request.delivery), context)
        return capability_pb2.DeliverOutcomeResponse()

    def ExecuteWorker(
        self, request: capability_pb2.ExecuteWorkerRequest, context: grpc.ServicerContext
    ) -> capability_pb2.ExecuteWorkerResponse:
        kind = request.worker.kind
        item = self._workers.get(kind)
        if item is None:
            context.abort(grpc.StatusCode.UNIMPLEMENTED, f"no worker handler registered for kind {kind or '<none>'}")
        facts, outcomes = _coerce_worker_result(
            item["handler"]({"worker": _worker_to_dict(request.worker), "delivery": _delivery_to_dict(request.delivery)}, context)
        )
        return capability_pb2.ExecuteWorkerResponse(facts=facts, outcomes=outcomes)
