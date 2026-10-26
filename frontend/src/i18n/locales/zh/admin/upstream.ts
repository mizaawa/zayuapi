export default { upstream: {
  description: '集中管理服务提供商、连接状态与余额快照',
  add: '添加服务提供商', overview: '上游概览', totalBalance: '上游总余额', userBalance: '用户总余额',
  balanceCoverage: '已记录 {known} 个 · 未知 {unknown} 个', userBalanceHint: '站内用户可用余额',
  search: '搜索名称或地址', allStatus: '全部状态', active: '运行中', inactive: '已停用', error: '异常', refresh: '刷新',
  loading: '正在加载上游…', empty: '暂无上游账号', balance: '上游余额', unknown: '未知', manualSnapshot: '手动快照',
  balanceUsd: '美元余额', clearHint: '留空保存可清除余额', edit: '编辑配置', recordBalance: '记录余额',
  discover: '获取模型', discovered: '找到 {count} 个模型', test: '测试连接', enable: '启用', disable: '停用',
  pagination: '第 {page} / {pages} 页 · 共 {total} 个', previous: '上一页', next: '下一页',
  invalidBalance: '请输入有效的非负余额', saved: '余额已保存', saveFailed: '保存失败', loadFailed: '加载上游失败',
  discoverFailed: '获取模型失败', testDone: '连接测试已完成', testFailed: '连接测试失败',
  deleteTitle: '删除上游', deleteConfirm: '确定删除“{name}”吗？', deleted: '上游已删除', deleteFailed: '删除失败',
  editProvider: '编辑服务提供商', connection: '连接配置', models: '可用模型', availableModels: '上游模型 ID',
  modelsPlaceholder: '每行一个模型 ID，例如 gpt-4.1', modelsHint: '留空表示不限制模型；获取模型后仍可手动增删。',
  modelAliases: '模型别名映射', routing: '路由设置', keepKey: '留空保留原 API Key',
  savingProvider: '保存中…', providerSaved: '服务提供商已保存', providerSaveFailed: '保存服务提供商失败',
  invalidProvider: '请检查并发、优先级和模型映射是否填写完整'
} }
