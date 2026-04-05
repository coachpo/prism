import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";

export function UsageStatisticsPageSkeleton() {
  const { messages } = useLocale();

  return (
    <section
      aria-label={messages.statistics.usageStatisticsPagePlaceholder}
      aria-live="polite"
      role="status"
      className="space-y-6 rounded-xl border bg-card p-6 shadow-none"
    >
      <span className="sr-only">{messages.statistics.usageStatisticsPagePlaceholder}</span>

      <div className="space-y-2">
        <Skeleton className="h-5 w-40 rounded-md" />
        <Skeleton className="h-4 w-full max-w-2xl rounded-md" />
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {[1, 2, 3, 4].map((index) => (
          <Skeleton key={index} className="h-28 rounded-xl" />
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,0.65fr)]">
        <Skeleton className="h-72 rounded-xl" />
        <div className="grid gap-4">
          <Skeleton className="h-36 rounded-xl" />
          <Skeleton className="h-36 rounded-xl" />
        </div>
      </div>

      <Skeleton className="h-80 rounded-xl" />
    </section>
  );
}
