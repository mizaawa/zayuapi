.PHONY: build build-backend build-frontend test test-backend test-frontend test-frontend-critical

FRONTEND_CRITICAL_VITEST := \
	src/api/__tests__/client.spec.ts \
	src/api/__tests__/tokenRefresh.spec.ts \
	src/stores/__tests__/paymentAccess.spec.ts \
	src/router/__tests__/feature-access.spec.ts \
	src/components/layout/__tests__/AppSidebar.customMenu.spec.ts \
	src/components/admin/payment/__tests__/PaymentPermissionsPanel.spec.ts \
	src/components/payment/__tests__/AmountInput.spec.ts \
	src/components/admin/user/__tests__/UserEditModal.paymentPermissions.spec.ts \
	src/api/__tests__/imageWorkbench.spec.ts \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/components/keys/__tests__/SystemPromptSettings.spec.ts \
	src/components/keys/__tests__/UseKeyModal.spec.ts \
	src/components/account/__tests__/ModelWhitelistSelector.spec.ts \
	src/components/account/__tests__/EditAccountModal.upstreamModels.spec.ts \
	src/views/user/__tests__/KeysView.spec.ts \
	src/views/user/__tests__/UsageView.spec.ts \
	src/views/user/__tests__/ImageWorkbenchView.spec.ts \
	src/views/user/__tests__/VideoWorkbenchView.spec.ts \
	src/views/user/__tests__/LeaderboardView.spec.ts \
	src/components/user/__tests__/ImageWorkbenchTaskRow.spec.ts \
	src/utils/__tests__/imageWorkbench.spec.ts \
	src/utils/__tests__/usageRequest.spec.ts \
	src/utils/__tests__/usageRequestQueue.spec.ts \
	src/components/common/__tests__/Pagination.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts \
	src/views/admin/__tests__/ResourceLibraryView.spec.ts \
	src/i18n/__tests__/localesNoKeyCollision.spec.ts

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@pnpm --dir frontend run build

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend:
	@pnpm --dir frontend run test:security-deps
	@pnpm --dir frontend run lint:check
	@pnpm --dir frontend run typecheck
	@$(MAKE) test-frontend-critical

test-frontend-critical:
	@pnpm --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)
