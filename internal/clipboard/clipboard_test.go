package clipboard

import "testing"

func TestPickImageMIMEPrefersPNG(t *testing.T) {
	list := "text/plain\nimage/gif\nimage/png\nimage/jpeg"
	if got := pickImageMIME(list); got != "image/png" {
		t.Fatalf("pickImageMIME = %q, want image/png", got)
	}
}

func TestPickImageMIMEFallsBackByPriority(t *testing.T) {
	list := "image/jpeg\nimage/webp\ntext/html"
	if got := pickImageMIME(list); got != "image/jpeg" {
		t.Fatalf("pickImageMIME = %q, want image/jpeg", got)
	}
}

func TestPickImageMIMERejectsNonRaster(t *testing.T) {
	if got := pickImageMIME("image/svg+xml\ntext/plain"); got != "" {
		t.Fatalf("pickImageMIME = %q, want empty", got)
	}
}

func TestPSQuoteEscapesSingleQuotes(t *testing.T) {
	if got := psQuote(`C:\it's\a.png`); got != `'C:\it''s\a.png'` {
		t.Fatalf("psQuote = %q", got)
	}
}
