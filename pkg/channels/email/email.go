package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
)

const smtpOperationTimeout = 30 * time.Second

type EmailChannel struct {
	*channels.BaseChannel
	config   *config.EmailSettings
	smtpAddr string
	imapAddr string
	logStore *emailLogStore

	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	lastUID uint32
}

type emailReplyPolicy struct {
	Allowed   bool
	Remaining int // -1 = unlimited / unknown
	Reason    string
}

type emailAttachment struct {
	Filename    string
	ContentType string
	SizeBytes   int64
	TempPath    string
	Ref         string
	Skipped     bool
	SkipReason  string
}

type parsedEmail struct {
	FromName        string
	FromEmail       string
	Subject         string
	MessageID       string
	InReplyTo       string
	References      string
	Date            string
	Body            string
	Attachments     []emailAttachment
	ReplyPolicy     emailReplyPolicy
	RawHeaderFields map[string]string
}

func NewEmailChannel(bc *config.Channel, settings *config.EmailSettings, messageBus *bus.MessageBus) (*EmailChannel, error) {
	if bc == nil {
		return nil, fmt.Errorf("email channel config is nil")
	}
	if settings == nil {
		return nil, fmt.Errorf("email settings are nil")
	}
	if strings.TrimSpace(settings.SMTPServer) == "" && strings.TrimSpace(settings.IMAPServer) == "" {
		return nil, fmt.Errorf("email requires at least smtp_server or imap_server")
	}
	config.ApplyEmailSettingsDefaults(settings)

	base := channels.NewBaseChannel(
		bc.Name(),
		settings,
		messageBus,
		nil,
		channels.WithMaxMessageLength(0),
		channels.WithGroupTrigger(bc.GroupTrigger),
		channels.WithReasoningChannelID(bc.ReasoningChannelID),
	)

	ch := &EmailChannel{
		BaseChannel: base,
		config:      settings,
		smtpAddr:    resolveAddress(settings.SMTPServer, settings.SMTPPort, emailSMTPImplicitTLS(settings), 587, 465),
		imapAddr:    resolveAddress(settings.IMAPServer, settings.IMAPPort, emailIMAPTLS(settings), 143, 993),
	}
	ch.initLogStore()
	base.SetOwner(ch)
	return ch, nil
}

func boolWithFallback(value *bool, fallback bool) bool {
	if value != nil {
		return *value
	}
	return fallback
}

func emailSMTPImplicitTLS(settings *config.EmailSettings) bool {
	if settings == nil {
		return false
	}
	return boolWithFallback(settings.SMTPTLS, settings.TLS)
}

func emailIMAPTLS(settings *config.EmailSettings) bool {
	if settings == nil {
		return false
	}
	return boolWithFallback(settings.IMAPTLS, settings.TLS)
}

func resolveAddress(host string, port int, secure bool, plainDefault int, secureDefault int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if port > 0 {
		return net.JoinHostPort(host, fmt.Sprintf("%d", port))
	}
	if secure {
		return net.JoinHostPort(host, fmt.Sprintf("%d", secureDefault))
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", plainDefault))
}

func (c *EmailChannel) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.SetRunning(true)
	if c.imapAddr != "" && c.config.IMAPUser != "" && c.config.IMAPPassword.String() != "" {
		go c.pollLoop()
	}
	logger.InfoCF("email", "Email channel started", map[string]any{
		"name":         c.Name(),
		"smtp":         c.smtpAddr,
		"imap":         c.imapAddr,
		"from":         c.config.From,
		"mailbox":      c.mailbox(),
		"smtp_tls":     c.smtpImplicitTLS(),
		"imap_tls":     c.imapTLS(),
		"enable_quota": c.config.EnableQuota,
		"quota_file":   c.config.QuotaFile,
	})
	return nil
}

func (c *EmailChannel) Stop(ctx context.Context) error {
	_ = ctx
	if c.cancel != nil {
		c.cancel()
	}
	c.SetRunning(false)
	return nil
}

func (c *EmailChannel) Send(ctx context.Context, msg bus.OutboundMessage) ([]string, error) {
	if !c.IsRunning() {
		return nil, channels.ErrNotRunning
	}
	if strings.TrimSpace(msg.Content) == "" {
		return nil, nil
	}
	if !c.shouldSendReply(msg) {
		logger.InfoCF("email", "Email reply suppressed by policy", map[string]any{
			"channel": c.Name(),
			"chat_id": msg.ChatID,
		})
		return nil, nil
	}
	if c.smtpAddr == "" {
		return nil, fmt.Errorf("smtp_server not configured: %w", channels.ErrSendFailed)
	}

	to := strings.TrimSpace(msg.ChatID)
	if to == "" {
		return nil, fmt.Errorf("recipient email is required: %w", channels.ErrSendFailed)
	}

	from := strings.TrimSpace(c.config.From)
	if from == "" {
		from = strings.TrimSpace(c.config.SMTPUser)
	}
	if from == "" {
		return nil, fmt.Errorf("from address is required: %w", channels.ErrSendFailed)
	}

	subject := c.replySubject(msg)
	inReplyTo, references := c.replyHeaders(msg)
	raw := c.buildRawMessage(from, to, subject, msg.Content, inReplyTo, references)
	if err := c.sendRawEmail(ctx, raw, from, to); err != nil {
		return nil, err
	}

	c.logOutboundEmail(to, subject, msg.Content, nil)
	c.decrementUsageIfNeeded(to)
	logger.InfoCF("email", "Email reply sent", map[string]any{
		"to":      to,
		"subject": subject,
	})
	return nil, nil
}

