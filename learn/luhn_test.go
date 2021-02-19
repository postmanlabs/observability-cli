package learn

import (
	"testing"
)

type luhnTest struct {
	number  string
	isValid bool
}

func TestLuhn(t *testing.T) {
	// test numbers generated with https://www.dcode.fr/luhn-algorithm
	tests := []luhnTest{
		luhnTest{
			number:  "918935682500299092",
			isValid: true,
		},
		luhnTest{
			number:  "723995839039616367",
			isValid: true,
		},
		luhnTest{
			number:  "1678238893127352",
			isValid: true,
		},
		luhnTest{
			number:  "42", // would normally be valid but is too short
			isValid: false,
		},
		luhnTest{
			number:  "creepybugs",
			isValid: false,
		},
		luhnTest{
			number:  "1679999999999352",
			isValid: false,
		},
	}

	for _, test := range tests {
		if returnedValid := ValidLuhn(test.number); returnedValid != test.isValid {
			t.Fatalf("error on luhn test, %s is valid: %t, contrary to expected", test.number, test.isValid)
		}
	}
}
