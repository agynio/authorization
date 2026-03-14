package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	authorizationv1 "github.com/agynio/authorization/gen/go/agynio/api/authorization/v1"
	openfga "github.com/openfga/go-sdk"
	openfgaclient "github.com/openfga/go-sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	authorizationv1.UnimplementedAuthorizationServiceServer
	client  openfgaclient.SdkClient
	storeID string
	modelID string
}

func New(client openfgaclient.SdkClient, storeID, modelID string) *Server {
	return &Server{client: client, storeID: storeID, modelID: modelID}
}

func (s *Server) Check(ctx context.Context, req *authorizationv1.CheckRequest) (*authorizationv1.CheckResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must be provided")
	}

	tupleKey, err := tupleKeyFromProto(req.GetTupleKey(), "tuple_key")
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	resp, err := s.client.Check(ctx).
		Body(openfgaclient.ClientCheckRequest{
			User:     tupleKey.User,
			Relation: tupleKey.Relation,
			Object:   tupleKey.Object,
		}).
		Options(openfgaclient.ClientCheckOptions{
			AuthorizationModelId: s.modelIDPointer(),
			StoreId:              s.storeIDPointer(),
		}).
		Execute()
	if err != nil {
		return nil, toStatusError(err)
	}
	return &authorizationv1.CheckResponse{Allowed: resp.GetAllowed()}, nil
}

func (s *Server) BatchCheck(ctx context.Context, req *authorizationv1.BatchCheckRequest) (*authorizationv1.BatchCheckResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must be provided")
	}

	checks := req.GetChecks()
	if len(checks) == 0 {
		return nil, status.Error(codes.InvalidArgument, "checks must be provided")
	}

	items := make([]openfgaclient.ClientBatchCheckItem, len(checks))
	for i, check := range checks {
		correlationID := strings.TrimSpace(check.GetCorrelationId())
		if correlationID == "" {
			return nil, status.Errorf(codes.InvalidArgument, "checks[%d].correlation_id must be provided", i)
		}
		tupleKey, err := tupleKeyFromProto(check.GetTupleKey(), fmt.Sprintf("checks[%d].tuple_key", i))
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
		items[i] = openfgaclient.ClientBatchCheckItem{
			User:          tupleKey.User,
			Relation:      tupleKey.Relation,
			Object:        tupleKey.Object,
			CorrelationId: correlationID,
		}
	}

	resp, err := s.client.BatchCheck(ctx).
		Body(openfgaclient.ClientBatchCheckRequest{Checks: items}).
		Options(openfgaclient.BatchCheckOptions{
			AuthorizationModelId: s.modelIDPointer(),
			StoreId:              s.storeIDPointer(),
		}).
		Execute()
	if err != nil {
		return nil, toStatusError(err)
	}

	results := map[string]*authorizationv1.BatchCheckResult{}
	for correlationID, result := range resp.GetResult() {
		protoResult := &authorizationv1.BatchCheckResult{Allowed: result.GetAllowed()}
		if result.Error != nil {
			protoResult.Error = checkErrorMessage(*result.Error)
		}
		results[correlationID] = protoResult
	}

	return &authorizationv1.BatchCheckResponse{Results: results}, nil
}

func (s *Server) Write(ctx context.Context, req *authorizationv1.WriteRequest) (*authorizationv1.WriteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must be provided")
	}

	writes, err := tupleKeysFromProto(req.GetWrites(), "writes")
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	deletes, err := tupleKeysWithoutConditionFromProto(req.GetDeletes(), "deletes")
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if len(writes) == 0 && len(deletes) == 0 {
		return nil, status.Error(codes.InvalidArgument, "writes or deletes must be provided")
	}

	_, err = s.client.Write(ctx).
		Body(openfgaclient.ClientWriteRequest{Writes: writes, Deletes: deletes}).
		Options(openfgaclient.ClientWriteOptions{
			AuthorizationModelId: s.modelIDPointer(),
			StoreId:              s.storeIDPointer(),
		}).
		Execute()
	if err != nil {
		return nil, toStatusError(err)
	}

	return &authorizationv1.WriteResponse{}, nil
}

func (s *Server) Read(ctx context.Context, req *authorizationv1.ReadRequest) (*authorizationv1.ReadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must be provided")
	}

	readRequest, err := readRequestFromProto(req.GetTupleKey())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	options := openfgaclient.ClientReadOptions{StoreId: s.storeIDPointer()}
	if req.GetPageSize() > 0 {
		pageSize := req.GetPageSize()
		options.PageSize = &pageSize
	}
	if token := strings.TrimSpace(req.GetPageToken()); token != "" {
		options.ContinuationToken = &token
	}

	resp, err := s.client.Read(ctx).
		Body(readRequest).
		Options(options).
		Execute()
	if err != nil {
		return nil, toStatusError(err)
	}

	tuples := make([]*authorizationv1.Tuple, len(resp.GetTuples()))
	for i, tuple := range resp.GetTuples() {
		tuples[i] = tupleToProto(tuple)
	}

	return &authorizationv1.ReadResponse{
		Tuples:        tuples,
		NextPageToken: resp.GetContinuationToken(),
	}, nil
}