func (c *EmailChannel) pollLoop() {
	interval := time.Duration(c.config.PollIntervalSecs) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.fetchAndProcessEmails()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.fetchAndProcessEmails()
		}
	}
}

func (c *EmailChannel) fetchAndProcessEmails() {
	if c.imapAddr == "" || c.config.IMAPUser == "" || c.config.IMAPPassword.String() == "" {
		return
	}

	var cl *imapclient.Client
	var err error
	if c.imapTLS() {
		cl, err = imapclient.DialTLS(c.imapAddr, &tls.Config{ServerName: imapHost(c.imapAddr)})
	} else {
		cl, err = imapclient.Dial(c.imapAddr)
	}
	if err != nil {
		logger.WarnCF("email", "IMAP connect failed", map[string]any{
			"server": c.imapAddr,
			"error":  err.Error(),
		})
		return
	}
	defer cl.Logout()

	if err := cl.Login(c.config.IMAPUser, c.config.IMAPPassword.String()); err != nil {
		logger.WarnCF("email", "IMAP login failed", map[string]any{"error": err.Error()})
		return
	}

	mailbox := c.mailbox()
	if _, err := cl.Select(mailbox, false); err != nil {
		logger.WarnCF("email", "IMAP select failed", map[string]any{
			"mailbox": mailbox,
			"error":   err.Error(),
		})
		return
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	uids, err := cl.UidSearch(criteria)
	if err != nil {
		logger.WarnCF("email", "IMAP search failed", map[string]any{"error": err.Error()})
		return
	}
	if len(uids) == 0 {
		return
	}

	section := &imap.BodySectionName{}
	set := new(imap.SeqSet)
	hasUID := false
	for _, uid := range uids {
		if uid <= c.lastUID {
			continue
		}
		set.AddNum(uid)
		hasUID = true
	}
	if !hasUID {
		return
	}

	items := []imap.FetchItem{imap.FetchUid, section.FetchItem()}
	messages := startFetchMessages(func(messages chan *imap.Message) error {
		return cl.UidFetch(set, items, messages)
	}, len(uids))

	var maxUID uint32
	handledUIDs := make([]uint32, 0, len(uids))
	for msg := range messages {
		if msg == nil {
			continue
		}
		if msg.Uid <= c.lastUID {
			continue
		}
		if msg.Uid > maxUID {
			maxUID = msg.Uid
		}
		c.processFetchedEmail(msg, section)
		handledUIDs = append(handledUIDs, msg.Uid)
	}
	if err := markSeenUIDs(cl, handledUIDs); err != nil {
		logger.WarnCF("email", "Failed to mark emails as seen", map[string]any{"error": err.Error()})
	}
	if maxUID > c.lastUID {
		c.lastUID = maxUID
	}
}

func startFetchMessages(fetch func(chan *imap.Message) error, bufSize int) <-chan *imap.Message {
	messages := make(chan *imap.Message, bufSize)
	go func() {
		// github.com/emersion/go-imap/client closes the provided channel before
		// Fetch/UidFetch returns, so the caller must not close it again here.
		if err := fetch(messages); err != nil {
			logger.ErrorCF("email", "IMAP fetch failed", map[string]any{"error": err.Error()})
		}
	}()
	return messages
}

func (c *EmailChannel) processFetchedEmail(msg *imap.Message, section *imap.BodySectionName) {
	bodyReader := msg.GetBody(section)
	if bodyReader == nil {
		return
	}
	raw, err := io.ReadAll(bodyReader)
	if err != nil {
		logger.WarnCF("email", "Read email body failed", map[string]any{"error": err.Error()})
		return
	}

	parsed, err := c.parseRawEmail(raw)
	if err != nil {
		logger.WarnCF("email", "Parse email failed", map[string]any{"error": err.Error()})
		return
	}
	if parsed.FromEmail == "" {
		c.cleanupTemporaryAttachments(parsed.Attachments)
		return
	}
	if ok, reason := c.checkQuotaAccess(parsed.FromEmail); !ok {
		c.logInboundEmail(parsed, "用户限额不足，不能处理。")
		c.cleanupTemporaryAttachments(parsed.Attachments)
		logger.InfoCF("email", "Skipping sender by quota policy", map[string]any{
			"from":   parsed.FromEmail,
			"reason": reason,
		})
		return
	}

	c.logInboundEmail(parsed, "")

	policy := c.evaluateReplyPolicy(parsed.FromEmail)
	parsed.ReplyPolicy = policy
	parsed.Attachments = c.storeAttachments(parsed.Attachments, parsed.FromEmail, parsed.MessageID)
	content, rawFields := c.composeInboundContent(parsed)
	if rawFields == nil {
		rawFields = map[string]string{}
	}
	rawFields["email_from"] = parsed.FromEmail
	rawFields["email_from_name"] = parsed.FromName
	rawFields["email_subject"] = parsed.Subject
	rawFields["email_message_id"] = parsed.MessageID
	rawFields["email_in_reply_to"] = parsed.InReplyTo
	rawFields["email_references"] = parsed.References
	rawFields["email_reply_allowed"] = fmt.Sprintf("%t", policy.Allowed)
	if policy.Remaining >= 0 {
		rawFields["email_remaining_uses"] = fmt.Sprintf("%d", policy.Remaining)
	} else {
		rawFields["email_remaining_uses"] = "unlimited"
	}

	attachments := make([]string, 0, len(parsed.Attachments))
	for _, att := range parsed.Attachments {
		if att.Ref != "" {
			attachments = append(attachments, att.Ref)
		}
	}

	inbound := bus.InboundContext{
		Channel:          c.Name(),
		Account:          strings.TrimSpace(c.config.From),
		ChatID:           parsed.FromEmail,
		ChatType:         "direct",
		SenderID:         parsed.FromEmail,
		MessageID:        parsed.MessageID,
		ReplyToMessageID: parsed.MessageID,
		Raw:              rawFields,
	}

	sender := bus.SenderInfo{
		Platform:    "email",
		PlatformID:  parsed.FromEmail,
		CanonicalID: identity.BuildCanonicalID("email", parsed.FromEmail),
		Username:    parsed.FromEmail,
		DisplayName: parsed.FromName,
	}

	inboundCtx := c.ctx
	if inboundCtx == nil {
		inboundCtx = context.Background()
	}
	c.HandleInboundContext(inboundCtx, parsed.FromEmail, content, attachments, inbound, sender)
}

func (c *EmailChannel) parseRawEmail(raw []byte) (*parsedEmail, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	headers := map[string]string{}
	for _, key := range []string{"From", "Subject", "Message-ID", "In-Reply-To", "References", "Date"} {
		headers[strings.ToLower(key)] = strings.TrimSpace(msg.Header.Get(key))
	}

	fromName, fromEmail := c.parseFromHeader(msg.Header.Get("From"))
	subject := decodeHeaderValue(msg.Header.Get("Subject"))
	messageID := normalizeMessageID(msg.Header.Get("Message-ID"))
	inReplyTo := normalizeMessageID(msg.Header.Get("In-Reply-To"))
	references := normalizeReferences(msg.Header.Get("References"))
	date := strings.TrimSpace(msg.Header.Get("Date"))

	contentType := msg.Header.Get("Content-Type")
	body, attachments, err := c.extractBodyAndAttachments(msg.Body, contentType, msg.Header)
	if err != nil {
		return nil, err
	}

	return &parsedEmail{
		FromName:        fromName,
		FromEmail:       fromEmail,
		Subject:         subject,
		MessageID:       messageID,
		InReplyTo:       inReplyTo,
		References:      references,
		Date:            date,
		Body:            strings.TrimSpace(body),
		Attachments:     attachments,
		RawHeaderFields: headers,
	}, nil
}

func (c *EmailChannel) extractBodyAndAttachments(body io.Reader, contentType string, headers mail.Header) (string, []emailAttachment, error) {
	if contentType == "" {
		contentType = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
		params = map[string]string{}
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", nil, nil
		}
		return c.walkMultipart(body, boundary, headers)
	}

	if attachment := classifyAttachmentMeta(mediaType, headers, params); attachment != nil {
		storedAttachment, err := c.readAttachmentPart(body, headers.Get("Content-Transfer-Encoding"), *attachment)
		if err != nil {
			return "", nil, err
		}
		return "", []emailAttachment{storedAttachment}, nil
	}

	text, err := readTextPart(body, headers.Get("Content-Transfer-Encoding"), mediaType)
	if err != nil {
		return "", nil, err
	}
	return text, nil, nil
}

