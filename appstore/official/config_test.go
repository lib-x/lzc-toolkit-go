package official_test

import (
	"testing"

	"github.com/lib-x/lzc-toolkit-go/appstore/official"
)

func TestMetadataBaseURL(t *testing.T) {
	if got, err := official.MetadataBaseURL(""); err != nil || got != official.DefaultMetadataBaseURL {
		t.Fatalf("default=%q err=%v", got, err)
	}
	if got, err := official.MetadataBaseURL("cos.example.invalid"); err != nil || got != "https://cos.example.invalid/appstore/metarepo" {
		t.Fatalf("custom=%q err=%v", got, err)
	}
	for _, value := range []string{"https://cos.example.com", "cos.example.com:443", "cos.example.com/path", "cos.example.com?x=1", "cos example.com"} {
		if _, err := official.MetadataBaseURL(value); err == nil {
			t.Fatalf("unsafe domain %q accepted", value)
		}
	}
}
