package metadata

import (
	"context"
	"testing"
)

func TestWithLanguage(t *testing.T) {
	ctx := context.Background()
	for l, ok := range map[string]bool{"hr": true, "en-US": true, "": false, "HR": false, "en_US": false, "en-us": false, "eng": false} {
		if got := LanguageFrom(WithLanguage(ctx, l)); (got != "") != ok {
			t.Errorf("%q: %q", l, got)
		}
	}
}