func (c *EmailChannel) walkMultipart(body io.Reader, boundary string, headers mail.Header) (string, []emailAttachment, error) {
	_ = headers
	reader := multipart.NewReader(body, boundary)
	var plainParts []string
	var htmlParts []string
	var attachments []emailAttachment

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		contentType := part.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			mediaType = "text/plain"
			params = map[string]string{}
		}

		if strings.HasPrefix(mediaType, "multipart/") {
			nestedBody, nestedAttachments, err := c.extractBodyAndAttachments(part, contentType, mail.Header(part.Header))
			if err != nil {
				return "", nil, err
			}
			if strings.TrimSpace(nestedBody) != "" {
				if strings.Contains(strings.ToLower(mediaType), "alternative") {
					plainParts = append(plainParts, nestedBody)
				} else {
					plainParts = append(plainParts, nestedBody)
				}
			}
			attachments = append(attachments, nestedAttachments...)
			continue
		}

		partHeader := mail.Header(part.Header)
		if attachment := classifyAttachmentMeta(mediaType, partHeader, params); attachment != nil {
			storedAttachment, err := c.readAttachmentPart(part, part.Header.Get("Content-Transfer-Encoding"), *attachment)
			if err != nil {
				return "", nil, err
			}
			attachments = append(attachments, storedAttachment)
			continue
		}

		text, err := readTextPart(part, part.Header.Get("Content-Transfer-Encoding"), mediaType)
		if err != nil {
			return "", nil, err
		}
		if text == "" {
			continue
		}
		if mediaType == "text/html" {
			htmlParts = append(htmlParts, text)
		} else {
			plainParts = append(plainParts, text)
		}
	}

	if len(plainParts) > 0 {
		return strings.Join(plainParts, "\n\n"), attachments, nil
	}
	if len(htmlParts) > 0 {
		return strings.Join(htmlParts, "\n\n"), attachments, nil
	}
	return "", attachments, nil
}

