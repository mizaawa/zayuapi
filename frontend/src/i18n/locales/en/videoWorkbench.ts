export default {
  videoWorkbench: {
    protocol: 'Image and video protocol', protocolCompatible: 'Default compatible protocol', protocolHint: 'Match the provider documentation. Sora uses /v1/videos; 968 uses /v1/videos/generations and converts image size and reference fields.',
    title: 'Video Workbench', navLabel: 'Video Generation', workbench: 'Workbench', tasks: 'Tasks',
    guide: 'Tutorial', documentation: 'API Docs', prompt: 'Prompt',
    promptPlaceholder: 'Describe your video...', apiKey: 'API key', model: 'Model',
    chooseKey: 'Select a key', chooseModel: 'Select a model',
    images: 'Reference images', videos: 'Reference videos', audios: 'Reference audio',
    addImages: 'Add reference images', addVideos: 'Upload reference videos', addAudios: 'Upload reference audio', removeReference: 'Remove reference',
    aspectRatio: 'Aspect ratio', resolution: 'Resolution', auto: 'Auto', custom: 'Custom', width: 'Width', height: 'Height',
    duration: 'Video duration', customDuration: 'Custom duration (seconds)', customDurationPlaceholder: 'Enter the video duration in seconds',
    seconds: '{count} sec', create: 'Create video task', submitting: 'Creating...',
    activeCount: 'Active {count} / {max}', queued: 'Queued', processing: 'Processing', completed: 'Generated', failed: 'Failed', all: 'All',
    createdAt: 'Created', elapsed: 'Elapsed', remaining: 'Remaining', remainingMinutes: '{count} min',
    download: 'Download', copyPrompt: 'Copy prompt', delete: 'Delete task', refresh: 'Refresh tasks', retry: 'Retry',
    deleteTitle: 'Delete this task?', deleteMessage: 'The task and its local video file will be deleted.',
    retention: 'Videos are generated in the cloud; you do not need to keep this page open. Results are cached in the cloud for {minutes} minutes by default. Please download them in time.', emptyTasks: 'No video tasks yet', emptyFiltered: 'No matching tasks',
    noKeys: 'No active keys in declared video groups', manageKeys: 'Manage API keys', noModels: 'No available models for this key',
    keysError: 'Unable to load API keys', modelsError: 'Unable to load models', tasksError: 'Unable to load tasks',
    unavailable: 'Video workbench is unavailable', uploadType: 'Unsupported reference format', uploadLimit: 'Maximum {max} files of this type',
    uploadSize: 'Reference files exceed the request upload capacity', uploadRead: 'Unable to read reference file',
    invalidDuration: 'Enter a whole number from 1 to 86400 seconds', invalidRatio: 'Width and height must be integers from 1 to 10000',
    created: 'Video task created', submitError: 'Creation is unconfirmed; check the task list before submitting again',
    downloadError: 'Download failed; try again', deleteError: 'Deletion failed; try again',
    modelDuration: 'This model supports whole seconds between {min} and {max}', fixedModelDuration: 'Supported durations in seconds: {durations}',
    invalidModelRatio: 'Supported aspect ratios: {ratios}', referenceImageRequired: 'This model requires at least one reference image',
    errors: {
      generation_failed: 'Cloud video generation failed. Check usage records.',
      request_rejected: 'Video creation failed. Check the model and generation parameters.',
      model_unavailable: 'This channel does not provide the model. Choose another model or contact the administrator.',
      storage_failed: 'Video download or local storage failed. Contact the administrator.',
      authorization_failed: 'Key or group access changed. Select an available key.',
      interrupted: 'The task was interrupted. Submit a new task.', timeout: 'The video task timed out. Contact the administrator to check the cloud task.'
    }
  }
}
