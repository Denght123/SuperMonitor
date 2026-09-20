const providerShortNames: Readonly<Record<string, string>> = {
  'trae-cn': 'TRAE CN',
  'qoder-cn': 'Qoder CN',
  'workbuddy-cn': 'WorkBuddy CN',
  'coze-cn': '扣子',
  bailian: '阿里云百炼',
  mimo: '小米 MiMo',
  tokenrhythm: '基元律动',
  deepseek: 'DeepSeek',
  zhipu: '智谱 AI',
  codex: 'Codex',
  'gemini-cli': 'Gemini',
  'claude-code': 'Claude Code',
  'qoder-global': 'Qoder',
  'workbuddy-global': 'WorkBuddy',
  kiro: 'Kiro',
  cursor: 'Cursor',
}

export function providerShortName(providerId: string, providerName?: string) {
  const mapped = providerShortNames[providerId]
  if (mapped) return mapped

  const fallback = providerName?.trim()
  if (fallback && fallback.toLocaleLowerCase() !== providerId.trim().toLocaleLowerCase()) return fallback
  return '其他平台'
}
