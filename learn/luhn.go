package learn

const (
	asciiZero = 48
	asciiTen  = 57
)

// code adopted from https://github.com/ShiraazMoollatjie/goluhn
func ValidLuhn(number string) bool {
	if len(number) < 13 {
		return false
	}

	parity := len(number) % 2
	var sum int64
	for i, d := range number {
		if d < asciiZero || d > asciiTen {
			return false
		}

		d = d - asciiZero
		// Double the value of every second digit.
		if i%2 == parity {
			d *= 2
			// If the result of this doubling operation is greater than 9.
			if d > 9 {
				// The same final result can be found by subtracting 9 from that result.
				d -= 9
			}
		}

		// Take the sum of all the digits.
		sum += int64(d)
	}

	return sum%10 == 0
}
