# Email Channel

The `email` channel lets PicoClaw receive emails over IMAP, process the message
body and attachments through the normal agent pipeline, and reply over SMTP when
policy allows it.

If `whitelist_file` is set, only senders listed in that file are processed. If
no whitelist file is configured, the channel falls back to the channel-level
`allow_from` list. Replies are additionally gated by the optional usage quota
file.

## Configuration

Example:

```yaml
channel_list:
  email:
    enabled: true
    type: email
    allow_from:
      - alice@example.com
      - bob@example.com
    settings:
      from: bot@example.com
      smtp_server: smtp.example.com
      smtp_port: 587
      smtp_user: bot@example.com
      smtp_password: your-smtp-password
      smtp_tls: false
      smtp_starttls: true
      imap_server: imap.example.com
      imap_port: 993
      imap_user: bot@example.com
      imap_password: your-imap-password
      imap_tls: true
      poll_interval_secs: 30
      whitelist_file: /etc/picoclaw/email-whitelist.txt
      usage_file: /etc/picoclaw/email-usage.json
      mailbox: INBOX
      max_attachment_size_bytes: 10485760
```

The legacy `tls` field is still accepted as a fallback for both SMTP and IMAP
when `smtp_tls` or `imap_tls` are not set, but new configurations should prefer
the explicit transport-specific flags.

## Whitelist file

The whitelist file is a plain text file with one sender address per line.
Blank lines and lines starting with `#` or `;` are ignored.

## Usage file

The usage file is a JSON object mapping sender email addresses to the number of
replies left:

```json
{
  "alice@example.com": 3,
  "bob@example.com": 0
}
```

If a sender has no entry in the usage file, the channel treats the sender as
unlimited for reply-count purposes.