func classifyAttachmentMeta(mediaType string, headers mail.Header, params map[string]string) *emailAttachment {
	disposition, dispParams, _ := mime.ParseMediaType(headers.Get("Content-Disposition"))
	filename := dispParams["filename"]
	if filename == "" {
		filename = params["name"]
	}

	isAttachment := strings.EqualFold(disposition, "attachment") ||
		filename != "" ||
		!strings.HasPrefix(mediaType, "text/")

	if isAttachment {
		filename = sanitizeFilename(filename)
		if filename == "" {
			filename = "attachment"
		}
		return &emailAttachment{
			Filename:    filename,
			ContentType: mediaType,
		}
	}
	return nil
}

func readTextPart(body io.Reader, transferEncoding, mediaType string) (string, error) {
	raw, err := io.ReadAll(decodeTransferEncodingReader(transferEncoding, body))
	if err != nil {
		return "", err
	}

	switch mediaType {
	case "text/plain", "":
		return strings.TrimSpace(string(raw)), nil
	case "text/html":
		return stripHTML(string(raw)), nil
	default:
		return strings.TrimSpace(string(raw)), nil
	}
}

func (c *EmailChannel) readAttachmentPart(body io.Reader, transferEncoding string, attachment emailAttachment) (emailAttachment, error) {
	if err := os.MkdirAll(media.TempDir(), 0o755); err != nil {
		return attachment, err
	}

	tmp, err := os.CreateTemp(media.TempDir(), "email-*"+filepath.Ext(attachment.Filename))
	if err != nil {
		return attachment, err
	}

	keepTempFile := false
	defer func() {
		_ = tmp.Close()
		if !keepTempFile {
			_ = os.Remove(tmp.Name())
		}
	}()

	decoded := decodeTransferEncodingReader(transferEncoding, body)
	maxSize := c.config.MaxAttachmentSizeBytes
	reader := io.Reader(decoded)
	if maxSize > 0 {
		reader = io.LimitReader(decoded, maxSize+1)
	}

	sizeBytes, err := io.Copy(tmp, reader)
	if err != nil {
		return attachment, err
	}
	attachment.SizeBytes = sizeBytes

	if maxSize > 0 && sizeBytes > maxSize {
		_, _ = io.Copy(io.Discard, decoded)
		attachment.Skipped = true
		attachment.SkipReason = fmt.Sprintf("attachment exceeds max_attachment_size_bytes (%d)", maxSize)
		return attachment, nil
	}

	attachment.TempPath = tmp.Name()
	keepTempFile = true
	return attachment, nil
}

func (c *EmailChannel) storeAttachments(attachments []emailAttachment, senderEmail, messageID string) []emailAttachment {
	if len(attachments) == 0 {
		return attachments
	}
	scope := channels.BuildMediaScope(c.Name(), senderEmail, messageID)
	out := make([]emailAttachment, 0, len(attachments))
	for _, att := range attachments {
		ref, skipped, reason := c.storeAttachment(scope, &att)
		att.Ref = ref
		att.Skipped = skipped
		att.SkipReason = reason
		att.TempPath = ""
		out = append(out, att)
	}
	return out
}

func (c *EmailChannel) storeAttachment(scope string, attachment *emailAttachment) (ref string, skipped bool, reason string) {
	if attachment == nil {
		return "", true, "attachment is nil"
	}
	if attachment.Skipped {
		return "", true, attachment.SkipReason
	}

	localPath := attachment.TempPath
	if localPath == "" {
		return "", true, "attachment data unavailable"
	}

	removeOnFailure := true
	defer func() {
		if removeOnFailure {
			_ = os.Remove(localPath)
		}
	}()

	store := c.GetMediaStore()
	if store == nil {
		return "", true, "media store unavailable"
	}

	var err error
	ref, err = store.Store(localPath, media.MediaMeta{
		Filename:    attachment.Filename,
		ContentType: attachment.ContentType,
		Source:      "email",
	}, scope)
	if err != nil {
		return "", true, err.Error()
	}
	removeOnFailure = false
	return ref, false, ""
}

