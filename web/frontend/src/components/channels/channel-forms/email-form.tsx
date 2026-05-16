import {
  IconCheck,
  IconChevronDown,
  IconChevronRight,
  IconLoader2,
  IconMail,
  IconServer,
  IconX,
} from "@tabler/icons-react"
import { useCallback, useState } from "react"
import { useTranslation } from "react-i18next"

import { testEmailConnection, type TestStep } from "@/api/email-test"
import type { ChannelConfig } from "@/api/channels"
import { getSecretInputPlaceholder } from "@/components/channels/channel-config-fields"
import { Field, KeyInput, SwitchCardField } from "@/components/shared-form"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"

interface EmailFormProps {
  config: ChannelConfig
  onChange: (key: string, value: unknown) => void
  configuredSecrets: string[]
  fieldErrors?: Record<string, string>
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function asBool(value: unknown): boolean {
  return value === true
}

function resolveTLSValue(
  specificValue: unknown,
  legacyValue: unknown,
): boolean {
  if (typeof specificValue === "boolean") {
    return specificValue
  }
  return legacyValue === true
}

function asNumberString(value: unknown): string {
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(value)
  }
  return asString(value)
}

function stepIcon(step: TestStep) {
  if (step.success) {
    return <IconCheck className="text-emerald-500 shrink-0 size-3.5" />
  }
  return <IconX className="text-destructive shrink-0 size-3.5" />
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export function EmailForm({
  config,
  onChange,
  configuredSecrets,
  fieldErrors = {},
}: EmailFormProps) {
  const { t } = useTranslation()
  const smtpTLS = resolveTLSValue(config.smtp_tls, config.tls)
  const imapTLS = resolveTLSValue(config.imap_tls, config.tls)

  const [testing, setTesting] = useState<"imap" | "smtp" | null>(null)
  const [imapResult, setImapResult] = useState<Awaited<
    ReturnType<typeof testEmailConnection>
  >["imap"] | null>(null)
  const [smtpResult, setSmtpResult] = useState<Awaited<
    ReturnType<typeof testEmailConnection>
  >["smtp"] | null>(null)
  const [showImapDetail, setShowImapDetail] = useState(false)
  const [showSmtpDetail, setShowSmtpDetail] = useState(false)

  const handleTest = useCallback(
    async (type: "imap" | "smtp") => {
      setTesting(type)
      setShowImapDetail(false)
      setShowSmtpDetail(false)
      try {
        const result = await testEmailConnection({
          test_imap: type === "imap",
          test_smtp: type === "smtp",
        })
        setImapResult(result.imap ?? null)
        setSmtpResult(result.smtp ?? null)
        if (type === "imap") setShowImapDetail(true)
        else setShowSmtpDetail(true)
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Unknown error"
        const failStep: TestStep = {
          name: "error",
          success: false,
          duration_ms: 0,
          error: msg,
        }
        if (type === "imap") {
          setImapResult({
            server: "",
            tls: false,
            steps: [failStep],
            total_ms: 0,
            success: false,
          })
          setShowImapDetail(true)
        } else {
          setSmtpResult({
            server: "",
            tls: false,
            starttls: false,
            steps: [failStep],
            total_ms: 0,
            success: false,
          })
          setShowSmtpDetail(true)
        }
      } finally {
        setTesting(null)
      }
    },
    [],
  )

  return (
    <div className="space-y-6">
      {/* Connection Test */}
      <Card className="shadow-sm">
        <CardContent className="px-6 py-5">
          <h3 className="mb-3 font-medium">{t("emailTest.title")}</h3>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleTest("imap")}
              disabled={testing !== null}
            >
              {testing === "imap" ? (
                <IconLoader2 className="mr-1.5 size-3.5 animate-spin" />
              ) : (
                <IconMail className="mr-1.5 size-3.5" />
              )}
              {t("emailTest.testImap")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleTest("smtp")}
              disabled={testing !== null}
            >
              {testing === "smtp" ? (
                <IconLoader2 className="mr-1.5 size-3.5 animate-spin" />
              ) : (
                <IconServer className="mr-1.5 size-3.5" />
              )}
              {t("emailTest.testSmtp")}
            </Button>
          </div>

          {imapResult && (
            <div className="border-border/60 mt-4 rounded-lg border">
              <button
                type="button"
                className="flex w-full items-center gap-2 px-4 py-2.5 text-sm"
                onClick={() => setShowImapDetail(!showImapDetail)}
              >
                {showImapDetail ? (
                  <IconChevronDown className="text-muted-foreground size-3.5" />
                ) : (
                  <IconChevronRight className="text-muted-foreground size-3.5" />
                )}
                <Badge
                  variant={imapResult.success ? "default" : "destructive"}
                >
                  {imapResult.success
                    ? t("emailTest.success")
                    : t("emailTest.failed")}
                </Badge>
                <span className="text-muted-foreground">
                  IMAP — {imapResult.server}
                </span>
                <span className="text-muted-foreground ml-auto text-xs">
                  {formatDuration(imapResult.total_ms)}
                </span>
              </button>
              {showImapDetail && (
                <div className="border-border/60 border-t px-4 pb-3 pt-2">
                  {imapResult.unread_count !== undefined && (
                    <p className="text-muted-foreground mb-2 text-xs">
                      {t("emailTest.unreadCount")}: {imapResult.unread_count}
                    </p>
                  )}
                  <div className="space-y-1.5">
                    {imapResult.steps.map((step, i) => (
                      <div key={i} className="flex items-start gap-2 text-xs">
                        {stepIcon(step)}
                        <span className="font-medium">{step.name}</span>
                        <span className="text-muted-foreground ml-auto">
                          {formatDuration(step.duration_ms)}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {smtpResult && (
            <div className="border-border/60 mt-4 rounded-lg border">
              <button
                type="button"
                className="flex w-full items-center gap-2 px-4 py-2.5 text-sm"
                onClick={() => setShowSmtpDetail(!showSmtpDetail)}
              >
                {showSmtpDetail ? (
                  <IconChevronDown className="text-muted-foreground size-3.5" />
                ) : (
                  <IconChevronRight className="text-muted-foreground size-3.5" />
                )}
                <Badge
                  variant={smtpResult.success ? "default" : "destructive"}
                >
                  {smtpResult.success
                    ? t("emailTest.success")
                    : t("emailTest.failed")}
                </Badge>
                <span className="text-muted-foreground">
                  SMTP — {smtpResult.server}
                </span>
                <span className="text-muted-foreground ml-auto text-xs">
                  {formatDuration(smtpResult.total_ms)}
                </span>
              </button>
              {showSmtpDetail && (
                <div className="border-border/60 border-t px-4 pb-3 pt-2">
                  <div className="space-y-1.5">
                    {smtpResult.steps.map((step, i) => (
                      <div key={i} className="flex items-start gap-2 text-xs">
                        {stepIcon(step)}
                        <span className="font-medium">{step.name}</span>
                        <span className="text-muted-foreground ml-auto">
                          {formatDuration(step.duration_ms)}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <Card className="shadow-sm">
        <CardContent className="divide-border/60 divide-y px-6 py-0 [&>div]:py-5">
          <Field
            label={t("channels.field.from")}
            hint={t("channels.form.desc.from")}
          >
            <Input
              value={asString(config.from)}
              onChange={(e) => onChange("from", e.target.value)}
              placeholder="bot@example.com"
            />
          </Field>

          <Field
            label={t("channels.field.smtpServer")}
            hint={t("channels.form.desc.smtpServer")}
            error={fieldErrors.smtp_server}
          >
            <Input
              value={asString(config.smtp_server)}
              onChange={(e) => onChange("smtp_server", e.target.value)}
              placeholder="smtp.example.com"
            />
          </Field>

          <Field
            label={t("channels.field.smtpPort")}
            hint={t("channels.form.desc.smtpPort")}
          >
            <Input
              type="number"
              min={1}
              value={asNumberString(config.smtp_port)}
              onChange={(e) =>
                onChange(
                  "smtp_port",
                  e.target.value === "" ? 0 : Number(e.target.value),
                )
              }
              placeholder="587"
            />
          </Field>

          <Field
            label={t("channels.field.smtpUser")}
            hint={t("channels.form.desc.smtpUser")}
          >
            <Input
              value={asString(config.smtp_user)}
              onChange={(e) => onChange("smtp_user", e.target.value)}
              placeholder="bot@example.com"
            />
          </Field>

          <Field
            label={t("channels.field.smtpPassword")}
            hint={t("channels.form.desc.smtpPassword")}
            error={fieldErrors.smtp_password}
          >
            <KeyInput
              value={asString(config._smtp_password)}
              onChange={(v) => onChange("_smtp_password", v)}
              placeholder={getSecretInputPlaceholder(
                configuredSecrets,
                "smtp_password",
                t("channels.field.secretHintSet"),
                t("channels.field.secretPlaceholder"),
              )}
            />
          </Field>

          <SwitchCardField
            label={t("channels.field.smtpStarttls")}
            hint={t("channels.form.desc.smtpStarttls")}
            checked={asBool(config.smtp_starttls)}
            onCheckedChange={(checked) => onChange("smtp_starttls", checked)}
            ariaLabel={t("channels.field.smtpStarttls")}
          />

          <SwitchCardField
            label={t("channels.field.smtpTls")}
            hint={t("channels.form.desc.smtpTls")}
            checked={smtpTLS}
            onCheckedChange={(checked) => onChange("smtp_tls", checked)}
            ariaLabel={t("channels.field.smtpTls")}
          />
        </CardContent>
      </Card>

      <Card className="shadow-sm">
        <CardContent className="divide-border/60 divide-y px-6 py-0 [&>div]:py-5">
          <Field
            label={t("channels.field.imapServer")}
            hint={t("channels.form.desc.imapServer")}
            error={fieldErrors.imap_server}
          >
            <Input
              value={asString(config.imap_server)}
              onChange={(e) => onChange("imap_server", e.target.value)}
              placeholder="imap.example.com"
            />
          </Field>

          <Field
            label={t("channels.field.imapPort")}
            hint={t("channels.form.desc.imapPort")}
          >
            <Input
              type="number"
              min={1}
              value={asNumberString(config.imap_port)}
              onChange={(e) =>
                onChange(
                  "imap_port",
                  e.target.value === "" ? 0 : Number(e.target.value),
                )
              }
              placeholder="993"
            />
          </Field>

          <Field
            label={t("channels.field.imapUser")}
            hint={t("channels.form.desc.imapUser")}
          >
            <Input
              value={asString(config.imap_user)}
              onChange={(e) => onChange("imap_user", e.target.value)}
              placeholder="bot@example.com"
            />
          </Field>

          <Field
            label={t("channels.field.imapPassword")}
            hint={t("channels.form.desc.imapPassword")}
            error={fieldErrors.imap_password}
          >
            <KeyInput
              value={asString(config._imap_password)}
              onChange={(v) => onChange("_imap_password", v)}
              placeholder={getSecretInputPlaceholder(
                configuredSecrets,
                "imap_password",
                t("channels.field.secretHintSet"),
                t("channels.field.secretPlaceholder"),
              )}
            />
          </Field>

          <SwitchCardField
            label={t("channels.field.imapTls")}
            hint={t("channels.form.desc.imapTls")}
            checked={imapTLS}
            onCheckedChange={(checked) => onChange("imap_tls", checked)}
            ariaLabel={t("channels.field.imapTls")}
          />

          <Field
            label={t("channels.field.mailbox")}
            hint={t("channels.form.desc.mailbox")}
          >
            <Input
              value={asString(config.mailbox)}
              onChange={(e) => onChange("mailbox", e.target.value)}
              placeholder="INBOX"
            />
          </Field>

          <Field
            label={t("channels.field.pollIntervalSecs")}
            hint={t("channels.form.desc.pollIntervalSecs")}
          >
            <Input
              type="number"
              min={1}
              value={asNumberString(config.poll_interval_secs)}
              onChange={(e) =>
                onChange(
                  "poll_interval_secs",
                  e.target.value === "" ? 0 : Number(e.target.value),
                )
              }
              placeholder="30"
            />
          </Field>

          <Field
            label={t("channels.field.maxAttachmentSizeBytes")}
            hint={t("channels.form.desc.maxAttachmentSizeBytes")}
          >
            <Input
              type="number"
              min={0}
              value={asNumberString(config.max_attachment_size_bytes)}
              onChange={(e) =>
                onChange(
                  "max_attachment_size_bytes",
                  e.target.value === "" ? 0 : Number(e.target.value),
                )
              }
              placeholder="10485760"
            />
          </Field>
        </CardContent>
      </Card>
    </div>
  )
}
