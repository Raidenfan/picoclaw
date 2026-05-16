package email

import (
	"context"
	"crypto/tls"
	"net"
	"net/smtp"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
)

// TestStep records the result of a single connection test step.
type TestStep struct {
	Name       string `json:"name"`
	Success    bool   `json:"success"`
	DurationMs int    `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// IMAPTestResult is the result of an IMAP connection test.
type IMAPTestResult struct {
	Server      string     `json:"server"`
	TLS         bool       `json:"tls"`
	Steps       []TestStep `json:"steps"`
	UnreadCount int        `json:"unread_count,omitempty"`
	TotalMs     int        `json:"total_ms"`
	Success     bool       `json:"success"`
}

// SMTPTestResult is the result of an SMTP connection test.
type SMTPTestResult struct {
	Server   string     `json:"server"`
	TLS      bool       `json:"tls"`
	StartTLS bool       `json:"starttls"`
	Steps    []TestStep `json:"steps"`
	TotalMs  int        `json:"total_ms"`
	Success  bool       `json:"success"`
}

// TestIMAPConnection tests the IMAP connection: connect → login → select → search unread.
func (c *EmailChannel) TestIMAPConnection(ctx context.Context) *IMAPTestResult {
	start := time.Now()
	result := &IMAPTestResult{
		Server: c.imapAddr,
		TLS:    c.imapTLS(),
	}

	if c.imapAddr == "" || c.config.IMAPUser == "" || c.config.IMAPPassword.String() == "" {
		result.Steps = append(result.Steps, TestStep{
			Name:    "connect",
			Success: false,
			Error:   "IMAP not configured (missing server, user, or password)",
		})
		result.TotalMs = msSince(start)
		return result
	}

	// Step 1: Connect
	var cl *imapclient.Client
	step := testStep("connect", func() error {
		var err error
		if c.imapTLS() {
			cl, err = imapclient.DialTLS(c.imapAddr, &tls.Config{ServerName: imapHost(c.imapAddr)})
		} else {
			cl, err = imapclient.Dial(c.imapAddr)
		}
		return err
	})
	result.Steps = append(result.Steps, step)
	if !step.Success {
		result.TotalMs = msSince(start)
		return result
	}
	defer cl.Logout()

	// Step 2: Login
	step = testStep("login", func() error {
		return cl.Login(c.config.IMAPUser, c.config.IMAPPassword.String())
	})
	result.Steps = append(result.Steps, step)
	if !step.Success {
		result.TotalMs = msSince(start)
		return result
	}

	// Step 3: Select mailbox
	mailbox := c.mailbox()
	step = testStep("select_mailbox", func() error {
		_, err := cl.Select(mailbox, false)
		return err
	})
	step.Detail = mailbox
	result.Steps = append(result.Steps, step)
	if !step.Success {
		result.TotalMs = msSince(start)
		return result
	}

	// Step 4: Search unread
	step = testStep("search", func() error {
		criteria := imap.NewSearchCriteria()
		criteria.WithoutFlags = []string{imap.SeenFlag}
		uids, err := cl.UidSearch(criteria)
		if err != nil {
			return err
		}
		result.UnreadCount = len(uids)
		return nil
	})
	result.Steps = append(result.Steps, step)

	result.TotalMs = msSince(start)
	result.Success = allStepsSuccess(result.Steps)
	return result
}

// TestSMTPConnection tests the SMTP connection: connect → starttls (optional) → auth → noop.
// It does NOT send an email.
func (c *EmailChannel) TestSMTPConnection(ctx context.Context) *SMTPTestResult {
	start := time.Now()
	result := &SMTPTestResult{
		Server:   c.smtpAddr,
		TLS:      c.smtpImplicitTLS(),
		StartTLS: c.config.SMTPStartTLS,
	}

	if c.smtpAddr == "" {
		result.Steps = append(result.Steps, TestStep{
			Name:    "connect",
			Success: false,
			Error:   "SMTP not configured (missing server)",
		})
		result.TotalMs = msSince(start)
		return result
	}

	var cl *smtp.Client

	// Step 1: Connect
	step := testStep("connect", func() error {
		sendCtx, cancel := context.WithTimeout(ctx, smtpOperationTimeout)
		defer cancel()
		host := smtpHost(c.smtpAddr)

		if c.smtpImplicitTLS() {
			dialer := tls.Dialer{
				NetDialer: &net.Dialer{Timeout: smtpOperationTimeout},
				Config:    &tls.Config{ServerName: host},
			}
			conn, err := dialer.DialContext(sendCtx, "tcp", c.smtpAddr)
			if err != nil {
				return err
			}
			conn.SetDeadline(time.Now().Add(smtpOperationTimeout))
			cl, err = smtp.NewClient(conn, host)
			return err
		}

		// Plain dial + optional STARTTLS
		dialer := net.Dialer{Timeout: smtpOperationTimeout}
		conn, err := dialer.DialContext(sendCtx, "tcp", c.smtpAddr)
		if err != nil {
			return err
		}
		conn.SetDeadline(time.Now().Add(smtpOperationTimeout))
		cl, err = smtp.NewClient(conn, host)
		return err
	})
	result.Steps = append(result.Steps, step)
	if !step.Success || cl == nil {
		result.TotalMs = msSince(start)
		return result
	}
	defer cl.Close()

	// Step 2: STARTTLS (if configured)
	if c.config.SMTPStartTLS {
		step = testStep("starttls", func() error {
			if ok, _ := cl.Extension("STARTTLS"); !ok {
				return smtpSendFailed("SMTP server does not support STARTTLS", nil)
			}
			host := smtpHost(c.smtpAddr)
			return cl.StartTLS(&tls.Config{ServerName: host})
		})
		result.Steps = append(result.Steps, step)
		if !step.Success {
			result.TotalMs = msSince(start)
			return result
		}
	}

	// Step 3: Auth
	if c.config.SMTPUser != "" && c.config.SMTPPassword.String() != "" {
		step = testStep("auth", func() error {
			host := smtpHost(c.smtpAddr)
			auth := smtp.PlainAuth("", c.config.SMTPUser, c.config.SMTPPassword.String(), host)
			return cl.Auth(auth)
		})
		result.Steps = append(result.Steps, step)
		if !step.Success {
			result.TotalMs = msSince(start)
			return result
		}
	} else {
		result.Steps = append(result.Steps, TestStep{
			Name:    "auth",
			Success: true,
			Detail:  "no credentials configured, skipped",
		})
	}

	// Step 4: Noop (verify connection health)
	step = testStep("noop", func() error {
		return cl.Noop()
	})
	result.Steps = append(result.Steps, step)

	result.TotalMs = msSince(start)
	result.Success = allStepsSuccess(result.Steps)
	return result
}

func testStep(name string, fn func() error) TestStep {
	start := time.Now()
	err := fn()
	step := TestStep{
		Name:       name,
		Success:    err == nil,
		DurationMs: msSince(start),
	}
	if err != nil {
		step.Error = err.Error()
	}
	return step
}

func msSince(t time.Time) int {
	return int(time.Since(t).Milliseconds())
}

func allStepsSuccess(steps []TestStep) bool {
	for _, s := range steps {
		if !s.Success {
			return false
		}
	}
	return true
}
