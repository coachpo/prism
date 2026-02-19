import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "@/lib/api";
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
import { Plus, Pencil, Trash2, MoreHorizontal } from "lucide-react";
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

  // Form state
  const [formData, setFormData] = useState<ModelConfigCreate>({
    provider_id: 0,
    model_id: "",
    display_name: "",
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
        lb_strategy: model.lb_strategy,
        is_enabled: model.is_enabled,
      });
    } else {
      setEditingModel(null);
      setFormData({
        provider_id: providers.length > 0 ? providers[0].id : 0,
        model_id: "",
        display_name: "",
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
          display_name: formData.display_name,
          lb_strategy: formData.lb_strategy,
          is_enabled: formData.is_enabled,
        };
        await api.models.update(editingModel.id, updateData);
        toast.success("Model updated successfully");
      } else {
        await api.models.create(formData);
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

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h2 className="text-3xl font-bold tracking-tight">Models</h2>
        <Button onClick={() => handleOpenDialog()}>
          <Plus className="mr-2 h-4 w-4" /> Add Model
        </Button>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Model ID</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead>Display Name</TableHead>
                <TableHead>Strategy</TableHead>
                <TableHead>Endpoints</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {models.map((model) => (
                <TableRow 
                  key={model.id} 
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={(e) => {
                    // Prevent navigation if clicking on actions
                    if ((e.target as HTMLElement).closest("button")) return;
                    navigate(`/models/${model.id}`);
                  }}
                >
                  <TableCell className="font-medium">{model.model_id}</TableCell>
                  <TableCell>{model.provider.name}</TableCell>
                  <TableCell>{model.display_name || "-"}</TableCell>
                  <TableCell className="capitalize">{model.lb_strategy.replace("_", " ")}</TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {model.active_endpoint_count} / {model.endpoint_count}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Badge variant={model.is_enabled ? "default" : "secondary"}>
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
                      <DropdownMenuContent align="end">
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
              {models.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="text-center py-8 text-muted-foreground">
                    No models found. Create one to get started.
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
                onValueChange={(val) => setFormData({ ...formData, provider_id: parseInt(val) })}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select provider" />
                </SelectTrigger>
                <SelectContent>
                  {providers.map((p) => (
                    <SelectItem key={p.id} value={p.id.toString()}>
                      {p.name} ({p.provider_type})
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
                disabled={!!editingModel}
              />
              {editingModel && <p className="text-xs text-muted-foreground">Model ID cannot be changed.</p>}
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
