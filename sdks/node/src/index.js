const path = require("node:path");
const grpc = require("@grpc/grpc-js");
const protoLoader = require("@grpc/proto-loader");
const googleProtoFiles = require("google-proto-files");

const DEFAULT_RETRY = Object.freeze({
  attempts: 3,
  initialBackoffMs: 100,
  maxBackoffMs: 1000,
  multiplier: 2,
  retryableStatusCodes: [grpc.status.UNAVAILABLE, grpc.status.DEADLINE_EXCEEDED],
});

const serviceProtoPath = path.join(__dirname, "..", "proto", "arbiter", "v1", "service.proto");
const capabilityProtoPath = path.join(__dirname, "..", "proto", "arbiter", "v1", "capability.proto");
const packageDefinition = protoLoader.loadSync([serviceProtoPath, capabilityProtoPath], {
  keepCase: false,
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
  json: true,
  includeDirs: [
    path.join(__dirname, "..", "proto"),
    googleProtoFiles.getProtoPath(),
  ],
});
const proto = grpc.loadPackageDefinition(packageDefinition).arbiter.v1;
const RESERVED_SOURCE_SCHEMES = new Set(["chain", "worker"]);
const RESERVED_HANDLER_KINDS = new Set(["chain", "worker"]);

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function isChannelCredentials(value) {
  return Boolean(value) && typeof value._getConnectionOptions === "function";
}

function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value) && !Buffer.isBuffer(value);
}

function normalizeClientArgs(target, credentialsOrOptions, maybeOptions) {
  let explicitCredentials = undefined;
  let options = maybeOptions || {};
  if (isChannelCredentials(credentialsOrOptions)) {
    explicitCredentials = credentialsOrOptions;
  } else if (isPlainObject(credentialsOrOptions)) {
    options = credentialsOrOptions;
    explicitCredentials = options.credentials;
  } else if (credentialsOrOptions !== undefined) {
    explicitCredentials = credentialsOrOptions;
  }
  return {
    target,
    credentials: explicitCredentials,
    options: isPlainObject(options) ? options : {},
  };
}

function normalizeRetryOptions(retryOptions = {}) {
  const merged = { ...DEFAULT_RETRY, ...retryOptions };
  merged.attempts = Math.max(1, Number(merged.attempts) || DEFAULT_RETRY.attempts);
  merged.initialBackoffMs = Math.max(0, Number(merged.initialBackoffMs) || DEFAULT_RETRY.initialBackoffMs);
  merged.maxBackoffMs = Math.max(merged.initialBackoffMs, Number(merged.maxBackoffMs) || DEFAULT_RETRY.maxBackoffMs);
  merged.multiplier = Math.max(1, Number(merged.multiplier) || DEFAULT_RETRY.multiplier);
  merged.retryableStatusCodes = Array.isArray(merged.retryableStatusCodes)
    ? merged.retryableStatusCodes
    : DEFAULT_RETRY.retryableStatusCodes;
  return merged;
}

function resolveTarget(target, secureHint) {
  if (typeof target !== "string" || target.trim() === "") {
    throw new TypeError("target must be a non-empty string");
  }
  if (target.startsWith("https://")) {
    return { target: target.slice("https://".length), secure: true };
  }
  if (target.startsWith("grpcs://")) {
    return { target: target.slice("grpcs://".length), secure: true };
  }
  if (target.startsWith("http://")) {
    return { target: target.slice("http://".length), secure: false };
  }
  if (target.startsWith("grpc://")) {
    return { target: target.slice("grpc://".length), secure: false };
  }
  return { target, secure: Boolean(secureHint) };
}

function resolveCredentials(target, explicitCredentials, options) {
  if (explicitCredentials) {
    return {
      target: resolveTarget(target, false).target,
      credentials: explicitCredentials,
    };
  }
  const secureHint = options.secure ?? options.rootCerts ?? options.privateKey ?? options.certChain ?? options.serverNameOverride;
  const normalized = resolveTarget(target, secureHint);
  if (!normalized.secure) {
    return {
      target: normalized.target,
      credentials: grpc.credentials.createInsecure(),
    };
  }
  const credentials = grpc.credentials.createSsl(
    options.rootCerts || null,
    options.privateKey || null,
    options.certChain || null,
  );
  return {
    target: normalized.target,
    credentials,
  };
}

