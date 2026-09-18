import type { ComponentType, SVGProps } from 'react'
import Bailian from '@lobehub/icons/es/Bailian/components/Color'
import ClaudeCode from '@lobehub/icons/es/ClaudeCode/components/Color'
import CodeBuddy from '@lobehub/icons/es/CodeBuddy/components/Color'
import Codex from '@lobehub/icons/es/Codex/components/Color'
import Coze from '@lobehub/icons/es/Coze/components/Mono'
import Cursor from '@lobehub/icons/es/Cursor/components/Mono'
import DeepSeek from '@lobehub/icons/es/DeepSeek/components/Color'
import GeminiCLI from '@lobehub/icons/es/GeminiCLI/components/Color'
import Kiro from '@lobehub/icons/es/Kiro/components/Color'
import Qoder from '@lobehub/icons/es/Qoder/components/Color'
import Trae from '@lobehub/icons/es/Trae/components/Color'
import XiaomiMiMo from '@lobehub/icons/es/XiaomiMiMo/components/Mono'
import Zhipu from '@lobehub/icons/es/Zhipu/components/Color'
import { Globe2 } from 'lucide-react'

type LogoComponent = ComponentType<SVGProps<SVGSVGElement> & { size?: string | number }>

const logos: Record<string, LogoComponent> = {
  'trae-cn': Trae,
  'qoder-cn': Qoder,
  'qoder-global': Qoder,
  'workbuddy-cn': CodeBuddy,
  'workbuddy-global': CodeBuddy,
  'coze-cn': Coze,
  bailian: Bailian,
  mimo: XiaomiMiMo,
  deepseek: DeepSeek,
  zhipu: Zhipu,
  codex: Codex,
  'gemini-cli': GeminiCLI,
  'claude-code': ClaudeCode,
  kiro: Kiro,
  cursor: Cursor,
}

const nameFallbacks: Array<[RegExp, string]> = [
  [/trae/i, 'trae-cn'], [/qoder/i, 'qoder-global'], [/(workbuddy|codebuddy)/i, 'workbuddy-global'],
  [/coze|扣子/i, 'coze-cn'], [/百炼|bailian/i, 'bailian'], [/mimo|基元/i, 'mimo'],
  [/deepseek/i, 'deepseek'], [/智谱|zhipu/i, 'zhipu'], [/codex/i, 'codex'],
  [/gemini/i, 'gemini-cli'], [/claude/i, 'claude-code'], [/kiro/i, 'kiro'], [/cursor/i, 'cursor'],
]

export function ProviderLogo({ providerId, name, size = 25 }: { providerId?: string; name?: string; size?: number }) {
  const fallbackId = nameFallbacks.find(([pattern]) => pattern.test(name ?? ''))?.[1]
  const Icon = logos[providerId ?? ''] ?? logos[fallbackId ?? '']
  return <span className="provider-logo" aria-hidden="true">{Icon ? <Icon size={size} /> : <Globe2 size={size} />}</span>
}
