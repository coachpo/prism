import type { ComponentProps } from "react"
import { RequestLogAuditPage } from "@/pages/request-logs/RequestLogAuditPage"

export default function RequestLogAuditFeaturePage(props: ComponentProps<typeof RequestLogAuditPage>) {
  return (
    <section data-testid="request-log-audit-feature-page">
      <RequestLogAuditPage {...props} />
    </section>
  )
}
