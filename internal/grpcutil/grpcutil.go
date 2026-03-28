package grpcutil

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// DialConfig describes one outbound gRPC connection.
type DialConfig struct {
	Target        string
	Token         string
	CAFile        string
	ServerName    string
	ForceInsecure bool
}

// Dial opens one outbound gRPC connection with consistent Arbiter auth/TLS semantics.
func Dial(cfg DialConfig) (*grpc.ClientConn, string, error) {
	target, secure, err := NormalizeTarget(cfg.Target, cfg.ForceInsecure, cfg.CAFile != "" || cfg.ServerName != "")
	if err != nil {
		return nil, "", err
	}
	transportCreds, err := LoadClientTransportCredentials(secure, cfg.CAFile, cfg.ServerName)
	if err != nil {
		return nil, "", err
	}
	opts := []grpc.DialOption{grpc.WithTransportCredentials(transportCreds)}
	if strings.TrimSpace(cfg.Token) != "" {
		opts = append(opts,
			grpc.WithUnaryInterceptor(BearerUnaryInterceptor(cfg.Token)),
			grpc.WithStreamInterceptor(BearerStreamInterceptor(cfg.Token)),
		)
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, "", err
	}
	return conn, target, nil
}

// NormalizeTarget resolves one user-facing gRPC target to a dial address plus transport mode.
func NormalizeTarget(target string, forceInsecure bool, tlsHint bool) (string, bool, error) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", false, errors.New("gRPC target is required")
	}
	switch {
	case strings.HasPrefix(trimmed, "https://"):
		if forceInsecure {
			return "", false, fmt.Errorf("cannot combine secure target %q with plaintext transport", trimmed)
		}
		return strings.TrimPrefix(trimmed, "https://"), true, nil
	case strings.HasPrefix(trimmed, "grpcs://"):
		if forceInsecure {
			return "", false, fmt.Errorf("cannot combine secure target %q with plaintext transport", trimmed)
		}
		return strings.TrimPrefix(trimmed, "grpcs://"), true, nil
	case strings.HasPrefix(trimmed, "http://"):
		return strings.TrimPrefix(trimmed, "http://"), false, nil
	case strings.HasPrefix(trimmed, "grpc://"):
		return strings.TrimPrefix(trimmed, "grpc://"), false, nil
	default:
		return trimmed, !forceInsecure && tlsHint, nil
	}
}

// LoadAuthTokens merges CLI tokens plus an optional token file into one deduplicated list.
func LoadAuthTokens(cliTokens []string, tokenFile string) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(cliTokens))
	addToken := func(raw string) {
		token := strings.TrimSpace(raw)
		if token == "" {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	for _, token := range cliTokens {
		addToken(token)
	}
	if tokenFile == "" {
		return out, nil
	}
	file, err := os.Open(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("open auth token file: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		for _, token := range strings.Split(scanner.Text(), ",") {
			addToken(token)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read auth token file: %w", err)
	}
	return out, nil
}

// LoadServerTLSConfig loads optional server TLS or mTLS configuration.
func LoadServerTLSConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if certFile == "" && keyFile == "" && clientCAFile == "" {
		return nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("--tls-cert and --tls-key must be provided together")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS key pair: %w", err)
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if clientCAFile != "" {
		caBytes, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("parse client CA file: no certificates found")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}

// LoadClientTransportCredentials loads TLS or plaintext client transport credentials.
func LoadClientTransportCredentials(secure bool, caFile, serverName string) (credentials.TransportCredentials, error) {
	if !secure {
		return insecure.NewCredentials(), nil
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: serverName,
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("parse CA bundle: no certificates found")
		}
		tlsConfig.RootCAs = pool
	}
	return credentials.NewTLS(tlsConfig), nil
}

// BearerUnaryInterceptor attaches one bearer token to unary RPCs.
func BearerUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(WithBearerToken(ctx, token), method, req, reply, cc, opts...)
	}
}

// BearerStreamInterceptor attaches one bearer token to streaming RPCs.
func BearerStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(WithBearerToken(ctx, token), desc, cc, method, opts...)
	}
}

// WithBearerToken appends one bearer token to outgoing metadata.
func WithBearerToken(ctx context.Context, token string) context.Context {
	token = strings.TrimSpace(token)
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// IsPublicListenAddr reports whether a listen address is reachable beyond localhost.
func IsPublicListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return true
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip == nil || !ip.IsLoopback()
}
