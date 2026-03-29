use std::{collections::HashMap, future::Future, sync::Arc, time::Duration};

use prost_types::{value::Kind, Struct, Value};
use serde_json::Value as JsonValue;
use tonic::{
    async_trait,
    metadata::{Ascii, MetadataKey, MetadataValue},
    transport::{Channel, Endpoint},
    Code, Request, Response, Status,
};

pub mod arbiter {
    pub mod v1 {
        tonic::include_proto!("arbiter.v1");
    }
}

use arbiter::v1::{
    agent_service_client::AgentServiceClient,
    arbiter_service_client::ArbiterServiceClient,
    capability_service_server::{CapabilityService, CapabilityServiceServer},
    control_service_client::ControlServiceClient,
    runtime_service_client::RuntimeServiceClient,
    ActivateBundleRequest, ActivateBundleResponse, AssertFactsRequest, AssertFactsResponse,
    BundleEvent, CloseSessionRequest, CloseSessionResponse, DeliverOutcomeRequest,
    DeliverOutcomeResponse, EvaluateRulesRequest, EvaluateRulesResponse, EvaluateStrategyRequest,
    EvaluateStrategyResponse, ExecuteWorkerRequest, ExecuteWorkerResponse, ExpertFact, FactRef,
    GetAgentStatusRequest, GetAgentStatusResponse, GetBundleRequest, GetBundleResponse,
    GetCapabilitiesRequest, GetCapabilitiesResponse, GetControlStatusRequest,
    GetControlStatusResponse, GetOverridesRequest, GetOverridesResponse,
    GetRuntimeCapabilitiesRequest, GetRuntimeCapabilitiesResponse, GetRuntimeStatusRequest,
    GetSessionTraceRequest, GetSessionTraceResponse, ListBundlesRequest, ListBundlesResponse,
    LoadSourceRequest, LoadSourceResponse, OverrideEvent, PublishBundleRequest,
    PublishBundleResponse, ResolveFlagRequest, ResolveFlagResponse, RetractFactsRequest,
    RetractFactsResponse, RollbackBundleRequest, RollbackBundleResponse, RunSessionRequest,
    RunSessionResponse, SetFlagOverrideRequest, SetFlagOverrideResponse,
    SetFlagRuleOverrideRequest, SetFlagRuleOverrideResponse, SetRuleOverrideRequest,
    SetRuleOverrideResponse, SetStrategyOverrideRequest, SetStrategyOverrideResponse,
    SinkCapability, SourceCapability, StartSessionRequest, StartSessionResponse,
    WatchBundlesRequest, WatchOverridesRequest, WorkerCapability, WorkerSpec,
};

#[derive(Clone, Debug)]
pub struct RetryPolicy {
    pub attempts: usize,
    pub initial_backoff: Duration,
    pub max_backoff: Duration,
    pub multiplier: u32,
}

impl Default for RetryPolicy {
    fn default() -> Self {
        Self {
            attempts: 3,
            initial_backoff: Duration::from_millis(100),
            max_backoff: Duration::from_secs(1),
            multiplier: 2,
        }
    }
}

#[derive(Clone)]
pub struct ArbiterClient {
    inner: ArbiterServiceClient<Channel>,
    auth_token: Option<String>,
    metadata: Vec<(String, String)>,
    retry_policy: RetryPolicy,
}

#[derive(Clone)]
pub struct RuntimeClient {
    inner: RuntimeServiceClient<Channel>,
    auth_token: Option<String>,
    metadata: Vec<(String, String)>,
    retry_policy: RetryPolicy,
}

#[derive(Clone)]
pub struct AgentClient {
    inner: AgentServiceClient<Channel>,
    auth_token: Option<String>,
    metadata: Vec<(String, String)>,
    retry_policy: RetryPolicy,
}

#[derive(Clone)]
pub struct ControlClient {
    inner: ControlServiceClient<Channel>,
    auth_token: Option<String>,
    metadata: Vec<(String, String)>,
    retry_policy: RetryPolicy,
}

impl RuntimeClient {
    pub async fn connect(dst: impl Into<String>) -> Result<Self, tonic::transport::Error> {
        let endpoint = Endpoint::from_shared(normalize_endpoint(dst.into()))?;
        let inner = RuntimeServiceClient::connect(endpoint).await?;
        Ok(Self {
            inner,
            auth_token: None,
            metadata: Vec::new(),
            retry_policy: RetryPolicy::default(),
        })
    }

    pub fn with_token(mut self, token: impl Into<String>) -> Self {
        self.auth_token = Some(token.into());
        self
    }

    pub fn with_header(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.metadata.push((key.into(), value.into()));
        self
    }

    pub fn with_retry_policy(mut self, retry_policy: RetryPolicy) -> Self {
        self.retry_policy = retry_policy;
        self
    }