func (c *EmailChannel) composeInboundContent(email *parsedEmail) (string, map[string]string) {
	var sb strings.Builder
	sb.WriteString("Email message received.\n")
	if email.FromName != "" {
		sb.WriteString("From: ")
		sb.WriteString(email.FromName)
		sb.WriteString(" <")
		sb.WriteString(email.FromEmail)
		sb.WriteString(">\n")
	} else {
		sb.WriteString("From: ")
		sb.WriteString(email.FromEmail)
		sb.WriteString("\n")
	}
	if email.Subject != "" {
		sb.WriteString("Subject: ")
		sb.WriteString(email.Subject)
		sb.WriteString("\n")
	}
	if email.MessageID != "" {
		sb.WriteString("Message-ID: ")
		sb.WriteString(email.MessageID)
		sb.WriteString("\n")
	}
	if email.Date != "" {
		sb.WriteString("Date: ")
		sb.WriteString(email.Date)
		sb.WriteString("\n")
	}
	sb.WriteString("Reply allowed: ")
	if email.ReplyPolicy.Allowed {
		sb.WriteString("yes")
	} else {
		sb.WriteString("no")
	}
	sb.WriteString("\n")
	if email.ReplyPolicy.Remaining >= 0 {
		sb.WriteString("Remaining replies: ")
		sb.WriteString(fmt.Sprintf("%d", email.ReplyPolicy.Remaining))
		sb.WriteString("\n")
	} else {
		sb.WriteString("Remaining replies: unlimited\n")
	}
	if email.ReplyPolicy.Reason != "" {
		sb.WriteString("Policy note: ")
		sb.WriteString(email.ReplyPolicy.Reason)
		sb.WriteString("\n")
	}
	if len(email.Attachments) > 0 {
		sb.WriteString("\nAttachments:\n")
		for i, att := range email.Attachments {
			sb.WriteString(fmt.Sprintf("- #%d %s", i+1, att.Filename))
			if att.ContentType != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", att.ContentType))
			}
			if att.SizeBytes > 0 {
				sb.WriteString(fmt.Sprintf(", %d bytes", att.SizeBytes))
			}
			if att.Ref != "" {
				sb.WriteString(fmt.Sprintf(" -> %s", att.Ref))
			}
			if att.Skipped && att.SkipReason != "" {
				sb.WriteString(fmt.Sprintf(" [skipped: %s]", att.SkipReason))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n--- Email body ---\n")
	sb.WriteString(strings.TrimSpace(email.Body))

	raw := map[string]string{
		"email_from":           email.FromEmail,
		"email_from_name":      email.FromName,
		"email_subject":        email.Subject,
		"email_message_id":     email.MessageID,
		"email_in_reply_to":    email.InReplyTo,
		"email_references":     email.References,
		"email_reply_allowed":  fmt.Sprintf("%t", email.ReplyPolicy.Allowed),
		"email_date":           email.Date,
		"email_attachment_cnt": fmt.Sprintf("%d", len(email.Attachments)),
	}
	if email.ReplyPolicy.Remaining >= 0 {
		raw["email_remaining_uses"] = fmt.Sprintf("%d", email.ReplyPolicy.Remaining)
	} else {
		raw["email_remaining_uses"] = "unlimited"
	}
	return sb.String(), raw
}

func (c *EmailChannel) parseFromHeader(header string) (string, string) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", ""
	}
	addrs, err := mail.ParseAddressList(header)
	if err != nil || len(addrs) == 0 {
		return "", normalizeEmailAddress(header)
	}
	addr := addrs[0]
	return strings.TrimSpace(addr.Name), normalizeEmailAddress(addr.Address)
}

type emailQuotaEntry struct {
	Name      string `json:"name"`
	Remaining int    `json:"remaining"`
	ExpiresAt string `json:"expires_at"`
}

// checkQuotaAccess determines whether an inbound sender should be processed.
// When enable_quota is false, all senders are allowed.
// When enable_quota is true, the sender must exist in the quota file with
// remaining > 0 and an unexpired date.
func (c *EmailChannel) checkQuotaAccess(sender string) (bool, string) {
	sender = normalizeEmailAddress(sender)
	if !c.config.EnableQuota {
		return true, "quota disabled"
	}

	policy := c.checkQuotaFile(sender)
	if policy == nil {
		return false, "sender not in quota list"
	}
	return policy.Allowed, policy.Reason
}

func (c *EmailChannel) evaluateReplyPolicy(sender string) emailReplyPolicy {
	sender = normalizeEmailAddress(sender)
	if !c.config.EnableQuota {
		return emailReplyPolicy{Allowed: true, Remaining: -1, Reason: "quota disabled"}
	}
	policy := c.checkQuotaFile(sender)
	if policy == nil {
		return emailReplyPolicy{Allowed: false, Remaining: 0, Reason: "sender not in quota list"}
	}
	return *policy
}

func (c *EmailChannel) checkQuotaFile(sender string) *emailReplyPolicy {
	path := config.ExpandPath(c.config.QuotaFile)
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	quotas := make(map[string]emailQuotaEntry)
	if err := json.Unmarshal(data, &quotas); err != nil {
		return nil
	}

	entry, hasQuota := quotas[sender]
	if !hasQuota {
		return nil
	}

	if entry.Remaining == 0 || entry.Remaining < -1 {
		return &emailReplyPolicy{Allowed: false, Remaining: 0, Reason: "sender quota exhausted"}
	}

	if entry.ExpiresAt != "" {
		if expires, err := time.Parse("2006-01-02", entry.ExpiresAt); err == nil {
			now := time.Now().Truncate(24 * time.Hour)
			if now.After(expires) {
				return &emailReplyPolicy{Allowed: false, Remaining: entry.Remaining, Reason: "quota expired"}
			}
		}
	}

	return &emailReplyPolicy{Allowed: true, Remaining: entry.Remaining, Reason: "quota available"}
}

func (c *EmailChannel) checkSendReplyPolicy(chatID string, raw map[string]string) bool {
	if chatID == "" {
		return false
	}
	if raw != nil {
		if v := strings.ToLower(strings.TrimSpace(raw["email_reply_allowed"])); v == "false" || v == "0" || v == "no" {
			return false
		}
	}
	policy := c.evaluateReplyPolicy(chatID)
	return policy.Allowed
}

func (c *EmailChannel) shouldSendReply(msg bus.OutboundMessage) bool {
	return c.checkSendReplyPolicy(msg.ChatID, msg.Context.Raw)
}

