export default {
    resourceLibrary: {
      title: '资源库',
      description: '集中监管生图与视频工作台已生成、尚未被删除的图片与视频，并可将需要留存的内容归档保存。',
      disabled: '资源库功能未开启，请先在「系统设置 → 拓展功能」中开启资源库管理功能。',
      loadFailed: '资源库数据加载失败，请稍后重试。',
      truncated: '当前仅展示最近 5000 条生成记录；请使用筛选条件缩小范围。',
      emptyLive: '暂无已生成且未被删除的图片或视频。',
      emptySaved: '还没有保存任何图片或视频。可在「生成资源」页选择需要留存的资源点击「保存」。',
      previewUnavailable: '预览不可用，文件可能已被删除或过期。',
      thumbLoading: '加载预览…',
      savedBadge: '已保存',
      archivedBadge: '永久保存',
      promptCopied: '提示词已复制',
      saved: '资源已保存到资源库',
      alreadySaved: '该资源已保存过，已定位到存档',
      saveFailed: '保存失败，请稍后重试',
      saveTooLarge: '文件超过单条保存上限（视频 1GB / 图片 64MB）',
      deleted: '已删除',
      deleteFailed: '删除失败，请稍后重试',
      downloadFailed: '下载失败，请稍后重试',
      tabs: {
        live: '生成资源',
        saved: '我的保存'
      },
      kinds: {
        image: '图片',
        video: '视频'
      },
      billing: {
        pending: '待结算',
        refunded: '已退款'
      },
      filters: {
        q: '搜索',
        qPlaceholder: '图片名称 / 任务 ID / 用户邮箱 / 提示词 / 模型',
        kind: '类型',
        model: '模型',
        group: '分组',
        allKinds: '全部类型',
        allModels: '全部模型',
        allGroups: '全部分组'
      },
      fields: {
        createdAt: '创建时间',
        completedAt: '完成时间',
        expiresAt: '销毁时间',
        sourceExpiresAt: '原销毁时间',
        savedAt: '保存时间',
        savedBy: '保存人',
        userEmail: '用户邮箱',
        userId: '用户 / 密钥',
        model: '模型',
        group: '分组',
        size: '大小',
        cost: '费用',
        taskId: '任务 ID',
        billingId: '计费单号',
        contentType: '类型',
        fileName: '文件名',
        prompt: '提示词（原文，未脱敏）',
        sha256: 'SHA256'
      },
      details: {
        quality: '画质',
        size: '尺寸',
        count: '张数',
        ratio: '宽高比',
        resolution: '分辨率',
        duration: '时长（秒）',
        reference_images: '参考图',
        reference_videos: '参考视频',
        reference_audios: '参考音频',
        completed_at: '完成时间',
        billing_state: '计费状态'
      },
      actions: {
        preview: '预览',
        download: '下载',
        copyPrompt: '复制提示词',
        save: '保存',
        saved: '已保存',
        delete: '删除'
      },
      deleteConfirm: {
        title: '确认删除',
        liveMessage: '将永久删除「{name}」及其在服务器上的文件，用户工作台会立即失去该结果。此操作不可撤销。',
        savedMessage: '将永久删除已保存的「{name}」及其存档文件，此操作不可撤销。'
      }
    }
}
