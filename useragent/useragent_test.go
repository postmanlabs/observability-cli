package useragent

import (
	"testing"

	ver "github.com/hashicorp/go-version"
	"github.com/stretchr/testify/assert"
)

func TestString(t *testing.T) {
	ua := UA{
		Version: ver.Must(ver.NewSemver("1.2.3")),
		OS:      "linux",
		Arch:    "amd64",
		EnvType: ENV_DOCKER,
	}
	assert.Equal(t, "akita-cli/1.2.3 (linux; amd64; docker)", ua.String())
}

func TestFromString(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		expected  UA
		expectErr bool
	}{
		{
			name:  "good case",
			input: "akita-cli/1.2.3 (linux; amd64; docker)",
			expected: UA{
				Version: ver.Must(ver.NewSemver("1.2.3")),
				OS:      "linux",
				Arch:    "amd64",
				EnvType: ENV_DOCKER,
			},
		},
		{
			name:  "empty os is ok",
			input: "akita-cli/1.2.3 (; amd64; docker)",
			expected: UA{
				Version: ver.Must(ver.NewSemver("1.2.3")),
				OS:      "",
				Arch:    "amd64",
				EnvType: ENV_DOCKER,
			},
		},
		{
			name:  "empty arch is ok",
			input: "akita-cli/1.2.3 (linux; ; docker)",
			expected: UA{
				Version: ver.Must(ver.NewSemver("1.2.3")),
				OS:      "linux",
				Arch:    "",
				EnvType: ENV_DOCKER,
			},
		},
		{
			name:      "bad version",
			input:     "akita-cli/1.2x.3 (linux; amd64; docker)",
			expectErr: true,
		},
		{
			name:      "bad env",
			input:     "akita-cli/1.2.3 (linux; amd64; i-dont-exist)",
			expectErr: true,
		},
	}

	for _, c := range testCases {
		actual, err := FromString(c.input)
		if c.expectErr {
			assert.Error(t, err, c.name)
		} else {
			assert.NoError(t, err, c.name)
			assert.Equal(t, c.expected, actual, c.name)
		}
	}
}