    pub async fn get_runtime_capabilities(
        &self,
    ) -> Result<GetRuntimeCapabilitiesResponse, tonic::Status> {
        self.unary_with_retry(
            GetRuntimeCapabilitiesRequest {},
            |mut client, request| async move { client.get_runtime_capabilities(request).await },
        )
        .await
    }

    pub async fn get_runtime_status(
        &self,
    ) -> Result<arbiter::v1::GetRuntimeStatusResponse, tonic::Status> {
        self.unary_with_retry(
            GetRuntimeStatusRequest {},
            |mut client, request| async move { client.get_runtime_status(request).await },
        )
        .await
    }

    async fn unary_with_retry<Req, Resp, F, Fut>(
        &self,
        request: Req,
        mut call: F,
    ) -> Result<Resp, Status>
    where
        Req: Clone + Send + 'static,
        Resp: Send + 'static,
        F: FnMut(RuntimeServiceClient<Channel>, Request<Req>) -> Fut,
        Fut: Future<Output = Result<tonic::Response<Resp>, Status>>,
    {
        let attempts = self.retry_policy.attempts.max(1);
        let mut backoff = self.retry_policy.initial_backoff;
        for attempt in 1..=attempts {
            let request = self.attach_metadata(request.clone())?;
            match call(self.inner.clone(), request).await {
                Ok(response) => return Ok(response.into_inner()),
                Err(status) if attempt < attempts && should_retry(&status) => {
                    if !backoff.is_zero() {
                        tokio::time::sleep(backoff).await;
                    }
                    let next = backoff.saturating_mul(self.retry_policy.multiplier.max(1));
                    backoff = next.min(self.retry_policy.max_backoff);
                }
                Err(status) => return Err(status),
            }
        }
        Err(Status::unavailable("exhausted retries"))
    }

    fn attach_metadata<Req>(&self, message: Req) -> Result<Request<Req>, Status> {
        let mut request = Request::new(message);
        if let Some(token) = &self.auth_token {
            let value = MetadataValue::try_from(format!("Bearer {token}"))
                .map_err(|_| Status::invalid_argument("invalid auth token"))?;
            request.metadata_mut().insert("authorization", value);
        }
        for (key, value) in &self.metadata {
            let key = MetadataKey::<Ascii>::from_bytes(key.as_bytes())
                .map_err(|_| Status::invalid_argument(format!("invalid metadata key: {key}")))?;
            let value = MetadataValue::try_from(value.as_str()).map_err(|_| {
                Status::invalid_argument(format!("invalid metadata value for {key}"))
            })?;
            request.metadata_mut().insert(key, value);
        }
        Ok(request)
    }
}

impl AgentClient {
    pub async fn connect(dst: impl Into<String>) -> Result<Self, tonic::transport::Error> {
        let endpoint = Endpoint::from_shared(normalize_endpoint(dst.into()))?;
        let inner = AgentServiceClient::connect(endpoint).await?;
        Ok(Self {
            inner,
            auth_token: None,
            metadata: Vec::new(),
            retry_policy: RetryPolicy::default(),
        })
    }

    pub fn with_token(mut self, token: impl Into<String>) -> Self {
        self.auth_token = Some(token.into());
        self
    }

    pub fn with_header(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.metadata.push((key.into(), value.into()));
        self
    }

    pub fn with_retry_policy(mut self, retry_policy: RetryPolicy) -> Self {
        self.retry_policy = retry_policy;
        self
    }

    pub async fn get_agent_status(&self) -> Result<GetAgentStatusResponse, tonic::Status> {
        self.unary_with_retry(GetAgentStatusRequest {}, |mut client, request| async move {
            client.get_agent_status(request).await
        })
        .await
    }

    async fn unary_with_retry<Req, Resp, F, Fut>(
        &self,
        request: Req,
        mut call: F,
    ) -> Result<Resp, Status>
    where
        Req: Clone + Send + 'static,
        Resp: Send + 'static,
        F: FnMut(AgentServiceClient<Channel>, Request<Req>) -> Fut,
        Fut: Future<Output = Result<tonic::Response<Resp>, Status>>,
    {
        let attempts = self.retry_policy.attempts.max(1);
        let mut backoff = self.retry_policy.initial_backoff;
        for attempt in 1..=attempts {
            let request = self.attach_metadata(request.clone())?;
            match call(self.inner.clone(), request).await {
                Ok(response) => return Ok(response.into_inner()),
                Err(status) if attempt < attempts && should_retry(&status) => {
                    if !backoff.is_zero() {
                        tokio::time::sleep(backoff).await;
                    }
                    let next = backoff.saturating_mul(self.retry_policy.multiplier.max(1));
                    backoff = next.min(self.retry_policy.max_backoff);
                }
                Err(status) => return Err(status),
            }
        }
        Err(Status::unavailable("exhausted retries"))
    }

