// 分组 Cyber 自动扣款表单辅助：上游按协议返回 cyber 拦截时按固定金额扣款一次。
// 是否命中只看上游协议返回的信号（OpenAI 协议的 cyber_policy、Claude 协议的
// cyber 拒答），与分组平台无关，所以每个平台的分组都能开启。

export const normalizeCyberAutoChargeAmount = (
  value: number | string | null | undefined,
): number => {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
};

export type CyberAutoChargeFormState = {
  cyber_auto_charge_enabled: boolean;
  cyber_auto_charge_amount: number | string | null;
};

// 返回 null 表示通过，否则返回错误信息的 i18n key（相对 admin.groups.cyberAutoCharge）。
export const validateCyberAutoChargeFormState = (
  form: CyberAutoChargeFormState,
): string | null => {
  if (!form.cyber_auto_charge_enabled) {
    return null;
  }
  return normalizeCyberAutoChargeAmount(form.cyber_auto_charge_amount) > 0
    ? null
    : "amountRequired";
};

// 提交字段：关闭时照常提交金额，再次开启时沿用。
export const cyberAutoChargePayload = (form: CyberAutoChargeFormState) => ({
  cyber_auto_charge_enabled: form.cyber_auto_charge_enabled,
  cyber_auto_charge_amount: normalizeCyberAutoChargeAmount(
    form.cyber_auto_charge_amount,
  ),
});
