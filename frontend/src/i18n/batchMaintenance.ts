/** Safe operator explanations for the bounded maintenance API's failure states. */
export function batchMaintenanceErrorMessage(cause: unknown): string {
  const message = cause instanceof Error ? cause.message : typeof cause === "string" ? cause : "";
  if (message.includes("batch_preview_stale")) return "预览后配置或引用已变更，整批未写入。请重新预览当前差异。";
  if (message.includes("manual_override_confirmation_required")) return "请明确确认替换预览中的人工覆盖后再应用。";
  if (message.includes("batch_invalid")) return "所选项目包含无效配置，整批未写入。请修复逐项提示后重新预览。";
  if (message.includes("Bind a reliable models.dev")) return "请先为此模型绑定可靠的 models.dev 目录来源，再填写限额。";
  if (message.includes("input_limit exceeds")) return "已有输入上限超过拟设置的上下文上限，请先修复该模型的输入上限。";
  if (message.includes("limits must be positive") || message.includes("require reliable explicit values")) return "请填写有依据的正整数限额；输出上限不得超过上下文上限。";
  if (message.includes("Model not found") || message.includes("Target owner not found")) return "模型已不存在，请刷新并重新选择。";
  if (message.includes("Terminal Target not found")) return "终端目标已不存在，请刷新并重新选择。";
  if (message.includes("Ban Policy not found")) return "所选路由策略已不存在，请选择当前有效策略。";
  if (message.includes("Pricing template unavailable") || message.includes("pricing_template_shape")) return "所选价格模板已删除或形状不完整，请修复或重新选择模板。";
  if (message.includes("openai_text_capability")) return "目标的 OpenAI 文本模式与所属模型不一致，请先修复能力配置；禁用目标也必须保持一致。";
  if (message.includes("openai_image_capability")) return "目标的图片能力未覆盖所属模型的图片操作，请先修复能力配置。";
  if (/[\u4e00-\u9fff]/.test(message)) return message;
  return "服务端未能完成本次操作，请刷新相关配置后重新预览；连接故障时不能确认是否已经应用。";
}
