package grpcserver

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestStaticTokenAuthUnaryAllowsBearerToken(t *testing.T) {
	auth, err := NewStaticTokenAuth([]string{"top-secret"})
	if err != nil {
		t.Fatalf("NewStaticTokenAuth: %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer top-secret"))
	called := false
	_, err = auth.UnaryServerInterceptor()(ctx, "ignored", &grpc.UnaryServerInfo{FullMethod: "/arbiter.v1.ArbiterService/PublishBundle"}, func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("auth interceptor: %v", err)
	}
	if !called {
		t.Fatal("expected handler to run")
	}
}

func TestStaticTokenAuthUnaryRejectsMissingToken(t *testing.T) {
	auth, err := NewStaticTokenAuth([]string{"top-secret"})
	if err != nil {
		t.Fatalf("NewStaticTokenAuth: %v", err)
	}
	_, err = auth.UnaryServerInterceptor()(context.Background(), "ignored", &grpc.UnaryServerInfo{FullMethod: "/arbiter.v1.ArbiterService/PublishBundle"}, func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not run")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}
}

func TestRateLimiterRejectsBurstOverflowPerIdentity(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	now := time.Now().UTC()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer tenant-a"))
	if err := limiter.allow(ctx, now); err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
	if err := limiter.allow(ctx, now); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("expected ResourceExhausted, got %v", err)
	}
	other := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer tenant-b"))
	if err := limiter.allow(other, now); err != nil {
		t.Fatalf("other identity should get its own bucket: %v", err)
	}
}

func TestUnaryRecoveryInterceptorConvertsPanic(t *testing.T) {
	interceptor := UnaryRecoveryInterceptor(nil)
	_, err := interceptor(context.Background(), "ignored", &grpc.UnaryServerInfo{FullMethod: "/arbiter.v1.ArbiterService/PublishBundle"}, func(ctx context.Context, req any) (any, error) {
		panic("boom")
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal, got %v", err)
	}
}
