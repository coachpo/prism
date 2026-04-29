import { KeyRound } from "lucide-react";
import { CopyButton } from "@/components/CopyButton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";

interface ProxyKeySecretRevealProps {
  latestGeneratedKey: string | null;
}

export function ProxyKeySecretReveal({ latestGeneratedKey }: ProxyKeySecretRevealProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;

  if (!latestGeneratedKey) {
    return null;
  }

  return (
    <Card className="overflow-hidden border-primary/20 bg-primary/5">
      <CardHeader className="border-b bg-background/70">
        <CardTitle className="flex items-center gap-2 text-base">
          <KeyRound className="text-primary" />
          {copy.newSecret}
        </CardTitle>
        <CardDescription>{copy.newSecretDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col gap-3 rounded-lg border bg-background p-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="min-w-0 break-all font-mono text-sm text-foreground">{latestGeneratedKey}</p>
          <CopyButton
            value={latestGeneratedKey}
            label={copy.copyKey}
            targetLabel={copy.apiKey}
            variant="outline"
            className="shrink-0"
          />
        </div>
      </CardContent>
    </Card>
  );
}
