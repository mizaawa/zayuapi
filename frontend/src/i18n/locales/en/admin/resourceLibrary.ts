export default {
    resourceLibrary: {
      title: 'Resource Library',
      description: 'Review every generated image and video still retained by the image and video workbenches, and archive the ones that must be kept.',
      disabled: 'The resource library is disabled. Enable it under System Settings → Extended Features first.',
      loadFailed: 'Failed to load resource library data. Please try again later.',
      truncated: 'Only the 5000 most recent generated results are listed. Narrow the range with filters.',
      emptyLive: 'No generated images or videos are currently retained.',
      emptySaved: 'Nothing has been archived yet. Use Save on the Generated Resources tab to keep an item.',
      previewUnavailable: 'Preview unavailable. The file may have been deleted or expired.',
      thumbLoading: 'Loading preview…',
      savedBadge: 'Saved',
      archivedBadge: 'Archived',
      promptCopied: 'Prompt copied',
      saved: 'Resource saved to the library',
      alreadySaved: 'This resource was already saved; showing the archived copy',
      saveFailed: 'Failed to save. Please try again later.',
      saveTooLarge: 'File exceeds the per-item limit (1GB for videos, 64MB for images)',
      deleted: 'Deleted',
      deleteFailed: 'Failed to delete. Please try again later.',
      downloadFailed: 'Failed to download. Please try again later.',
      tabs: {
        live: 'Generated Resources',
        saved: 'My Saves'
      },
      kinds: {
        image: 'Image',
        video: 'Video'
      },
      billing: {
        pending: 'Pending settlement',
        refunded: 'Refunded'
      },
      filters: {
        q: 'Search',
        qPlaceholder: 'File name / task ID / user email / prompt / model',
        kind: 'Kind',
        model: 'Model',
        group: 'Group',
        allKinds: 'All kinds',
        allModels: 'All models',
        allGroups: 'All groups'
      },
      fields: {
        createdAt: 'Created',
        completedAt: 'Completed',
        expiresAt: 'Destruction time',
        sourceExpiresAt: 'Original destruction time',
        savedAt: 'Saved',
        savedBy: 'Saved by',
        userEmail: 'User email',
        userId: 'User / key',
        model: 'Model',
        group: 'Group',
        size: 'Size',
        cost: 'Cost',
        taskId: 'Task ID',
        billingId: 'Billing ID',
        contentType: 'Content type',
        fileName: 'File name',
        prompt: 'Prompt (raw, unmasked)',
        sha256: 'SHA256'
      },
      details: {
        quality: 'Quality',
        size: 'Size',
        count: 'Count',
        ratio: 'Aspect ratio',
        resolution: 'Resolution',
        duration: 'Duration (s)',
        reference_images: 'Reference images',
        reference_videos: 'Reference videos',
        reference_audios: 'Reference audios',
        completed_at: 'Completed',
        billing_state: 'Billing state'
      },
      actions: {
        preview: 'Preview',
        download: 'Download',
        copyPrompt: 'Copy prompt',
        save: 'Save',
        saved: 'Saved',
        delete: 'Delete'
      },
      deleteConfirm: {
        title: 'Confirm deletion',
        liveMessage: 'This permanently deletes "{name}" and its file on the server; the user workbench loses the result immediately. This cannot be undone.',
        savedMessage: 'This permanently deletes the archived "{name}" and its file. This cannot be undone.'
      }
    }
}
