package email

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestNewMailerDisabledReturnsNoopCompatibleMailer(t *testing.T) {
	t.Parallel()

	mailer, enabled, err := NewMailer(config.MailConfig{})
	if err != nil {
		t.Fatalf("create disabled mailer: %v", err)
	}
	if enabled {
		t.Fatal("expected disabled config to report disabled")
	}
	if err := mailer.SendEmailVerificationOTP(context.Background(), "operator@example.com", "123456"); err != nil {
		t.Fatalf("disabled verification send returned error: %v", err)
	}
	if err := mailer.SendPasswordResetEmail(context.Background(), "operator@example.com", "654321"); err != nil {
		t.Fatalf("disabled password reset send returned error: %v", err)
	}
}

func TestSMTPMailerPlaintextLocalOnlyCapturesVerificationMessage(t *testing.T) {
	t.Parallel()

	server := newFakeSMTPServer(t, fakeSMTPOptions{})
	defer server.Close()
	mailer := newTestMailer(t, server, config.MailSMTPModePlaintextLocalOnly, config.MailSMTPAuthNone)

	if err := mailer.SendEmailVerificationOTP(context.Background(), "Alice <alice@example.com>", "123456"); err != nil {
		t.Fatalf("send verification OTP: %v", err)
	}
	delivery := server.mustDelivery(t)
	if delivery.mailFrom != "noreply@example.com" {
		t.Fatalf("expected envelope sender noreply@example.com, got %q", delivery.mailFrom)
	}
	if delivery.rcptTo != "alice@example.com" {
		t.Fatalf("expected envelope recipient alice@example.com, got %q", delivery.rcptTo)
	}
	for _, want := range []string{
		"From: Prism <noreply@example.com>",
		"To: Alice <alice@example.com>",
		"Reply-To: Support <support@example.com>",
		"Subject: Prism email verification code",
		"Use this code to verify your Prism recovery email:",
		"123456",
	} {
		if !strings.Contains(delivery.message, want) {
			t.Fatalf("expected captured message to contain %q, got:\n%s", want, delivery.message)
		}
	}
}

func TestSMTPMailerPlainAuthCapturesPasswordResetMessage(t *testing.T) {
	t.Parallel()

	server := newFakeSMTPServer(t, fakeSMTPOptions{authUsername: "smtp-user", authPassword: "smtp-password"})
	defer server.Close()
	mailer := newTestMailer(t, server, config.MailSMTPModePlaintextLocalOnly, config.MailSMTPAuthPlain)

	if err := mailer.SendPasswordResetEmail(context.Background(), "alice@example.com", "654321"); err != nil {
		t.Fatalf("send password reset OTP: %v", err)
	}
	delivery := server.mustDelivery(t)
	if !delivery.authenticated {
		t.Fatal("expected fake server to record SMTP AUTH before delivery")
	}
	for _, want := range []string{
		"Subject: Prism password reset code",
		"Use this code to reset your Prism password:",
		"654321",
	} {
		if !strings.Contains(delivery.message, want) {
			t.Fatalf("expected captured reset message to contain %q, got:\n%s", want, delivery.message)
		}
	}
}

func TestSMTPMailerStartTLSRequiredFailsWhenServerDoesNotAdvertiseStartTLS(t *testing.T) {
	t.Parallel()

	server := newFakeSMTPServer(t, fakeSMTPOptions{})
	defer server.Close()
	mailer := newTestMailer(t, server, config.MailSMTPModeStartTLSRequired, config.MailSMTPAuthNone)

	err := mailer.SendEmailVerificationOTP(context.Background(), "alice@example.com", "123456")
	if err == nil {
		t.Fatal("expected STARTTLS-required send to fail without STARTTLS advertisement")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected STARTTLS error, got %v", err)
	}
	server.mustNoDelivery(t)
}

func TestSMTPMailerReturnsTransient4xxFailureWithoutSecretLeak(t *testing.T) {
	t.Parallel()

	server := newFakeSMTPServer(t, fakeSMTPOptions{
		authUsername: "smtp-user",
		authPassword: "smtp-password",
		mailResponse: "451 Requested action aborted: local error in processing",
	})
	defer server.Close()
	mailer := newTestMailer(t, server, config.MailSMTPModePlaintextLocalOnly, config.MailSMTPAuthPlain)

	err := mailer.SendEmailVerificationOTP(context.Background(), "alice@example.com", "123456")
	if err == nil {
		t.Fatal("expected transient MAIL FROM failure")
	}
	assertSMTPErrorContains(t, err, "MAIL FROM", "451")
	assertSMTPErrorRedacted(t, err)
	server.mustNoDelivery(t)
}

func TestSMTPMailerReturnsPermanent5xxFailureWithoutSecretLeak(t *testing.T) {
	t.Parallel()

	server := newFakeSMTPServer(t, fakeSMTPOptions{rcptResponse: "550 Mailbox unavailable"})
	defer server.Close()
	mailer := newTestMailer(t, server, config.MailSMTPModePlaintextLocalOnly, config.MailSMTPAuthNone)

	err := mailer.SendPasswordResetEmail(context.Background(), "alice@example.com", "654321")
	if err == nil {
		t.Fatal("expected permanent RCPT TO failure")
	}
	assertSMTPErrorContains(t, err, "RCPT TO", "550")
	assertSMTPErrorRedacted(t, err)
	server.mustNoDelivery(t)
}

