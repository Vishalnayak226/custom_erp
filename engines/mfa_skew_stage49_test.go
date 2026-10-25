package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Stage 49.2.8 (clock skew): a code from the step before or after is accepted
// at the default tolerance of one step (+/-30 s); one three steps away is not,
// and a code of the wrong length is refused before any comparison.
func TestStage49TOTPClockSkew(t *testing.T) {
	db.InitDB(testConnStr())
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		step := uint64(time.Now().Unix()) / uint64(totpPeriod.Seconds())
		code := func(offset int64) string {
			c, err := totpCodeAt(secret, uint64(int64(step)+offset))
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
		accepted := map[int64]bool{}
		for _, offset := range []int64{-3, -1, 0, 1, 3} {
			accepted[offset] = VerifyTOTPCode("default", secret, code(offset))
		}
		if uint64(time.Now().Unix())/uint64(totpPeriod.Seconds()) != step {
			continue // the 30-second step turned over mid-check; check again
		}
		for offset, want := range map[int64]bool{-3: false, -1: true, 0: true, 1: true, 3: false} {
			if accepted[offset] != want {
				t.Errorf("a code %d step(s) away: accepted=%v, want %v", offset, accepted[offset], want)
			}
		}
		if VerifyTOTPCode("default", secret, code(0)+"0") || VerifyTOTPCode("default", secret, code(0)[:5]) {
			t.Error("a code of the wrong length must be refused")
		}
		return
	}
	t.Fatal("the time step kept turning over")
}
