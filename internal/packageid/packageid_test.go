package packageid_test

import (
	"testing"

	"github.com/lib-x/lzc-toolkit-go/internal/packageid"
)

func TestPackageIDValid(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "single segment", value: "app", want: true},
		{name: "qualified", value: "community.lazycat.app.demo", want: true},
		{name: "hyphenated", value: "community.lazycat.app.demo-web", want: true},
		{name: "empty", value: "", want: false},
		{name: "uppercase", value: "Community.lazycat.app.demo", want: false},
		{name: "leading digit", value: "1community.lazycat.app.demo", want: false},
		{name: "empty segment", value: "community..demo", want: false},
		{name: "trailing hyphen", value: "community.lazycat.demo-", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := packageid.Valid(test.value); got != test.want {
				t.Fatalf("Valid(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}
