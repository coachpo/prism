import { Skeleton } from "@/components/ui/skeleton";

export function StatisticsPageSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-56" />
      <Skeleton className="h-10 w-64" />
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[1, 2, 3, 4].map((index) => (
          <Skeleton key={index} className="h-[100px] rounded-xl" />
        ))}
      </div>
      <Skeleton className="h-[300px] rounded-xl" />
    </div>
  );
}
