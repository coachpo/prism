import {
  Activity,
  Loader2,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Switch } from "@/components/ui/switch";
import type { Connection } from "@/lib/types";

export function ConnectionCardActions({
  connection,
  isChecking,
  onDelete,
  onEdit,
  onHealthCheck,
  onToggleActive,
}: {
  connection: Connection;
  isChecking: boolean;
  onEdit: (connection: Connection) => void;
  onDelete: (id: number) => void;
  onHealthCheck: (id: number) => void;
  onToggleActive: (connection: Connection) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;

  return (
    <div className="flex shrink-0 items-center gap-2">
      <Switch
        checked={connection.is_active}
        onCheckedChange={() => onToggleActive(connection)}
        className="scale-90 data-[state=checked]:bg-emerald-500"
      />

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="h-8 w-8" aria-label={copy.connectionActions}>
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={() => onEdit(connection)}>
            <Pencil className="mr-2 h-3.5 w-3.5" />
            {copy.edit}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => onHealthCheck(connection.id)} disabled={isChecking}>
            {isChecking ? (
              <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
            ) : (
              <Activity className="mr-2 h-3.5 w-3.5" />
            )}
            {copy.healthCheck}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            className="text-destructive focus:text-destructive"
            onClick={() => onDelete(connection.id)}
          >
            <Trash2 className="mr-2 h-3.5 w-3.5" />
            {copy.delete}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