    fn attach_metadata<Req>(&self, message: Req) -> Result<Request<Req>, Status> {
        let mut request = Request::new(message);
        if let Some(token) = &self.auth_token {
            let value = MetadataValue::try_from(format!("Bearer {token}"))
                .map_err(|_| Status::invalid_argument("invalid auth token"))?;
            request.metadata_mut().insert("authorization", value);
        }
        for (key, value) in &self.metadata {
            let key = MetadataKey::<Ascii>::from_bytes(key.as_bytes())
                .map_err(|_| Status::invalid_argument(format!("invalid metadata key: {key}")))?;
            let value = MetadataValue::try_from(value.as_str()).map_err(|_| {
                Status::invalid_argument(format!("invalid metadata value for {key}"))
            })?;
            request.metadata_mut().insert(key, value);
        }
        Ok(request)
    }
}

impl ControlClient {
    pub async fn connect(dst: impl Into<String>) -> Result<Self, tonic::transport::Error> {
        let endpoint = Endpoint::from_shared(normalize_endpoint(dst.into()))?;
        let inner = ControlServiceClient::connect(endpoint).await?;
        Ok(Self {
            inner,
            auth_token: None,
            metadata: Vec::new(),
            retry_policy: RetryPolicy::default(),
        })
    }

    pub fn with_token(mut self, token: impl Into<String>) -> Self {
        self.auth_token = Some(token.into());
        self
    }

    pub fn with_header(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.metadata.push((key.into(), value.into()));
        self
    }

    pub fn with_retry_policy(mut self, retry_policy: RetryPolicy) -> Self {
        self.retry_policy = retry_policy;
        self
    }

    pub async fn get_control_status(&self) -> Result<GetControlStatusResponse, tonic::Status> {
        self.unary_with_retry(
            GetControlStatusRequest {},
            |mut client, request| async move { client.get_control_status(request).await },
        )
        .await
    }

    async fn unary_with_retry<Req, Resp, F, Fut>(
        &self,
        request: Req,
        mut call: F,
    ) -> Result<Resp, Status>
    where
        Req: Clone + Send + 'static,
        Resp: Send + 'static,
        F: FnMut(ControlServiceClient<Channel>, Request<Req>) -> Fut,
        Fut: Future<Output = Result<tonic::Response<Resp>, Status>>,
    {
        let attempts = self.retry_policy.attempts.max(1);
        let mut backoff = self.retry_policy.initial_backoff;
        for attempt in 1..=attempts {
            let request = self.attach_metadata(request.clone())?;
            match call(self.inner.clone(), request).await {
                Ok(response) => return Ok(response.into_inner()),
                Err(status) if attempt < attempts && should_retry(&status) => {
                    if !backoff.is_zero() {
                        tokio::time::sleep(backoff).await;
                    }
                    let next = backoff.saturating_mul(self.retry_policy.multiplier.max(1));
                    backoff = next.min(self.retry_policy.max_backoff);
                }
                Err(status) => return Err(status),
            }
        }
        Err(Status::unavailable("exhausted retries"))
    }

    fn attach_metadata<Req>(&self, message: Req) -> Result<Request<Req>, Status> {
        let mut request = Request::new(message);
        if let Some(token) = &self.auth_token {
            let value = MetadataValue::try_from(format!("Bearer {token}"))
                .map_err(|_| Status::invalid_argument("invalid auth token"))?;
            request.metadata_mut().insert("authorization", value);
        }
        for (key, value) in &self.metadata {
            let key = MetadataKey::<Ascii>::from_bytes(key.as_bytes())
                .map_err(|_| Status::invalid_argument(format!("invalid metadata key: {key}")))?;
            let value = MetadataValue::try_from(value.as_str()).map_err(|_| {
                Status::invalid_argument(format!("invalid metadata value for {key}"))
            })?;
            request.metadata_mut().insert(key, value);
        }
        Ok(request)
    }
}

impl ArbiterClient {
    pub async fn connect(dst: impl Into<String>) -> Result<Self, tonic::transport::Error> {
        let endpoint = Endpoint::from_shared(normalize_endpoint(dst.into()))?;
        let inner = ArbiterServiceClient::connect(endpoint).await?;
        Ok(Self {
            inner,
            auth_token: None,
            metadata: Vec::new(),
            retry_policy: RetryPolicy::default(),
        })
    }

    pub fn with_token(mut self, token: impl Into<String>) -> Self {
        self.auth_token = Some(token.into());
        self
    }

    pub fn with_header(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.metadata.push((key.into(), value.into()));
        self
    }

    pub fn with_retry_policy(mut self, retry_policy: RetryPolicy) -> Self {
        self.retry_policy = retry_policy;
        self
    }