func (s *Server) ListObjects(ctx context.Context, req *authorizationv1.ListObjectsRequest) (*authorizationv1.ListObjectsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must be provided")
	}

	objectType := strings.TrimSpace(req.GetType())
	if objectType == "" {
		return nil, status.Error(codes.InvalidArgument, "type must be provided")
	}
	relation := strings.TrimSpace(req.GetRelation())
	if relation == "" {
		return nil, status.Error(codes.InvalidArgument, "relation must be provided")
	}
	user := strings.TrimSpace(req.GetUser())
	if user == "" {
		return nil, status.Error(codes.InvalidArgument, "user must be provided")
	}

	resp, err := s.client.ListObjects(ctx).
		Body(openfgaclient.ClientListObjectsRequest{
			User:     user,
			Relation: relation,
			Type:     objectType,
		}).
		Options(openfgaclient.ClientListObjectsOptions{
			AuthorizationModelId: s.modelIDPointer(),
			StoreId:              s.storeIDPointer(),
		}).
		Execute()
	if err != nil {
		return nil, toStatusError(err)
	}

	return &authorizationv1.ListObjectsResponse{Objects: resp.GetObjects()}, nil
}

func (s *Server) ListUsers(ctx context.Context, req *authorizationv1.ListUsersRequest) (*authorizationv1.ListUsersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must be provided")
	}

	objectValue := strings.TrimSpace(req.GetObject())
	if objectValue == "" {
		return nil, status.Error(codes.InvalidArgument, "object must be provided")
	}
	object, err := parseObject(objectValue)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "object: %v", err)
	}

	relation := strings.TrimSpace(req.GetRelation())
	if relation == "" {
		return nil, status.Error(codes.InvalidArgument, "relation must be provided")
	}

	filters := req.GetUserFilters()
	if len(filters) == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_filters must be provided")
	}

	userFilters := make([]openfga.UserTypeFilter, len(filters))
	for i, filter := range filters {
		filterType := strings.TrimSpace(filter.GetType())
		if filterType == "" {
			return nil, status.Errorf(codes.InvalidArgument, "user_filters[%d].type must be provided", i)
		}
		relationValue := strings.TrimSpace(filter.GetRelation())
		var relationPtr *string
		if relationValue != "" {
			relationPtr = &relationValue
		}
		userFilters[i] = openfga.UserTypeFilter{Type: filterType, Relation: relationPtr}
	}

	resp, err := s.client.ListUsers(ctx).
		Body(openfgaclient.ClientListUsersRequest{
			Object:      object,
			Relation:    relation,
			UserFilters: userFilters,
		}).
		Options(openfgaclient.ClientListUsersOptions{
			AuthorizationModelId: s.modelIDPointer(),
			StoreId:              s.storeIDPointer(),
		}).
		Execute()
	if err != nil {
		return nil, toStatusError(err)
	}

	users := make([]*authorizationv1.User, len(resp.GetUsers()))
	for i, user := range resp.GetUsers() {
		protoUser, err := userToProto(user)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "convert user: %v", err)
		}
		users[i] = protoUser
	}

	return &authorizationv1.ListUsersResponse{Users: users}, nil
}

func (s *Server) storeIDPointer() *string {
	return openfga.ToPtr(s.storeID)
}

func (s *Server) modelIDPointer() *string {
	if strings.TrimSpace(s.modelID) == "" {
		return nil
	}
	return openfga.ToPtr(s.modelID)
}

func tupleKeyFromProto(key *authorizationv1.TupleKey, field string) (openfga.TupleKey, error) {
	if key == nil {
		return openfga.TupleKey{}, fmt.Errorf("%s must be provided", field)
	}
	user, err := requiredString(key.GetUser(), field+".user")
	if err != nil {
		return openfga.TupleKey{}, err
	}
	relation, err := requiredString(key.GetRelation(), field+".relation")
	if err != nil {
		return openfga.TupleKey{}, err
	}
	object, err := requiredString(key.GetObject(), field+".object")
	if err != nil {
		return openfga.TupleKey{}, err
	}
	return openfga.TupleKey{User: user, Relation: relation, Object: object}, nil
}

