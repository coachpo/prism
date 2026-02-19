import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "@/lib/api";
import { formatProviderType } from "@/lib/utils";
import type { ModelConfigListItem, Provider, ModelConfigCreate, ModelConfigUpdate } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import { Plus, Pencil, Trash2, MoreHorizontal, Search, ArrowRight } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

export function ModelsPage() {
  const navigate = useNavigate();
  const [models, setModels] = useState<ModelConfigListItem[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [loading, setLoading] = useState(true);
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<ModelConfigListItem | null>(null);
  const [search, setSearch] = useState("");
  const [providerFilter, setProviderFilter] = useState<string>("all");
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [typeFilter, setTypeFilter] = useState<string>("all");

  // Form state
  const [formData, setFormData] = useState<ModelConfigCreate>({
    provider_id: 0,
    model_id: "",
    display_name: "",
    model_type: "native",
    redirect_to: null,
    lb_strategy: "single",
    is_enabled: true,
  });

  const fetchData = async () => {
    try {
      const [modelsData, providersData] = await Promise.all([
        api.models.list(),
        api.providers.list(),
      ]);
      setModels(modelsData);
      setProviders(providersData);
    } catch (error) {
      toast.error("Failed to fetch data");
      console.error(error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  const handleOpenDialog = (model?: ModelConfigListItem) => {
    if (model) {
      setEditingModel(model);
      setFormData({
        provider_id: model.provider_id,
        model_id: model.model_id,
        display_name: model.display_name || "",
        model_type: model.model_type,
        redirect_to: model.redirect_to,
        lb_strategy: model.lb_strategy,
        is_enabled: model.is_enabled,
      });
    } else {
      setEditingModel(null);
      setFormData({
        provider_id: providers.length > 0 ? providers[0].id : 0,
        model_id: "",
        display_name: "",
        model_type: "native",
        redirect_to: null,
        lb_strategy: "single",
        is_enabled: true,
      });
    }
    setIsDialogOpen(true);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      if (editingModel) {
        const updateData: ModelConfigUpdate = {
          provider_id: formData.provider_id,
          model_id: formData.model_id,
          display_name: formData.display_name,
          model_type: formData.model_type,
          redirect_to: formData.model_type === "redirect" ? formData.redirect_to : null,
          lb_strategy: formData.lb_strategy,
          is_enabled: formData.is_enabled,
        };
        await api.models.update(editingModel.id, updateData);
        toast.success("Model updated successfully");
      } else {
        const createData: ModelConfigCreate = {
          ...formData,
          redirect_to: formData.model_type === "redirect" ? formData.redirect_to : null,
        };
        await api.models.create(createData);
        toast.success("Model created successfully");
      }
      setIsDialogOpen(false);
      fetchData();
    } catch (error: any) {
      toast.error(error.message || "Operation failed");
    }
  };

  const handleDelete = async (id: number) => {
    if (!confirm("Are you sure you want to delete this model?")) return;
    try {
      await api.models.delete(id);
      toast.success("Model deleted successfully");
      fetchData();
    } catch (error: any) {
      toast.error(error.message || "Delete failed");
    }
  };

  if (loading) return <div className="p-8">Loading models...</div>;

  const uniqueProviders = [...new Set(models.map(m => m.provider.name))];
  const filteredModels = models.filter(m => {
    const matchesSearch = !search || m.model_id.toLowerCase().includes(search.toLowerCase()) || (m.display_name?.toLowerCase().includes(search.toLowerCase()));
    const matchesProvider = providerFilter === "all" || m.provider.name === providerFilter;
    const matchesStatus = statusFilter === "all" || (statusFilter === "enabled" ? m.is_enabled : !m.is_enabled);
    const matchesType = typeFilter === "all" || m.model_type === typeFilter;
    return matchesSearch && matchesProvider && matchesStatus && matchesType;
  });

  // Get native models for the currently selected provider (for redirect_to selector)
  const selectedProvider = providers.find(p => p.id === formData.provider_id);
  const nativeModelsForProvider = models.filter(
    m => m.model_type === "native" && m.provider_id === formData.provider_id && (!editingModel || m.model_id !== formData.model_id)
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-3xl font-bold tracking-tight">Models</h2>
        <Button onClick={() => handleOpenDialog()}>
          <Plus className="mr-2 h-4 w-4" /> Add Model
        </Button>
      </div>

      <div className="flex items-center gap-3">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input placeholder="Search models..." value={search} onChange={e => setSearch(e.target.value)} className="pl-9" />
        </div>
        <Select value={providerFilter} onValueChange={setProviderFilter}>
          <SelectTrigger className="w-[180px]"><SelectValue placeholder="All Providers" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Providers</SelectItem>
            {uniqueProviders.map(p => <SelectItem key={p} value={p}>{p}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={statusFilter} onValueChange={setStatusFilter}>
          <SelectTrigger className="w-[140px]"><SelectValue placeholder="All Status" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Status</SelectItem>
            <SelectItem value="enabled">Enabled</SelectItem>
            <SelectItem value="disabled">Disabled</SelectItem>
          </SelectContent>
        </Select>
        <Select value={typeFilter} onValueChange={setTypeFilter}>
          <SelectTrigger className="w-[140px]"><SelectValue placeholder="All Types" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Types</SelectItem>
            <SelectItem value="native">Native</SelectItem>
            <SelectItem value="redirect">Redirect</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Model</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead>Strategy</TableHead>
                <TableHead>Endpoints</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredModels.map((model) => (
                <TableRow 
                  key={model.id} 
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={(e) => {
                    if ((e.target as HTMLElement).closest("button")) return;
                    navigate(`/models/${model.id}`);
                  }}
                >
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
                    <Badge variant="outline">
                      {model.active_endpoint_count} / {model.endpoint_count}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={model.is_enabled ? "default" : "secondary"}
                      className={model.is_enabled ? "bg-primary/90" : ""}
                    >
                      {model.is_enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" className="h-8 w-8 p-0">
                          <span className="sr-only">Open menu</span>
                          <MoreHorizontal className="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
                        <DropdownMenuItem onClick={() => handleOpenDialog(model)}>
                          <Pencil className="mr-2 h-4 w-4" /> Edit
                        </DropdownMenuItem>
                        <DropdownMenuItem 
                          className="text-destructive focus:text-destructive"
                          onClick={() => handleDelete(model.id)}
                        >
                          <Trash2 className="mr-2 h-4 w-4" /> Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
              {filteredModels.length === 0 && (
                <TableRow>
                   <TableCell colSpan={7} className="text-center py-8 text-muted-foreground">
                    {models.length === 0 ? "No models found. Create one to get started." : "No models match your filters."}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={isDialogOpen} onOpenChange={setIsDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingModel ? "Edit Model" : "Add New Model"}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="grid gap-2">
              <Label htmlFor="provider">Provider</Label>
              <Select
                value={formData.provider_id.toString()}
                onValueChange={(val) => setFormData({ ...formData, provider_id: parseInt(val), redirect_to: null })}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select provider" />
                </SelectTrigger>
                <SelectContent>
                  {providers.map((p) => (
                    <SelectItem key={p.id} value={p.id.toString()}>
                      {p.name} ({formatProviderType(p.provider_type)})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-2">
              <Label htmlFor="model_id">Model ID</Label>
              <Input
                id="model_id"
                value={formData.model_id}
                onChange={(e) => setFormData({ ...formData, model_id: e.target.value })}
                placeholder="e.g. gpt-4-turbo"
                required
              />
            </div>

            <div className="grid gap-2">
              <Label htmlFor="display_name">Display Name</Label>
              <Input
                id="display_name"
                value={formData.display_name || ""}
                onChange={(e) => setFormData({ ...formData, display_name: e.target.value })}
                placeholder="Optional friendly name"
              />
            </div>

            <div className="grid gap-2">
              <Label>Model Type</Label>
              <Select
                value={formData.model_type || "native"}
                onValueChange={(val) => setFormData({ ...formData, model_type: val, redirect_to: val === "native" ? null : formData.redirect_to })}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="native">Native</SelectItem>
                  <SelectItem value="redirect">Redirect</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {formData.model_type === "redirect" && (
              <div className="grid gap-2">
                <Label>Redirect To</Label>
                {nativeModelsForProvider.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    No native models available for {selectedProvider?.name || "this provider"}. Create a native model first.
                  </p>
                ) : (
                  <Select
                    value={formData.redirect_to || ""}
                    onValueChange={(val) => setFormData({ ...formData, redirect_to: val })}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select target model" />
                    </SelectTrigger>
                    <SelectContent>
                      {nativeModelsForProvider.map((m) => (
                        <SelectItem key={m.model_id} value={m.model_id}>
                          {m.display_name || m.model_id}
                          {m.display_name && ` (${m.model_id})`}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </div>
            )}

            <div className="grid gap-2">
              <Label htmlFor="lb_strategy">Load Balancing Strategy</Label>
              <Select
                value={formData.lb_strategy}
                onValueChange={(val) => setFormData({ ...formData, lb_strategy: val })}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="single">Single</SelectItem>
                  <SelectItem value="round_robin">Round Robin</SelectItem>
                  <SelectItem value="failover">Failover</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-center justify-between rounded-lg border p-3 shadow-sm">
              <div className="space-y-0.5">
                <Label>Enabled</Label>
                <div className="text-sm text-muted-foreground">
                  Enable or disable this model configuration
                </div>
              </div>
              <Switch
                checked={formData.is_enabled}
                onCheckedChange={(checked) => setFormData({ ...formData, is_enabled: checked })}
              />
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setIsDialogOpen(false)}>
                Cancel
              </Button>
              <Button type="submit">Save</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
