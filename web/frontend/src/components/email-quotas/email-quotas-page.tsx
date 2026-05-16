import {
  IconAlertTriangle,
  IconCircleCheck,
  IconInfinity,
  IconLoader2,
  IconMail,
  IconPencil,
  IconPlus,
  IconSettings,
  IconTrash,
  IconUsers,
} from "@tabler/icons-react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import { getChannelConfig, patchAppConfig } from "@/api/channels"
import {
  type QuotaEntry,
  createEmailQuota,
  deleteEmailQuota,
  getEmailQuotas,
  updateEmailQuota,
} from "@/api/email-quotas"
import { PageHeader } from "@/components/page-header"
import { Field, SwitchCardField } from "@/components/shared-form"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"

interface EmailQuotaSettings {
  enabled: boolean
  quotaFile: string
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback
}

async function getEmailQuotaSettings(): Promise<EmailQuotaSettings> {
  const response = await getChannelConfig("email")
  const config = response.config
  return {
    enabled: config.enable_quota === true,
    quotaFile: asString(config.quota_file),
  }
}

function isExpired(entry: QuotaEntry, today: string): boolean {
  return entry.expiresAt !== "" && entry.expiresAt < today
}

function isUsedUp(entry: QuotaEntry): boolean {
  return entry.remaining === 0 || entry.remaining < -1
}

function isUnlimited(entry: QuotaEntry): boolean {
  return entry.remaining === -1
}

function currentYearEnd(): string {
  return `${new Date().getFullYear()}-12-31`
}

