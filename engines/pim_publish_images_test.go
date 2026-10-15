package engines

import (
	"strings"
	"testing"
)

func TestChannelMinimumImagesExcludesNonProductMedia(t *testing.T) {
	media := []ProductMediaAsset{
		{FileType: "image/jpeg", MediaRole: "Main Image", Status: "Active"},
		{FileType: "image/png", MediaRole: "Gallery", Status: "Active"},
		{FileType: "image/webp", MediaRole: "Variant Image", Status: "Active"},
		{FileType: "image/gif", MediaRole: "Lifestyle", Status: "Active"},
		{FileType: "application/pdf", MediaRole: "Certificate", Status: "Active"},
		{FileType: "image/jpeg", MediaRole: "Internal QC", Status: "Active"},
		{FileType: "image/png", MediaRole: "Gallery", Status: "Inactive"},
	}
	if got := countChannelImages(media); got != 4 {
		t.Fatalf("countChannelImages = %d, want 4 publishable product images", got)
	}
}

func TestCustomChannelRuleMessageRetainsCorrectionType(t *testing.T) {
	got := channelRuleFailureMessage(channelValidationRule{RuleType: "Min Images", Message: "Add more pictures"}, "fallback")
	if !strings.HasPrefix(got, "Min Images:") || !strings.Contains(got, "Add more pictures") {
		t.Fatalf("custom rule message lost the UI correction hint: %q", got)
	}
}
