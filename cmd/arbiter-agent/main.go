package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/audit"
	"github.com/odvcencio/arbiter/dataplane"
	"github.com/odvcencio/arbiter/grpcserver"
	"github.com/odvcencio/arbiter/internal/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type bundleNamesFlag struct {
	values []string
	seen   map[string]struct{}
}

func (f *bundleNamesFlag) String() string {
	return strings.Join(f.values, ",")
}

func (f *bundleNamesFlag) Set(value string) error {
	for _, name := range splitBundleNames(value) {
		if f.seen == nil {
			f.seen = make(map[string]struct{})
		}
		if _, ok := f.seen[name]; ok {
			continue
		}
		f.seen[name] = struct{}{}
		f.values = append(f.values, name)
	}
	return nil
}

func (f *bundleNamesFlag) Values() []string {
	return append([]string(nil), f.values...)
}

func main() {
	authTokens := []string{}
	bundleNames := bundleNamesFlag{}
	if err := bundleNames.Set(envOr("ARBITER_BUNDLE_NAMES", envOr("ARBITER_BUNDLE_NAME", ""))); err != nil {
		log.Fatalf("parse bundle names: %v", err)
	}
	readyMaxStalenessDefault, err := parseDurationEnv("ARBITER_AGENT_READY_MAX_STALENESS", "0s")
	if err != nil {
		log.Fatalf("parse ARBITER_AGENT_READY_MAX_STALENESS: %v", err)
	}
	var (
		upstreamAddr       = flag.String("upstream", envOr("ARBITER_UPSTREAM_ADDR", "127.0.0.1:8081"), "upstream Arbiter control-plane gRPC address")
		upstreamToken      = flag.String("upstream-token", envOr("ARBITER_UPSTREAM_TOKEN", ""), "optional bearer token for upstream Arbiter control plane")
		upstreamCAFile     = flag.String("upstream-ca-file", envOr("ARBITER_UPSTREAM_CA_FILE", ""), "optional PEM CA bundle for upstream TLS verification")
		upstreamServerName = flag.String("upstream-server-name", envOr("ARBITER_UPSTREAM_SERVER_NAME", ""), "optional upstream TLS server name override")
		upstreamPlaintext  = flag.Bool("upstream-plaintext", envOrBool("ARBITER_UPSTREAM_PLAINTEXT", false), "force plaintext transport to the upstream even when TLS options are set")
		listenAddr         = flag.String("grpc", envOr("ARBITER_AGENT_ADDR", "127.0.0.1:7081"), "local agent gRPC listen address")
		authTokenFile      = flag.String("auth-token-file", envOr("ARBITER_AGENT_AUTH_TOKEN_FILE", ""), "optional file with one or more local agent bearer tokens")
		tlsCertFile        = flag.String("tls-cert", envOr("ARBITER_AGENT_TLS_CERT", ""), "optional PEM certificate for local agent gRPC")
		tlsKeyFile         = flag.String("tls-key", envOr("ARBITER_AGENT_TLS_KEY", ""), "optional PEM private key for local agent gRPC")
		tlsClientCAFile    = flag.String("tls-client-ca", envOr("ARBITER_AGENT_TLS_CLIENT_CA", ""), "optional PEM CA bundle for local client certificate verification")
		statusAddr         = flag.String("status", envOr("ARBITER_AGENT_STATUS_ADDR", "127.0.0.1:7082"), "local agent health/status HTTP listen address")
		overridesFile      = flag.String("overrides-file", envOr("ARBITER_OVERRIDES_FILE", ""), "optional override snapshot file to sync")
		readyMaxStaleness  = flag.Duration("ready-max-staleness", readyMaxStalenessDefault, "max acceptable age for bundle/override sync before /readyz returns 503; 0 disables freshness enforcement")
	)
	flag.Var(&bundleNames, "bundle-name", "active bundle name to sync from the control plane; repeat or comma-separate to sync multiple bundles")
	flag.Func("auth-token", "repeatable local agent bearer token", func(value string) error {
		authTokens = append(authTokens, value)
		return nil
	})
	flag.Parse()

	names := bundleNames.Values()
	if len(names) == 0 {
		log.Fatal("at least one --bundle-name is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	upstreamConn, normalizedUpstream, err := dialUpstream(upstreamDialConfig{
		target:        *upstreamAddr,
		token:         *upstreamToken,
		caFile:        *upstreamCAFile,
		serverName:    *upstreamServerName,
		forceInsecure: *upstreamPlaintext,
	})
	if err != nil {
		log.Fatalf("connect upstream: %v", err)
	}
	defer func() { _ = upstreamConn.Close() }()
	upstreamTransport, err := describeUpstreamTransport(upstreamDialConfig{
		target:        *upstreamAddr,
		token:         *upstreamToken,
		caFile:        *upstreamCAFile,
		serverName:    *upstreamServerName,
		forceInsecure: *upstreamPlaintext,
	})
	if err != nil {
		log.Fatalf("describe upstream transport: %v", err)
	}
	localAuthTokens, err := grpcutil.LoadAuthTokens(authTokens, *authTokenFile)
	if err != nil {
		log.Fatalf("load local auth tokens: %v", err)
	}
	localTLSConfig, err := grpcutil.LoadServerTLSConfig(*tlsCertFile, *tlsKeyFile, *tlsClientCAFile)
	if err != nil {
		log.Fatalf("load local TLS config: %v", err)
	}
	controlTransport := newAgentControlTransport(*listenAddr, localAuthTokens, localTLSConfig)

	upstreamCP := dataplane.NewGRPCControlPlane(arbiterv1.NewArbiterServiceClient(upstreamConn))
	var overrideCP dataplane.OverrideControlPlane = upstreamCP
	if *overridesFile != "" {
		overrideCP = dataplane.NewFileOverrideControlPlane(*overridesFile)
	}
	syncer := dataplane.New(upstreamCP, overrideCP)
	statusPolicy := readinessPolicy{maxStaleness: *readyMaxStaleness}
	statusTransport := agentTransportStatus{
		Control:  controlTransport,
		Upstream: upstreamTransport,
	}

	localListener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", *listenAddr, err)
	}
	defer func() { _ = localListener.Close() }()

	unaryInterceptors := []grpc.UnaryServerInterceptor{
		grpcserver.UnaryRecoveryInterceptor(nil),
	}
	streamInterceptors := []grpc.StreamServerInterceptor{
		grpcserver.StreamRecoveryInterceptor(nil),
	}
	if len(localAuthTokens) > 0 {
		auth, err := grpcserver.NewStaticTokenAuth(localAuthTokens)
		if err != nil {
			log.Fatalf("configure local auth: %v", err)
		}
		unaryInterceptors = append(unaryInterceptors, auth.UnaryServerInterceptor())
		streamInterceptors = append(streamInterceptors, auth.StreamServerInterceptor())
	}
	serverOptions := []grpc.ServerOption{}
	if len(unaryInterceptors) > 0 {
		serverOptions = append(serverOptions, grpc.ChainUnaryInterceptor(unaryInterceptors...))
	}
	if len(streamInterceptors) > 0 {
		serverOptions = append(serverOptions, grpc.ChainStreamInterceptor(streamInterceptors...))
	}
	if localTLSConfig != nil {
		serverOptions = append(serverOptions, grpc.Creds(credentials.NewTLS(localTLSConfig)))
	}

	localServer := grpc.NewServer(serverOptions...)
	arbiterv1.RegisterArbiterServiceServer(localServer, grpcserver.NewServer(syncer.Registry(), syncer.Overrides(), audit.NopSink{}))
	arbiterv1.RegisterAgentServiceServer(localServer, newAgentRPCServer(syncer, statusPolicy, statusTransport))
	go func() {
		if controlTransport.PublicListener && !controlTransport.AuthEnabled && !controlTransport.TLSEnabled {
			log.Printf("arbiter-agent: local gRPC listener is public without TLS or auth addr=%s", *listenAddr)
		}
		log.Printf(
			"arbiter-agent: local gRPC listening addr=%s auth_enabled=%t tls_enabled=%t mutual_tls_enabled=%t",
			*listenAddr,
			controlTransport.AuthEnabled,
			controlTransport.TLSEnabled,
			controlTransport.MutualTLSEnabled,
		)
		if serveErr := localServer.Serve(localListener); serveErr != nil && ctx.Err() == nil {
			log.Printf("arbiter-agent serve: %v", serveErr)
			stop()
		}
	}()
	defer localServer.Stop()

	var statusServer *http.Server
	if *statusAddr != "" {
		statusServer = &http.Server{
			Addr:              *statusAddr,
			Handler:           newStatusHandler(syncer, statusPolicy, statusTransport),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			if serveErr := statusServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && ctx.Err() == nil {
				log.Printf("arbiter-agent status: %v", serveErr)
				stop()
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = statusServer.Shutdown(shutdownCtx)
		}()
	}

	go func() {
		if runErr := syncer.RunMany(ctx, names); runErr != nil && runErr != context.Canceled {
			log.Printf("arbiter-agent sync: %v", runErr)
			stop()
		}
	}()

	select {
	case <-syncer.Ready():
		status := syncer.Status()
		if snap, ok := syncer.Current(); ok {
			log.Printf(
				"arbiter-agent: synced primary=%s bundle=%s checksum=%s bundles=%d listening=%s upstream=%s status=%s ready_max_staleness=%s",
				status.PrimaryName,
				snap.Bundle.Name,
				snap.Bundle.Checksum,
				len(status.Bundles),
				*listenAddr,
				normalizedUpstream,
				*statusAddr,
				readyMaxStaleness.String(),
			)
		}
	case <-ctx.Done():
		return
	}

	<-ctx.Done()
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envOrBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func splitBundleNames(value string) []string {
	parts := strings.Split(value, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func parseDurationEnv(key, fallback string) (time.Duration, error) {
	value := envOr(key, fallback)
	if value == "" {
		return 0, nil
	}
	return time.ParseDuration(value)
}

type upstreamDialConfig struct {
	target        string
	token         string
	caFile        string
	serverName    string
	forceInsecure bool
}

func dialUpstream(cfg upstreamDialConfig) (*grpc.ClientConn, string, error) {
	return grpcutil.Dial(grpcutil.DialConfig{
		Target:        cfg.target,
		Token:         cfg.token,
		CAFile:        cfg.caFile,
		ServerName:    cfg.serverName,
		ForceInsecure: cfg.forceInsecure,
	})
}

func describeUpstreamTransport(cfg upstreamDialConfig) (agentUpstreamTransport, error) {
	target, tlsEnabled, err := grpcutil.NormalizeTarget(cfg.target, cfg.forceInsecure, cfg.caFile != "" || cfg.serverName != "")
	if err != nil {
		return agentUpstreamTransport{}, err
	}
	return newAgentUpstreamTransport(target, strings.TrimSpace(cfg.token) != "", tlsEnabled, cfg.serverName), nil
}