func TestSMTPMailerStartTLSUpgradeFailureDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()

	server := newFakeSMTPServer(t, fakeSMTPOptions{
		advertiseStartTLS: true,
		startTLSResponse:  "454 TLS temporarily unavailable",
	})
	defer server.Close()
	mailer := newTestMailer(t, server, config.MailSMTPModeStartTLSRequired, config.MailSMTPAuthNone)

	err := mailer.SendEmailVerificationOTP(context.Background(), "alice@example.com", "123456")
	if err == nil {
		t.Fatal("expected STARTTLS upgrade failure")
	}
	assertSMTPErrorContains(t, err, "TLS", "454")
	assertSMTPErrorRedacted(t, err)
	server.mustNoDelivery(t)
}

func TestSMTPMailerTimeoutDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()

	server := newFakeSMTPServer(t, fakeSMTPOptions{hangOnMail: true})
	defer server.Close()
	mailer := newTestMailer(t, server, config.MailSMTPModePlaintextLocalOnly, config.MailSMTPAuthNone)
	mailer.timeout = 100 * time.Millisecond

	err := mailer.SendPasswordResetEmail(context.Background(), "alice@example.com", "654321")
	if err == nil {
		t.Fatal("expected MAIL FROM timeout")
	}
	assertSMTPErrorContains(t, err, "MAIL FROM")
	assertSMTPErrorRedacted(t, err)
	server.mustNoDelivery(t)
}

func TestSMTPMailerDisconnectMidCommandDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()

	server := newFakeSMTPServer(t, fakeSMTPOptions{disconnectOnData: true})
	defer server.Close()
	mailer := newTestMailer(t, server, config.MailSMTPModePlaintextLocalOnly, config.MailSMTPAuthNone)

	err := mailer.SendEmailVerificationOTP(context.Background(), "alice@example.com", "123456")
	if err == nil {
		t.Fatal("expected DATA disconnect failure")
	}
	assertSMTPErrorContains(t, err, "DATA")
	assertSMTPErrorRedacted(t, err)
	server.mustNoDelivery(t)
}

func newTestMailer(t *testing.T, server *fakeSMTPServer, mode config.MailSMTPMode, auth config.MailSMTPAuth) *SMTPMailer {
	t.Helper()
	username := ""
	password := ""
	if auth == config.MailSMTPAuthPlain {
		username = "smtp-user"
		password = "smtp-password"
	}
	mailer, err := NewSMTPMailer(config.MailConfig{
		Enabled: true,
		From:    "Prism <noreply@example.com>",
		ReplyTo: "Support <support@example.com>",
		SMTP: config.MailSMTPConfig{
			Host:          server.host,
			Port:          server.port,
			Mode:          mode,
			EHLOHostname:  "prism.test",
			Auth:          auth,
			Username:      username,
			Password:      password,
			Timeout:       2 * time.Second,
			TLSServerName: server.host,
		},
	})
	if err != nil {
		t.Fatalf("create SMTP mailer: %v", err)
	}
	return mailer
}

type fakeSMTPOptions struct {
	authUsername      string
	authPassword      string
	advertiseStartTLS bool
	startTLSResponse  string
	mailResponse      string
	rcptResponse      string
	hangOnMail        bool
	disconnectOnData  bool
}

type fakeSMTPDelivery struct {
	mailFrom      string
	rcptTo        string
	message       string
	authenticated bool
}

type fakeSMTPServer struct {
	listener net.Listener
	host     string
	port     int
	options  fakeSMTPOptions
	delivery chan fakeSMTPDelivery
	done     chan struct{}
	mu       sync.Mutex
	err      error
	conns    map[net.Conn]struct{}
}

func newFakeSMTPServer(t *testing.T, options fakeSMTPOptions) *fakeSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for fake SMTP server: %v", err)
	}
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		t.Fatalf("split fake SMTP listener address: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		_ = listener.Close()
		t.Fatalf("parse fake SMTP listener port: %v", err)
	}
	server := &fakeSMTPServer{
		listener: listener,
		host:     host,
		port:     port,
		options:  options,
		delivery: make(chan fakeSMTPDelivery, 1),
		done:     make(chan struct{}),
		conns:    make(map[net.Conn]struct{}),
	}
	go server.serve()
	return server
}

func (s *fakeSMTPServer) Close() {
	_ = s.listener.Close()
	s.closeTrackedConnections()
	<-s.done
}

func (s *fakeSMTPServer) mustDelivery(t *testing.T) fakeSMTPDelivery {
	t.Helper()
	select {
	case delivery := <-s.delivery:
		return delivery
	case <-time.After(2 * time.Second):
		s.mu.Lock()
		defer s.mu.Unlock()
		t.Fatalf("timed out waiting for fake SMTP delivery; server error: %v", s.err)
		return fakeSMTPDelivery{}
	}
}