func (c *EmailChannel) decrementQuotaFile(sender string) bool {
	path := config.ExpandPath(c.config.QuotaFile)
	if path == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	quotas := make(map[string]emailQuotaEntry)
	if err := json.Unmarshal(data, &quotas); err != nil {
		return false
	}

	entry, hasQuota := quotas[sender]
	if !hasQuota || entry.Remaining == 0 || entry.Remaining < -1 {
		return false
	}
	if entry.Remaining == -1 {
		return false
	}

	entry.Remaining--
	quotas[sender] = entry

	out, err := json.MarshalIndent(quotas, "", "  ")
	if err != nil {
		return false
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return false
	}
	_ = os.Rename(tmp, path)
	return true
}

func (c *EmailChannel) decrementUsageIfNeeded(sender string) {
	sender = normalizeEmailAddress(sender)
	if !c.config.EnableQuota {
		return
	}
	_ = c.decrementQuotaFile(sender)
}

func (c *EmailChannel) replySubject(msg bus.OutboundMessage) string {
	if msg.Context.Raw != nil {
		if subject := strings.TrimSpace(msg.Context.Raw["email_subject"]); subject != "" {
			return prefixReplySubject(subject)
		}
	}
	return prefixReplySubject("")
}

func (c *EmailChannel) replyHeaders(msg bus.OutboundMessage) (string, string) {
	inReplyTo := ""
	references := ""
	if msg.Context.Raw != nil {
		inReplyTo = strings.TrimSpace(msg.Context.Raw["email_message_id"])
		references = strings.TrimSpace(msg.Context.Raw["email_references"])
		if inReplyTo == "" {
			inReplyTo = strings.TrimSpace(msg.Context.Raw["email_in_reply_to"])
		}
	}
	if inReplyTo == "" {
		inReplyTo = strings.TrimSpace(msg.ReplyToMessageID)
	}
	if references == "" && inReplyTo != "" {
		references = inReplyTo
	}
	return inReplyTo, references
}

func (c *EmailChannel) buildRawMessage(from, to, subject, body, inReplyTo, references string) []byte {
	var sb strings.Builder
	sb.WriteString("From: ")
	sb.WriteString(formatAddressHeader(from))
	sb.WriteString("\r\n")
	sb.WriteString("To: ")
	sb.WriteString(formatAddressHeader(to))
	sb.WriteString("\r\n")
	sb.WriteString("Subject: ")
	sb.WriteString(encodeHeaderValue(subject))
	sb.WriteString("\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	if inReplyTo != "" {
		sb.WriteString("In-Reply-To: ")
		sb.WriteString(sanitizeHeaderValue(inReplyTo))
		sb.WriteString("\r\n")
	}
	if references != "" {
		sb.WriteString("References: ")
		sb.WriteString(sanitizeHeaderValue(references))
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")
	w := quotedprintable.NewWriter(&sb)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
	return []byte(sb.String())
}

func (c *EmailChannel) SendMedia(ctx context.Context, msg bus.OutboundMediaMessage) ([]string, error) {
	if !c.IsRunning() {
		return nil, channels.ErrNotRunning
	}
	if len(msg.Parts) == 0 {
		return nil, nil
	}
	if !c.checkSendReplyPolicy(msg.ChatID, msg.Context.Raw) {
		logger.InfoCF("email", "Email media reply suppressed by policy", map[string]any{
			"channel": c.Name(),
			"chat_id": msg.ChatID,
		})
		return nil, nil
	}
	if c.smtpAddr == "" {
		return nil, fmt.Errorf("smtp_server not configured: %w", channels.ErrSendFailed)
	}

	to := strings.TrimSpace(msg.ChatID)
	if to == "" {
		return nil, fmt.Errorf("recipient email is required: %w", channels.ErrSendFailed)
	}

	from := strings.TrimSpace(c.config.From)
	if from == "" {
		from = strings.TrimSpace(c.config.SMTPUser)
	}
	if from == "" {
		return nil, fmt.Errorf("from address is required: %w", channels.ErrSendFailed)
	}

	store := c.GetMediaStore()
	if store == nil {
		return nil, fmt.Errorf("media store unavailable: %w", channels.ErrSendFailed)
	}

	subject := c.replySubjectMedia(msg)
	inReplyTo, references := c.replyHeadersMedia(msg)

	raw := c.buildMultipartMessage(from, to, subject, msg.Parts, inReplyTo, references, store)
	if raw == nil {
		return nil, nil
	}

	if err := c.sendRawEmail(ctx, raw, from, to); err != nil {
		return nil, err
	}

	var atts []EmailLogAttachment
	for _, p := range msg.Parts {
		atts = append(atts, EmailLogAttachment{
			Filename:    p.Filename,
			ContentType: p.ContentType,
			SizeBytes:   0,
		})
	}
	c.logOutboundEmail(to, subject, "", atts)
	c.decrementUsageIfNeeded(to)
	logger.InfoCF("email", "Email with attachments sent", map[string]any{
		"to":          to,
		"subject":     subject,
		"attachments": len(msg.Parts),
	})
	return nil, nil
}

func (c *EmailChannel) replySubjectMedia(msg bus.OutboundMediaMessage) string {
	if msg.Context.Raw != nil {
		if subject := strings.TrimSpace(msg.Context.Raw["email_subject"]); subject != "" {
			return prefixReplySubject(subject)
		}
	}
	return prefixReplySubject("")
}

func (c *EmailChannel) replyHeadersMedia(msg bus.OutboundMediaMessage) (string, string) {
	inReplyTo := ""
	references := ""
	if msg.Context.Raw != nil {
		inReplyTo = strings.TrimSpace(msg.Context.Raw["email_message_id"])
		references = strings.TrimSpace(msg.Context.Raw["email_references"])
		if inReplyTo == "" {
			inReplyTo = strings.TrimSpace(msg.Context.Raw["email_in_reply_to"])
		}
	}
	if references == "" && inReplyTo != "" {
		references = inReplyTo
	}
	return inReplyTo, references
}

const multipartBoundary = "----=_PicoClaw_Multipart_Boundary"

type lineWriter struct {
	w      io.Writer
	line   int
	maxLen int
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		remain := lw.maxLen - lw.line
		if remain <= 0 {
			if _, err := lw.w.Write([]byte("\r\n")); err != nil {
				return written, err
			}
			lw.line = 0
			remain = lw.maxLen
		}
		n := remain
		if n > len(p) {
			n = len(p)
		}
		nn, err := lw.w.Write(p[:n])
		written += nn
		if err != nil {
			return written, err
		}
		lw.line += n
		p = p[n:]
	}
	return written, nil
}

