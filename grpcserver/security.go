package grpcserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// StaticTokenAuth validates one of a fixed set of bearer tokens.
type StaticTokenAuth struct {
	tokens map[string]struct{}
}

// NewStaticTokenAuth creates an auth validator for one or more tokens.
func NewStaticTokenAuth(tokens []string) (*StaticTokenAuth, error) {
	allowed := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		allowed[token] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil, errors.New("at least one non-empty auth token is required")
	}
	return &StaticTokenAuth{tokens: allowed}, nil
}

// UnaryServerInterceptor returns a unary auth interceptor.
func (a *StaticTokenAuth) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	if a == nil || len(a.tokens) == 0 {
		return nil
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := a.authorize(ctx); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns a stream auth interceptor.
func (a *StaticTokenAuth) StreamServerInterceptor() grpc.StreamServerInterceptor {
	if a == nil || len(a.tokens) == 0 {
		return nil
	}
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := a.authorize(ss.Context()); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

func (a *StaticTokenAuth) authorize(ctx context.Context) error {
	token := ClientAuthToken(ctx)
	if token == "" {
		return status.Error(codes.Unauthenticated, "missing authorization token")
	}
	if _, ok := a.tokens[token]; !ok {
		return status.Error(codes.Unauthenticated, "invalid authorization token")
	}
	return nil
}

// ClientAuthToken extracts the caller token from gRPC metadata.
func ClientAuthToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if values := md.Get("x-arbiter-token"); len(values) > 0 {
		if token := strings.TrimSpace(values[0]); token != "" {
			return token
		}
	}
	for _, value := range md.Get("authorization") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(value), "bearer ") {
			value = strings.TrimSpace(value[7:])
		}
		if value != "" {
			return value
		}
	}
	return ""
}

// ClientIdentity returns a stable per-caller identity for rate limits and session ownership.
// Token-authenticated callers are identified by a truncated hash of the token so the raw
// credential never appears in error messages or logs. Unauthenticated callers fall back to
// peer address.
func ClientIdentity(ctx context.Context) string {
	if token := ClientAuthToken(ctx); token != "" {
		h := sha256.Sum256([]byte(token))
		return "token:" + hex.EncodeToString(h[:8])
	}
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return "peer:" + p.Addr.String()
	}
	return "anonymous"
}

type tokenBucketLimiter struct {
	mu         sync.Mutex
	perMinute  int
	burst      int
	buckets    map[string]bucketState
	operations uint64
}

type bucketState struct {
	tokens    float64
	updatedAt time.Time
}

// NewRateLimiter creates a token-bucket limiter keyed by caller identity.
func NewRateLimiter(perMinute, burst int) *tokenBucketLimiter {
	if perMinute <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = perMinute
	}
	return &tokenBucketLimiter{
		perMinute: perMinute,
		burst:     burst,
		buckets:   make(map[string]bucketState),
	}
}

// UnaryServerInterceptor returns a unary rate-limit interceptor.
func (l *tokenBucketLimiter) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	if l == nil {
		return nil
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := l.allow(ctx, time.Now().UTC()); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns a stream rate-limit interceptor.
func (l *tokenBucketLimiter) StreamServerInterceptor() grpc.StreamServerInterceptor {
	if l == nil {
		return nil
	}
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := l.allow(ss.Context(), time.Now().UTC()); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

func (l *tokenBucketLimiter) allow(ctx context.Context, now time.Time) error {
	key := ClientIdentity(ctx)
	retryAfter, ok := l.take(key, now)
	if ok {
		return nil
	}
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	return status.Errorf(codes.ResourceExhausted, "rate limit exceeded; retry after %s", retryAfter.Round(time.Second))
}

func (l *tokenBucketLimiter) take(key string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.operations++
	if l.operations%1024 == 0 {
		l.pruneLocked(now)
	}

	state := l.buckets[key]
	if state.updatedAt.IsZero() {
		state.updatedAt = now
		state.tokens = float64(l.burst)
	} else {
		elapsed := now.Sub(state.updatedAt).Seconds()
		refillPerSecond := float64(l.perMinute) / 60
		state.tokens = min(float64(l.burst), state.tokens+(elapsed*refillPerSecond))
		state.updatedAt = now
	}
	if state.tokens >= 1 {
		state.tokens -= 1
		l.buckets[key] = state
		return 0, true
	}
	l.buckets[key] = state
	deficit := 1 - state.tokens
	retryAfter := time.Duration(deficit / (float64(l.perMinute) / 60) * float64(time.Second))
	return retryAfter, false
}

func (l *tokenBucketLimiter) pruneLocked(now time.Time) {
	if len(l.buckets) == 0 {
		return
	}
	expireBefore := now.Add(-10 * time.Minute)
	for key, state := range l.buckets {
		if state.updatedAt.Before(expireBefore) {
			delete(l.buckets, key)
		}
	}
}

// UnaryRecoveryInterceptor converts handler panics into gRPC internal errors.
func UnaryRecoveryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				if logger != nil {
					logger.Error("grpc unary panic recovered", "method", info.FullMethod, "panic", fmt.Sprint(r))
				}
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// StreamRecoveryInterceptor converts handler panics into gRPC internal errors.
func StreamRecoveryInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				if logger != nil {
					logger.Error("grpc stream panic recovered", "method", info.FullMethod, "panic", fmt.Sprint(r))
				}
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(srv, ss)
	}
}

