package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

const (
	emailVerificationSubject = "Prism email verification code"
	passwordResetSubject     = "Prism password reset code"
)

type Mailer interface {
	SendEmailVerificationOTP(context.Context, string, string) error
	SendPasswordResetEmail(context.Context, string, string) error
}

type DisabledMailer struct{}

func NewMailer(mailConfig config.MailConfig) (Mailer, bool, error) {
	if !mailConfig.Enabled {
		return DisabledMailer{}, false, nil
	}
	mailer, err := NewSMTPMailer(mailConfig)
	if err != nil {
		return nil, false, err
	}
	return mailer, true, nil
}

func (DisabledMailer) SendEmailVerificationOTP(context.Context, string, string) error {
	return nil
}

func (DisabledMailer) SendPasswordResetEmail(context.Context, string, string) error {
	return nil
}

type SMTPMailer struct {
	from       string
	replyTo    string
	host       string
	port       int
	mode       config.MailSMTPMode
	ehloName   string
	auth       config.MailSMTPAuth
	username   string
	password   string
	timeout    time.Duration
	serverName string
}

func NewSMTPMailer(mailConfig config.MailConfig) (*SMTPMailer, error) {
	if !mailConfig.Enabled {
		return nil, errors.New("mail config is disabled")
	}
	if _, err := mail.ParseAddress(mailConfig.From); err != nil {
		return nil, fmt.Errorf("mail from address is invalid: %w", err)
	}
	if strings.TrimSpace(mailConfig.ReplyTo) != "" {
		if _, err := mail.ParseAddress(mailConfig.ReplyTo); err != nil {
			return nil, fmt.Errorf("mail reply-to address is invalid: %w", err)
		}
	}

	smtpConfig := mailConfig.SMTP
	password := strings.TrimSpace(smtpConfig.Password)
	if password == "" && strings.TrimSpace(smtpConfig.PasswordFile) != "" {
		contents, err := os.ReadFile(strings.TrimSpace(smtpConfig.PasswordFile))
		if err != nil {
			return nil, fmt.Errorf("read SMTP password file: %w", err)
		}
		password = strings.TrimSpace(string(contents))
	}

	timeout := smtpConfig.Timeout
	if timeout <= 0 {
		return nil, errors.New("SMTP timeout must be positive")
	}
	mode := smtpConfig.Mode
	switch mode {
	case config.MailSMTPModeStartTLSRequired, config.MailSMTPModeImplicitTLS, config.MailSMTPModePlaintextLocalOnly:
	default:
		return nil, fmt.Errorf("unsupported SMTP mode %q", mode)
	}
	authMode := smtpConfig.Auth
	if authMode == "" {
		authMode = config.MailSMTPAuthNone
	}
	switch authMode {
	case config.MailSMTPAuthNone:
	case config.MailSMTPAuthPlain:
		if strings.TrimSpace(smtpConfig.Username) == "" || password == "" {
			return nil, errors.New("SMTP plain auth requires username and password")
		}
	default:
		return nil, fmt.Errorf("unsupported SMTP auth mode %q", authMode)
	}

	host := strings.TrimSpace(smtpConfig.Host)
	if host == "" || smtpConfig.Port <= 0 {
		return nil, errors.New("SMTP host and port are required")
	}
	serverName := strings.TrimSpace(smtpConfig.TLSServerName)
	if serverName == "" {
		serverName = host
	}

	return &SMTPMailer{
		from:       strings.TrimSpace(mailConfig.From),
		replyTo:    strings.TrimSpace(mailConfig.ReplyTo),
		host:       host,
		port:       smtpConfig.Port,
		mode:       mode,
		ehloName:   strings.TrimSpace(smtpConfig.EHLOHostname),
		auth:       authMode,
		username:   strings.TrimSpace(smtpConfig.Username),
		password:   password,
		timeout:    timeout,
		serverName: serverName,
	}, nil
}