function addMetadataEntries(metadata, entries) {
  if (!entries) {
    return;
  }
  if (entries instanceof grpc.Metadata) {
    for (const key of Object.keys(entries.getMap())) {
      for (const value of entries.get(key)) {
        metadata.add(key, value);
      }
    }
    return;
  }
  for (const [key, value] of Object.entries(entries)) {
    if (Array.isArray(value)) {
      for (const item of value) {
        metadata.add(key, String(item));
      }
      continue;
    }
    metadata.set(key, String(value));
  }
}

function createMetadataFactory(options) {
  return function createMetadata(extra) {
    const metadata = new grpc.Metadata();
    if (options.token) {
      metadata.set("authorization", `Bearer ${options.token}`);
    }
    addMetadataEntries(metadata, options.metadata);
    addMetadataEntries(metadata, extra);
    return metadata;
  };
}

function isRetryableError(err, retryOptions) {
  return Boolean(err) && retryOptions.retryableStatusCodes.includes(err.code);
}

function normalizeCapabilityName(value, label) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new TypeError(`${label} must be a non-empty string`);
  }
  return value.trim();
}

function sourceScheme(target) {
  if (typeof target !== "string") {
    return "";
  }
  const index = target.indexOf("://");
  if (index <= 0) {
    return "";
  }
  return target.slice(0, index).toLowerCase();
}

function grpcError(code, details) {
  const err = new Error(details);
  err.code = code;
  return err;
}

function toFactList(result) {
  if (result === undefined || result === null) {
    return [];
  }
  if (Array.isArray(result)) {
    return result;
  }
  if (isPlainObject(result) && Array.isArray(result.facts)) {
    return result.facts;
  }
  throw new TypeError("source handler must return an array of facts or { facts }");
}

function toWorkerResult(result) {
  if (result === undefined || result === null) {
    return { facts: [], outcomes: [] };
  }
  if (!isPlainObject(result)) {
    throw new TypeError("worker handler must return an object with optional facts/outcomes arrays");
  }
  return {
    facts: Array.isArray(result.facts) ? result.facts : [],
    outcomes: Array.isArray(result.outcomes) ? result.outcomes : [],
  };
}

class CapabilityServer {
  constructor({ name = "", version = "" } = {}) {
    this.name = String(name || "");
    this.version = String(version || "");
    this._sources = new Map();
    this._sinks = new Map();
    this._workers = new Map();
  }

  registerSource(scheme, handler, { description = "" } = {}) {
    const normalized = normalizeCapabilityName(scheme, "source scheme").toLowerCase();
    if (RESERVED_SOURCE_SCHEMES.has(normalized)) {
      throw new Error(`source scheme ${normalized} is reserved`);
    }
    if (typeof handler !== "function") {
      throw new TypeError("source handler must be a function");
    }
    this._sources.set(normalized, { description: String(description || ""), handler });
    return this;
  }

  registerSink(kind, handler, { description = "" } = {}) {
    const normalized = normalizeCapabilityName(kind, "sink kind");
    if (RESERVED_HANDLER_KINDS.has(normalized)) {
      throw new Error(`sink kind ${normalized} is reserved`);
    }
    if (typeof handler !== "function") {
      throw new TypeError("sink handler must be a function");
    }
    this._sinks.set(normalized, { description: String(description || ""), handler });
    return this;
  }

  registerWorker(kind, handler, { description = "" } = {}) {
    const normalized = normalizeCapabilityName(kind, "worker kind");
    if (RESERVED_HANDLER_KINDS.has(normalized)) {
      throw new Error(`worker kind ${normalized} is reserved`);
    }
    if (typeof handler !== "function") {
      throw new TypeError("worker handler must be a function");
    }
    this._workers.set(normalized, { description: String(description || ""), handler });
    return this;
  }

  manifest() {
    return {
      name: this.name,
      version: this.version,
      sources: [...this._sources.entries()].map(([scheme, item]) => ({ scheme, description: item.description })),
      sinks: [...this._sinks.entries()].map(([kind, item]) => ({ kind, description: item.description })),
      workers: [...this._workers.entries()].map(([kind, item]) => ({ kind, description: item.description })),
    };
  }