func (s *fakeSMTPServer) mustNoDelivery(t *testing.T) {
	t.Helper()
	select {
	case delivery := <-s.delivery:
		t.Fatalf("expected no fake SMTP delivery, got %+v", delivery)
	case <-time.After(100 * time.Millisecond):
	}
}

func (s *fakeSMTPServer) serve() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.trackConn(conn)
		if err := s.handle(conn); err != nil {
			s.mu.Lock()
			s.err = err
			s.mu.Unlock()
		}
	}
}

func (s *fakeSMTPServer) handle(conn net.Conn) error {
	defer s.untrackConn(conn)
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	writeSMTPLine(writer, "220 fake.smtp.local ESMTP")
	var delivery fakeSMTPDelivery
	requireAuth := s.options.authUsername != "" || s.options.authPassword != ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO ") || strings.HasPrefix(upper, "HELO "):
			writeSMTPLine(writer, "250-fake.smtp.local")
			if s.options.advertiseStartTLS {
				writeSMTPLine(writer, "250-STARTTLS")
			}
			if requireAuth {
				writeSMTPLine(writer, "250 AUTH PLAIN")
			} else {
				writeSMTPLine(writer, "250 OK")
			}
		case upper == "STARTTLS":
			response := s.options.startTLSResponse
			if response == "" {
				response = "454 TLS temporarily unavailable"
			}
			writeSMTPLine(writer, response)
			return nil
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			if err := s.handleAuth(line, &delivery); err != nil {
				writeSMTPLine(writer, "535 Authentication failed")
				return err
			}
			writeSMTPLine(writer, "235 Authentication succeeded")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			if s.options.hangOnMail {
				_, err := reader.Peek(1)
				return err
			}
			if requireAuth && !delivery.authenticated {
				writeSMTPLine(writer, "530 Authentication required")
				continue
			}
			delivery.mailFrom = extractSMTPPath(line)
			if s.options.mailResponse != "" {
				writeSMTPLine(writer, s.options.mailResponse)
				continue
			}
			writeSMTPLine(writer, "250 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			delivery.rcptTo = extractSMTPPath(line)
			if s.options.rcptResponse != "" {
				writeSMTPLine(writer, s.options.rcptResponse)
				continue
			}
			writeSMTPLine(writer, "250 OK")
		case upper == "DATA":
			if s.options.disconnectOnData {
				return nil
			}
			writeSMTPLine(writer, "354 End data with <CR><LF>.<CR><LF>")
			message, err := readSMTPData(reader)
			if err != nil {
				return err
			}
			delivery.message = message
			s.delivery <- delivery
			writeSMTPLine(writer, "250 OK")
		case upper == "QUIT":
			writeSMTPLine(writer, "221 Bye")
			return nil
		default:
			writeSMTPLine(writer, "250 OK")
		}
	}
}

func (s *fakeSMTPServer) trackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[conn] = struct{}{}
}

func (s *fakeSMTPServer) untrackConn(conn net.Conn) {
	_ = conn.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

func (s *fakeSMTPServer) closeTrackedConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.conns {
		_ = conn.Close()
	}
}

func assertSMTPErrorContains(t *testing.T, err error, wants ...string) {
	t.Helper()
	message := err.Error()
	for _, want := range wants {
		if !strings.Contains(message, want) {
			t.Fatalf("expected SMTP error %q to contain %q", message, want)
		}
	}
}

func assertSMTPErrorRedacted(t *testing.T, err error) {
	t.Helper()
	message := err.Error()
	for _, leaked := range []string{
		"123456",
		"654321",
		"smtp-password",
		"Use this code to verify your Prism recovery email",
		"Use this code to reset your Prism password",
	} {
		if strings.Contains(message, leaked) {
			t.Fatalf("SMTP error leaked sensitive value %q in %q", leaked, message)
		}
	}
}

func (s *fakeSMTPServer) handleAuth(line string, delivery *fakeSMTPDelivery) error {
	parts := strings.Fields(line)
	if len(parts) != 3 {
		return fmt.Errorf("expected AUTH PLAIN initial response, got %q", line)
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return err
	}
	fields := strings.Split(string(decoded), "\x00")
	if len(fields) != 3 || fields[1] != s.options.authUsername || fields[2] != s.options.authPassword {
		return fmt.Errorf("unexpected SMTP auth credentials")
	}
	delivery.authenticated = true
	return nil
}

func readSMTPData(reader *bufio.Reader) (string, error) {
	var builder strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "." {
			return builder.String(), nil
		}
		builder.WriteString(line)
	}
}

func writeSMTPLine(writer *bufio.Writer, line string) {
	_, _ = writer.WriteString(line)
	_, _ = writer.WriteString("\r\n")
	_ = writer.Flush()
}

func extractSMTPPath(line string) string {
	start := strings.Index(line, "<")
	end := strings.LastIndex(line, ">")
	if start >= 0 && end > start {
		return line[start+1 : end]
	}
	_, value, found := strings.Cut(line, ":")
	if !found {
		return ""
	}
	return strings.TrimSpace(value)
}
