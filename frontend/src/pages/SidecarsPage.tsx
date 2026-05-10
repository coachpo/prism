import { PageHeader } from "@/components/PageHeader";
import { useLocale } from "@/i18n/useLocale";
import { SidecarsScaffold } from "./sidecars/SidecarsScaffold";

export function SidecarsPage() {
  const { messages } = useLocale();
  const copy = messages.sidecarsPage;

  return (
    <div className="space-y-6">
      <PageHeader title={messages.nav.sidecars} description={copy.description} />
      <SidecarsScaffold />
    </div>
  );
}
