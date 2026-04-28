import { launcherFetch } from "./http"

const BASE_URL = ""

export type EmailLogDirection = "in" | "out"

export interface EmailLogAttachment {
  filename: string
  contentType: string
  sizeBytes: number
  path?: string
}

export interface EmailLogEntry {
  id: string
  timestamp: string
  direction: EmailLogDirection
  fromEmail: string
  fromName: string
  toEmail: string
  toName: string
  subject: string
  messageId: string
  bodyText: string
  attachments: EmailLogAttachment[]
  status?: string
}

export interface EmailLogListResponse {
  entries: EmailLogEntry[]
  total: number
  page: number
}

interface EmailLogEntryWire {
  id: string
  timestamp: string
  direction: EmailLogDirection
  from_email: string
  from_name: string
  to_email: string
  to_name: string
  subject: string
  message_id: string
  body_text: string
  attachments?: Array<{
    filename: string
    content_type: string
    size_bytes: number
    path?: string
  }>
  status?: string
}

interface EmailLogListResponseWire {
  entries: EmailLogEntryWire[]
  total: number
  page: number
}

function mapEmailLogEntry(w: EmailLogEntryWire): EmailLogEntry {
  return {
    id: w.id,
    timestamp: w.timestamp,
    direction: w.direction,
    fromEmail: w.from_email,
    fromName: w.from_name,
    toEmail: w.to_email,
    toName: w.to_name,
    subject: w.subject,
    messageId: w.message_id,
    bodyText: w.body_text,
    attachments: (w.attachments ?? []).map((a) => ({
      filename: a.filename,
      contentType: a.content_type,
      sizeBytes: a.size_bytes,
      path: a.path,
    })),
    status: w.status,
  }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await launcherFetch(`${BASE_URL}${path}`, options)
  if (!res.ok) {
    const ct = res.headers.get("content-type") || ""
    let message = `Request failed: ${res.status} ${res.statusText}`
    if (ct.includes("application/json")) {
      try {
        const body = await res.json()
        message = body.error || body.message || message
      } catch {
        // ignore
      }
    }
    throw new Error(message)
  }
  return res.json() as Promise<T>
}

export async function getEmailLog(params: {
  direction?: string
  search?: string
  page?: number
  pageSize?: number
}): Promise<EmailLogListResponse> {
  const qs = new URLSearchParams()
  if (params.direction) qs.set("direction", params.direction)
  if (params.search) qs.set("search", params.search)
  if (params.page) qs.set("page", String(params.page))
  if (params.pageSize) qs.set("page_size", String(params.pageSize))

  const query = qs.toString()
  const path = `/api/channels/email/log${query ? `?${query}` : ""}`
  const resp = await request<EmailLogListResponseWire>(path)
  return {
    entries: resp.entries.map(mapEmailLogEntry),
    total: resp.total,
    page: resp.page,
  }
}

export async function getEmailLogEntry(id: string): Promise<EmailLogEntry> {
  const resp = await request<EmailLogEntryWire>(
    `/api/channels/email/log/${encodeURIComponent(id)}`,
  )
  return mapEmailLogEntry(resp)
}

export async function deleteEmailLogEntry(id: string): Promise<void> {
  await request(`/api/channels/email/log/${encodeURIComponent(id)}`, {
    method: "DELETE",
  })
}
