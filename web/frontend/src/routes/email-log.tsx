import { createFileRoute } from "@tanstack/react-router"
import { EmailLogPage } from "@/components/email-log/email-log-page"

export const Route = createFileRoute("/email-log")({
  component: EmailLogPage,
})
