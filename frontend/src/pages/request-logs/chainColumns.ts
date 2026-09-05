// 入口链视图的列定义。列选择器读的必须是当前视图真正渲染的那一套：
// 拿尝试视图的 19 列去描述入口链的 11 列，勾掉「端点」毫无视觉变化，
// 而想让「已知成本」不被裁掉的人在面板里只找得到「费用」。
import { getStaticMessages } from "@/i18n/staticMessages";

export interface ChainColumnDef {
  key: string;
  label: string;
}

/** 展开箭头与行操作列不可隐藏，因此不在这份可切换列表里。 */
export function getChainColumns(): ChainColumnDef[] {
  const copy = getStaticMessages().requestLogs;
  return [
    { key: "time", label: copy.chainColumnTime },
    { key: "result", label: copy.chainColumnResult },
    { key: "requested_model", label: copy.chainColumnRequestedModel },
    { key: "final_target", label: copy.chainColumnFinalTarget },
    { key: "endpoint", label: copy.chainColumnEndpoint },
    { key: "attempts", label: copy.chainColumnAttempts },
    { key: "ttft", label: copy.ttft },
    { key: "token_rate", label: copy.tokenRate },
    { key: "tokens", label: copy.chainColumnTokens },
    { key: "cost", label: copy.chainColumnCost },
    { key: "pricing", label: copy.chainColumnPricing },
  ];
}

export function allChainColumnKeys(): string[] {
  return getChainColumns().map((column) => column.key);
}
