import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { api } from "@/lib/api";
import type { ModelConfig, Endpoint, EndpointCreate, EndpointUpdate } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import { ArrowLeft, Plus, Pencil, Trash2, RefreshCw, MoreHorizontal } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";

export function ModelDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [model, setModel] = useState<ModelConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [isEndpointDialogOpen, setIsEndpointDialogOpen] = useState(false);
  const [editingEndpoint, setEditingEndpoint] = useState<Endpoint | null>(null);

  // Endpoint Form State
  const [endpointForm, setEndpointForm] = useState<EndpointCreate>({
    base_url: "",
    api_key: "",
    priority: 0,
    description: "",
    is_active: true,
  });

  const fetchModel = async () => {
    if (!id) return;
    try {
      const data = await api.models.get(parseInt(id));
      setModel(data);
    } catch (error) {
      toast.error("Failed to fetch model details");
      console.error(error);
      navigate("/models");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchModel();
  }, [id]);

  const handleOpenEndpointDialog = (endpoint?: Endpoint) => {
    if (endpoint) {
      setEditingEndpoint(endpoint);
      setEndpointForm({
        base_url: endpoint.base_url,
        api_key: "",
        priority: endpoint.priority,
        description: endpoint.description || "",
        is_active: endpoint.is_active,
      });
    } else {
      setEditingEndpoint(null);
      setEndpointForm({
        base_url: "",
        api_key: "",
        priority: 0,
        description: "",
        is_active: true,
      });
    }
    setIsEndpointDialogOpen(true);
  };

  const handleEndpointSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!model) return;

    try {
      if (editingEndpoint) {
        const updateData: EndpointUpdate = {
          base_url: endpointForm.base_url,
          priority: endpointForm.priority,
          description: endpointForm.description,
          is_active: endpointForm.is_active,
        };
        if (endpointForm.api_key) {
          updateData.api_key = endpointForm.api_key;
        }

        await api.endpoints.update(editingEndpoint.id, updateData);
        toast.success("Endpoint updated");
      } else {
        await api.endpoints.create(model.id, endpointForm);
        toast.success("Endpoint added");
      }
      setIsEndpointDialogOpen(false);
      fetchModel();
    } catch (error: any) {
      toast.error(error.message || "Operation failed");
    }
  };

  const handleDeleteEndpoint = async (endpointId: number) => {
    if (!confirm("Delete this endpoint?")) return;
    try {
      await api.endpoints.delete(endpointId);
      toast.success("Endpoint deleted");
      fetchModel();
    } catch (error: any) {
      toast.error(error.message || "Delete failed");
    }
  };

  const handleResetHealth = async (endpointId: number) => {
    try {
      await api.endpoints.resetHealth(endpointId);
      toast.success("Health stats reset");
      fetchModel();
    } catch (error: any) {
      toast.error(error.message || "Reset failed");
    }
  };

  const handleToggleActive = async (endpoint: Endpoint, checked: boolean) => {
    try {
      await api.endpoints.update(endpoint.id, { is_active: checked });
      toast.success(`Endpoint ${checked ? "enabled" : "disabled"}`);
      fetchModel();
    } catch (error: any) {
      toast.error(error.message || "Update failed");
    }
  };

  const maskApiKey = (key: string) => {
    if (!key) return "";
    if (key.length <= 4) return key;
    return `••••${key.slice(-4)}`;
  };

  const getHealthBadge = (status: string) => {
    switch (status) {
      case "healthy":
        return <Badge className="bg-green-500 hover:bg-green-600">Healthy</Badge>;
      case "unhealthy":
        return <Badge variant="destructive">Unhealthy</Badge>;
      default:
        return <Badge variant="secondary">Unknown</Badge>;
    }
  };

  if (loading) return <div className="p-8">Loading model details...</div>;
  if (!model) return <div className="p-8">Model not found</div>;

  return (
    <div className="space-y-8">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="icon" onClick={() => navigate("/models")}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <h2 className="text-3xl font-bold tracking-tight">Model Details</h2>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>{model.display_name || model.model_id}</CardTitle>
              <CardDescription>{model.provider.name} • {model.model_id}</CardDescription>
            </div>
            <Badge variant={model.is_enabled ? "default" : "secondary"}>
              {model.is_enabled ? "Enabled" : "Disabled"}
            </Badge>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <span className="font-medium text-muted-foreground">Load Balancing Strategy:</span>
              <span className="ml-2 capitalize">{model.lb_strategy.replace("_", " ")}</span>
            </div>
            <div>
              <span className="font-medium text-muted-foreground">Provider Type:</span>
              <span className="ml-2 capitalize">{model.provider.provider_type}</span>
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-xl font-semibold tracking-tight">Endpoints</h3>
          <Button onClick={() => handleOpenEndpointDialog()}>
            <Plus className="mr-2 h-4 w-4" /> Add Endpoint
          </Button>
        </div>

        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Base URL</TableHead>
                  <TableHead>API Key</TableHead>
                  <TableHead>Priority</TableHead>
                  <TableHead>Health</TableHead>
                  <TableHead>Stats (S/F)</TableHead>
                  <TableHead>Active</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {model.endpoints.map((endpoint) => (
                  <TableRow key={endpoint.id}>
                    <TableCell className="font-medium max-w-[200px] truncate" title={endpoint.base_url}>
                      {endpoint.base_url}
                      {endpoint.description && (
                        <div className="text-xs text-muted-foreground">{endpoint.description}</div>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {maskApiKey(endpoint.api_key)}
                    </TableCell>
                    <TableCell>{endpoint.priority}</TableCell>
                    <TableCell>{getHealthBadge(endpoint.health_status)}</TableCell>
                    <TableCell>
                      <span className="text-green-600">{endpoint.success_count}</span> /{" "}
                      <span className="text-red-600">{endpoint.failure_count}</span>
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={endpoint.is_active}
                        onCheckedChange={(checked) => handleToggleActive(endpoint, checked)}
                      />
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
                          <DropdownMenuItem onClick={() => handleOpenEndpointDialog(endpoint)}>
                            <Pencil className="mr-2 h-4 w-4" /> Edit
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => handleResetHealth(endpoint.id)}>
                            <RefreshCw className="mr-2 h-4 w-4" /> Reset Health
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem 
                            className="text-destructive focus:text-destructive"
                            onClick={() => handleDeleteEndpoint(endpoint.id)}
                          >
                            <Trash2 className="mr-2 h-4 w-4" /> Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))}
                {model.endpoints.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={7} className="text-center py-8 text-muted-foreground">
                      No endpoints configured. Add one to start routing requests.
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>

      <Dialog open={isEndpointDialogOpen} onOpenChange={setIsEndpointDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingEndpoint ? "Edit Endpoint" : "Add Endpoint"}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleEndpointSubmit} className="space-y-4">
            <div className="grid gap-2">
              <Label htmlFor="base_url">Base URL</Label>
              <Input
                id="base_url"
                value={endpointForm.base_url}
                onChange={(e) => setEndpointForm({ ...endpointForm, base_url: e.target.value })}
                placeholder="https://api.openai.com/v1"
                required
              />
            </div>

            <div className="grid gap-2">
              <Label htmlFor="api_key">API Key</Label>
              <Input
                id="api_key"
                type="password"
                value={endpointForm.api_key}
                onChange={(e) => setEndpointForm({ ...endpointForm, api_key: e.target.value })}
                placeholder={editingEndpoint ? "Leave blank to keep unchanged" : "sk-..."}
                required={!editingEndpoint}
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="grid gap-2">
                <Label htmlFor="priority">Priority</Label>
                <Input
                  id="priority"
                  type="number"
                  value={endpointForm.priority}
                  onChange={(e) => setEndpointForm({ ...endpointForm, priority: parseInt(e.target.value) || 0 })}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="is_active" className="mb-2 block">Active Status</Label>
                <div className="flex items-center space-x-2">
                  <Switch
                    id="is_active"
                    checked={endpointForm.is_active}
                    onCheckedChange={(checked) => setEndpointForm({ ...endpointForm, is_active: checked })}
                  />
                  <Label htmlFor="is_active" className="font-normal">
                    {endpointForm.is_active ? "Active" : "Inactive"}
                  </Label>
                </div>
              </div>
            </div>

            <div className="grid gap-2">
              <Label htmlFor="description">Description</Label>
              <Input
                id="description"
                value={endpointForm.description || ""}
                onChange={(e) => setEndpointForm({ ...endpointForm, description: e.target.value })}
                placeholder="Optional notes"
              />
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setIsEndpointDialogOpen(false)}>
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
