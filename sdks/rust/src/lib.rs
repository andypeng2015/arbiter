use std::{future::Future, time::Duration};

use prost_types::{value::Kind, Struct, Value};
use serde_json::Value as JsonValue;
use tonic::{
    metadata::{Ascii, MetadataKey, MetadataValue},
    transport::{Channel, Endpoint},
    Code, Request, Status,
};

pub mod arbiter {
    pub mod v1 {
        tonic::include_proto!("arbiter.v1");
    }
}

use arbiter::v1::{
    arbiter_service_client::ArbiterServiceClient, ActivateBundleRequest, ActivateBundleResponse,
    AssertFactsRequest, AssertFactsResponse, CloseSessionRequest, CloseSessionResponse,
    EvaluateRulesRequest, EvaluateRulesResponse, ExpertFact, FactRef, GetSessionTraceRequest,
    GetSessionTraceResponse, ListBundlesRequest, ListBundlesResponse, PublishBundleRequest,
    PublishBundleResponse, ResolveFlagRequest, ResolveFlagResponse, RetractFactsRequest,
    RetractFactsResponse, RollbackBundleRequest, RollbackBundleResponse, RunSessionRequest,
    RunSessionResponse, SetFlagOverrideRequest, SetFlagOverrideResponse,
    SetFlagRuleOverrideRequest, SetFlagRuleOverrideResponse, SetRuleOverrideRequest,
    SetRuleOverrideResponse, StartSessionRequest, StartSessionResponse,
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

fn should_retry(status: &Status) -> bool {
    matches!(status.code(), Code::Unavailable | Code::DeadlineExceeded)
}