    pub async fn publish_bundle(
        &self,
        name: impl Into<String>,
        source: impl Into<Vec<u8>>,
    ) -> Result<PublishBundleResponse, tonic::Status> {
        self.unary_with_retry(
            PublishBundleRequest {
                name: name.into(),
                source: source.into(),
            },
            |mut client, request| async move { client.publish_bundle(request).await },
        )
        .await
    }

    pub async fn list_bundles(
        &self,
        name: impl Into<String>,
    ) -> Result<ListBundlesResponse, tonic::Status> {
        self.unary_with_retry(
            ListBundlesRequest { name: name.into() },
            |mut client, request| async move { client.list_bundles(request).await },
        )
        .await
    }

    pub async fn activate_bundle(
        &self,
        name: impl Into<String>,
        bundle_id: impl Into<String>,
    ) -> Result<ActivateBundleResponse, tonic::Status> {
        self.unary_with_retry(
            ActivateBundleRequest {
                name: name.into(),
                bundle_id: bundle_id.into(),
            },
            |mut client, request| async move { client.activate_bundle(request).await },
        )
        .await
    }

    pub async fn rollback_bundle(
        &self,
        name: impl Into<String>,
    ) -> Result<RollbackBundleResponse, tonic::Status> {
        self.unary_with_retry(
            RollbackBundleRequest { name: name.into() },
            |mut client, request| async move { client.rollback_bundle(request).await },
        )
        .await
    }

    pub async fn get_bundle_by_id(
        &self,
        bundle_id: impl Into<String>,
    ) -> Result<GetBundleResponse, tonic::Status> {
        self.unary_with_retry(
            GetBundleRequest {
                bundle_id: bundle_id.into(),
                bundle_name: String::new(),
            },
            |mut client, request| async move { client.get_bundle(request).await },
        )
        .await
    }

    pub async fn get_bundle_by_name(
        &self,
        bundle_name: impl Into<String>,
    ) -> Result<GetBundleResponse, tonic::Status> {
        self.unary_with_retry(
            GetBundleRequest {
                bundle_id: String::new(),
                bundle_name: bundle_name.into(),
            },
            |mut client, request| async move { client.get_bundle(request).await },
        )
        .await
    }

    pub async fn watch_bundles(
        &self,
        names: Vec<String>,
        active_only: bool,
    ) -> Result<tonic::codec::Streaming<BundleEvent>, tonic::Status> {
        let request = self.attach_metadata(WatchBundlesRequest { names, active_only })?;
        let response = self.inner.clone().watch_bundles(request).await?;
        Ok(response.into_inner())
    }

    pub async fn get_overrides_by_id(
        &self,
        bundle_id: impl Into<String>,
    ) -> Result<GetOverridesResponse, tonic::Status> {
        self.unary_with_retry(
            GetOverridesRequest {
                bundle_id: bundle_id.into(),
                bundle_name: String::new(),
            },
            |mut client, request| async move { client.get_overrides(request).await },
        )
        .await
    }

    pub async fn get_overrides_by_name(
        &self,
        bundle_name: impl Into<String>,
    ) -> Result<GetOverridesResponse, tonic::Status> {
        self.unary_with_retry(
            GetOverridesRequest {
                bundle_id: String::new(),
                bundle_name: bundle_name.into(),
            },
            |mut client, request| async move { client.get_overrides(request).await },
        )
        .await
    }

    pub async fn watch_overrides(
        &self,
        bundle_id: impl Into<String>,
    ) -> Result<tonic::codec::Streaming<OverrideEvent>, tonic::Status> {
        let request = self.attach_metadata(WatchOverridesRequest {
            bundle_id: bundle_id.into(),
        })?;
        let response = self.inner.clone().watch_overrides(request).await?;
        Ok(response.into_inner())
    }

    pub async fn evaluate_rules_by_id(
        &self,
        bundle_id: impl Into<String>,
        context: JsonValue,
        request_id: impl Into<String>,
    ) -> Result<EvaluateRulesResponse, tonic::Status> {
        self.unary_with_retry(
            EvaluateRulesRequest {
                bundle_id: bundle_id.into(),
                bundle_name: String::new(),
                context: Some(json_to_struct(context)),
                request_id: request_id.into(),
            },
            |mut client, request| async move { client.evaluate_rules(request).await },
        )
        .await
    }

    pub async fn evaluate_rules_by_name(
        &self,
        bundle_name: impl Into<String>,
        context: JsonValue,
        request_id: impl Into<String>,
    ) -> Result<EvaluateRulesResponse, tonic::Status> {
        self.unary_with_retry(
            EvaluateRulesRequest {
                bundle_id: String::new(),
                bundle_name: bundle_name.into(),
                context: Some(json_to_struct(context)),
                request_id: request_id.into(),
            },
            |mut client, request| async move { client.evaluate_rules(request).await },
        )
        .await
    }

