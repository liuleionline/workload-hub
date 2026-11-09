package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type reminderSMTP struct {
	client *smtp.Client
	conn   net.Conn
	from   string
}

func newReminderSMTP(ctx context.Context) (*reminderSMTP, error) {
	host, port := envOr("APP_SMTP_HOST", "smtp.gmail.com"), envOr("APP_SMTP_PORT", "465")
	from := envOr("APP_SMTP_USERNAME", "")
	if !validReminderMailbox(from) {
		return nil, errors.New("SMTP账号格式无效")
	}
	passwordPath := envOr("APP_SMTP_PASSWORD_FILE", "")
	if passwordPath == "" && os.Getenv("CREDENTIALS_DIRECTORY") != "" {
		passwordPath = filepath.Join(os.Getenv("CREDENTIALS_DIRECTORY"), "gmail-app-password")
	}
	secret, err := os.ReadFile(passwordPath)
	if err != nil {
		return nil, errors.New("SMTP凭据不可读取")
	}
	defer clear(secret)
	password := strings.TrimSpace(string(secret))
	if password == "" || len(password) > 1024 {
		return nil, errors.New("SMTP凭据格式无效")
	}
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, errors.New("SMTP加密连接失败")
	}
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, errors.New("SMTP握手失败")
	}
	if err := client.Auth(smtp.PlainAuth("", from, password, host)); err != nil {
		client.Close()
		return nil, errors.New("SMTP认证失败，请核对应用密码")
	}
	return &reminderSMTP{client: client, conn: conn, from: from}, nil
}

func (s *reminderSMTP) send(ctx context.Context, recipient string, message []byte) error {
	if !validReminderMailbox(recipient) {
		return errors.New("invalid recipient")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err := s.conn.SetDeadline(deadline); err != nil {
		return err
	}
	if err := s.client.Mail(s.from); err != nil {
		return err
	}
	if err := s.client.Rcpt(recipient); err != nil {
		return err
	}
	writer, err := s.client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		s.client.Close()
		return err
	}
	return writer.Close()
}

func (s *reminderSMTP) close() {
	_ = s.conn.SetDeadline(time.Now().Add(3 * time.Second))
	_ = s.client.Quit()
	_ = s.conn.Close()
}

func encodeReminderMail(from, to, subject, body, week string, userID int64, now time.Time) []byte {
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))
	var lines strings.Builder
	for len(encodedBody) > 76 {
		lines.WriteString(encodedBody[:76] + "\r\n")
		encodedBody = encodedBody[76:]
	}
	lines.WriteString(encodedBody + "\r\n")
	_, domain, _ := strings.Cut(from, "@")
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <worklog-%s-%d@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\nAuto-Submitted: auto-generated\r\n\r\n%s",
		from, to, mime.BEncoding.Encode("UTF-8", subject), now.Format(time.RFC1123Z), week, userID, domain, lines.String()))
}
