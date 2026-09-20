/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useToast } from '../hooks/useToast'
import { ToastProvider } from './Toast'

function ToastHarness() {
  const { notify } = useToast()
  return <div>
    <button onClick={() => notify({ tone: 'success', title: '账号删除成功', message: '本机数据已清除。' })}>显示成功</button>
    <button onClick={() => notify({ tone: 'error', title: '连接失败', message: '请检查配置。' })}>显示错误</button>
  </div>
}

describe('ToastProvider', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    vi.useFakeTimers()
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
    act(() => root.render(<ToastProvider><ToastHarness /></ToastProvider>))
  })
  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    vi.useRealTimers()
  })

  const click = (label: string) => {
    const button = [...container.querySelectorAll('button')].find((item) => item.textContent === label)
    if (!button) throw new Error(`Button not found: ${label}`)
    act(() => button.dispatchEvent(new MouseEvent('click', { bubbles: true })))
  }

  it('announces success and removes it after the success timeout', () => {
    click('显示成功')

    expect(container.querySelector('[aria-label="操作通知"]')?.getAttribute('aria-live')).toBe('polite')
    expect(container.querySelector('[role="status"]')?.textContent).toContain('账号删除成功')

    act(() => vi.advanceTimersByTime(4399))
    expect(container.querySelector('[role="status"]')).not.toBeNull()
    act(() => vi.advanceTimersByTime(1))
    expect(container.querySelector('[role="status"]')).toBeNull()
  })

  it('allows an error toast to be closed immediately', () => {
    click('显示错误')
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('请检查配置。')

    const closeButton = container.querySelector<HTMLButtonElement>('[aria-label="关闭通知：连接失败"]')
    if (!closeButton) throw new Error('Close button not found')
    act(() => closeButton.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    expect(container.querySelector('[role="alert"]')).toBeNull()
  })
})
