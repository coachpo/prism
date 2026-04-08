import { useEffect, useMemo } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useLocale } from "@/i18n/useLocale";
import type { Endpoint } from "@/lib/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";

const buildEndpointSchema = (copy: { nameRequired: string; baseUrlInvalid: string }) =>
  z.object({
    name: z.string().min(1, copy.nameRequired),
    base_url: z.string().url(copy.baseUrlInvalid),
    api_key: z.string(),
  });

export type EndpointFormValues = z.infer<ReturnType<typeof buildEndpointSchema>>;

interface EndpointDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (values: EndpointFormValues) => Promise<void>;
  initialValues?: Endpoint;
  title: string;
  submitLabel: string;
}

export function EndpointDialog({
  open,
  onOpenChange,
  onSubmit,
  initialValues,
  title,
  submitLabel,
}: EndpointDialogProps) {
  const { messages } = useLocale();
  const copy = messages.endpointsUi;
  const endpointSchema = useMemo(
    () => buildEndpointSchema({ nameRequired: copy.nameRequired, baseUrlInvalid: copy.baseUrlInvalid }),
    [copy.baseUrlInvalid, copy.nameRequired],
  );
  const form = useForm<EndpointFormValues>({
    resolver: zodResolver(endpointSchema),
    defaultValues: {
      name: "",
      base_url: "",
      api_key: "",
    },
  });

  useEffect(() => {
    if (open && initialValues) {
      form.reset({
        name: initialValues.name,
        base_url: initialValues.base_url,
        api_key: "",
      });
    } else if (open) {
      form.reset({
        name: "",
        base_url: "",
        api_key: "",
      });
    }
  }, [open, initialValues, form]);

  const handleSubmit = async (values: EndpointFormValues) => {
    if (!initialValues && !values.api_key.trim()) {
      form.setError("api_key", { message: copy.apiKeyRequired });
      return;
    }
    await onSubmit(values);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent aria-describedby={undefined} className="max-h-[90vh] sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(handleSubmit)} className="flex min-h-0 flex-col gap-5">
            <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
              <div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
                <FormField
                  control={form.control}
                  name="name"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{copy.name}</FormLabel>
                      <FormControl>
                        <Input autoComplete="off" placeholder={copy.namePlaceholder} {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="base_url"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{copy.baseUrl}</FormLabel>
                      <FormControl>
                        <Input autoComplete="off" placeholder={copy.baseUrlPlaceholder} {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <div className="flex flex-col gap-4 rounded-lg border p-4">
                <FormField
                  control={form.control}
                  name="api_key"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{messages.proxyApiKeys.apiKey}</FormLabel>
                      <FormControl>
                        <Input
                          type="password"
                          autoComplete="off"
                          placeholder={initialValues?.masked_api_key || messages.modelDetail.endpointApiKeyPlaceholder}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {initialValues ? copy.keepStoredKey : copy.apiKeyRequired}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </DialogBody>

            <DialogFooter className="sm:justify-between">
              <Button
                type="button"
                variant="outline"
                disabled={form.formState.isSubmitting}
                onClick={() => onOpenChange(false)}
              >
                {messages.settingsDialogs.cancel}
              </Button>
              <Button type="submit" disabled={form.formState.isSubmitting}>
                {submitLabel}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