func (c *EmailChannel) buildMultipartMessage(
	from, to, subject string,
	parts []bus.MediaPart,
	inReplyTo, references string,
	store media.MediaStore,
) []byte {
	var captions []string
	for _, p := range parts {
		if p.Caption != "" {
			captions = append(captions, p.Caption)
		}
	}
	preamble := strings.Join(captions, "\n\n")
	if preamble == "" {
		preamble = " "
	}

	type resolvedPart struct {
		filename    string
		contentType string
		data        []byte
	}
	var resolved []resolvedPart
	for _, p := range parts {
		localPath, err := store.Resolve(p.Ref)
		if err != nil {
			logger.WarnCF("email", "Failed to resolve media ref", map[string]any{
				"ref":   p.Ref,
				"error": err.Error(),
			})
			continue
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			logger.WarnCF("email", "Failed to read media file", map[string]any{
				"path":  localPath,
				"error": err.Error(),
			})
			continue
		}
		filename := p.Filename
		if filename == "" {
			filename = "attachment"
		}
		contentType := p.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		resolved = append(resolved, resolvedPart{
			filename:    filename,
			contentType: contentType,
			data:        data,
		})
	}

	if len(resolved) == 0 {
		return nil
	}

	var sb strings.Builder

	sb.WriteString("From: ")
	sb.WriteString(formatAddressHeader(from))
	sb.WriteString("\r\n")
	sb.WriteString("To: ")
	sb.WriteString(formatAddressHeader(to))
	sb.WriteString("\r\n")
	sb.WriteString("Subject: ")
	sb.WriteString(encodeHeaderValue(subject))
	sb.WriteString("\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", multipartBoundary))
	if inReplyTo != "" {
		sb.WriteString("In-Reply-To: ")
		sb.WriteString(sanitizeHeaderValue(inReplyTo))
		sb.WriteString("\r\n")
	}
	if references != "" {
		sb.WriteString("References: ")
		sb.WriteString(sanitizeHeaderValue(references))
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")

	sb.WriteString(fmt.Sprintf("--%s\r\n", multipartBoundary))
	sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	sb.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&sb)
	_, _ = qp.Write([]byte(preamble))
	_ = qp.Close()
	sb.WriteString("\r\n")

	for _, rp := range resolved {
		sb.WriteString(fmt.Sprintf("--%s\r\n", multipartBoundary))
		sb.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n",
			sanitizeHeaderValue(rp.contentType), sanitizeHeaderValue(rp.filename)))
		sb.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n",
			sanitizeHeaderValue(rp.filename)))
		sb.WriteString("Content-Transfer-Encoding: base64\r\n")
		sb.WriteString("\r\n")

		lw := &lineWriter{w: &sb, maxLen: 76}
		enc := base64.NewEncoder(base64.StdEncoding, lw)
		_, _ = enc.Write(rp.data)
		_ = enc.Close()
		sb.WriteString("\r\n")
	}

	sb.WriteString(fmt.Sprintf("--%s--\r\n", multipartBoundary))

	return []byte(sb.String())
}

func (c *EmailChannel) sendRawEmail(ctx context.Context, raw []byte, from, to string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sendCtx, cancel := context.WithTimeout(ctx, smtpOperationTimeout)
	defer cancel()

	host := smtpHost(c.smtpAddr)
	envelopeFrom := envelopeEmailAddress(from)
	envelopeTo := envelopeEmailAddress(to)
	if envelopeFrom == "" {
		return smtpSendFailed("sender envelope address is required", nil)
	}
	if envelopeTo == "" {
		return smtpSendFailed("recipient envelope address is required", nil)
	}
	auth := smtp.Auth(nil)
	if strings.TrimSpace(c.config.SMTPUser) != "" {
		auth = smtp.PlainAuth("", c.config.SMTPUser, c.config.SMTPPassword.String(), host)
	}

	if c.smtpImplicitTLS() {
		dialer := tls.Dialer{
			NetDialer: &net.Dialer{Timeout: smtpOperationTimeout},
			Config:    &tls.Config{ServerName: host},
		}
		conn, err := dialer.DialContext(sendCtx, "tcp", c.smtpAddr)
		if err != nil {
			return smtpSendFailed("SMTP TLS dial failed", err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(smtpOperationTimeout))

		cl, err := smtp.NewClient(conn, host)
		if err != nil {
			return smtpSendFailed("SMTP client creation failed", err)
		}
		defer cl.Close()
		return sendSMTPClient(cl, auth, envelopeFrom, envelopeTo, raw)
	}

	dialer := net.Dialer{Timeout: smtpOperationTimeout}
	conn, err := dialer.DialContext(sendCtx, "tcp", c.smtpAddr)
	if err != nil {
		return smtpSendFailed("SMTP dial failed", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(smtpOperationTimeout))

	cl, err := smtp.NewClient(conn, host)
	if err != nil {
		return smtpSendFailed("SMTP client creation failed", err)
	}
	defer cl.Close()

	if c.config.SMTPStartTLS {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return smtpSendFailed("SMTP server does not support STARTTLS", nil)
		}
		if err := cl.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return smtpSendFailed("SMTP STARTTLS failed", err)
		}
	}

	return sendSMTPClient(cl, auth, envelopeFrom, envelopeTo, raw)
}

