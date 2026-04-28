import { launcherFetch } from "./http"

const BASE_URL = ""

export interface QuotaEntry {
  email: string
  name: string
  remaining: number
  expiresAt: string
}

interface QuotaEntryWire {
  email: string
  name: string
  remaining: number
  expires_at: string
}

interface QuotaListResponse {
  quotas: QuotaEntryWire[]
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

export async function getEmailQuotas(): Promise<QuotaEntry[]> {
  const resp = await request<QuotaListResponse>("/api/channels/email/quotas")
  return resp.quotas.map((entry) => ({
    email: entry.email,
    name: entry.name,
    remaining: entry.remaining,
    expiresAt: entry.expires_at,
  }))
}

export async function createEmailQuota(entry: QuotaEntry): Promise<void> {
  await request("/api/channels/email/quotas", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      email: entry.email,
      name: entry.name,
      remaining: entry.remaining,
      expires_at: entry.expiresAt,
    }),
  })
}

export async function updateEmailQuota(
  email: string,
  patch: Partial<Omit<QuotaEntry, "email">>,
): Promise<void> {
  const payload: Partial<Omit<QuotaEntryWire, "email">> = {}
  if ("name" in patch) {
    payload.name = patch.name
  }
  if ("remaining" in patch) {
    payload.remaining = patch.remaining
  }
  if ("expiresAt" in patch) {
    payload.expires_at = patch.expiresAt
  }

  await request(`/api/channels/email/quotas/${encodeURIComponent(email)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  })
}

export async function deleteEmailQuota(email: string): Promise<void> {
  await request(`/api/channels/email/quotas/${encodeURIComponent(email)}`, {
    method: "DELETE",
  })
}
