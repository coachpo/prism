import type {
  AuthSettings,
  ProxyApiKeyCreateResponse,
  ProxyApiKeyRotateResponse,
  ProxyKeyCapacity,
} from "@/lib/types";
import { useProxyKeyCreateMutation } from "./useProxyKeyCreateMutation";
import { useProxyKeyDeleteMutation } from "./useProxyKeyDeleteMutation";
import { useProxyKeyEditMutation } from "./useProxyKeyEditMutation";
import { useProxyKeyRotateMutation } from "./useProxyKeyRotateMutation";

interface UseProxyKeyMutationsInput {
  authSettings: AuthSettings | null;
  capacity: ProxyKeyCapacity | null;
  proxyKeyLimit: number;
  remainingKeys: number;
  showCreatedSecret: (created: ProxyApiKeyCreateResponse) => void;
  showRotatedSecret: (rotated: ProxyApiKeyRotateResponse) => void;
}

export function useProxyKeyMutations({
  authSettings,
  capacity,
  proxyKeyLimit,
  remainingKeys,
  showCreatedSecret,
  showRotatedSecret,
}: UseProxyKeyMutationsInput) {
  const create = useProxyKeyCreateMutation({
    authSettings,
    capacity,
    proxyKeyLimit,
    remainingKeys,
    showCreatedSecret,
  });
  const edit = useProxyKeyEditMutation();
  const rotate = useProxyKeyRotateMutation({ showRotatedSecret });
  const deletion = useProxyKeyDeleteMutation();

  return { ...create, ...edit, ...rotate, ...deletion };
}
