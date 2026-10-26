export default { upstream: {
  description: 'Manage providers, connection status, and balance snapshots',
  add: 'Add provider', overview: 'Upstream overview', totalBalance: 'Upstream balance', userBalance: 'User balance',
  balanceCoverage: '{known} recorded · {unknown} unknown', userBalanceHint: 'Available user balance',
  search: 'Search name or endpoint', allStatus: 'All statuses', active: 'Active', inactive: 'Inactive', error: 'Error', refresh: 'Refresh',
  loading: 'Loading providers…', empty: 'No providers yet', balance: 'Upstream balance', unknown: 'Unknown', manualSnapshot: 'Manual snapshot',
  balanceUsd: 'Balance in USD', clearHint: 'Save an empty value to clear', edit: 'Edit settings', recordBalance: 'Record balance',
  discover: 'Discover models', discovered: '{count} models found', test: 'Test connection', enable: 'Enable', disable: 'Disable',
  pagination: 'Page {page} / {pages} · {total} total', previous: 'Previous', next: 'Next',
  invalidBalance: 'Enter a valid nonnegative balance', saved: 'Balance saved', saveFailed: 'Save failed', loadFailed: 'Failed to load providers',
  discoverFailed: 'Model discovery failed', testDone: 'Connection test completed', testFailed: 'Connection test failed',
  deleteTitle: 'Delete provider', deleteConfirm: 'Delete “{name}”?', deleted: 'Provider deleted', deleteFailed: 'Delete failed',
  editProvider: 'Edit provider', connection: 'Connection', models: 'Available models', availableModels: 'Upstream model IDs',
  modelsPlaceholder: 'One model ID per line, e.g. gpt-4.1', modelsHint: 'Leave empty to allow all models; discovered models can be edited.',
  modelAliases: 'Model aliases', routing: 'Routing', keepKey: 'Leave empty to keep the existing API key',
  savingProvider: 'Saving…', providerSaved: 'Provider saved', providerSaveFailed: 'Failed to save provider',
  invalidProvider: 'Check concurrency, priority, and complete all model mappings'
} }