    pub async fn evaluate_strategy_by_id(
        &self,
        bundle_id: impl Into<String>,
        strategy_name: impl Into<String>,
        context: JsonValue,
        request_id: impl Into<String>,
    ) -> Result<EvaluateStrategyResponse, tonic::Status> {
        self.unary_with_retry(
            EvaluateStrategyRequest {
                bundle_id: bundle_id.into(),
                bundle_name: String::new(),
                strategy_name: strategy_name.into(),
                context: Some(json_to_struct(context)),
                request_id: request_id.into(),
            },
            |mut client, request| async move { client.evaluate_strategy(request).await },
        )
        .await
    }

    pub async fn evaluate_strategy_by_name(
        &self,
        bundle_name: impl Into<String>,
        strategy_name: impl Into<String>,
        context: JsonValue,
        request_id: impl Into<String>,
    ) -> Result<EvaluateStrategyResponse, tonic::Status> {
        self.unary_with_retry(
            EvaluateStrategyRequest {
                bundle_id: String::new(),
                bundle_name: bundle_name.into(),
                strategy_name: strategy_name.into(),
                context: Some(json_to_struct(context)),
                request_id: request_id.into(),
            },
            |mut client, request| async move { client.evaluate_strategy(request).await },
        )
        .await
    }

    pub async fn resolve_flag_by_name(
        &self,
        bundle_name: impl Into<String>,
        flag_key: impl Into<String>,
        context: JsonValue,
        request_id: impl Into<String>,
    ) -> Result<ResolveFlagResponse, tonic::Status> {
        self.unary_with_retry(
            ResolveFlagRequest {
                bundle_id: String::new(),
                bundle_name: bundle_name.into(),
                flag_key: flag_key.into(),
                context: Some(json_to_struct(context)),
                request_id: request_id.into(),
            },
            |mut client, request| async move { client.resolve_flag(request).await },
        )
        .await
    }

    pub async fn resolve_flag_by_id(
        &self,
        bundle_id: impl Into<String>,
        flag_key: impl Into<String>,
        context: JsonValue,
        request_id: impl Into<String>,
    ) -> Result<ResolveFlagResponse, tonic::Status> {
        self.unary_with_retry(
            ResolveFlagRequest {
                bundle_id: bundle_id.into(),
                bundle_name: String::new(),
                flag_key: flag_key.into(),
                context: Some(json_to_struct(context)),
                request_id: request_id.into(),
            },
            |mut client, request| async move { client.resolve_flag(request).await },
        )
        .await
    }

    pub async fn start_session_by_name(
        &self,
        bundle_name: impl Into<String>,
        envelope: JsonValue,
        facts: Vec<ExpertFact>,
    ) -> Result<StartSessionResponse, tonic::Status> {
        self.unary_with_retry(
            StartSessionRequest {
                bundle_id: String::new(),
                bundle_name: bundle_name.into(),
                envelope: Some(json_to_struct(envelope)),
                facts,
            },
            |mut client, request| async move { client.start_session(request).await },
        )
        .await
    }

    pub async fn start_session_by_id(
        &self,
        bundle_id: impl Into<String>,
        envelope: JsonValue,
        facts: Vec<ExpertFact>,
    ) -> Result<StartSessionResponse, tonic::Status> {
        self.unary_with_retry(
            StartSessionRequest {
                bundle_id: bundle_id.into(),
                bundle_name: String::new(),
                envelope: Some(json_to_struct(envelope)),
                facts,
            },
            |mut client, request| async move { client.start_session(request).await },
        )
        .await
    }

    pub async fn run_session(
        &self,
        session_id: impl Into<String>,
        request_id: impl Into<String>,
    ) -> Result<RunSessionResponse, tonic::Status> {
        self.unary_with_retry(
            RunSessionRequest {
                session_id: session_id.into(),
                request_id: request_id.into(),
            },
            |mut client, request| async move { client.run_session(request).await },
        )
        .await
    }

    pub async fn assert_facts(
        &self,
        session_id: impl Into<String>,
        facts: Vec<ExpertFact>,
    ) -> Result<AssertFactsResponse, tonic::Status> {
        self.unary_with_retry(
            AssertFactsRequest {
                session_id: session_id.into(),
                facts,
            },
            |mut client, request| async move { client.assert_facts(request).await },
        )
        .await
    }

    pub async fn retract_facts(
        &self,
        session_id: impl Into<String>,
        facts: Vec<FactRef>,
    ) -> Result<RetractFactsResponse, tonic::Status> {
        self.unary_with_retry(
            RetractFactsRequest {
                session_id: session_id.into(),
                facts,
            },
            |mut client, request| async move { client.retract_facts(request).await },
        )
        .await
    }

    pub async fn get_session_trace(
        &self,
        session_id: impl Into<String>,
    ) -> Result<GetSessionTraceResponse, tonic::Status> {
        self.unary_with_retry(
            GetSessionTraceRequest {
                session_id: session_id.into(),
            },
            |mut client, request| async move { client.get_session_trace(request).await },
        )
        .await
    }

