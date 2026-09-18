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

function TokenRhythm({ size = 25, ...props }: SVGProps<SVGSVGElement> & { size?: string | number }) {
  return <svg {...props} width={size} height={size} viewBox="10 15 62 38" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M28.2869 15.3313L52.3032 15.3318C56.6495 15.332 61.1409 15.3885 65.4782 15.3038C62.4476 17.8251 58.7745 21.2659 55.8259 23.9557C54.451 24.0375 52.5027 23.9839 51.0859 23.9842C48.459 23.9946 45.832 23.9874 43.2052 23.9626C44.8543 25.4287 47.4485 28.1941 49.1119 29.8604L68.4647 49.2348C68.9123 49.6831 71.0764 51.7891 71.2735 52.1321C67.1352 52.0344 62.9706 52.1522 58.9688 51.9728L45.811 38.8082C42.8897 35.885 39.8219 32.8966 36.9616 29.9257L36.9693 44.1437C34.1378 46.911 31.1624 49.6754 28.2845 52.4082L28.2869 15.3313Z" fill="currentColor"/><path d="M66.8075 16.3432C66.9854 16.5409 66.8988 26.5212 66.8939 27.8531C64.2096 30.6301 61.1162 33.5951 58.3448 36.3093L52.2334 30.0688L66.8075 16.3432Z" fill="currentColor"/><path d="M20.3634 15.2565C22.2816 15.2079 24.3745 15.2491 26.3065 15.2484C26.241 18.0694 26.2922 21.1045 26.2817 23.9411L10.4213 23.964C13.7597 21.1535 17.0176 18.0347 20.3634 15.2565Z" fill="currentColor"/></svg>
}

const logos: Record<string, LogoComponent> = {
  'trae-cn': Trae,
  'qoder-cn': Qoder,
  'qoder-global': Qoder,
  'workbuddy-cn': CodeBuddy,
  'workbuddy-global': CodeBuddy,
  'coze-cn': Coze,
  bailian: Bailian,
  mimo: XiaomiMiMo,
  tokenrhythm: TokenRhythm,
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
  [/coze|扣子/i, 'coze-cn'], [/百炼|bailian/i, 'bailian'], [/tokenrhythm|基元律动/i, 'tokenrhythm'], [/mimo|小米/i, 'mimo'],
  [/deepseek/i, 'deepseek'], [/智谱|zhipu/i, 'zhipu'], [/codex/i, 'codex'],
  [/gemini/i, 'gemini-cli'], [/claude/i, 'claude-code'], [/kiro/i, 'kiro'], [/cursor/i, 'cursor'],
]

export function ProviderLogo({ providerId, name, size = 25 }: { providerId?: string; name?: string; size?: number }) {
  const fallbackId = nameFallbacks.find(([pattern]) => pattern.test(name ?? ''))?.[1]
  const Icon = logos[providerId ?? ''] ?? logos[fallbackId ?? '']
  return <span className="provider-logo" aria-hidden="true">{Icon ? <Icon size={size} /> : <Globe2 size={size} />}</span>
}