func (m *SMTPMailer) SendEmailVerificationOTP(ctx context.Context, recipient string, otpCode string) error {
	body := "Use this code to verify your Prism recovery email:\n\n" + strings.TrimSpace(otpCode) + "\n"
	return m.send(ctx, recipient, emailVerificationSubject, body)
}

func (m *SMTPMailer) SendPasswordResetEmail(ctx context.Context, recipient string, otpCode string) error {
	body := "Use this code to reset your Prism password:\n\n" + strings.TrimSpace(otpCode) + "\n"
	return m.send(ctx, recipient, passwordResetSubject, body)
}

func (m *SMTPMailer) send(ctx context.Context, recipient string, subject string, body string) error {
	if m == nil {
		return errors.New("SMTP mailer is nil")
	}
	recipient = strings.TrimSpace(recipient)
	if _, err := mail.ParseAddress(recipient); err != nil {
		return fmt.Errorf("recipient address is invalid: %w", err)
	}
	message := buildPlainTextMessage(m.from, m.replyTo, recipient, subject, body)
	return m.sendSMTP(ctx, recipient, message)
}

func (m *SMTPMailer) sendSMTP(ctx context.Context, recipient string, message []byte) error {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	address := net.JoinHostPort(m.host, fmt.Sprintf("%d", m.port))
	dialer := &net.Dialer{Timeout: m.timeout}
	var conn net.Conn
	var err error
	if m.mode == config.MailSMTPModeImplicitTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: m.tlsConfig()}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect SMTP server: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(m.timeout)); err != nil {
		return fmt.Errorf("set SMTP deadline: %w", err)
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()

	if m.ehloName != "" {
		if err := client.Hello(m.ehloName); err != nil {
			return fmt.Errorf("send SMTP EHLO: %w", err)
		}
	}
	if m.mode == config.MailSMTPModeStartTLSRequired {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return errors.New("SMTP server does not advertise STARTTLS")
		}
		if err := client.StartTLS(m.tlsConfig()); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if m.auth == config.MailSMTPAuthPlain {
		auth := plainAuth{username: m.username, password: m.password}
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	if err := client.Mail(envelopeAddress(m.from)); err != nil {
		return fmt.Errorf("send SMTP MAIL FROM: %w", err)
	}
	if err := client.Rcpt(envelopeAddress(recipient)); err != nil {
		return fmt.Errorf("send SMTP RCPT TO: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP DATA writer: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close SMTP DATA writer: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("send SMTP QUIT: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (m *SMTPMailer) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.serverName}
}

func buildPlainTextMessage(from string, replyTo string, to string, subject string, body string) []byte {
	var buffer bytes.Buffer
	writeHeader(&buffer, "From", from)
	writeHeader(&buffer, "To", to)
	if replyTo != "" {
		writeHeader(&buffer, "Reply-To", replyTo)
	}
	writeHeader(&buffer, "Subject", subject)
	writeHeader(&buffer, "MIME-Version", "1.0")
	writeHeader(&buffer, "Content-Type", `text/plain; charset="utf-8"`)
	writeHeader(&buffer, "Content-Transfer-Encoding", "8bit")
	buffer.WriteString("\r\n")
	buffer.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return buffer.Bytes()
}

func writeHeader(buffer *bytes.Buffer, key string, value string) {
	buffer.WriteString(key)
	buffer.WriteString(": ")
	buffer.WriteString(sanitizeHeaderValue(value))
	buffer.WriteString("\r\n")
}

func sanitizeHeaderValue(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}

func envelopeAddress(address string) string {
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return strings.TrimSpace(address)
	}
	return parsed.Address
}

type plainAuth struct {
	username string
	password string
}

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	response := "\x00" + a.username + "\x00" + a.password
	return "PLAIN", []byte(response), nil
}

func (a plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected SMTP PLAIN auth challenge")
	}
	return nil, nil
}
