import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function ProxyApiKeysPageSkeleton() {
  return (
    <div className="space-y-6">
      <Card className="border-slate-200/80 shadow-none dark:border-slate-800">
        <CardContent className="space-y-5 p-6">
          <Skeleton className="h-5 w-36" />
          <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
            <Skeleton className="h-20 rounded-xl" />
            <Skeleton className="h-20 rounded-xl" />
            <Skeleton className="h-10 rounded-xl md:self-end" />
          </div>
        </CardContent>
      </Card>

      <Skeleton className="h-20 rounded-xl" />

      <div className="grid gap-4">
        <Skeleton className="h-64 rounded-xl" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    </div>
  );
}
