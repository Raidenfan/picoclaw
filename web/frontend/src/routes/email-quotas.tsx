import { createFileRoute } from "@tanstack/react-router"
import { EmailQuotasPage } from "@/components/email-quotas/email-quotas-page"

export const Route = createFileRoute("/email-quotas")({
  component: EmailQuotasPage,
})
