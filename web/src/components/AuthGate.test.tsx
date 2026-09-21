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
    window.history.replaceState({}, '', '/')
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

  it('prepares a same-origin connection form without storing the password', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ required: true, authenticated: false }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await renderGate()
    const heading = await find<HTMLHeadingElement>('h1')
    expect(heading.textContent).toBe('连接管理服务')
    expect(container.textContent).toContain('至少 12 个字符，区分大小写。')

    const address = container.querySelector<HTMLInputElement>('#connection-address')
    const password = container.querySelector<HTMLInputElement>('#admin-password')
    const form = container.querySelector<HTMLFormElement>('form')
    if (!address || !password || !form) throw new Error('Connection form not found')
    expect(address.value).toBe(window.location.origin)
    expect(form.action).toBe(`${window.location.origin}/api/v1/auth/connect`)
    expect(password.getAttribute('name')).toBe('password')
    expect(password.getAttribute('minlength')).toBe('12')
    setInputValue(password, 'correct-horse')

    const submit = container.querySelector<HTMLButtonElement>('button[type="submit"]')
    if (!submit) throw new Error('Submit button not found')
    expect(submit.disabled).toBe(false)
    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)
  })

  it('shows connection feedback after a successful form login', async () => {
    window.history.replaceState({}, '', '/?auth=connected')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ required: true, authenticated: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })))

    await renderGate()
    await find('[role="status"]')
    expect(container.textContent).toContain('受保护控制台')
    expect(container.querySelector('[role="status"]')?.textContent).toContain('连接成功')
    expect(window.location.search).toBe('')
  })

  it('returns to the login gate when an authenticated API reports 401', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ required: true, authenticated: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await renderGate()
    expect(container.textContent).toContain('受保护控制台')

    act(() => window.dispatchEvent(new Event(authenticationRequiredEvent)))
    const heading = await find<HTMLHeadingElement>('h1')
    expect(heading.textContent).toBe('连接管理服务')
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('管理会话已过期')
  })

  it('explains a rejected password returned by the server', async () => {
    window.history.replaceState({}, '', '/?auth_error=invalid_password')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ required: true, authenticated: false }), { status: 200, headers: { 'Content-Type': 'application/json' } })))

    await renderGate()
    await find<HTMLHeadingElement>('h1')
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('管理密码不正确')
    expect(window.location.search).toBe('')
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