func sendSMTPClient(cl *smtp.Client, auth smtp.Auth, from, to string, raw []byte) error {
	if auth != nil {
		if err := cl.Auth(auth); err != nil {
			return smtpSendFailed("SMTP auth failed", err)
		}
	}
	if err := cl.Mail(from); err != nil {
		return smtpSendFailed("SMTP MAIL FROM failed", err)
	}
	if err := cl.Rcpt(to); err != nil {
		return smtpSendFailed("SMTP RCPT TO failed", err)
	}
	w, err := cl.Data()
	if err != nil {
		return smtpSendFailed("SMTP DATA failed", err)
	}
	if _, err := w.Write(raw); err != nil {
		return smtpSendFailed("SMTP write failed", err)
	}
	if err := w.Close(); err != nil {
		return smtpSendFailed("SMTP close failed", err)
	}
	if err := cl.Quit(); err != nil {
		return smtpSendFailed("SMTP quit failed", err)
	}
	return nil
}

func smtpSendFailed(message string, err error) error {
	if err == nil {
		return fmt.Errorf("%s: %w", message, channels.ErrSendFailed)
	}
	return fmt.Errorf("%s: %w: %w", message, err, channels.ErrSendFailed)
}

func (c *EmailChannel) smtpImplicitTLS() bool {
	return emailSMTPImplicitTLS(c.config)
}

func (c *EmailChannel) imapTLS() bool {
	return emailIMAPTLS(c.config)
}

func markSeenUIDs(cl *imapclient.Client, uids []uint32) error {
	if cl == nil || len(uids) == 0 {
		return nil
	}

	set := new(imap.SeqSet)
	seen := make(map[uint32]struct{}, len(uids))
	for _, uid := range uids {
		if uid == 0 {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		set.AddNum(uid)
	}
	if len(seen) == 0 {
		return nil
	}

	return cl.UidStore(set, imap.FormatFlagsOp(imap.AddFlags, true), []interface{}{imap.SeenFlag}, nil)
}

func (c *EmailChannel) mailbox() string {
	if strings.TrimSpace(c.config.Mailbox) != "" {
		return strings.TrimSpace(c.config.Mailbox)
	}
	return "INBOX"
}

func imapHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func smtpHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func normalizeEmailAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}

func normalizeMessageID(s string) string {
	return strings.TrimSpace(s)
}

func normalizeReferences(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func decodeHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		return sanitizeHeaderValue(value)
	}
	return sanitizeHeaderValue(decoded)
}

func encodeHeaderValue(value string) string {
	value = sanitizeHeaderValue(value)
	if value == "" {
		return ""
	}
	return mime.QEncoding.Encode("utf-8", value)
}

func sanitizeHeaderValue(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func formatAddressHeader(value string) string {
	value = sanitizeHeaderValue(value)
	if value == "" {
		return ""
	}
	if addr, err := mail.ParseAddress(value); err == nil {
		return addr.String()
	}
	return value
}

func envelopeEmailAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if addr, err := mail.ParseAddress(value); err == nil {
		return normalizeEmailAddress(addr.Address)
	}
	value = strings.Trim(value, "<>")
	if strings.Contains(value, "@") && !strings.ContainsAny(value, " \t\r\n,;") {
		return normalizeEmailAddress(value)
	}
	return ""
}

func (c *EmailChannel) cleanupTemporaryAttachments(attachments []emailAttachment) {
	for _, attachment := range attachments {
		if attachment.TempPath == "" {
			continue
		}
		_ = os.Remove(attachment.TempPath)
	}
}

var htmlTagRe = regexp.MustCompile(`(?s)<[^>]*>`)

func stripHTML(s string) string {
	s = html.UnescapeString(s)
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "\r", "")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func decodeTransferEncoding(encoding string, body []byte) []byte {
	decoded, err := io.ReadAll(decodeTransferEncodingReader(encoding, bytes.NewReader(body)))
	if err == nil {
		return decoded
	}
	return body
}

func decodeTransferEncodingReader(encoding string, body io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	default:
		return body
	}
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '\x00':
			return -1
		default:
			return r
		}
	}, name)
	return strings.TrimSpace(name)
}

func prefixReplySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "Re: Your message"
	}
	low := strings.ToLower(subject)
	if strings.HasPrefix(low, "re:") || strings.HasPrefix(low, "fwd:") || strings.HasPrefix(low, "fw:") {
		return subject
	}
	return "Re: " + subject
}

var _ channels.Channel = (*EmailChannel)(nil)
var _ channels.MediaSender = (*EmailChannel)(nil)
