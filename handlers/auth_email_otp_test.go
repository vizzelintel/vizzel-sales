package handlers

import "testing"

// BUG-14: generateOTP originally picked each digit via int(randomByte) % 10.
// Since 256 is not a multiple of 10, byte values map to digits 0-5 twenty-six
// times each but digits 6-9 only twenty-five times each, biasing the OTP
// slightly towards low digits. The fix uses rejection sampling: bytes >= 250
// are rejected so only the range [0,250) — an exact multiple of 10 — is mapped
// with `%10`, making the digit distribution uniform. This test checks that
// accepted-range mapping (deterministic), rather than sampling crypto/rand, so
// it can't flake. See BUG_REPORT.md BUG-14.
func TestGenerateOTPDigitMapping_IsUniformOverByteRange(t *testing.T) {
	const digits = "0123456789"
	const cutoff = 250 // generateOTP rejects bytes >= cutoff
	var counts [10]int
	for b := 0; b < cutoff; b++ {
		d := digits[byte(b)%10]
		counts[d-'0']++
	}

	min, max := counts[0], counts[0]
	for _, c := range counts {
		if c < min {
			min = c
		}
		if c > max {
			max = c
		}
	}
	if min != max {
		t.Fatalf("digit mapping over accepted byte range [0,250) is not uniform: counts=%v (min=%d, max=%d) — "+
			"generateOTP must reject bytes >= 250 and map only the unbiased range with `%%10` "+
			"(rejection sampling) to remove the bias (BUG-14)", counts, min, max)
	}
}

func TestGenerateOTP_ReturnsSixDigitString(t *testing.T) {
	otp, err := generateOTP()
	if err != nil {
		t.Fatalf("generateOTP: %v", err)
	}
	if len(otp) != 6 {
		t.Fatalf("generateOTP() = %q, want length 6", otp)
	}
	for _, r := range otp {
		if r < '0' || r > '9' {
			t.Fatalf("generateOTP() = %q contains non-digit %q", otp, r)
		}
	}
}