  addToServer(server) {
    if (!server || typeof server.addService !== "function") {
      throw new TypeError("server must be a grpc.Server");
    }
    server.addService(proto.CapabilityService.service, {
      GetCapabilities: (_call, callback) => callback(null, this.manifest()),
      LoadSource: (call, callback) => {
        this._handleUnary(callback, async () => {
          const scheme = sourceScheme(call.request.target);
          const item = this._sources.get(scheme);
          if (!item) {
            throw grpcError(grpc.status.UNIMPLEMENTED, `no source handler registered for scheme ${scheme || "<none>"}`);
          }
          return { facts: toFactList(await Promise.resolve(item.handler(call.request.target, call))) };
        });
      },
      DeliverOutcome: (call, callback) => {
        this._handleUnary(callback, async () => {
          const kind = call.request?.delivery?.handlerKind || "";
          const item = this._sinks.get(kind);
          if (!item) {
            throw grpcError(grpc.status.UNIMPLEMENTED, `no sink handler registered for kind ${kind || "<none>"}`);
          }
          await Promise.resolve(item.handler(call.request.delivery, call));
          return {};
        });
      },
      ExecuteWorker: (call, callback) => {
        this._handleUnary(callback, async () => {
          const kind = call.request?.worker?.kind || "";
          const item = this._workers.get(kind);
          if (!item) {
            throw grpcError(grpc.status.UNIMPLEMENTED, `no worker handler registered for kind ${kind || "<none>"}`);
          }
          return toWorkerResult(await Promise.resolve(item.handler({
            worker: call.request.worker,
            delivery: call.request.delivery,
          }, call)));
        });
      },
    });
    return server;
  }

  async listen(address, credentials = grpc.ServerCredentials.createInsecure(), server = undefined) {
    const boundServer = server || new grpc.Server();
    this.addToServer(boundServer);
    const port = await new Promise((resolve, reject) => {
      boundServer.bindAsync(address, credentials, (err, value) => {
        if (err) {
          reject(err);
          return;
        }
        resolve(value);
      });
    });
    boundServer.start();
    return { server: boundServer, port };
  }

  _handleUnary(callback, fn) {
    Promise.resolve()
      .then(fn)
      .then(result => callback(null, result))
      .catch(err => callback(err));
  }
}

async function unary(client, method, request, createMetadata, retryOptions, extraMetadata) {
  let backoff = retryOptions.initialBackoffMs;
  for (let attempt = 1; attempt <= retryOptions.attempts; attempt += 1) {
    try {
      return await new Promise((resolve, reject) => {
        client[method](request, createMetadata(extraMetadata), (err, response) => {
          if (err) {
            reject(err);
            return;
          }
          resolve(response);
        });
      });
    } catch (err) {
      if (attempt >= retryOptions.attempts || !isRetryableError(err, retryOptions)) {
        throw err;
      }
      await sleep(backoff);
      backoff = Math.min(retryOptions.maxBackoffMs, backoff * retryOptions.multiplier);
    }
  }
  throw new Error(`exhausted retries for ${method}`);
}

class ArbiterClient {
  constructor(target, credentialsOrOptions = undefined, maybeOptions = undefined) {
    const { credentials: explicitCredentials, options } = normalizeClientArgs(target, credentialsOrOptions, maybeOptions);
    const { target: normalizedTarget, credentials } = resolveCredentials(target, explicitCredentials, options);
    const channelOptions = { ...(options.channelOptions || {}) };
    if (options.serverNameOverride) {
      channelOptions["grpc.ssl_target_name_override"] = options.serverNameOverride;
      channelOptions["grpc.default_authority"] = options.serverNameOverride;
    }
    this._createMetadata = createMetadataFactory(options);
    this._retry = normalizeRetryOptions(options.retry);
    this.client = new proto.ArbiterService(normalizedTarget, credentials, channelOptions);
  }

  close() {
    this.client.close();
  }

  publishBundle({ name, source }, metadata = undefined) {
    return unary(this.client, "PublishBundle", {
      name,
      source: Buffer.isBuffer(source) ? source : Buffer.from(source),
    }, this._createMetadata, this._retry, metadata);
  }

