package engines

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestS3SignRequestAWSExample is AWS's own worked example ("Example: GET
// Object" in the S3 SigV4 header-signing docs): the same keys, date, bucket
// and Range header must give exactly the signature AWS publishes.
func TestS3SignRequestAWSExample(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-9")
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	s3SignRequest(req, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", s3EmptyPayloadHash, at)

	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization\n got %s\nwant %s", got, want)
	}
}

func TestS3URIEncode(t *testing.T) {
	cases := map[string]string{
		"designs/Design-HO-26-27-000006/ab12.jpg": "designs/Design-HO-26-27-000006/ab12.jpg",
		"a b$c":   "a%20b%24c",
		"x~y_z.-": "x~y_z.-",
	}
	for in, want := range cases {
		if got := s3URIEncode(in, false); got != want {
			t.Errorf("s3URIEncode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := s3URIEncode("a/b", true); !strings.Contains(got, "%2F") {
		t.Errorf("encodeSlash should encode /, got %q", got)
	}
}