    pub async fn close_session(
        &self,
        session_id: impl Into<String>,
    ) -> Result<CloseSessionResponse, tonic::Status> {
        self.unary_with_retry(
            CloseSessionRequest {
                session_id: session_id.into(),
            },
            |mut client, request| async move { client.close_session(request).await },
        )
        .await
    }

    pub async fn set_rule_override(
        &self,
        bundle_id: impl Into<String>,
        rule_name: impl Into<String>,
        kill_switch: Option<bool>,
        rollout: Option<u32>,
    ) -> Result<SetRuleOverrideResponse, tonic::Status> {
        self.unary_with_retry(
            SetRuleOverrideRequest {
                bundle_id: bundle_id.into(),
                rule_name: rule_name.into(),
                kill_switch,
                rollout,
            },
            |mut client, request| async move { client.set_rule_override(request).await },
        )
        .await
    }

    pub async fn set_flag_override(
        &self,
        bundle_id: impl Into<String>,
        flag_key: impl Into<String>,
        kill_switch: Option<bool>,
    ) -> Result<SetFlagOverrideResponse, tonic::Status> {
        self.unary_with_retry(
            SetFlagOverrideRequest {
                bundle_id: bundle_id.into(),
                flag_key: flag_key.into(),
                kill_switch,
            },
            |mut client, request| async move { client.set_flag_override(request).await },
        )
        .await
    }

    pub async fn set_flag_rule_override(
        &self,
        bundle_id: impl Into<String>,
        flag_key: impl Into<String>,
        rule_index: u32,
        rollout: Option<u32>,
    ) -> Result<SetFlagRuleOverrideResponse, tonic::Status> {
        self.unary_with_retry(
            SetFlagRuleOverrideRequest {
                bundle_id: bundle_id.into(),
                flag_key: flag_key.into(),
                rule_index,
                rollout,
            },
            |mut client, request| async move { client.set_flag_rule_override(request).await },
        )
        .await
    }

    pub async fn set_strategy_override(
        &self,
        bundle_id: impl Into<String>,
        strategy_name: impl Into<String>,
        candidate_label: impl Into<String>,
        kill_switch: Option<bool>,
        rollout: Option<u32>,
    ) -> Result<SetStrategyOverrideResponse, tonic::Status> {
        self.unary_with_retry(
            SetStrategyOverrideRequest {
                bundle_id: bundle_id.into(),
                strategy_name: strategy_name.into(),
                candidate_label: candidate_label.into(),
                kill_switch,
                rollout,
            },
            |mut client, request| async move { client.set_strategy_override(request).await },
        )
        .await
    }

    async fn unary_with_retry<Req, Resp, F, Fut>(
        &self,
        request: Req,
        mut call: F,
    ) -> Result<Resp, Status>
    where
        Req: Clone + Send + 'static,
        Resp: Send + 'static,
        F: FnMut(ArbiterServiceClient<Channel>, Request<Req>) -> Fut,
        Fut: Future<Output = Result<tonic::Response<Resp>, Status>>,
    {
        let attempts = self.retry_policy.attempts.max(1);
        let mut backoff = self.retry_policy.initial_backoff;
        for attempt in 1..=attempts {
            let request = self.attach_metadata(request.clone())?;
            match call(self.inner.clone(), request).await {
                Ok(response) => return Ok(response.into_inner()),
                Err(status) if attempt < attempts && should_retry(&status) => {
                    if !backoff.is_zero() {
                        tokio::time::sleep(backoff).await;
                    }
                    let next = backoff.saturating_mul(self.retry_policy.multiplier.max(1));
                    backoff = next.min(self.retry_policy.max_backoff);
                }
                Err(status) => return Err(status),
            }
        }
        Err(Status::unavailable("exhausted retries"))
    }

    fn attach_metadata<Req>(&self, message: Req) -> Result<Request<Req>, Status> {
        let mut request = Request::new(message);
        if let Some(token) = &self.auth_token {
            let value = MetadataValue::try_from(format!("Bearer {token}"))
                .map_err(|_| Status::invalid_argument("invalid auth token"))?;
            request.metadata_mut().insert("authorization", value);
        }
        for (key, value) in &self.metadata {
            let key = MetadataKey::<Ascii>::from_bytes(key.as_bytes())
                .map_err(|_| Status::invalid_argument(format!("invalid metadata key: {key}")))?;
            let value = MetadataValue::try_from(value.as_str()).map_err(|_| {
                Status::invalid_argument(format!("invalid metadata value for {key}"))
            })?;
            request.metadata_mut().insert(key, value);
        }
        Ok(request)
    }
}

