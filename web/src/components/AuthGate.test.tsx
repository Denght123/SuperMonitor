/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { authenticationRequiredEvent } from '../api/client'
import { AuthGate } from './AuthGate'
import { ToastProvider } from './Toast'

describe('AuthGate', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  })

  const renderGate = async () => {
    await act(async () => {
      root.render(<ToastProvider><AuthGate><div>受保护控制台</div></AuthGate></ToastProvider>)
    })
  }

  const find = async <T extends Element>(selector: string) => {
    for (let attempt = 0; attempt < 20; attempt += 1) {
      const element = container.querySelector<T>(selector)
      if (element) return element
      await act(async () => { await new Promise((resolve) => window.setTimeout(resolve, 0)) })
    }
    throw new Error(`Element not found: ${selector}`)
  }

  const setInputValue = (input: HTMLInputElement, value: string) => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    act(() => {
      setter?.call(input, value)
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
  }

  it('unlocks the console after a valid administrator token exchange', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ required: true, authenticated: false }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ authenticated: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await renderGate()
    const heading = await find<HTMLHeadingElement>('h1')
    expect(heading.textContent).toBe('管理员验证')
    expect(container.textContent).toContain('至少 24 个字符，区分大小写。')

    const input = container.querySelector<HTMLInputElement>('#admin-token')
    if (!input) throw new Error('Administrator token input not found')
    setInputValue(input, '123456789012345678901234')

    const submit = container.querySelector<HTMLButtonElement>('button[type="submit"]')
    if (!submit) throw new Error('Submit button not found')
    act(() => submit.click())

    await find('[role="status"]')
    expect(container.textContent).toContain('受保护控制台')
    expect(container.querySelector('[role="status"]')?.textContent).toContain('管理员验证成功')
  })

  it('returns to the login gate when an authenticated API reports 401', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ required: true, authenticated: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await renderGate()
    expect(container.textContent).toContain('受保护控制台')

    act(() => window.dispatchEvent(new Event(authenticationRequiredEvent)))
    const heading = await find<HTMLHeadingElement>('h1')
    expect(heading.textContent).toBe('管理员验证')
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('管理会话已过期')
  })

  it('provides a labelled recovery action when the service is unavailable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('连接被拒绝')))
    await renderGate()

    const heading = await find<HTMLHeadingElement>('h1')
    expect(heading.textContent).toBe('暂时无法连接服务')
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('连接被拒绝')
    expect([...container.querySelectorAll('button')].some((button) => button.textContent === '重新连接')).toBe(true)
  })
})
