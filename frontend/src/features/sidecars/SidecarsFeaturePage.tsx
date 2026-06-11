import { PageHeader } from "@/components/PageHeader"
import { useLocale } from "@/i18n/useLocale"
import { SidecarsScaffold } from "./SidecarsScaffold"

export default function SidecarsFeaturePage() {
  const { messages } = useLocale()
  const copy = messages.sidecarsPage

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={messages.nav.sidecars} description={copy.description} />
      <SidecarsScaffold />
    </div>
  )
}
