import { Skeleton } from "@/components/ui/skeleton";

export function DashboardPageSkeleton() {
  return (
    <div className="flex flex-col gap-[var(--density-page-gap)]">
      <div className="grid gap-[var(--density-card-gap)] md:grid-cols-2 lg:grid-cols-4">
        {[1, 2, 3, 4].map((item) => (
          <Skeleton key={item} className="h-32 rounded-xl" />
        ))}
      </div>
      <div className="grid gap-[var(--density-card-gap)] md:grid-cols-2 lg:grid-cols-7">
        <Skeleton className="md:col-span-2 lg:col-span-4 h-[400px] rounded-xl" />
        <Skeleton className="md:col-span-2 lg:col-span-3 h-[400px] rounded-xl" />
      </div>
    </div>
  );
}
