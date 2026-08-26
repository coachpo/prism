package endpointdomain

import (
	"strings"
	"testing"
)

func TestNormalizeBaseURLOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"trims surrounding whitespace", "  https://api.openai.com  ", "https://api.openai.com"},
		{"removes trailing slash", "https://api.openai.com/", "https://api.openai.com"},
		{"removes multiple trailing slashes", "https://api.openai.com/v1///", "https://api.openai.com/v1"},
		{"preserves path prefix", "https://api.openai.com/v1", "https://api.openai.com/v1"},
		{"keeps invalid bare origin for validation", "https://", "https://"},
		{"keeps empty", "", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeBaseURL(test.raw)
			if got != test.want {
				t.Fatalf("NormalizeBaseURL(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	valid := "https://api.openai.com/v1"
	if codes := ValidateBaseURL(valid); len(codes) != 0 {
		t.Fatalf("expected valid URL to pass, got %v", codes)
	}
	for name, raw := range map[string]string{
		"missing scheme":     "api.openai.com",
		"bare scheme":        "https://",
		"unsupported scheme": "ftp://api.openai.com",
		"query rejected":     "https://api.openai.com/v1?key=1",
		"fragment rejected":  "https://api.openai.com/v1#frag",
		"userinfo rejected":  "https://operator:secret@api.openai.com/v1",
	} {
		t.Run(name, func(t *testing.T) {
			codes := ValidateBaseURL(raw)
			if len(codes) == 0 {
				t.Fatalf("expected %q to be invalid", raw)
			}
			if codes[0] != FieldErrorBaseURLInvalid {
				t.Fatalf("expected base_url_invalid code, got %v", codes)
			}
		})
	}

	overlong := "https://api.example.com/" + strings.Repeat("a", 513)
	if codes := ValidateBaseURL(overlong); len(codes) == 0 || codes[0] != FieldErrorBaseURLTooLong {
		t.Fatalf("expected base_url_too_long code, got %v", codes)
	}
	// 512 code points is acceptable even with multibyte characters.
	boundary := "https://api.example.com/" + strings.Repeat("界", 512-24)
	if ValidateBaseURL(boundary) != nil {
		t.Fatalf("expected code-point counting to accept 512 boundary, codes=%v", ValidateBaseURL(boundary))
	}
	tooLong := "https://api.example.com/" + strings.Repeat("界", 513-24)
	if codes := ValidateBaseURL(tooLong); len(codes) == 0 || codes[0] != FieldErrorBaseURLTooLong {
		t.Fatalf("expected 513 code points to be too long, got %v", codes)
	}
}

func TestValidateEndpointName(t *testing.T) {
	t.Parallel()

	if code := ValidateEndpointName(""); code != FieldErrorNameRequired {
		t.Fatalf("expected name_required, got %q", code)
	}
	if code := ValidateEndpointName(strings.Repeat("a", 128)); code != "" {
		t.Fatalf("expected 128 code points to pass, got %q", code)
	}
	if code := ValidateEndpointName(strings.Repeat("界", 129)); code != FieldErrorNameTooLong {
		t.Fatalf("expected name_too_long for 129 code points, got %q", code)
	}
}

func TestBuildDuplicateEndpointName(t *testing.T) {
	t.Parallel()

	existing := map[string]struct{}{"Primary copy": {}}
	got := BuildDuplicateEndpointName("Primary", existing)
	if got != "Primary copy 2" {
		t.Fatalf("expected suffix after collision, got %q", got)
	}
}
