import { launcherFetch } from "./http"

export interface TestStep {
  name: string
  success: boolean
  duration_ms: number
  error?: string
  detail?: string
  unread_count?: number
}

export interface IMAPTestResult {
  server: string
  tls: boolean
  steps: TestStep[]
  unread_count?: number
  total_ms: number
  success: boolean
}

export interface SMTPTestResult {
  server: string
  tls: boolean
  starttls: boolean
  steps: TestStep[]
  total_ms: number
  success: boolean
}

export async function testEmailConnection(params: {
  test_imap?: boolean
  test_smtp?: boolean
}): Promise<{ imap?: IMAPTestResult; smtp?: SMTPTestResult }> {
  const res = await launcherFetch("/api/channels/email/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      test_imap: params.test_imap ?? true,
      test_smtp: params.test_smtp ?? true,
    }),
  })
  if (!res.ok) {
    const ct = res.headers.get("content-type") || ""
    let message = `Request failed: ${res.status} ${res.statusText}`
    if (ct.includes("application/json")) {
      try {
        const body = (await res.json()) as { error?: string }
        message = body.error || message
      } catch {
        // ignore
      }
    }
    throw new Error(message)
  }
  return res.json() as Promise<{ imap?: IMAPTestResult; smtp?: SMTPTestResult }>
}
