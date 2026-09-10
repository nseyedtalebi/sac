package cas

import "testing"

func TestValidDigest(t *testing.T) {
	cases := map[string]bool{
		"":                 false,
		"not-hex-at-all!!": false,
		"E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855": false, // uppercase, right length
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85":  false, // 63 chars, too short
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855": true,  // valid lowercase 64-hex
	}
	for digest, want := range cases {
		if got := ValidDigest(digest); got != want {
			t.Errorf("ValidDigest(%q) = %v, want %v", digest, got, want)
		}
	}
}