func tupleKeysFromProto(keys []*authorizationv1.TupleKey, field string) ([]openfga.TupleKey, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	values := make([]openfga.TupleKey, len(keys))
	for i, key := range keys {
		value, err := tupleKeyFromProto(key, fmt.Sprintf("%s[%d]", field, i))
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}

func tupleKeysWithoutConditionFromProto(keys []*authorizationv1.TupleKey, field string) ([]openfga.TupleKeyWithoutCondition, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	values := make([]openfga.TupleKeyWithoutCondition, len(keys))
	for i, key := range keys {
		value, err := tupleKeyFromProto(key, fmt.Sprintf("%s[%d]", field, i))
		if err != nil {
			return nil, err
		}
		values[i] = openfga.TupleKeyWithoutCondition{
			User:     value.User,
			Relation: value.Relation,
			Object:   value.Object,
		}
	}
	return values, nil
}

func readRequestFromProto(key *authorizationv1.TupleKey) (openfgaclient.ClientReadRequest, error) {
	if key == nil {
		return openfgaclient.ClientReadRequest{}, fmt.Errorf("tuple_key must be provided")
	}

	user := strings.TrimSpace(key.GetUser())
	relation := strings.TrimSpace(key.GetRelation())
	object := strings.TrimSpace(key.GetObject())

	if user == "" && relation == "" && object == "" {
		return openfgaclient.ClientReadRequest{}, fmt.Errorf("at least one of tuple_key.user, tuple_key.relation, tuple_key.object must be provided")
	}

	readRequest := openfgaclient.ClientReadRequest{}
	if user != "" {
		readRequest.User = &user
	}
	if relation != "" {
		readRequest.Relation = &relation
	}
	if object != "" {
		readRequest.Object = &object
	}

	return readRequest, nil
}

func parseObject(value string) (openfga.FgaObject, error) {
	if value == "" {
		return openfga.FgaObject{}, fmt.Errorf("value must be provided")
	}
	if strings.Contains(value, "#") {
		return openfga.FgaObject{}, fmt.Errorf("value must be in type:id format")
	}
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return openfga.FgaObject{}, fmt.Errorf("value must be in type:id format")
	}
	return openfga.FgaObject{Type: parts[0], Id: parts[1]}, nil
}

func tupleToProto(tuple openfga.Tuple) *authorizationv1.Tuple {
	return &authorizationv1.Tuple{
		Key: &authorizationv1.TupleKey{
			User:     tuple.Key.User,
			Relation: tuple.Key.Relation,
			Object:   tuple.Key.Object,
		},
		Timestamp: timestamppb.New(tuple.Timestamp),
	}
}

func userToProto(user openfga.User) (*authorizationv1.User, error) {
	if user.Object != nil {
		return &authorizationv1.User{User: &authorizationv1.User_Object{Object: formatObject(*user.Object)}}, nil
	}
	if user.Wildcard != nil {
		return &authorizationv1.User{User: &authorizationv1.User_Wildcard{Wildcard: formatWildcard(*user.Wildcard)}}, nil
	}
	if user.Userset != nil {
		return &authorizationv1.User{User: &authorizationv1.User_Object{Object: formatUserset(*user.Userset)}}, nil
	}
	return nil, fmt.Errorf("user has no object, userset, or wildcard")
}

func formatObject(object openfga.FgaObject) string {
	return fmt.Sprintf("%s:%s", object.Type, object.Id)
}

func formatUserset(userset openfga.UsersetUser) string {
	return fmt.Sprintf("%s:%s#%s", userset.Type, userset.Id, userset.Relation)
}

func formatWildcard(wildcard openfga.TypedWildcard) string {
	return fmt.Sprintf("%s:*", wildcard.Type)
}

func requiredString(value, field string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s must be provided", field)
	}
	return trimmed, nil
}

func checkErrorMessage(err openfga.CheckError) string {
	if err.Message != nil {
		message := strings.TrimSpace(*err.Message)
		if message != "" {
			return message
		}
	}
	if err.InputError != nil && *err.InputError != openfga.ERRORCODE_NO_ERROR {
		return string(*err.InputError)
	}
	if err.InternalError != nil && *err.InternalError != openfga.INTERNALERRORCODE_NO_INTERNAL_ERROR {
		return string(*err.InternalError)
	}
	return "check failed"
}

func toStatusError(err error) error {
	var requiredErr openfgaclient.FgaRequiredParamError
	if errors.As(err, &requiredErr) {
		return status.Error(codes.InvalidArgument, requiredErr.Error())
	}
	var invalidErr openfgaclient.FgaInvalidError
	if errors.As(err, &invalidErr) {
		return status.Error(codes.InvalidArgument, invalidErr.Error())
	}
	var authErr openfga.FgaApiAuthenticationError
	if errors.As(err, &authErr) {
		return status.Error(codes.Unauthenticated, authErr.Error())
	}
	var validationErr openfga.FgaApiValidationError
	if errors.As(err, &validationErr) {
		return status.Error(codes.InvalidArgument, validationErr.Error())
	}
	var notFoundErr openfga.FgaApiNotFoundError
	if errors.As(err, &notFoundErr) {
		return status.Error(codes.NotFound, notFoundErr.Error())
	}
	var rateLimitErr openfga.FgaApiRateLimitExceededError
	if errors.As(err, &rateLimitErr) {
		return status.Error(codes.ResourceExhausted, rateLimitErr.Error())
	}
	var internalErr openfga.FgaApiInternalError
	if errors.As(err, &internalErr) {
		return status.Error(codes.Internal, internalErr.Error())
	}
	var apiErr openfga.FgaApiError
	if errors.As(err, &apiErr) {
		return status.Error(codes.Internal, apiErr.Error())
	}
	var genericErr openfga.GenericOpenAPIError
	if errors.As(err, &genericErr) {
		return status.Error(codes.Internal, genericErr.Error())
	}
	return status.Errorf(codes.Internal, "internal error: %v", err)
}
