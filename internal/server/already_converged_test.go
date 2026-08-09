package server

import (
	"testing"

	openfga "github.com/openfga/go-sdk"
	"google.golang.org/grpc/codes"
)

// The real message, as the SDK assembles it: the operation line, then the code
// and the message OpenFGA returned.
const duplicateWrite = "POST validation error for Write POST with body {...} with error code " +
	"write_failed_due_to_invalid_input error message: cannot write a tuple which already exists: " +
	"user: 'identity:2202fd0a-2bd2-4874-976c-b602fb217112', relation: 'admin', object: 'cluster:global'"

const missingDelete = "POST validation error for Write POST with body {...} with error code " +
	"write_failed_due_to_invalid_input error message: cannot delete a tuple which does not exist: " +
	"user: 'identity:2202fd0a-2bd2-4874-976c-b602fb217112', relation: 'admin', object: 'cluster:global'"

// A grant that cannot survive being repeated is not a grant anything converges
// on: this arrives as a validation error, which callers read as "you are wrong,
// do not retry", and it stranded every declared cluster administrator.
func TestAlreadyConvergedCode(t *testing.T) {
	cases := []struct {
		name    string
		code    openfga.ErrorCode
		message string
		want    codes.Code
	}{
		{
			name:    "writing a tuple that exists is the state already holding",
			code:    openfga.ERRORCODE_WRITE_FAILED_DUE_TO_INVALID_INPUT,
			message: duplicateWrite,
			want:    codes.AlreadyExists,
		},
		{
			name:    "deleting one that does not is too",
			code:    openfga.ERRORCODE_WRITE_FAILED_DUE_TO_INVALID_INPUT,
			message: missingDelete,
			want:    codes.NotFound,
		},
		{
			name:    "a genuinely malformed write stays invalid",
			code:    openfga.ERRORCODE_WRITE_FAILED_DUE_TO_INVALID_INPUT,
			message: "... error message: invalid tuple key: object is required",
			want:    codes.OK,
		},
		{
			name:    "so does any other validation failure",
			code:    openfga.ERRORCODE_VALIDATION_ERROR,
			message: duplicateWrite,
			want:    codes.OK,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := alreadyConvergedCode(testCase.code, testCase.message); got != testCase.want {
				t.Fatalf("expected %v, got %v", testCase.want, got)
			}
		})
	}
}
