package engines

import "testing"

// Stage 49.2.2/49.2.8: the password baseline function itself, independent
// of any HTTP handler - handlers_profile.go, password_reset.go and
// handlers_admin_identity.go all funnel through this one choke point, so a
// gap caught here is caught everywhere a human chooses their own password.
func TestValidatePasswordStrengthBaseline(t *testing.T) {
	cases := []struct {
		name     string
		password string
		username string
		wantErr  bool
	}{
		{"too short is rejected", "Ab1!Ab1!", "priya", true},
		{"a common password is rejected even at full length", "welcome123456", "priya", true},
		{"the password equal to the username is rejected", "priyapriya12", "priyapriya12", true},
		{"an ascending sequence is rejected", "abcdefghijkl", "priya", true},
		{"a descending sequence is rejected", "nmlkjihgfedc", "priya", true},
		{"every character the same is rejected", "aaaaaaaaaaaa", "priya", true},
		{"a genuinely random-looking long passphrase passes", "Kb7$mQdx29zT!wR", "priya", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePasswordStrength("default", tc.password, tc.username)
			if tc.wantErr && err == nil {
				t.Errorf("ValidatePasswordStrength(%q) = nil, want a rejection", tc.password)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidatePasswordStrength(%q) = %v, want no error", tc.password, err)
			}
		})
	}
}

func TestValidatePasswordStrengthIsCaseInsensitive(t *testing.T) {
	if err := ValidatePasswordStrength("default", "WELCOME123456", "someone"); err == nil {
		t.Error("a common password must be rejected regardless of case")
	}
	if err := ValidatePasswordStrength("default", "PRIYASHARMA12", "priyasharma12"); err == nil {
		t.Error("a password equal to the username must be rejected regardless of case")
	}
}