  listBundles({ name = "" } = {}, metadata = undefined) {
    return unary(this.client, "ListBundles", { name }, this._createMetadata, this._retry, metadata);
  }

  activateBundle({ name, bundleId }, metadata = undefined) {
    return unary(this.client, "ActivateBundle", { name, bundleId }, this._createMetadata, this._retry, metadata);
  }

  rollbackBundle({ name }, metadata = undefined) {
    return unary(this.client, "RollbackBundle", { name }, this._createMetadata, this._retry, metadata);
  }

  getBundle({ bundleId = "", bundleName = "" } = {}, metadata = undefined) {
    return unary(this.client, "GetBundle", { bundleId, bundleName }, this._createMetadata, this._retry, metadata);
  }

  watchBundles({ names = [], activeOnly = false } = {}, metadata = undefined) {
    return this.client.WatchBundles({ names, activeOnly }, this._createMetadata(metadata));
  }

  getOverrides({ bundleId = "", bundleName = "" } = {}, metadata = undefined) {
    return unary(this.client, "GetOverrides", { bundleId, bundleName }, this._createMetadata, this._retry, metadata);
  }

  watchOverrides({ bundleId }, metadata = undefined) {
    return this.client.WatchOverrides({ bundleId }, this._createMetadata(metadata));
  }

  evaluateRules({ bundleId = "", bundleName = "", context = {}, requestId = "" }, metadata = undefined) {
    return unary(this.client, "EvaluateRules", {
      bundleId,
      bundleName,
      context,
      requestId,
    }, this._createMetadata, this._retry, metadata);
  }

  resolveFlag({ bundleId = "", bundleName = "", flagKey, context = {}, requestId = "" }, metadata = undefined) {
    return unary(this.client, "ResolveFlag", {
      bundleId,
      bundleName,
      flagKey,
      context,
      requestId,
    }, this._createMetadata, this._retry, metadata);
  }

  evaluateStrategy({ bundleId = "", bundleName = "", strategyName, context = {}, requestId = "" }, metadata = undefined) {
    return unary(this.client, "EvaluateStrategy", {
      bundleId,
      bundleName,
      strategyName,
      context,
      requestId,
    }, this._createMetadata, this._retry, metadata);
  }

  startSession({ bundleId = "", bundleName = "", envelope = {}, facts = [] }, metadata = undefined) {
    return unary(this.client, "StartSession", {
      bundleId,
      bundleName,
      envelope,
      facts,
    }, this._createMetadata, this._retry, metadata);
  }

  runSession({ sessionId, requestId = "" }, metadata = undefined) {
    return unary(this.client, "RunSession", { sessionId, requestId }, this._createMetadata, this._retry, metadata);
  }

  assertFacts({ sessionId, facts }, metadata = undefined) {
    return unary(this.client, "AssertFacts", { sessionId, facts }, this._createMetadata, this._retry, metadata);
  }

  retractFacts({ sessionId, facts }, metadata = undefined) {
    return unary(this.client, "RetractFacts", { sessionId, facts }, this._createMetadata, this._retry, metadata);
  }

  getSessionTrace({ sessionId }, metadata = undefined) {
    return unary(this.client, "GetSessionTrace", { sessionId }, this._createMetadata, this._retry, metadata);
  }

  closeSession({ sessionId }, metadata = undefined) {
    return unary(this.client, "CloseSession", { sessionId }, this._createMetadata, this._retry, metadata);
  }

  setRuleOverride({ bundleId, ruleName, killSwitch = undefined, rollout = undefined }, metadata = undefined) {
    return unary(this.client, "SetRuleOverride", {
      bundleId,
      ruleName,
      killSwitch: killSwitch === undefined ? undefined : { value: killSwitch },
      rollout: rollout === undefined ? undefined : { value: rollout },
    }, this._createMetadata, this._retry, metadata);
  }

  setFlagOverride({ bundleId, flagKey, killSwitch = undefined }, metadata = undefined) {
    return unary(this.client, "SetFlagOverride", {
      bundleId,
      flagKey,
      killSwitch: killSwitch === undefined ? undefined : { value: killSwitch },
    }, this._createMetadata, this._retry, metadata);
  }

