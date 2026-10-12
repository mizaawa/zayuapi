export default {
  videoWorkbench: {
    protocol: '图片与视频协议', protocolCompatible: '默认兼容协议', protocolHint: '按上游文档选择协议。Sora 使用 /v1/videos；968 使用 /v1/videos/generations，并转换图片尺寸与参考图字段。',
    title: '视频工作台', navLabel: '视频生成', workbench: '工作台', tasks: '任务列表',
    guide: '使用教程', documentation: '对接文档', prompt: '提示词',
    promptPlaceholder: '在此处编辑视频提示词...', apiKey: 'API 密钥', model: '模型',
    chooseKey: '选择密钥', chooseModel: '选择模型',
    images: '参考图', videos: '参考视频', audios: '参考音频',
    addImages: '添加参考图', addVideos: '上传参考视频', addAudios: '上传参考音频', removeReference: '移除参考文件',
    aspectRatio: '宽高比', resolution: '分辨率', auto: '自动', custom: '自定义', width: '宽', height: '高',
    duration: '生成时长', customDuration: '自定义时长（秒）', customDurationPlaceholder: '请输入自定义的视频生成时长',
    seconds: '{count} 秒', create: '创建视频任务', submitting: '正在创建...',
    activeCount: '进行中 {count} / {max}', queued: '待生成', processing: '处理中', completed: '已生成', failed: '失败', all: '全部',
    createdAt: '生成日期', elapsed: '耗时', remaining: '剩余时间', remainingMinutes: '{count} 分钟',
    download: '下载', copyPrompt: '复制提示词', delete: '删除任务', refresh: '刷新任务', retry: '重试',
    deleteTitle: '删除此任务？', deleteMessage: '任务记录和本地视频文件将被删除。',
    retention: '视频生成在云端进行，无需前端等待，生成结果默认云端缓存{minutes}分钟，请及时下载', emptyTasks: '暂无视频任务', emptyFiltered: '没有符合条件的任务',
    noKeys: '暂无已声明视频分组的可用密钥', manageKeys: '管理 API 密钥', noModels: '此密钥没有可用模型',
    keysError: '密钥加载失败', modelsError: '模型加载失败', tasksError: '任务列表加载失败',
    unavailable: '视频工作台暂不可用', uploadType: '不支持此参考文件格式', uploadLimit: '此类参考文件最多 {max} 个',
    uploadSize: '参考文件总大小超出单次上传容量', uploadRead: '参考文件读取失败，请重新选择',
    invalidDuration: '请输入 1 至 86400 的整数秒数', invalidRatio: '宽和高须为 1 至 10000 的整数',
    modelDuration: '此模型支持 {min} 至 {max} 的整数秒数', fixedModelDuration: '此模型支持的时长（秒）：{durations}',
    invalidModelRatio: '此模型支持的宽高比：{ratios}', referenceImageRequired: '此模型需要至少一张参考图',
    created: '视频任务已创建', submitError: '创建未确认，请先检查任务列表，避免重复提交',
    downloadError: '下载失败，请重试', deleteError: '删除失败，请重试',
    errors: {
      generation_failed: '云端视频生成失败，请检查使用记录。',
      request_rejected: '视频请求未成功创建，请检查模型和生成参数。',
      model_unavailable: '当前渠道未提供此模型，请更换模型或联系管理员检查渠道。',
      storage_failed: '视频文件下载或本地保存失败，请联系管理员。',
      authorization_failed: '密钥或分组权限已变更，请重新选择密钥。',
      interrupted: '任务已中断，请重新提交。', timeout: '视频任务超时，请联系管理员检查云端任务。'
    }
  }
}
