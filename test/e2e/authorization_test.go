//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	authorizationv1 "github.com/agynio/authorization/.gen/go/agynio/api/authorization/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestAuthorizationCheckRequiresTupleKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, authorizationAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial authorization: %v", err)
	}
	defer conn.Close()

	client := authorizationv1.NewAuthorizationServiceClient(conn)

	_, err = client.Check(ctx, &authorizationv1.CheckRequest{})
	if err == nil {
		t.Fatal("expected invalid argument error")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %s: %s", st.Code(), st.Message())
	}
}
