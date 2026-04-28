import {
  IconChevronLeft,
  IconChevronRight,
  IconFileText,
  IconInbox,
  IconLoader2,
  IconPaperclip,
  IconRefresh,
  IconSearch,
  IconSend,
  IconTrash,
} from "@tabler/icons-react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  deleteEmailLogEntry,
  getEmailLog,
  getEmailLogEntry,
  type EmailLogEntry,
} from "@/api/email-log"
import { PageHeader } from "@/components/page-header"
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
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

const PAGE_SIZE = 30

function formatTimestamp(value: string): string {
  if (!value) return ""
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function senderLabel(entry: EmailLogEntry): string {
  return entry.fromName || entry.fromEmail
}

function recipientLabel(entry: EmailLogEntry): string {
  return entry.toName || entry.toEmail
}

export function EmailLogPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [direction, setDirection] = useState<string>("all")
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const {
    data: logData,
    error: logError,
    isFetching: logFetching,
    isLoading: logLoading,
  } = useQuery({
    queryKey: ["email-log", direction, search, page],
    queryFn: () =>
      getEmailLog({
        direction: direction === "all" ? undefined : direction,
        search: search || undefined,
        page,
        pageSize: PAGE_SIZE,
      }),
  })

  const selectedEntryQuery = useQuery({
    queryKey: ["email-log-detail", selectedId],
    queryFn: () => getEmailLogEntry(selectedId!),
    enabled: selectedId !== null,
  })

  useEffect(() => {
    if (logData?.entries.length === 0) {
      if (selectedId !== null) {
        setSelectedId(null)
      }
      return
    }

    if (
      selectedId !== null &&
      !logData?.entries.some((e) => e.id === selectedId)
    ) {
      setSelectedId(null)
    }
  }, [logData, selectedId])

  const deleteMutation = useMutation({
    mutationFn: deleteEmailLogEntry,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["email-log"] })
      if (selectedId === deleteTarget) {
        setSelectedId(null)
      }
      setDeleteTarget(null)
    },
  })

  const handleRefresh = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["email-log"] })
    if (selectedId !== null) {
      void queryClient.invalidateQueries({
        queryKey: ["email-log-detail", selectedId],
      })
    }
  }, [queryClient, selectedId])

  const logEntries = logData?.entries ?? []
  const totalPages = Math.ceil((logData?.total ?? 0) / PAGE_SIZE)

  const inCount = logEntries.filter((e) => e.direction === "in").length
  const outCount = logEntries.filter((e) => e.direction === "out").length

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={t("emailLog.title")} />

      <div className="flex min-h-0 flex-1 justify-center overflow-y-auto px-4 pb-8 sm:px-6">
        <div className="w-full max-w-6xl space-y-6 pt-5">
          {/* Stats cards */}
          <div className="grid gap-3 sm:grid-cols-3">
            {[
              {
                icon: IconFileText,
                label: t("emailLog.totalEntries"),
                value: logData?.total ?? 0,
              },
              {
                icon: IconInbox,
                label: t("emailLog.inboundCount"),
                value: inCount,
              },
              {
                icon: IconSend,
                label: t("emailLog.outboundCount"),
                value: outCount,
              },
            ].map((item) => {
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

          {/* Filter bar */}
          <section className="border-border/60 bg-card text-card-foreground rounded-xl border shadow-sm">
            <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
              <Select value={direction} onValueChange={setDirection}>
                <SelectTrigger className="w-[140px]">
                  <SelectValue placeholder={t("emailLog.filterDirection")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">
                    {t("emailLog.filterAll")}
                  </SelectItem>
                  <SelectItem value="in">
                    {t("emailLog.filterInbound")}
                  </SelectItem>
                  <SelectItem value="out">
                    {t("emailLog.filterOutbound")}
                  </SelectItem>
                </SelectContent>
              </Select>

              <div className="relative flex-1">
                <IconSearch className="text-muted-foreground absolute left-3 top-1/2 size-4 -translate-y-1/2" />
                <Input
                  value={search}
                  onChange={(e) => {
                    setSearch(e.target.value)
                    setPage(1)
                  }}
                  placeholder={t("emailLog.searchPlaceholder")}
                  className="pl-9"
                />
              </div>

              <Button
                variant="outline"
                onClick={handleRefresh}
                disabled={logFetching}
              >
                <IconRefresh
                  className={`mr-2 h-4 w-4 ${logFetching ? "animate-spin" : ""}`}
                />
                {t("emailLog.refresh")}
              </Button>
            </div>

            {/* Log entries list + detail */}
            <div className="grid min-h-[30rem] lg:grid-cols-[22rem_minmax(0,1fr)]">
              {/* Left: list */}
              <div className="border-border/60 border-b lg:border-r lg:border-b-0">
                {logLoading ? (
                  <div className="flex items-center justify-center py-16">
                    <IconLoader2 className="text-muted-foreground size-6 animate-spin" />
                  </div>
                ) : logError ? (
                  <div className="text-destructive px-5 py-8 text-sm">
                    {logError.message}
                  </div>
                ) : logEntries.length === 0 ? (
                  <div className="text-muted-foreground px-5 py-16 text-center">
                    <IconFileText className="mx-auto mb-3 h-8 w-8 opacity-40" />
                    <p className="text-sm">{t("emailLog.empty")}</p>
                  </div>
                ) : (
                  <div className="max-h-[34rem] overflow-y-auto p-2">
                    {logEntries.map((entry) => {
                      const selected = entry.id === selectedId
                      return (
                        <button
                          key={entry.id}
                          type="button"
                          onClick={() => setSelectedId(entry.id)}
                          className={`w-full rounded-lg px-3 py-3 text-left transition ${
                            selected
                              ? "bg-primary/10 text-primary"
                              : "hover:bg-muted/60"
                          }`}
                        >
                          <div className="flex min-w-0 items-start gap-2">
                            <Badge
                              variant={
                                entry.direction === "in"
                                  ? "default"
                                  : "secondary"
                              }
                              className="shrink-0 text-[10px]"
                            >
                              {entry.direction === "in"
                                ? t("emailLog.inboundBadge")
                                : t("emailLog.outboundBadge")}
                            </Badge>
                            <div className="min-w-0 flex-1">
                              <p className="min-w-0 truncate text-sm font-medium">
                                {entry.subject || t("emailLog.noSubject")}
                              </p>
                              <p className="text-muted-foreground mt-0.5 truncate text-xs">
                                {senderLabel(entry)} → {recipientLabel(entry)}
                              </p>
                              <div className="text-muted-foreground mt-1 flex items-center gap-2 text-xs">
                                <span>
                                  {formatTimestamp(entry.timestamp)}
                                </span>
                                {entry.attachments.length > 0 && (
                                  <span className="flex items-center gap-0.5">
                                    <IconPaperclip className="size-3" />
                                    {entry.attachments.length}
                                  </span>
                                )}
                              </div>
                            </div>
                          </div>
                        </button>
                      )
                    })}
                  </div>
                )}

                {/* Pagination */}
                {totalPages > 1 && (
                  <div className="border-border/60 flex items-center justify-between border-t px-4 py-2 text-xs">
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={page <= 1}
                      onClick={() => setPage((p) => Math.max(1, p - 1))}
                    >
                      <IconChevronLeft className="mr-1 h-3 w-3" />
                      {t("emailLog.prevPage")}
                    </Button>
                    <span className="text-muted-foreground">
                      {t("emailLog.page")} {page} {t("emailLog.of")}{" "}
                      {totalPages}
                    </span>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={page >= totalPages}
                      onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                    >
                      {t("emailLog.nextPage")}
                      <IconChevronRight className="ml-1 h-3 w-3" />
                    </Button>
                  </div>
                )}
              </div>

              {/* Right: detail */}
              <div className="min-w-0 p-5">
                {selectedId === null ? (
                  <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
                    {t("emailLog.selectMessage")}
                  </div>
                ) : selectedEntryQuery.isLoading ? (
                  <div className="flex h-full items-center justify-center">
                    <IconLoader2 className="text-muted-foreground size-6 animate-spin" />
                  </div>
                ) : selectedEntryQuery.error ? (
                  <div className="text-destructive text-sm">
                    {selectedEntryQuery.error.message}
                  </div>
                ) : selectedEntryQuery.data ? (
                  <div className="space-y-5">
                    <div className="flex items-start justify-between">
                      <div>
                        <div className="mb-3 flex flex-wrap items-center gap-2">
                          <Badge
                            variant={
                              selectedEntryQuery.data.direction === "in"
                                ? "default"
                                : "secondary"
                            }
                          >
                            {selectedEntryQuery.data.direction === "in"
                              ? t("emailLog.inboundBadge")
                              : t("emailLog.outboundBadge")}
                          </Badge>
                          <span className="text-muted-foreground text-xs">
                            {formatTimestamp(selectedEntryQuery.data.timestamp)}
                          </span>
                        </div>
                        <h3 className="text-lg font-semibold break-words">
                          {selectedEntryQuery.data.subject ||
                            t("emailLog.noSubject")}
                        </h3>
                      </div>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setDeleteTarget(selectedEntryQuery.data!.id)}
                        className="text-destructive shrink-0"
                      >
                        <IconTrash className="h-4 w-4" />
                      </Button>
                    </div>

                    <dl className="grid gap-3 text-sm sm:grid-cols-[7rem_minmax(0,1fr)]">
                      <dt className="text-muted-foreground">
                        {t("emailLog.messageFrom")}
                      </dt>
                      <dd className="min-w-0 break-words">
                        {senderLabel(selectedEntryQuery.data) ||
                          t("emailLog.unknownSender")}
                      </dd>
                      <dt className="text-muted-foreground">
                        {t("emailLog.messageTo")}
                      </dt>
                      <dd className="min-w-0 break-words">
                        {recipientLabel(selectedEntryQuery.data) ||
                          t("emailLog.unknownRecipient")}
                      </dd>
                      {selectedEntryQuery.data.messageId && (
                        <>
                          <dt className="text-muted-foreground">
                            {t("emailLog.messageId")}
                          </dt>
                          <dd className="min-w-0 font-mono text-xs break-all">
                            {selectedEntryQuery.data.messageId}
                          </dd>
                        </>
                      )}
                      {selectedEntryQuery.data.status && (
                        <>
                          <dt className="text-muted-foreground">
                            {t("emailLog.status")}
                          </dt>
                          <dd className="min-w-0 break-words">
                            <Badge
                              variant={
                                selectedEntryQuery.data.status === ""
                                  ? "default"
                                  : "destructive"
                              }
                            >
                              {selectedEntryQuery.data.status}
                            </Badge>
                          </dd>
                        </>
                      )}
                    </dl>

                    {selectedEntryQuery.data.bodyText && (
                      <div className="bg-muted/30 border-border/60 max-h-[28rem] overflow-y-auto rounded-lg border p-4">
                        <pre className="font-sans text-sm leading-relaxed break-words whitespace-pre-wrap">
                          {selectedEntryQuery.data.bodyText}
                        </pre>
                      </div>
                    )}

                    {selectedEntryQuery.data.attachments.length > 0 && (
                      <div>
                        <h4 className="mb-2 text-sm font-medium">
                          {t("emailLog.messageAttachments")}
                        </h4>
                        <div className="flex flex-col gap-2">
                          {selectedEntryQuery.data.attachments.map(
                            (att, index) => (
                              <div
                                key={`${att.filename}-${index}`}
                                className="border-border/60 bg-card rounded-lg border px-3 py-2"
                              >
                                <div className="flex items-center gap-2">
                                  <IconPaperclip className="text-muted-foreground shrink-0 size-3.5" />
                                  <span className="text-sm font-medium truncate">
                                    {att.filename}
                                  </span>
                                  <span className="text-muted-foreground text-xs">
                                    {att.contentType}
                                  </span>
                                </div>
                                {att.path && (
                                  <p className="text-muted-foreground mt-1 truncate font-mono text-xs">
                                    {att.path}
                                  </p>
                                )}
                              </div>
                            ),
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                ) : null}
              </div>
            </div>
          </section>
        </div>
      </div>

      {/* Delete confirmation */}
      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("emailLog.deleteConfirm")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t("common.cancel")}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                if (deleteTarget) {
                  void deleteMutation.mutate(deleteTarget)
                }
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