export function EmailQuotasPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const {
    data: settings,
    error: settingsLoadError,
    isLoading: settingsLoading,
  } = useQuery({
    queryKey: ["email-quota-settings"],
    queryFn: getEmailQuotaSettings,
  })

  const {
    data: quotas = [],
    error: quotasLoadError,
    isLoading: quotasLoading,
  } = useQuery({
    queryKey: ["email-quotas"],
    queryFn: getEmailQuotas,
  })

  const [quotaEnabled, setQuotaEnabled] = useState(false)
  const [quotaFile, setQuotaFile] = useState("")
  const [settingsError, setSettingsError] = useState("")

  useEffect(() => {
    if (!settings) return
    setQuotaEnabled(settings.enabled)
    setQuotaFile(settings.quotaFile)
    setSettingsError("")
  }, [settings])

  const settingsDirty =
    !!settings &&
    (quotaEnabled !== settings.enabled || quotaFile !== settings.quotaFile)

  const invalidateQuotaData = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["email-quota-settings"] }),
      queryClient.invalidateQueries({ queryKey: ["email-quotas"] }),
    ])
  }, [queryClient])

  const settingsMutation = useMutation({
    mutationFn: async (nextSettings: EmailQuotaSettings) => {
      await patchAppConfig({
        channel_list: {
          email: {
            settings: {
              enable_quota: nextSettings.enabled,
              quota_file: nextSettings.quotaFile,
            },
          },
        },
      })
    },
    onSuccess: () => {
      void invalidateQuotaData()
    },
  })

  const createMutation = useMutation({
    mutationFn: createEmailQuota,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["email-quotas"] })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      email,
      patch,
    }: {
      email: string
      patch: Partial<Omit<QuotaEntry, "email">>
    }) => updateEmailQuota(email, patch),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["email-quotas"] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteEmailQuota,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["email-quotas"] })
    },
  })

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<QuotaEntry | null>(null)
  const [formEmail, setFormEmail] = useState("")
  const [formName, setFormName] = useState("")
  const [formRemaining, setFormRemaining] = useState("-1")
  const [formExpiresAt, setFormExpiresAt] = useState("")
  const [dialogError, setDialogError] = useState("")

  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState("")

  const today = new Date().toISOString().split("T")[0]
  const sortedQuotas = useMemo(
    () => [...quotas].sort((a, b) => a.email.localeCompare(b.email)),
    [quotas],
  )

  const quotaStats = useMemo(() => {
    let active = 0
    let unlimited = 0
    let attention = 0

    for (const entry of quotas) {
      if (isExpired(entry, today) || isUsedUp(entry)) {
        attention += 1
        continue
      }
      active += 1
      if (isUnlimited(entry)) {
        unlimited += 1
      }
    }

    return {
      active,
      attention,
      total: quotas.length,
      unlimited,
    }
  }, [quotas, today])

  const saveSettings = useCallback(async () => {
    const trimmedQuotaFile = quotaFile.trim()
    if (quotaEnabled && trimmedQuotaFile === "") {
      throw new Error(t("emailQuotas.validation.quotaFileRequired"))
    }

    await settingsMutation.mutateAsync({
      enabled: quotaEnabled,
      quotaFile: trimmedQuotaFile,
    })
    setQuotaFile(trimmedQuotaFile)
  }, [quotaEnabled, quotaFile, settingsMutation, t])

  const handleSaveSettings = useCallback(async () => {
    setSettingsError("")
    try {
      await saveSettings()
    } catch (error) {
      setSettingsError(errorMessage(error, t("emailQuotas.saveError")))
    }
  }, [saveSettings, t])

  const ensureQuotaFileReady = useCallback(async () => {
    if (quotaFile.trim() === "") {
      throw new Error(t("emailQuotas.validation.quotaFileRequired"))
    }
    if (settingsDirty) {
      await saveSettings()
    }
  }, [quotaFile, saveSettings, settingsDirty, t])

  const openCreate = useCallback(() => {
    setEditing(null)
    setFormEmail("")
    setFormName("")
    setFormRemaining("-1")
    setFormExpiresAt(currentYearEnd())
    setDialogError("")
    setDialogOpen(true)
  }, [])

  const openEdit = useCallback((entry: QuotaEntry) => {
    setEditing(entry)
    setFormEmail(entry.email)
    setFormName(entry.name)
    setFormRemaining(String(entry.remaining))
    setFormExpiresAt(entry.expiresAt)
    setDialogError("")
    setDialogOpen(true)
  }, [])

  const handleSaveQuota = useCallback(async () => {
    setDialogError("")
    const remaining = Number.parseInt(formRemaining, 10)
    if (Number.isNaN(remaining)) {
      setDialogError(t("emailQuotas.validation.remainingRequired"))
      return
    }

    const entry = {
      email: formEmail.toLowerCase().trim(),
      name: formName.trim(),
      remaining,
      expiresAt: formExpiresAt,
    }
    if (entry.email === "") {
      setDialogError(t("emailQuotas.validation.emailRequired"))
      return
    }

    try {
      await ensureQuotaFileReady()
      if (editing) {
        await updateMutation.mutateAsync({
          email: editing.email,
          patch: {
            expiresAt: entry.expiresAt,
            name: entry.name,
            remaining: entry.remaining,
          },
        })
      } else {
        await createMutation.mutateAsync(entry)
      }
      setDialogOpen(false)
    } catch (error) {
      setDialogError(errorMessage(error, t("emailQuotas.saveError")))
    }
  }, [
    createMutation,
    editing,
    ensureQuotaFileReady,
    formEmail,
    formExpiresAt,
    formName,
    formRemaining,
    t,
    updateMutation,
  ])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleteError("")
    try {
      await ensureQuotaFileReady()
      await deleteMutation.mutateAsync(deleteTarget)
      setDeleteTarget(null)
    } catch (error) {
      setDeleteError(errorMessage(error, t("emailQuotas.deleteError")))
    }
  }, [deleteMutation, deleteTarget, ensureQuotaFileReady, t])

  const quotaMutationPending =
    createMutation.isPending ||
    updateMutation.isPending ||
    settingsMutation.isPending

  const loadError =
    errorMessage(settingsLoadError, "") || errorMessage(quotasLoadError, "")

  const stats = [
    {
      icon: IconUsers,
      label: t("emailQuotas.stats.total"),
      value: quotaStats.total,
    },
    {
      icon: IconCircleCheck,
      label: t("emailQuotas.stats.active"),
      value: quotaStats.active,
    },
    {
      icon: IconInfinity,
      label: t("emailQuotas.stats.unlimited"),
      value: quotaStats.unlimited,
    },
    {
      icon: IconAlertTriangle,
      label: t("emailQuotas.stats.attention"),
      value: quotaStats.attention,
    },
  ]

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={t("emailQuotas.title")} />

      <div className="flex min-h-0 flex-1 justify-center overflow-y-auto px-4 pb-8 sm:px-6">
        <div className="w-full max-w-6xl space-y-6 pt-5">
          <section className="border-border/60 bg-card text-card-foreground rounded-xl border shadow-sm">
            <div className="flex flex-col gap-5 p-5">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="flex items-start gap-3">
                  <div className="bg-primary/10 text-primary rounded-lg p-2">
                    <IconSettings className="size-5" />
                  </div>
                  <div>
                    <h2 className="text-base font-semibold">
                      {t("emailQuotas.settingsTitle")}
                    </h2>
                    <p className="text-muted-foreground mt-1 text-sm leading-relaxed">
                      {t("emailQuotas.settingsDescription")}
                    </p>
                  </div>
                </div>
                <Badge variant={quotaEnabled ? "default" : "secondary"}>
                  {quotaEnabled
                    ? t("emailQuotas.status.enabled")
                    : t("emailQuotas.status.disabled")}
                </Badge>
              </div>

              <div className="border-border/60 divide-border/60 divide-y rounded-lg border px-4">
                <SwitchCardField
                  label={t("emailQuotas.enableQuota")}
                  hint={t("emailQuotas.enableQuotaHint")}
                  checked={quotaEnabled}
                  onCheckedChange={(checked) => {
                    setQuotaEnabled(checked)
                    setSettingsError("")
                  }}
                  ariaLabel={t("emailQuotas.enableQuota")}
                  disabled={settingsLoading || settingsMutation.isPending}
                  layout="setting-row"
                  transparent
                />

                <Field
                  label={t("emailQuotas.quotaFile")}
                  hint={t("emailQuotas.quotaFileHint")}
                  layout="setting-row"
                >
                  <Input
                    value={quotaFile}
                    onChange={(event) => {
                      setQuotaFile(event.target.value)
                      setSettingsError("")
                    }}
                    placeholder="~/.picoclaw/email-quotas.json"
                    disabled={settingsLoading || settingsMutation.isPending}
                  />
                </Field>
              </div>

              {settingsDirty && (
                <p className="text-muted-foreground text-sm">
                  {t("emailQuotas.unsavedSettings")}
                </p>
              )}
              {(settingsError || loadError) && (
                <p className="text-destructive text-sm">
                  {settingsError || loadError}
                </p>
              )}
              <div className="flex justify-end">
                <Button
                  onClick={() => void handleSaveSettings()}
                  disabled={!settingsDirty || settingsMutation.isPending}
                >
                  {settingsMutation.isPending
                    ? t("common.saving")
                    : t("emailQuotas.saveSettings")}
                </Button>
              </div>
            </div>
          </section>

          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {stats.map((item) => {
              const Icon = item.icon
              return (
                <div
                  key={item.label}
                  className="border-border/60 bg-card rounded-xl border px-4 py-3 shadow-sm"
                >
                  <div className="flex items-center justify-between gap-3">
                    <p className="text-muted-foreground text-sm">
                      {item.label}
                    </p>
                    <Icon className="text-muted-foreground size-4" />
                  </div>
                  <p className="mt-2 text-2xl font-semibold">{item.value}</p>
                </div>
              )
            })}
          </div>

          <section className="border-border/60 bg-card text-card-foreground overflow-hidden rounded-xl border shadow-sm">
            <div className="border-border/60 flex flex-col gap-3 border-b p-5 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h2 className="text-base font-semibold">
                  {t("emailQuotas.listTitle")}
                </h2>
                <p className="text-muted-foreground mt-1 text-sm">
                  {quotaEnabled
                    ? t("emailQuotas.modeEnabled")
                    : t("emailQuotas.modeDisabled")}
                </p>
              </div>
              <Button onClick={openCreate}>
                <IconPlus className="mr-2 h-4 w-4" />
                {t("emailQuotas.addQuota")}
              </Button>
            </div>

            {quotasLoading ? (
              <div className="flex items-center justify-center py-16">
                <IconLoader2 className="text-muted-foreground size-6 animate-spin" />
              </div>
            ) : sortedQuotas.length === 0 ? (
              <div className="text-muted-foreground py-16 text-center">
                <IconMail className="mx-auto mb-3 h-8 w-8 opacity-40" />
                <p className="text-sm">{t("emailQuotas.empty")}</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[760px] text-sm">
                  <thead className="bg-muted/40 text-muted-foreground border-b">
                    <tr>
                      <th className="px-5 py-3 text-left font-medium">
                        {t("emailQuotas.fields.email")}
                      </th>
                      <th className="px-5 py-3 text-left font-medium">
                        {t("emailQuotas.fields.name")}
                      </th>
                      <th className="px-5 py-3 text-left font-medium">
                        {t("emailQuotas.fields.remaining")}
                      </th>
                      <th className="px-5 py-3 text-left font-medium">
                        {t("emailQuotas.fields.expiresAt")}
                      </th>
                      <th className="px-5 py-3 text-right font-medium">
                        {t("emailQuotas.fields.actions")}
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-border/60 divide-y">
                    {sortedQuotas.map((entry) => {
                      const expired = isExpired(entry, today)
                      const usedUp = isUsedUp(entry)
                      const unlimited = isUnlimited(entry)

                      return (
                        <tr key={entry.email} className="hover:bg-muted/30">
                          <td className="px-5 py-4">
                            <p className="font-mono text-xs">{entry.email}</p>
                          </td>
                          <td className="px-5 py-4">
                            {entry.name || t("emailQuotas.noName")}
                          </td>
                          <td className="px-5 py-4">
                            {unlimited ? (
                              <Badge variant="secondary">
                                {t("emailQuotas.status.unlimited")}
                              </Badge>
                            ) : usedUp ? (
                              <Badge variant="destructive">
                                {t("emailQuotas.status.usedUp")}
                              </Badge>
                            ) : (
                              <span className="font-medium">
                                {entry.remaining}
                              </span>
                            )}
                          </td>
                          <td className="px-5 py-4">
                            {entry.expiresAt === "" ? (
                              <Badge variant="outline">
                                {t("emailQuotas.status.permanent")}
                              </Badge>
                            ) : expired ? (
                              <Badge variant="destructive">
                                {t("emailQuotas.status.expired")}{" "}
                                {entry.expiresAt}
                              </Badge>
                            ) : (
                              entry.expiresAt
                            )}
                          </td>
                          <td className="px-5 py-4 text-right">
                            <div className="flex justify-end gap-2">
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => openEdit(entry)}
                              >
                                <IconPencil className="mr-2 h-4 w-4" />
                                {t("emailQuotas.editAction")}
                              </Button>
                              <Button
                                variant="destructive"
                                size="sm"
                                onClick={() => {
                                  setDeleteTarget(entry.email)
                                  setDeleteError("")
                                }}
                              >
                                <IconTrash className="mr-2 h-4 w-4" />
                                {t("emailQuotas.deleteAction")}
                              </Button>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>

        </div>
      </div>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editing ? t("emailQuotas.editQuota") : t("emailQuotas.addQuota")}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <Field label={t("emailQuotas.fields.email")}>
              <Input
                type="email"
                value={formEmail}
                onChange={(event) => setFormEmail(event.target.value)}
                placeholder="user@example.com"
                disabled={!!editing || quotaMutationPending}
              />
            </Field>
            <Field label={t("emailQuotas.fields.name")}>
              <Input
                value={formName}
                onChange={(event) => setFormName(event.target.value)}
                placeholder={t("emailQuotas.fields.name")}
                disabled={quotaMutationPending}
              />
            </Field>
            <Field
              label={t("emailQuotas.fields.remaining")}
              hint={t("emailQuotas.remainingHint")}
            >
              <Input
                type="number"
                value={formRemaining}
                onChange={(event) => setFormRemaining(event.target.value)}
                placeholder="-1"
                disabled={quotaMutationPending}
              />
            </Field>
            <Field label={t("emailQuotas.fields.expiresAt")}>
              <Input
                type="date"
                value={formExpiresAt}
                onChange={(event) => setFormExpiresAt(event.target.value)}
                disabled={quotaMutationPending}
              />
            </Field>
            {dialogError && (
              <p className="text-destructive text-sm">{dialogError}</p>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDialogOpen(false)}
              disabled={quotaMutationPending}
            >
              {t("common.cancel")}
            </Button>
            <Button
              onClick={() => void handleSaveQuota()}
              disabled={quotaMutationPending}
            >
              {quotaMutationPending ? t("common.saving") : t("common.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null)
            setDeleteError("")
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("emailQuotas.deleteConfirm")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget}
              {deleteError && (
                <span className="text-destructive mt-2 block">
                  {deleteError}
                </span>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t("common.cancel")}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault()
                void handleDelete()
              }}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending
                ? t("common.saving")
                : t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