pub fn json_to_struct(value: JsonValue) -> Struct {
    match value {
        JsonValue::Object(map) => Struct {
            fields: map
                .into_iter()
                .map(|(k, v)| (k, json_to_proto(v)))
                .collect(),
        },
        JsonValue::Null => Struct {
            fields: Default::default(),
        },
        other => panic!("expected JSON object for protobuf Struct, got {other}"),
    }
}

pub fn fact(typ: impl Into<String>, key: impl Into<String>, fields: JsonValue) -> ExpertFact {
    ExpertFact {
        r#type: typ.into(),
        key: key.into(),
        fields: Some(json_to_struct(fields)),
    }
}

pub fn fact_ref(typ: impl Into<String>, key: impl Into<String>) -> FactRef {
    FactRef {
        r#type: typ.into(),
        key: key.into(),
    }
}

#[async_trait]
pub trait SourceHandler: Send + Sync + 'static {
    async fn load_source(&self, target: String) -> Result<Vec<ExpertFact>, Status>;
}

#[async_trait]
pub trait SinkHandler: Send + Sync + 'static {
    async fn deliver_outcome(&self, delivery: arbiter::v1::DeliveryContext) -> Result<(), Status>;
}

#[async_trait]
pub trait WorkerHandler: Send + Sync + 'static {
    async fn execute_worker(
        &self,
        worker: WorkerSpec,
        delivery: arbiter::v1::DeliveryContext,
    ) -> Result<ExecuteWorkerResponse, Status>;
}

#[derive(Clone)]
struct RegisteredSource {
    description: String,
    handler: Arc<dyn SourceHandler>,
}

#[derive(Clone)]
struct RegisteredSink {
    description: String,
    handler: Arc<dyn SinkHandler>,
}

#[derive(Clone)]
struct RegisteredWorker {
    description: String,
    handler: Arc<dyn WorkerHandler>,
}

#[derive(Clone, Default)]
pub struct CapabilityPlugin {
    name: String,
    version: String,
    sources: HashMap<String, RegisteredSource>,
    sinks: HashMap<String, RegisteredSink>,
    workers: HashMap<String, RegisteredWorker>,
}

impl CapabilityPlugin {
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            version: String::new(),
            sources: HashMap::new(),
            sinks: HashMap::new(),
            workers: HashMap::new(),
        }
    }

    pub fn with_version(mut self, version: impl Into<String>) -> Self {
        self.version = version.into();
        self
    }

    pub fn register_source<H>(
        &mut self,
        scheme: impl Into<String>,
        description: impl Into<String>,
        handler: H,
    ) -> Result<&mut Self, Status>
    where
        H: SourceHandler,
    {
        let scheme = normalize_capability_id(scheme.into(), "source scheme", true)?;
        if is_reserved_source_scheme(&scheme) {
            return Err(Status::invalid_argument(format!(
                "source scheme {scheme} is reserved"
            )));
        }
        self.sources.insert(
            scheme,
            RegisteredSource {
                description: description.into(),
                handler: Arc::new(handler),
            },
        );
        Ok(self)
    }

    pub fn register_sink<H>(
        &mut self,
        kind: impl Into<String>,
        description: impl Into<String>,
        handler: H,
    ) -> Result<&mut Self, Status>
    where
        H: SinkHandler,
    {
        let kind = normalize_capability_id(kind.into(), "sink kind", false)?;
        if is_reserved_handler_kind(&kind) {
            return Err(Status::invalid_argument(format!(
                "sink kind {kind} is reserved"
            )));
        }
        self.sinks.insert(
            kind,
            RegisteredSink {
                description: description.into(),
                handler: Arc::new(handler),
            },
        );
        Ok(self)
    }

    pub fn register_worker<H>(
        &mut self,
        kind: impl Into<String>,
        description: impl Into<String>,
        handler: H,
    ) -> Result<&mut Self, Status>
    where
        H: WorkerHandler,
    {
        let kind = normalize_capability_id(kind.into(), "worker kind", false)?;
        if is_reserved_handler_kind(&kind) {
            return Err(Status::invalid_argument(format!(
                "worker kind {kind} is reserved"
            )));
        }
        self.workers.insert(
            kind,
            RegisteredWorker {
                description: description.into(),
                handler: Arc::new(handler),
            },
        );
        Ok(self)
    }

    pub fn manifest(&self) -> GetCapabilitiesResponse {
        let mut sources: Vec<_> = self
            .sources
            .iter()
            .map(|(scheme, item)| SourceCapability {
                scheme: scheme.clone(),
                description: item.description.clone(),
            })
            .collect();
        sources.sort_by(|a, b| a.scheme.cmp(&b.scheme));

        let mut sinks: Vec<_> = self
            .sinks
            .iter()
            .map(|(kind, item)| SinkCapability {
                kind: kind.clone(),
                description: item.description.clone(),
            })
            .collect();
        sinks.sort_by(|a, b| a.kind.cmp(&b.kind));

        let mut workers: Vec<_> = self
            .workers
            .iter()
            .map(|(kind, item)| WorkerCapability {
                kind: kind.clone(),
                description: item.description.clone(),
            })
            .collect();
        workers.sort_by(|a, b| a.kind.cmp(&b.kind));

        GetCapabilitiesResponse {
            name: self.name.clone(),
            version: self.version.clone(),
            sources,
            sinks,
            workers,
        }
    }

    pub fn into_service(self) -> CapabilityServiceServer<Self> {
        CapabilityServiceServer::new(self)
    }
}

