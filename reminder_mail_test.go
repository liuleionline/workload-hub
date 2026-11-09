package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/mail"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMailRemindersReadOnlyDryRunDedupeAndRecheck(t *testing.T) {
	app, db, admin, _ := newWorklogBugfixApp(t)
	app.cfg.BaseURL = "https://example.invalid"
	t.Setenv("APP_SMTP_USERNAME", "sender@example.invalid")
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 16, 0, 0, 0, app.cfg.Location)
	clock := func() time.Time { return now }
	addUser := func(name string, active, testUser bool) int64 {
		t.Helper()
		result, err := db.Exec(`INSERT INTO users(department_id,name,email,password_hash,role,active,is_test_user)
			VALUES (?,?,?,'unused','designer',?,?)`, admin.DepartmentID, name, name+"@example.invalid", active, testUser)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		return id
	}
	first := addUser("a-first", true, false)
	second := addUser("b-second", true, false)
	zero := addUser("zero", true, false)
	leave := addUser("leave", true, false)
	actual := addUser("actual", true, false)
	previous := addUser("c-previous", true, false)
	addUser("test-account", true, true)
	addUser("disabled", false, false)
	for _, id := range []int64{admin.ID, zero, leave} {
		if _, err := db.Exec("INSERT INTO leave_records(week_end,user_id,leave_days) VALUES ('2026-11-13',?,0)", id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("UPDATE leave_records SET leave_days=5 WHERE user_id=?", leave); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO actual_work_entries(week_end,user_id,hours) VALUES ('2026-11-13',?,8),('2026-11-06',?,8)", actual, previous); err != nil {
		t.Fatal(err)
	}
	var sourcePath string
	if err := db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&sourcePath); err != nil {
		t.Fatal(err)
	}
	readonly, err := openReminderReadOnlyDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	reader := &App{cfg: app.cfg, db: readonly}
	if _, err := readonly.Exec("UPDATE users SET name='should never write' WHERE id=?", admin.ID); err == nil {
		t.Fatal("reader unexpectedly allowed business writes")
	}
	state, err := openReminderMailState(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	calls := 0
	deliver := func(ctx context.Context, to string, body []byte) error {
		calls++
		if calls == 1 {
			// Another employee submits while this batch is running.
			_, err := db.ExecContext(ctx, "INSERT INTO leave_records(week_end,user_id,leave_days) VALUES ('2026-11-13',?,0)", second)
			if err != nil {
				return err
			}
		}
		message, err := mail.ReadMessage(strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		if message.Header.Get("To") != to || message.Header.Get("Bcc") != "" || message.Header.Get("Cc") != "" {
			t.Fatal("recipient leakage")
		}
		if !strings.HasPrefix(message.Header.Get("Subject"), "=?UTF-8?") {
			t.Fatal("subject is not MIME encoded")
		}
		encoded, _ := io.ReadAll(message.Body)
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(encoded), "\r\n", ""))
		if err != nil || !strings.Contains(string(decoded), "https://example.invalid/worklog") || !strings.Contains(string(decoded), "2026-11-14 12:00") {
			t.Fatal("incorrect reminder content")
		}
		return nil
	}
	result, err := reader.executeMailReminders(ctx, state, false, clock, deliver)
	if err != nil || result.Missing != 3 || calls != 0 {
		t.Fatalf("dry run=%+v err=%v calls=%d", result, err, calls)
	}
	var attempts int
	if err := state.QueryRow("SELECT COUNT(*) FROM reminder_mail_attempts").Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("dry run recorded attempts")
	}
	result, err = reader.executeMailReminders(ctx, state, true, clock, deliver)
	if err != nil || result.Sent != 2 || result.Skipped != 1 || calls != 2 {
		t.Fatalf("send=%+v err=%v calls=%d", result, err, calls)
	}
	for _, id := range []int64{first, previous} {
		var status string
		if err := state.QueryRow("SELECT status FROM reminder_mail_attempts WHERE week_end='2026-11-13' AND user_id=?", id).Scan(&status); err != nil || status != "sent" {
			t.Fatalf("status=%s err=%v", status, err)
		}
	}
	result, err = reader.executeMailReminders(ctx, state, true, clock, deliver)
	if err != nil || result.Sent != 0 || result.Skipped != 2 || calls != 2 {
		t.Fatalf("duplicate run=%+v err=%v calls=%d", result, err, calls)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='reminder_mail_attempts'").Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("mail state leaked into business DB")
	}
}

func TestMailRemindersFailClosedAndNeverRetryUnknownDelivery(t *testing.T) {
	for _, mode := range []string{"uncertain", "pending", "window_closed", "window_changes", "bad_settings", "invalid_email"} {
		t.Run(mode, func(t *testing.T) {
			app, db, admin, _ := newWorklogBugfixApp(t)
			app.cfg.BaseURL = "https://example.invalid"
			t.Setenv("APP_SMTP_USERNAME", "sender@example.invalid")
			now := time.Date(2026, 11, 13, 16, 0, 0, 0, app.cfg.Location)
			state, err := openReminderMailState(filepath.Join(t.TempDir(), "mail.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close()
			switch mode {
			case "pending":
				_, err = state.Exec("INSERT INTO reminder_mail_attempts(week_end,user_id,status) VALUES ('2026-11-13',?,'pending')", admin.ID)
			case "window_closed":
				now = now.AddDate(0, 0, -1)
			case "bad_settings":
				_, err = db.Exec("DELETE FROM settings WHERE key='report_open_minute'")
			case "invalid_email":
				_, err = db.Exec("UPDATE users SET email='a@example.invalid,b@example.invalid' WHERE id=?", admin.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			reads, sends := 0, 0
			clock := func() time.Time {
				reads++
				if mode == "window_changes" && reads > 1 {
					return now.AddDate(0, 0, 2)
				}
				return now
			}
			deliver := func(context.Context, string, []byte) error {
				sends++
				return errors.New("ambiguous SMTP disconnect; sensitive response must not appear in output")
			}
			result, err := app.executeMailReminders(context.Background(), state, true, clock, deliver)
			if mode == "uncertain" {
				if err == nil || result.Status != "failed" || result.Uncertain != 1 || sends != 1 || strings.Contains(err.Error(), "sensitive response") {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				result, err = app.executeMailReminders(context.Background(), state, true, clock, deliver)
				if err != nil || result.Skipped != 1 || sends != 1 {
					t.Fatal("uncertain delivery was retried")
				}
			} else if sends != 0 {
				t.Fatalf("unexpected mail in mode %s", mode)
			}
			if (mode == "bad_settings" || mode == "invalid_email") && err == nil {
				t.Fatal("expected safe failure")
			}
		})
	}
}

func TestReminderMailboxRejectsHeaderInjectionAndMultipleRecipients(t *testing.T) {
	for i, value := range []string{"", "a@example.invalid\r\nBcc: other@example.invalid", "a@example.invalid,b@example.invalid", "Name <a@example.invalid>", " a@example.invalid"} {
		if validReminderMailbox(value) {
			t.Fatal("invalid mailbox accepted at index " + strconv.Itoa(i))
		}
	}
	if !validReminderMailbox("test@example.invalid") {
		t.Fatal("plain mailbox rejected")
	}
}
