import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function ProxyApiKeysPageSkeleton() {
  return (
    <div className="flex flex-col gap-6">
      <Card className="overflow-hidden border-primary/20 bg-primary/5">
        <CardHeader className="border-b bg-background/70">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-full max-w-md" />
        </CardHeader>
        <CardContent>
          <Skeleton className="h-14 rounded-lg" />
        </CardContent>
      </Card>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <Card className="overflow-hidden">
          <CardHeader className="border-b bg-muted/20">
            <Skeleton className="h-5 w-36" />
            <Skeleton className="h-4 w-full max-w-lg" />
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            <div className="grid gap-4 md:grid-cols-2">
              <Skeleton className="h-20 rounded-lg" />
              <Skeleton className="h-20 rounded-lg" />
            </div>
            <Skeleton className="h-20 rounded-lg" />
            <Skeleton className="h-9 w-32 rounded-md" />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <Skeleton className="h-5 w-44" />
            <Skeleton className="h-4 w-full" />
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Skeleton className="h-24 rounded-lg" />
            <Skeleton className="h-24 rounded-lg" />
          </CardContent>
        </Card>
      </div>

      <Card className="overflow-hidden">
        <CardHeader className="border-b bg-muted/20">
          <Skeleton className="h-5 w-32" />
          <Skeleton className="h-4 w-full max-w-md" />
        </CardHeader>
        <CardContent>
          <Skeleton className="h-72 rounded-lg" />
        </CardContent>
      </Card>
    </div>
  );
}
