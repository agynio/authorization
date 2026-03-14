package server

import (
	"errors"
	"strings"
	"testing"

	authorizationv1 "github.com/agynio/authorization/gen/go/agynio/api/authorization/v1"
	openfga "github.com/openfga/go-sdk"
	openfgaclient "github.com/openfga/go-sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTupleKeyFromProto(t *testing.T) {
	key := &authorizationv1.TupleKey{
		User:     "identity:user_1",
		Relation: "member",
		Object:   "tenant:tenant_1",
	}

	got, err := tupleKeyFromProto(key, "tuple_key")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.User != key.User || got.Relation != key.Relation || got.Object != key.Object {
		t.Fatalf("unexpected tuple key: %+v", got)
	}

	_, err = tupleKeyFromProto(&authorizationv1.TupleKey{Relation: "member", Object: "tenant:tenant_1"}, "tuple_key")
	if err == nil || !strings.Contains(err.Error(), "tuple_key.user") {
		t.Fatalf("expected missing user error, got %v", err)
	}
}

func TestReadRequestFromProto(t *testing.T) {
	_, err := readRequestFromProto(nil)
	if err == nil {
		t.Fatal("expected error for nil tuple_key")
	}

	_, err = readRequestFromProto(&authorizationv1.TupleKey{})
	if err == nil {
		t.Fatal("expected error for empty tuple_key")
	}

	readRequest, err := readRequestFromProto(&authorizationv1.TupleKey{User: "identity:user_1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if readRequest.User == nil || *readRequest.User != "identity:user_1" {
		t.Fatalf("expected user to be set, got %v", readRequest.User)
	}
	if readRequest.Relation != nil || readRequest.Object != nil {
		t.Fatalf("expected only user to be set, got relation=%v object=%v", readRequest.Relation, readRequest.Object)
	}
}

func TestParseObject(t *testing.T) {
	obj, err := parseObject("tenant:tenant_1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if obj.Type != "tenant" || obj.Id != "tenant_1" {
		t.Fatalf("unexpected object: %+v", obj)
	}

	_, err = parseObject("")
	if err == nil {
		t.Fatal("expected error for empty object")
	}

	_, err = parseObject("tenant")
	if err == nil {
		t.Fatal("expected error for missing id")
	}

	_, err = parseObject("tenant:tenant_1#member")
	if err == nil {
		t.Fatal("expected error for userset format")
	}
}

func TestUserToProto(t *testing.T) {
	protoUser, err := userToProto(openfga.User{Object: &openfga.FgaObject{Type: "identity", Id: "user_1"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if protoUser.GetObject() != "identity:user_1" {
		t.Fatalf("unexpected object user: %s", protoUser.GetObject())
	}

	protoUser, err = userToProto(openfga.User{Wildcard: &openfga.TypedWildcard{Type: "identity"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if protoUser.GetWildcard() != "identity:*" {
		t.Fatalf("unexpected wildcard user: %s", protoUser.GetWildcard())
	}

	protoUser, err = userToProto(openfga.User{Userset: &openfga.UsersetUser{Type: "group", Id: "eng", Relation: "member"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if protoUser.GetObject() != "group:eng#member" {
		t.Fatalf("unexpected userset user: %s", protoUser.GetObject())
	}

	_, err = userToProto(openfga.User{})
	if err == nil {
		t.Fatal("expected error for empty user")
	}
}

func TestCheckErrorMessage(t *testing.T) {
	message := "explicit failure"
	if got := checkErrorMessage(openfga.CheckError{Message: &message}); got != message {
		t.Fatalf("expected message %q, got %q", message, got)
	}

	inputErr := openfga.ERRORCODE_INVALID_TUPLE
	if got := checkErrorMessage(openfga.CheckError{InputError: &inputErr}); got != string(inputErr) {
		t.Fatalf("expected input error %q, got %q", inputErr, got)
	}

	internalErr := openfga.INTERNALERRORCODE_INTERNAL_ERROR
	if got := checkErrorMessage(openfga.CheckError{InternalError: &internalErr}); got != string(internalErr) {
		t.Fatalf("expected internal error %q, got %q", internalErr, got)
	}

	if got := checkErrorMessage(openfga.CheckError{}); got != "check failed" {
		t.Fatalf("expected default message, got %q", got)
	}
}

func TestToStatusError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{"required", openfgaclient.FgaRequiredParamError{}, codes.InvalidArgument},
		{"invalid", openfgaclient.FgaInvalidError{}, codes.InvalidArgument},
		{"auth", openfga.FgaApiAuthenticationError{}, codes.Unauthenticated},
		{"validation", openfga.FgaApiValidationError{}, codes.InvalidArgument},
		{"notfound", openfga.FgaApiNotFoundError{}, codes.NotFound},
		{"ratelimit", openfga.FgaApiRateLimitExceededError{}, codes.ResourceExhausted},
		{"internal", openfga.FgaApiInternalError{}, codes.Internal},
		{"api", openfga.FgaApiError{}, codes.Internal},
		{"generic", openfga.GenericOpenAPIError{}, codes.Internal},
		{"fallback", errors.New("boom"), codes.Internal},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := status.Code(toStatusError(tc.err)); got != tc.code {
				t.Fatalf("expected code %v, got %v", tc.code, got)
			}
		})
	}
}

func TestModelIDPointer(t *testing.T) {
	server := &Server{modelID: ""}
	if server.modelIDPointer() != nil {
		t.Fatal("expected nil model ID pointer when model ID is empty")
	}

	server.modelID = "01H0H015178Y2V4CX10C2KGHF4"
	modelID := server.modelIDPointer()
	if modelID == nil || *modelID != server.modelID {
		t.Fatalf("expected model ID pointer to be %q, got %v", server.modelID, modelID)
	}
}