  setFlagRuleOverride({ bundleId, flagKey, ruleIndex, rollout = undefined }, metadata = undefined) {
    return unary(this.client, "SetFlagRuleOverride", {
      bundleId,
      flagKey,
      ruleIndex,
      rollout: rollout === undefined ? undefined : { value: rollout },
    }, this._createMetadata, this._retry, metadata);
  }

  setStrategyOverride({ bundleId, strategyName, candidateLabel, killSwitch = undefined, rollout = undefined }, metadata = undefined) {
    return unary(this.client, "SetStrategyOverride", {
      bundleId,
      strategyName,
      candidateLabel,
      killSwitch: killSwitch === undefined ? undefined : { value: killSwitch },
      rollout: rollout === undefined ? undefined : { value: rollout },
    }, this._createMetadata, this._retry, metadata);
  }
}

class RuntimeClient {
  constructor(target, credentialsOrOptions = undefined, maybeOptions = undefined) {
    const { credentials: explicitCredentials, options } = normalizeClientArgs(target, credentialsOrOptions, maybeOptions);
    const { target: normalizedTarget, credentials } = resolveCredentials(target, explicitCredentials, options);
    const channelOptions = { ...(options.channelOptions || {}) };
    if (options.serverNameOverride) {
      channelOptions["grpc.ssl_target_name_override"] = options.serverNameOverride;
      channelOptions["grpc.default_authority"] = options.serverNameOverride;
    }
    this._createMetadata = createMetadataFactory(options);
    this._retry = normalizeRetryOptions(options.retry);
    this.client = new proto.RuntimeService(normalizedTarget, credentials, channelOptions);
  }

  close() {
    this.client.close();
  }

  getRuntimeCapabilities(metadata = undefined) {
    return unary(this.client, "GetRuntimeCapabilities", {}, this._createMetadata, this._retry, metadata);
  }

  getRuntimeStatus(metadata = undefined) {
    return unary(this.client, "GetRuntimeStatus", {}, this._createMetadata, this._retry, metadata);
  }
}

class AgentClient {
  constructor(target, credentialsOrOptions = undefined, maybeOptions = undefined) {
    const { credentials: explicitCredentials, options } = normalizeClientArgs(target, credentialsOrOptions, maybeOptions);
    const { target: normalizedTarget, credentials } = resolveCredentials(target, explicitCredentials, options);
    const channelOptions = { ...(options.channelOptions || {}) };
    if (options.serverNameOverride) {
      channelOptions["grpc.ssl_target_name_override"] = options.serverNameOverride;
      channelOptions["grpc.default_authority"] = options.serverNameOverride;
    }
    this._createMetadata = createMetadataFactory(options);
    this._retry = normalizeRetryOptions(options.retry);
    this.client = new proto.AgentService(normalizedTarget, credentials, channelOptions);
  }

  close() {
    this.client.close();
  }

  getAgentStatus(metadata = undefined) {
    return unary(this.client, "GetAgentStatus", {}, this._createMetadata, this._retry, metadata);
  }
}

class ControlClient {
  constructor(target, credentialsOrOptions = undefined, maybeOptions = undefined) {
    const { credentials: explicitCredentials, options } = normalizeClientArgs(target, credentialsOrOptions, maybeOptions);
    const { target: normalizedTarget, credentials } = resolveCredentials(target, explicitCredentials, options);
    const channelOptions = { ...(options.channelOptions || {}) };
    if (options.serverNameOverride) {
      channelOptions["grpc.ssl_target_name_override"] = options.serverNameOverride;
      channelOptions["grpc.default_authority"] = options.serverNameOverride;
    }
    this._createMetadata = createMetadataFactory(options);
    this._retry = normalizeRetryOptions(options.retry);
    this.client = new proto.ControlService(normalizedTarget, credentials, channelOptions);
  }

  close() {
    this.client.close();
  }

  getControlStatus(metadata = undefined) {
    return unary(this.client, "GetControlStatus", {}, this._createMetadata, this._retry, metadata);
  }
}

module.exports = {
  AgentClient,
  ArbiterClient,
  CapabilityServer,
  ControlClient,
  RuntimeClient,
  capabilityProtoPath,
  serviceProtoPath,
};
