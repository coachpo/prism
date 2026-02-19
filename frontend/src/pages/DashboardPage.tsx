import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ModelConfigListItem } from "@/lib/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Server, Zap, Globe, ArrowRight } from "lucide-react";

export function DashboardPage() {
  const [models, setModels] = useState<ModelConfigListItem[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const modelsData = await api.models.list();
        setModels(modelsData);
      } catch (error) {
        console.error("Failed to fetch dashboard data:", error);
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, []);

  const totalModels = models.length;
  const activeEndpoints = models.reduce((sum, model) => sum + model.active_endpoint_count, 0);
  const activeProviders = new Set(models.map((m) => m.provider.name)).size;

  if (loading) {
    return <div className="p-8">Loading dashboard...</div>;
  }

  return (
    <div className="space-y-8">
      <h2 className="text-3xl font-bold tracking-tight">Dashboard</h2>

      <div className="grid gap-4 md:grid-cols-3">
        <Card className="transition-shadow hover:shadow-md">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Models</CardTitle>
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10">
              <Server className="h-4 w-4 text-primary" />
            </div>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalModels}</div>
            <p className="text-xs text-muted-foreground">Configured models</p>
          </CardContent>
        </Card>

        <Card className="transition-shadow hover:shadow-md">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Endpoints</CardTitle>
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-chart-2/15">
              <Zap className="h-4 w-4 text-chart-2" />
            </div>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{activeEndpoints}</div>
            <p className="text-xs text-muted-foreground">Across all models</p>
          </CardContent>
        </Card>

        <Card className="transition-shadow hover:shadow-md">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Providers</CardTitle>
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-chart-3/15">
              <Globe className="h-4 w-4 text-chart-3" />
            </div>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{activeProviders}</div>
            <p className="text-xs text-muted-foreground">Unique providers</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Model Overview</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Model</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead>Strategy</TableHead>
                <TableHead>Endpoints</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {models.map((model) => (
                <TableRow key={model.id}>
                  <TableCell className="font-medium">
                    {model.display_name || model.model_id}
                    {model.display_name && (
                      <div className="text-xs text-muted-foreground">{model.model_id}</div>
                    )}
                    {model.model_type === "redirect" && model.redirect_to && (
                      <div className="flex items-center gap-1 text-xs text-muted-foreground">
                        <ArrowRight className="h-3 w-3" /> {model.redirect_to}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <Badge variant={model.model_type === "native" ? "default" : "outline"} className={model.model_type === "native" ? "bg-primary/90" : ""}>
                      {model.model_type === "native" ? "Native" : "Redirect"}
                    </Badge>
                  </TableCell>
                  <TableCell>{model.provider.name}</TableCell>
                  <TableCell className="capitalize">{model.lb_strategy.replace("_", " ")}</TableCell>
                  <TableCell>
                    {model.active_endpoint_count} / {model.endpoint_count} active
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={model.is_enabled ? "default" : "secondary"}
                      className={model.is_enabled ? "bg-primary/90" : ""}
                    >
                      {model.is_enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
              {models.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-muted-foreground">
                    No models configured.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
