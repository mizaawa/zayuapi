// 分组 Cyber 自动扣款表单辅助：请求被上游 cyber_policy 拦截时按固定金额扣款一次。
// 只有走 OpenAI 网关的分组（OpenAI 与 Custom）会命中 cyber_policy。

export const isCyberAutoChargePlatform = (platform: string): boolean =>
  ["openai", "custom", "composite"].includes(platform);

export const normalizeCyberAutoChargeAmount = (
  value: number | string | null | undefined,
): number => {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
};

export type CyberAutoChargeFormState = {
  platform: string;
  cyber_auto_charge_enabled: boolean;
  cyber_auto_charge_amount: number | string | null;
};

// 返回 null 表示通过，否则返回错误信息的 i18n key（相对 admin.groups.cyberAutoCharge）。
export const validateCyberAutoChargeFormState = (
  form: CyberAutoChargeFormState,
): string | null => {
  if (!isCyberAutoChargePlatform(form.platform) || !form.cyber_auto_charge_enabled) {
    return null;
  }
  return normalizeCyberAutoChargeAmount(form.cyber_auto_charge_amount) > 0
    ? null
    : "amountRequired";
};

// 提交字段：不支持的平台一律关闭；关闭时照常提交金额，再次开启时沿用。
export const cyberAutoChargePayload = (form: CyberAutoChargeFormState) => ({
  cyber_auto_charge_enabled:
    isCyberAutoChargePlatform(form.platform) && form.cyber_auto_charge_enabled,
  cyber_auto_charge_amount: normalizeCyberAutoChargeAmount(
    form.cyber_auto_charge_amount,
  ),
});
