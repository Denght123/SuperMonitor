import { describe, expect, it } from 'vitest'

import { providerShortName } from './providerNames'

describe('providerShortName', () => {
  it.each([
    ['trae-cn', 'TRAE CN'],
    ['qoder-cn', 'Qoder CN'],
    ['workbuddy-cn', 'WorkBuddy CN'],
    ['coze-cn', '扣子'],
    ['bailian', '阿里云百炼'],
    ['mimo', '小米 MiMo'],
    ['tokenrhythm', '基元律动'],
    ['deepseek', 'DeepSeek'],
    ['zhipu', '智谱 AI'],
    ['codex', 'Codex'],
    ['gemini-cli', 'Gemini'],
    ['claude-code', 'Claude Code'],
    ['qoder-global', 'Qoder'],
    ['workbuddy-global', 'WorkBuddy'],
    ['kiro', 'Kiro'],
    ['cursor', 'Cursor'],
  ])('maps %s to a readable badge', (providerId, expected) => {
    expect(providerShortName(providerId)).toBe(expected)
  })

  it('uses the provider display name for a future adapter without exposing its raw id', () => {
    expect(providerShortName('future-provider', '未来智能平台')).toBe('未来智能平台')
    expect(providerShortName('future-provider', 'future-provider')).toBe('其他平台')
    expect(providerShortName('future-provider')).toBe('其他平台')
  })
})
