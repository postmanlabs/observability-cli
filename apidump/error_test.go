package apidump

import (
	"testing"

	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestNewApidumpError(t *testing.T) {
	err := NewApidumpError(api_schema.ApidumpError_PCAPPermission, "test")
	assert.Error(t, err)
	assert.Equal(t, api_schema.ApidumpError_PCAPPermission, GetErrorType(err))
	assert.Equal(t, err.Error(), "test")
}

func TestNewApidumpErrorf(t *testing.T) {
	err := NewApidumpErrorf(api_schema.ApidumpError_PCAPPermission, "test %d", 1)
	assert.Error(t, err)
	assert.Equal(t, api_schema.ApidumpError_PCAPPermission, GetErrorType(err))
	assert.Equal(t, err.Error(), "test 1")
}

func TestGetErrorType(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected api_schema.ApidumpErrorType
	}{
		{
			name:     "nil",
			err:      nil,
			expected: api_schema.ApidumpError_Other,
		},
		{
			name:     "other error",
			err:      errors.New("other error"),
			expected: api_schema.ApidumpError_Other,
		},
		{
			name:     "pcap permission error",
			err:      NewApidumpError(api_schema.ApidumpError_PCAPPermission, "test"),
			expected: api_schema.ApidumpError_PCAPPermission,
		},
		{
			name:     "wrapped",
			err:      errors.Wrap(NewApidumpError(api_schema.ApidumpError_PCAPPermission, "test"), "wrapped"),
			expected: api_schema.ApidumpError_PCAPPermission,
		},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.expected, GetErrorType(tc.err), "["+tc.name+"] GetErrorType")
		assert.Equal(t, tc.expected, GetErrorTypeWithDefault(tc.err, api_schema.ApidumpError_Other), "["+tc.name+"] GetErrorTypeWithDefault")
	}
}
