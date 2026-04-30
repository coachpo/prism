package auth

import (
	"os"
	"strings"
	"testing"
)

func TestAuthEmailVerificationOTPEnqueuesOutbox(t *testing.T) {
	raw := readAuthSource(t)
	for _, want := range []string{"outbox.KindEmailVerificationOTP", "outbox.TemplateEmailVerificationOTP", "enqueueAuthEmail", "email_verification_otp:%d:%d"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("expected email verification outbox evidence %q", want)
		}
	}
}

func TestAuthPasswordResetEnqueuesOutbox(t *testing.T) {
	raw := readAuthSource(t)
	for _, want := range []string{"outbox.KindPasswordReset", "outbox.TemplatePasswordReset", "enqueueAuthEmail", "password_reset:%d:%d"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("expected password reset outbox evidence %q", want)
		}
	}
}

func TestAuthPasswordResetEnumerationSafe(t *testing.T) {
	raw := readAuthSource(t)
	if !strings.Contains(raw, "writeJSON(w, http.StatusOK, successResponse{Success: true})") {
		t.Fatal("expected password reset request to keep generic success response")
	}
	if strings.Contains(raw, "writeDomainError(w, r, s.allowedOrigins, &domainError{StatusCode: http.StatusNotFound") {
		t.Fatal("password reset request must not expose account existence")
	}
}

func TestAuthEmailRoutesDoNotCallSMTP(t *testing.T) {
	raw := readAuthSource(t)
	for _, forbidden := range []string{"SendEmailVerificationOTP(", "SendPasswordResetEmail(", "NewSMTPMailer", "smtp."} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("auth routes contain direct email send evidence %q", forbidden)
		}
	}
}

func TestAuthEmailSecretsAreNotLogged(t *testing.T) {
	routes := readAuthSource(t)
	serviceRaw, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	combined := routes + string(serviceRaw)
	for _, forbidden := range []string{"otp_code", "reset token", "token_url"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("auth source contains loggable secret marker %q", forbidden)
		}
	}
}

func readAuthSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes.go: %v", err)
	}
	return string(raw)
}