#[async_trait]
impl CapabilityService for CapabilityPlugin {
    async fn get_capabilities(
        &self,
        _request: Request<GetCapabilitiesRequest>,
    ) -> Result<Response<GetCapabilitiesResponse>, Status> {
        Ok(Response::new(self.manifest()))
    }

    async fn load_source(
        &self,
        request: Request<LoadSourceRequest>,
    ) -> Result<Response<LoadSourceResponse>, Status> {
        let target = request.into_inner().target;
        let scheme = source_scheme(&target);
        let item = self.sources.get(&scheme).ok_or_else(|| {
            Status::unimplemented(format!(
                "no source handler registered for scheme {}",
                if scheme.is_empty() { "<none>" } else { &scheme }
            ))
        })?;
        let facts = item.handler.load_source(target).await?;
        Ok(Response::new(LoadSourceResponse { facts }))
    }

    async fn deliver_outcome(
        &self,
        request: Request<DeliverOutcomeRequest>,
    ) -> Result<Response<DeliverOutcomeResponse>, Status> {
        let delivery = request
            .into_inner()
            .delivery
            .ok_or_else(|| Status::invalid_argument("delivery is required"))?;
        let kind = delivery.handler_kind.clone();
        let item = self.sinks.get(&kind).ok_or_else(|| {
            Status::unimplemented(format!(
                "no sink handler registered for kind {}",
                if kind.is_empty() { "<none>" } else { &kind }
            ))
        })?;
        item.handler.deliver_outcome(delivery).await?;
        Ok(Response::new(DeliverOutcomeResponse {}))
    }

    async fn execute_worker(
        &self,
        request: Request<ExecuteWorkerRequest>,
    ) -> Result<Response<ExecuteWorkerResponse>, Status> {
        let input = request.into_inner();
        let worker = input
            .worker
            .ok_or_else(|| Status::invalid_argument("worker is required"))?;
        let delivery = input
            .delivery
            .ok_or_else(|| Status::invalid_argument("delivery is required"))?;
        let kind = worker.kind.clone();
        let item = self.workers.get(&kind).ok_or_else(|| {
            Status::unimplemented(format!(
                "no worker handler registered for kind {}",
                if kind.is_empty() { "<none>" } else { &kind }
            ))
        })?;
        Ok(Response::new(
            item.handler.execute_worker(worker, delivery).await?,
        ))
    }
}

fn json_to_proto(value: JsonValue) -> Value {
    let kind = match value {
        JsonValue::Null => Kind::NullValue(0),
        JsonValue::Bool(v) => Kind::BoolValue(v),
        JsonValue::Number(v) => Kind::NumberValue(v.as_f64().expect("number must fit f64")),
        JsonValue::String(v) => Kind::StringValue(v),
        JsonValue::Array(values) => Kind::ListValue(prost_types::ListValue {
            values: values.into_iter().map(json_to_proto).collect(),
        }),
        JsonValue::Object(values) => Kind::StructValue(Struct {
            fields: values
                .into_iter()
                .map(|(k, v)| (k, json_to_proto(v)))
                .collect(),
        }),
    };
    Value { kind: Some(kind) }
}

fn normalize_endpoint(dst: String) -> String {
    if dst.contains("://") {
        dst
    } else {
        format!("http://{dst}")
    }
}

fn normalize_capability_id(value: String, label: &str, lowercase: bool) -> Result<String, Status> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err(Status::invalid_argument(format!(
            "{label} must be non-empty"
        )));
    }
    if lowercase {
        Ok(trimmed.to_ascii_lowercase())
    } else {
        Ok(trimmed.to_string())
    }
}

fn source_scheme(target: &str) -> String {
    target
        .split_once("://")
        .map(|(scheme, _)| scheme.to_ascii_lowercase())
        .unwrap_or_default()
}

fn is_reserved_source_scheme(scheme: &str) -> bool {
    matches!(scheme, "chain" | "worker")
}

fn is_reserved_handler_kind(kind: &str) -> bool {
    matches!(kind, "chain" | "worker")
}

fn should_retry(status: &Status) -> bool {
    matches!(status.code(), Code::Unavailable | Code::DeadlineExceeded)
}
